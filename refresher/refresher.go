package refresher

import (
	"context"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/lithictech/go-aperitif/v2/logctx"
	"github.com/lithictech/go-aperitif/v2/parallel"
	"github.com/webhookdb/icalproxy/appglobals"
	"github.com/webhookdb/icalproxy/db"
	"github.com/webhookdb/icalproxy/feed"
	"github.com/webhookdb/icalproxy/internal"
	"github.com/webhookdb/icalproxy/pgxt"
	"github.com/webhookdb/icalproxy/types"
	"net/url"
	"strings"
	"sync"
	"time"
)

func New(ag *appglobals.AppGlobals) *Refresher {
	r := &Refresher{ag: ag}
	return r
}

func StartScheduler(ctx context.Context, r *Refresher) {
	ctx = logctx.AddTo(ctx, "logger", "refresher")
	internal.StartScheduler(ctx, r, 30*time.Second)
}

type Refresher struct {
	ag *appglobals.AppGlobals
}

func (r *Refresher) Run(ctx context.Context) error {
	for {
		rows, err := r.processChunk(ctx)
		if err != nil {
			return err
		} else if rows == 0 {
			return nil
		}
	}
}

func (r *Refresher) buildSelectQuery(now time.Time) string {
	whereSql := r.buildSelectQueryWhere(now)
	q := fmt.Sprintf(`SELECT url, contents_md5, fetch_status, fetch_headers
FROM icalproxy_feeds_v2
WHERE %s
LIMIT %d
FOR UPDATE SKIP LOCKED
`, whereSql, r.ag.Config.RefreshPageSize)
	return q
}

func (r *Refresher) buildSelectQueryWhere(now time.Time) string {
	now = now.UTC()
	nowFmt := now.Format(time.RFC3339)
	defaultTTLMillis := time.Duration(feed.DefaultTTL).Milliseconds()
	conditions := make([]string, 0, len(r.ag.Config.IcalTTLMap))
	for host, ttl := range r.ag.Config.IcalTTLMap {
		if host == "" {
			continue
		}
		stmt := fmt.Sprintf(
			"(starts_with(url_host_rev, '%s') and checked_at < '%s'::timestamptz - '%dms'::interval)",
			host.Reverse(), nowFmt, time.Duration(ttl).Milliseconds(),
		)
		conditions = append(conditions, stmt)
	}
	conditions = append(
		conditions,
		fmt.Sprintf("checked_at < '%s'::timestamptz - '%dms'::interval", nowFmt, defaultTTLMillis),
	)
	return strings.Join(conditions, "\nOR ")
}

func (r *Refresher) SelectRowsToProcess(ctx context.Context, tx pgx.Tx) ([]RowToProcess, error) {
	rows, err := tx.Query(ctx, r.buildSelectQuery(time.Now()))
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows[RowToProcess](rows, func(r pgx.CollectableRow) (RowToProcess, error) {
		rtp := RowToProcess{}
		return rtp, r.Scan(&rtp.Url, &rtp.MD5, &rtp.FetchStatus, &rtp.FetchHeaders)
	})
}

func (r *Refresher) ExplainSelectQuery(ctx context.Context) (string, error) {
	var lines []string
	err := pgxt.WithTransaction(ctx, r.ag.DB, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, "SET enable_seqscan = OFF; ANALYZE icalproxy_feeds_v2"); err != nil {
			return err
		}
		lns, err := pgxt.GetScalars[string](ctx, r.ag.DB, "EXPLAIN ANALYZE "+r.buildSelectQuery(time.Now()))
		if err != nil {
			return err
		}
		lines = lns
		return nil
	})
	return strings.Join(lines, "\n"), err
}

func (r *Refresher) CountRowsAwaitingRefresh(ctx context.Context) (int64, error) {
	whereSql := r.buildSelectQueryWhere(time.Now())
	q := fmt.Sprintf(`SELECT count(1) FROM icalproxy_feeds_v2 WHERE %s`, whereSql)
	return pgxt.GetScalar[int64](ctx, r.ag.DB, q)
}

