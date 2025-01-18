package refresher

import (
	"context"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/lithictech/go-aperitif/v2/logctx"
	"github.com/lithictech/go-aperitif/v2/parallel"
	"github.com/webhookdb/icalproxy/appglobals"
	"github.com/webhookdb/icalproxy/db"
	"github.com/webhookdb/icalproxy/internal"
	"github.com/webhookdb/icalproxy/pgxt"
	"github.com/webhookdb/icalproxy/proxy"
	"github.com/webhookdb/icalproxy/types"
	"net/url"
	"strings"
	"sync"
	"time"
)

func New(ag *appglobals.AppGlobals) *Refresher {
	r := &Refresher{ag: ag}
	r.selectQuery = r.buildSelectQuery()
	return r
}

func StartScheduler(ctx context.Context, r *Refresher) {
	logctx.Logger(ctx).Info("starting_scheduler")
	go func() {
		for {
			logctx.Logger(ctx).Info("running_scheduler")
			if err := r.Run(ctx); err != nil {
				logctx.Logger(ctx).With("error", err).Error("enqueue_refresh_tasks_error")
			} else {
				logctx.Logger(ctx).Info("enqueued_refresh_tasks")
			}
			select {
			case <-time.After(schedulerInterval):
				continue
			case <-ctx.Done():
				logctx.Logger(ctx).Info("canceling_scheduler")
				return
			}
		}
	}()
}

const schedulerInterval = 30 * time.Second

type Refresher struct {
	ag          *appglobals.AppGlobals
	selectQuery string
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

func (r *Refresher) buildSelectQuery() string {
	now := time.Now().UTC()
	nowFmt := now.Format(time.RFC3339)
	defaultTTLMillis := time.Duration(proxy.DefaultTTL).Milliseconds()
	var checkedAtCond string
	if len(r.ag.Config.IcalTTLMap) > 0 {
		whenStatements := make([]string, 0, len(r.ag.Config.IcalTTLMap))
		for host, ttl := range r.ag.Config.IcalTTLMap {
			if host == "" {
				continue
			}
			stmt := fmt.Sprintf(
				"WHEN url_host LIKE '%%' || '%s' THEN '%s'::timestamptz - '%dms'::interval",
				host, nowFmt, time.Duration(ttl).Milliseconds(),
			)
			whenStatements = append(whenStatements, stmt)
		}
		whenStatements = append(whenStatements, fmt.Sprintf("ELSE '%s'::timestamptz - '%dms'::interval", nowFmt, defaultTTLMillis))
		checkedAtCond = fmt.Sprintf("(CASE\n%s\nEND)", strings.Join(whenStatements, "\n"))
	} else {
		checkedAtCond = fmt.Sprintf("'%s'::timestamptz - '%dms'::interval", nowFmt, defaultTTLMillis)
	}
	q := fmt.Sprintf(`SELECT url, contents_md5
FROM icalproxy_feeds_v1
WHERE checked_at < %s
LIMIT %d
FOR UPDATE SKIP LOCKED
`, checkedAtCond, r.ag.Config.RefreshPageSize)
	return q
}

func (r *Refresher) SelectRowsToProcess(ctx context.Context, tx pgx.Tx) ([]RowToProcess, error) {
	rows, err := tx.Query(ctx, r.selectQuery)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows[RowToProcess](rows, func(r pgx.CollectableRow) (RowToProcess, error) {
		rtp := RowToProcess{}
		return rtp, r.Scan(&rtp.Url, &rtp.MD5)
	})

}

func (r *Refresher) processChunk(ctx context.Context) (int, error) {
	var count int
	err := pgxt.WithTransaction(ctx, r.ag.DB, func(tx pgx.Tx) error {
		logctx.Logger(ctx).Info("refresher_querying_chunk")
		rowsToProcess, err := r.SelectRowsToProcess(ctx, tx)
		if err != nil {
			return err
		}
		logctx.Logger(ctx).Info("refresher_processing_chunk", "row_count", len(rowsToProcess))
		if len(rowsToProcess) == 0 {
			return nil
		}
		// We are processing in multiple threads but can only call the transaction commit
		// with one thread at a time. Guard it with a mutex, it's a lot simpler
		// than rewriting this for producer/consumer for minimal benefit of lock-free.
		txMux := &sync.Mutex{}
		perr := parallel.ForEach(len(rowsToProcess), len(rowsToProcess), func(idx int) error {
			return r.processUrl(ctx, tx, txMux, rowsToProcess[idx])
		})
		count += len(rowsToProcess)
		return perr
	})
	return count, err
}

type RowToProcess struct {
	Url string
	MD5 types.MD5Hash
}

func (r *Refresher) processUrl(ctx context.Context, tx pgx.Tx, txMux *sync.Mutex, rtp RowToProcess) error {
	ctx = logctx.AddTo(ctx, "url", rtp.Url)
	uri, err := url.Parse(rtp.Url)
	if err != nil {
		return internal.ErrWrap(err, "url parsed failed, should not have been stored")
	}
	feed, err := proxy.Fetch(ctx, uri)
	if err != nil {
		//panic("handle fetch error by updating fetch time: " + err.Error())
		return err
	}
	if feed.MD5 == rtp.MD5 {
		logctx.Logger(ctx).Info("feed_unchanged")
	} else {
		txMux.Lock()
		defer txMux.Unlock()
		if err := db.CommitFeed(tx, ctx, uri, feed); err != nil {
			logctx.Logger(ctx).With("error", err).Error("refresh_commit_feed_error")
		}
		logctx.Logger(ctx).Info("feed_change_committed")
	}
	return nil
}
