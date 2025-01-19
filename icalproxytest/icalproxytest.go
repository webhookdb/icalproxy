package icalproxytest

import (
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/webhookdb/icalproxy/internal"
	"github.com/webhookdb/icalproxy/types"
	"time"
)

type FeedRow struct {
	Id                   int64
	Url                  string
	UrlHostRev           string
	CheckedAt            time.Time
	ContentsMD5          types.MD5Hash
	ContentsLastModified time.Time
	ContentsSize         int
	FetchStatus          int
	FetchHeaders         json.RawMessage
	FetchErrorBody       []byte
	WebhookPending       bool
}

// TruncateLocal deletes localhost and 127.0.0.1 urls,
// which are usually only generated during testing.
func TruncateLocal(ctx context.Context, db *pgxpool.Pool) error {
	_, err := db.Exec(ctx, `
DELETE FROM icalproxy_feeds_v1
WHERE starts_with(url_host_rev, reverse('127001')) OR starts_with(url_host_rev, reverse('LOCALHOST'))`)
	if err != nil {
		return err
	}
	return nil
}

func MustMD5(s string) types.MD5Hash {
	return internal.MD5HashHex([]byte(s))
}