func (r *Refresher) processChunk(ctx context.Context) (int, error) {
	var count int
	err := pgxt.WithTransaction(ctx, r.ag.DB, func(tx pgx.Tx) error {
		start := time.Now()
		logctx.Logger(ctx).DebugContext(ctx, "refresher_querying_chunk")
		rowsToProcess, err := r.SelectRowsToProcess(ctx, tx)
		if err != nil {
			return err
		}
		if len(rowsToProcess) == 0 {
			logctx.Logger(ctx).InfoContext(ctx, "refresher_empty_chunk")
			return nil
		}
		logctx.Logger(ctx).DebugContext(ctx, "refresher_processing_chunk", "row_count", len(rowsToProcess))
		// We are processing in multiple threads but can only call the transaction commit
		// with one thread at a time. Guard it with a mutex, it's a lot simpler
		// than rewriting this for producer/consumer for minimal benefit of lock-free.
		txMux := &sync.Mutex{}
		perr := parallel.ForEach(len(rowsToProcess), len(rowsToProcess), func(idx int) error {
			return r.processUrl(ctx, tx, txMux, rowsToProcess[idx])
		})
		count += len(rowsToProcess)
		logctx.Logger(ctx).InfoContext(ctx, "refresher_processed_chunk",
			"row_count", len(rowsToProcess),
			"elapsed_ms", time.Since(start).Milliseconds(),
		)
		return perr
	})
	return count, err
}

type RowToProcess struct {
	Url          string
	MD5          types.MD5Hash
	FetchStatus  int
	FetchHeaders feed.HeaderMap
}

func (r *Refresher) processUrl(ctx context.Context, tx pgx.Tx, txMux *sync.Mutex, rtp RowToProcess) error {
	ctx = logctx.AddTo(ctx, "url", rtp.Url)
	uri, err := url.Parse(rtp.Url)
	if err != nil {
		return internal.ErrWrap(err, "url parsed failed, should not have been stored")
	}
	start := time.Now()
	reqctx, cancel := context.WithTimeout(ctx, time.Duration(r.ag.Config.RefreshTimeout)*time.Second)
	defer cancel()
	fd, err := feed.Fetch(reqctx, uri, rtp.FetchHeaders)
	notModified := errors.Is(err, feed.ErrNotModified)
	if err != nil && !notModified {
		return err
	}
	txMux.Lock()
	defer txMux.Unlock()
	feedUnchanged := false
	if notModified {
		// 304 from server, or request avoided due to Cache-Control
		feedUnchanged = true
	} else if fd.MD5 == rtp.MD5 {
		// Body has not changed
		feedUnchanged = true
	} else if fd.HttpStatus >= 400 && fd.HttpStatus == rtp.FetchStatus {
		// If this is an error fetch, and the last fetch was also an error fetch with the same status code,
		// don't bother updating the row.
		feedUnchanged = true
	}
	if feedUnchanged {
		if err := db.New(tx).CommitUnchanged(ctx, fd); err != nil {
			logctx.Logger(ctx).With("error", err).ErrorContext(ctx, "refresh_commit_feed_error")
		}
		logctx.Logger(ctx).DebugContext(ctx, "feed_unchanged")
	} else {
		if err := db.New(tx).CommitFeed(ctx, r.ag.FeedStorage, fd, &db.CommitFeedOptions{WebhookPending: r.ag.Config.WebhookUrl != ""}); err != nil {
			logctx.Logger(ctx).With("error", err).ErrorContext(ctx, "refresh_commit_feed_error")
		}
		logctx.Logger(ctx).
			With("feed_http_status", fd.HttpStatus, "elapsed_ms", time.Now().Sub(start).Milliseconds()).
			Info("feed_change_committed")
	}
	return nil
}
