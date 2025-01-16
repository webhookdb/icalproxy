package refresher

import (
	"context"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/lithictech/go-aperitif/v2/logctx"
	"github.com/webhookdb/icalproxy/appglobals"
	"github.com/webhookdb/icalproxy/config"
	"github.com/webhookdb/icalproxy/db"
	"github.com/webhookdb/icalproxy/internal"
	"github.com/webhookdb/icalproxy/pgxt"
	"github.com/webhookdb/icalproxy/proxy"
	"net/url"
	"strings"
	"sync"
	"time"
)

func StartScheduler(ctx context.Context, ag *appglobals.AppGlobals) {
	logctx.Logger(ctx).Info("starting_scheduler")
	go func() {
		for {
			logctx.Logger(ctx).Info("running_scheduler")
			if err := enqueueRefreshTasks(ctx, ag); err != nil {
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

func enqueueRefreshTasks(ctx context.Context, ag *appglobals.AppGlobals) error {
	now := time.Now().UTC()
	nowFmt := now.Format(time.RFC3339)
	whenStatements := make([]string, 0, len(ag.Config.IcalTTLMap))
	for host, ttl := range ag.Config.IcalTTLMap {
		if host == "" {
			continue
		}
		stmt := fmt.Sprintf(
			"WHEN url_host LIKE '%%' || '%s' THEN '%s'::timestamptz - '%dms'::interval",
			host, nowFmt, ttl.Milliseconds(),
		)
		whenStatements = append(whenStatements, stmt)
	}
	whenStatements = append(whenStatements, fmt.Sprintf("ELSE '%s'::timestamptz - '%dms'::interval", nowFmt, config.IcalBaseTTL))
	q := fmt.Sprintf(`SELECT url, contents_md5
FROM icalproxy_feeds_v1
WHERE checked_at < (CASE
%s
END)
LIMIT %d
FOR UPDATE SKIP LOCKED
`, strings.Join(whenStatements, "\n"), ag.Config.RefreshPageSize)
	for {
		rows, err := processChunk(ctx, ag, q)
		if err != nil {
			return err
		} else if rows == 0 {
			return nil
		}
	}
}

func processChunk(ctx context.Context, ag *appglobals.AppGlobals, chunkQuery string) (int, error) {
	var count int
	err := pgxt.WithTransaction(ctx, ag.DB, func(tx pgx.Tx) error {
		logctx.Logger(ctx).Info("refresher_querying_chunk")
		rows, err := tx.Query(ctx, chunkQuery)
		if err != nil {
			return err
		}
		rowsToProcess, err := pgx.CollectRows[rowToProcess](rows, func(r pgx.CollectableRow) (rowToProcess, error) {
			rtp := rowToProcess{}
			return rtp, r.Scan(&rtp.Url, &rtp.MD5)
		})
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
				errs[i] = processUrl(ctx, ag, rowsToProcess[i])
			}(idx)
		}
		wg.Wait()
		return errors.Join(errs...)
	})
	return count, err
}

type rowToProcess struct {
	Url string
	MD5 string
}

func processUrl(ctx context.Context, ag *appglobals.AppGlobals, rtp rowToProcess) error {
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
		if err := db.CommitFeed(ag.DB, ctx, uri, feed); err != nil {
			logctx.Logger(ctx).With("error", err).Error("refresh_commit_feed_error")
		}
		logctx.Logger(ctx).Info("feed_change_committed")
	}
	return nil
}
