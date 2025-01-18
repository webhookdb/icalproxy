package db

import (
	"context"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/webhookdb/icalproxy/proxy"
	"github.com/webhookdb/icalproxy/types"
	"net/url"
	"time"
)

func Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	_, err := pool.Exec(ctx, schema)
	if err != nil {
		return err
	}
	return nil
}

const schema = `
CREATE TABLE IF NOT EXISTS icalproxy_feeds_v1 (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    url TEXT NOT NULL UNIQUE NOT NULL,
    url_host TEXT NOT NULL,
    
    checked_at timestamptz NOT NULL,
    
    contents BYTEA NOT NULL,
    contents_md5 TEXT NOT NULL,
    contents_last_modified timestamptz NOT NULL,
    contents_size INT NOT NULL
);
CREATE INDEX IF NOT EXISTS icalproxy_feeds_v1_url_host ON icalproxy_feeds_v1(url_host);
CREATE INDEX IF NOT EXISTS icalproxy_feeds_v1_checked_at ON icalproxy_feeds_v1(checked_at);
`

type ConditionalRow struct {
	ContentsMD5          types.MD5Hash
	ContentsLastModified time.Time
}

func FetchConditionalRow(db *pgxpool.Pool, ctx context.Context, uri *url.URL) (*ConditionalRow, error) {
	r := ConditionalRow{}
	const q = `SELECT contents_md5, contents_last_modified FROM icalproxy_feeds_v1 WHERE url = $1`
	err := db.QueryRow(ctx, q, uri.String()).Scan(&r.ContentsMD5, &r.ContentsLastModified)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &r, nil
}

func FetchContentsAsFeed(db *pgxpool.Pool, ctx context.Context, uri *url.URL) (*proxy.Feed, error) {
	r := proxy.Feed{}
	const q = `SELECT contents_last_modified, contents_md5, contents FROM icalproxy_feeds_v1 WHERE url = $1`
	err := db.QueryRow(ctx, q, uri.String()).Scan(&r.FetchedAt, &r.MD5, &r.Body)
	if err != nil {
		return nil, err
	}
	return &r, nil
}

func CommitFeed(db *pgxpool.Pool, ctx context.Context, uri *url.URL, feed *proxy.Feed) error {
	query := `INSERT INTO icalproxy_feeds_v1 
(url, url_host, checked_at, contents, contents_md5, contents_last_modified, contents_size)
VALUES ($1, $2, $3, $4, $5, $6, $7)
ON CONFLICT (url) DO UPDATE SET
	url_host=EXCLUDED.url_host,
	checked_at=EXCLUDED.checked_at,
	contents=EXCLUDED.contents,
	contents_md5=EXCLUDED.contents_md5,
	contents_last_modified=EXCLUDED.contents_last_modified,
	contents_size=EXCLUDED.contents_size
;
`
	// Truncate the second out, since http only knows about seconds
	fetchedTrunc := feed.FetchedAt.Truncate(time.Second)
	args := []any{uri.String(), types.NormalizeURLHostname(uri), fetchedTrunc, feed.Body, feed.MD5, fetchedTrunc, len(feed.Body)}
	_, err := db.Exec(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("unable to insert row: %w", err)
	}
	return nil
}

func Truncate(ctx context.Context, db *pgxpool.Pool) error {
	_, err := db.Exec(ctx, `TRUNCATE TABLE icalproxy_feeds_v1`)
	if err != nil {
		return err
	}
	return nil
}
