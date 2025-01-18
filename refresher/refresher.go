package refresher

import (
	"context"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/lithictech/go-aperitif/v2/logctx"
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

func StartScheduler(ctx context.Context, ag *appglobals.AppGlobals) {
	logctx.Logger(ctx).Info("starting_scheduler")
	go func() {
		r := New(ag)
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
	whenStatements = append(whenStatements, fmt.Sprintf("ELSE '%s'::timestamptz - '%dms'::interval", nowFmt, proxy.DefaultTTL))
	q := fmt.Sprintf(`SELECT url, contents_md5
FROM icalproxy_feeds_v1
WHERE checked_at < (CASE
%s
END)
LIMIT %d
FOR UPDATE SKIP LOCKED
`, strings.Join(whenStatements, "\n"), r.ag.Config.RefreshPageSize)
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
		count = len(rowsToProcess)
		wg := new(sync.WaitGroup)
		wg.Add(len(rowsToProcess))
		errs := make([]error, len(rowsToProcess))
		for idx := range rowsToProcess {
			go func(i int) {
				defer wg.Done()
				errs[i] = r.processUrl(ctx, rowsToProcess[i])
			}(idx)
		}
		wg.Wait()
		return errors.Join(errs...)
	})
	return count, err
}

type RowToProcess struct {
	Url string
	MD5 types.MD5Hash
}

func (r *Refresher) processUrl(ctx context.Context, rtp RowToProcess) error {
	ctx = logctx.AddTo(ctx, "url", rtp.Url)
	uri, err := url.Parse(rtp.Url)
	if err != nil {
		return internal.ErrWrap(err, "url parsed failed, should not have been stored")
	}
	feed, err := proxy.Fetch(ctx, uri)
	if err != nil {
		panic("handle fetch error by updating fetch time")
	}
	if feed.MD5 == rtp.MD5 {
		logctx.Logger(ctx).Info("feed_unchanged")
	} else {
		if err := db.CommitFeed(r.ag.DB, ctx, uri, feed); err != nil {
			logctx.Logger(ctx).With("error", err).Error("refresh_commit_feed_error")
		}
		logctx.Logger(ctx).Info("feed_change_committed")
	}
	return nil
}
