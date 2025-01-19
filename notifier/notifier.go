package notifier

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/lithictech/go-aperitif/v2/logctx"
	"github.com/webhookdb/icalproxy/appglobals"
	"github.com/webhookdb/icalproxy/config"
	"github.com/webhookdb/icalproxy/internal"
	"github.com/webhookdb/icalproxy/pgxt"
	"net/http"
	"time"
)

func New(ag *appglobals.AppGlobals) *Notifier {
	r := &Notifier{ag: ag}
	return r
}

func StartScheduler(ctx context.Context, r *Notifier) {
	ctx = logctx.AddTo(ctx, "logger", "notifier")
	if r.ag.Config.WebhookUrl == "" {
		logctx.Logger(ctx).InfoContext(ctx, "notifier_scheduler_webhook_not_configured")
		return
	}
	internal.StartScheduler(ctx, r, 10*time.Second)
}

type Notifier struct {
	ag *appglobals.AppGlobals
}

func (r *Notifier) Run(ctx context.Context) error {
	for {
		rows, err := r.processChunk(ctx)
		if err != nil {
			return err
		} else if rows == 0 {
			return nil
		}
	}
}

func (r *Notifier) processChunk(ctx context.Context) (int, error) {
	var count int
	err := pgxt.WithTransaction(ctx, r.ag.DB, func(tx pgx.Tx) error {
		logctx.Logger(ctx).DebugContext(ctx, "notifier_querying_chunk")
		q := fmt.Sprintf(`SELECT id, url
FROM icalproxy_feeds_v1
WHERE webhook_pending
LIMIT %d
FOR UPDATE SKIP LOCKED
`, r.ag.Config.WebhookPageSize)
		rows, err := tx.Query(ctx, q)
		if err != nil {
			return err
		}
		var ids []pgtype.Int8
		var urls []string
		var id pgtype.Int8
		var url string
		if _, err := pgx.ForEachRow(rows, []any{&id, &url}, func() error {
			ids = append(ids, id)
			urls = append(urls, url)
			return nil
		}); err != nil {
			return err
		}
		logctx.Logger(ctx).InfoContext(ctx, "notifier_processing_chunk", "row_count", len(urls))
		if len(urls) == 0 {
			return nil
		}
		body, err := json.Marshal(map[string]any{"urls": urls})
		if err != nil {
			return internal.ErrWrap(err, "marshaling webhook")
		}
		req, err := http.NewRequestWithContext(ctx, "POST", r.ag.Config.WebhookUrl, bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("User-Agent", config.UserAgent)
		if r.ag.Config.ApiKey != "" {
			req.Header.Set("Authorization", "Apikey "+r.ag.Config.ApiKey)
		}
		resp, err := httpClient.Do(req)
		if err != nil {
			return internal.ErrWrap(err, "requesting webhook")
		} else if resp.StatusCode >= 400 {
			return fmt.Errorf("error sending webhook: %d", resp.StatusCode)
		}
		if _, err := tx.Exec(ctx, `UPDATE icalproxy_feeds_v1 SET webhook_pending=false WHERE id = ANY($1)`, ids); err != nil {
			return internal.ErrWrap(err, "updating row")
		}
		count += len(urls)
		return nil
	})
	return count, err
}

type row struct {
	Id  int64
	Url string
}

var httpClient *http.Client

func init() {
	httpClient = &http.Client{
		Timeout: time.Second * 10,
	}
}
