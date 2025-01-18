package db

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/webhookdb/icalproxy/feed"
	"github.com/webhookdb/icalproxy/internal"
	"github.com/webhookdb/icalproxy/pgxt"
	"github.com/webhookdb/icalproxy/types"
	"net/url"
	"time"
)

func Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	const q = `
CREATE TABLE IF NOT EXISTS icalproxy_feeds_v1 (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    url TEXT NOT NULL UNIQUE NOT NULL,
    url_host TEXT NOT NULL,
    checked_at timestamptz NOT NULL,
    contents_md5 TEXT NOT NULL,
    contents_last_modified timestamptz NOT NULL,
    contents_size INT NOT NULL,
    fetch_status INT NOT NULL,
    fetch_headers JSONB NOT NULL DEFAULT '{}',
    fetch_error_body BYTEA
);
CREATE INDEX IF NOT EXISTS icalproxy_feeds_v1_url_host_idx ON icalproxy_feeds_v1(url_host);
CREATE INDEX IF NOT EXISTS icalproxy_feeds_v1_checked_at_idx ON icalproxy_feeds_v1(checked_at);
-- Keep the feed contents in a different table, since they can be very large.
-- This avoids loading gigabytes of data if you want to do a select *, can speed up updates, etc.
CREATE TABLE IF NOT EXISTS icalproxy_feed_contents_v1 (
  id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  feed_id BIGINT UNIQUE NOT NULL,
   CONSTRAINT feed_id_fk
       FOREIGN KEY (feed_id) REFERENCES icalproxy_feeds_v1(id) ON DELETE CASCADE,
	 contents BYTEA
);
CREATE INDEX IF NOT EXISTS icalproxy_feed_contents_v1_feed_id_idx ON icalproxy_feed_contents_v1(feed_id);
`
	_, err := pool.Exec(ctx, q)
	if err != nil {
		return err
	}
	return nil
}

func Reset(ctx context.Context, pool *pgxpool.Pool) error {
	const q = `DROP TABLE IF EXISTS icalproxy_feed_contents_v1; DROP TABLE IF EXISTS icalproxy_feeds_v1;`
	_, err := pool.Exec(ctx, q)
	if err != nil {
		return err
	}
	return nil
}

type FeedRow struct {
	ContentsMD5          types.MD5Hash
	ContentsLastModified time.Time
}

func FetchFeedRow(db *pgxpool.Pool, ctx context.Context, uri *url.URL) (*FeedRow, error) {
	r := FeedRow{}
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

func FetchContentsAsFeed(db *pgxpool.Pool, ctx context.Context, uri *url.URL) (*feed.Feed, error) {
	r := feed.Feed{}
	var fetchHeaders json.RawMessage
	const q = `SELECT
	fetch_headers, fetch_status, checked_at, contents_md5, (CASE WHEN fetch_status >= 400 THEN fetch_error_body ELSE contents END)
FROM icalproxy_feeds_v1
JOIN icalproxy_feed_contents_v1 ON feed_id = icalproxy_feeds_v1.id
WHERE url = $1`
	err := db.QueryRow(ctx, q, uri.String()).Scan(
		&fetchHeaders, &r.HttpStatus, &r.FetchedAt, &r.MD5, &r.Body,
	)
	if err != nil {
		return nil, internal.ErrWrap(err, "fetching row contents")
	}
	if err := json.Unmarshal(fetchHeaders, &r.HttpHeaders); err != nil {
		return nil, internal.ErrWrap(err, "unmarshaling db headers")
	}
	r.Url = uri
	return &r, nil
}

type CommitFeedDB interface {
	pgxt.IExec
	pgxt.IQueryRow
}

func CommitFeed(db CommitFeedDB, ctx context.Context, feed *feed.Feed) error {
	// Truncate the second out, since http only knows about seconds
	fetchedTrunc := feed.FetchedAt.Truncate(time.Second)
	urlHost := types.NormalizeURLHostname(feed.Url)

	if feed.HttpStatus >= 400 {
		const errQuery = `INSERT INTO icalproxy_feeds_v1 
(url, url_host, checked_at, fetch_status, fetch_headers, fetch_error_body, contents_md5, contents_last_modified, contents_size)
VALUES ($1, $2, $3, $4, $5, $6, '', $7, 0)
ON CONFLICT (url) DO UPDATE SET
	url_host=EXCLUDED.url_host,
	checked_at=EXCLUDED.checked_at,
	fetch_status=EXCLUDED.fetch_status,
	fetch_headers=EXCLUDED.fetch_headers,
	fetch_error_body=EXCLUDED.fetch_error_body`
		args := []any{
			feed.Url.String(),
			urlHost,
			fetchedTrunc,
			feed.HttpStatus,
			feed.HttpHeaders,
			feed.Body,
			fetchedTrunc,
		}
		if _, err := db.Exec(ctx, errQuery, args...); err != nil {
			return internal.ErrWrap(err, "unable to upsert error feed")
		}
		return nil
	}
	const feedQuery = `INSERT INTO icalproxy_feeds_v1 
(url, url_host, checked_at, fetch_status, fetch_headers, contents_md5, contents_last_modified, contents_size, fetch_error_body)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, '')
ON CONFLICT (url) DO UPDATE SET
	url_host=EXCLUDED.url_host,
	checked_at=EXCLUDED.checked_at,
	fetch_status=EXCLUDED.fetch_status,
	fetch_headers=EXCLUDED.fetch_headers,
	contents_md5=EXCLUDED.contents_md5,
	contents_last_modified=EXCLUDED.contents_last_modified,
	contents_size=EXCLUDED.contents_size,
	fetch_error_body=''
RETURNING id`
	const contentsQuery = `INSERT INTO icalproxy_feed_contents_v1
(feed_id, contents)
VALUES ($1, $2)
ON CONFLICT (feed_id) DO UPDATE SET contents = EXCLUDED.contents`
	feedArgs := []any{
		feed.Url.String(),
		types.NormalizeURLHostname(feed.Url),
		fetchedTrunc,
		feed.HttpStatus,
		feed.HttpHeaders,
		feed.MD5,
		fetchedTrunc,
		len(feed.Body),
	}
	var insertedId int64
	if err := db.QueryRow(ctx, feedQuery, feedArgs...).Scan(&insertedId); err != nil {
		return internal.ErrWrap(err, "unable to upsert feed")
	}
	if _, err := db.Exec(ctx, contentsQuery, insertedId, feed.Body); err != nil {
		return internal.ErrWrap(err, "unable to upsert contents")
	}
	return nil
}

// TruncateLocal deletes localhost and 127.0.0.1 urls,
// which are usually only generated during testing.
func TruncateLocal(ctx context.Context, db *pgxpool.Pool) error {
	_, err := db.Exec(ctx, `DELETE FROM icalproxy_feeds_v1 WHERE url_host='127001' OR url_host='LOCALHOST'`)
	if err != nil {
		return err
	}
	return nil
}
