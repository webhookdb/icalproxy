package db

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/webhookdb/icalproxy/feed"
	"github.com/webhookdb/icalproxy/internal"
	"github.com/webhookdb/icalproxy/pgxt"
	"github.com/webhookdb/icalproxy/types"
	"net/url"
	"time"
)

type IConn interface {
	pgxt.IBegin
	pgxt.IExec
	pgxt.IQueryRow
}

type DB struct {
	conn IConn
}

func New(conn IConn) *DB {
	return &DB{conn: conn}
}

func (db *DB) Conn() IConn {
	return db.conn
}

func (db *DB) exec(ctx context.Context, query string, args ...interface{}) error {
	_, err := db.conn.Exec(ctx, query, args...)
	return err
}

func (db *DB) Migrate(ctx context.Context) error {
	const q = `
CREATE TABLE IF NOT EXISTS icalproxy_feeds_v1 (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    url TEXT NOT NULL UNIQUE NOT NULL,
    -- Host TTL is specified using suffixes/ends with (icloud.com vs p123.icloud.com),
    -- but search performances requires prefixes (ie, 'starts with icloud.com).
    -- So store the url host reversed, so we can do "reverse('p123.icloud.com') starts with reverse('icloud.com')"
    -- See https://stackoverflow.com/questions/1566717/postgresql-like-query-performance-variations
    url_host_rev TEXT NOT NULL,
    checked_at timestamptz NOT NULL,
    contents_md5 TEXT NOT NULL,
    contents_last_modified timestamptz NOT NULL,
    contents_size INT NOT NULL,
    fetch_status INT NOT NULL,
    fetch_headers JSONB NOT NULL DEFAULT '{}',
    fetch_error_body BYTEA,
    webhook_pending BOOL NOT NULL DEFAULT FALSE
);
-- See above for details on this index.
CREATE INDEX IF NOT EXISTS icalproxy_feeds_v1_url_host_rev_idx ON icalproxy_feeds_v1(url_host_rev COLLATE "C");
-- Index checked_at since we need to know recent rows.
CREATE INDEX IF NOT EXISTS icalproxy_feeds_v1_checked_at_idx ON icalproxy_feeds_v1(checked_at);
-- Use partial index, we only need to check where something is pending, never where it's not.
CREATE INDEX IF NOT EXISTS icalproxy_feeds_v1_webhook_pending_idx ON icalproxy_feeds_v1((1)) WHERE webhook_pending;

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
	return db.exec(ctx, q)
}

func (db *DB) Reset(ctx context.Context) error {
	const q = `DROP TABLE IF EXISTS icalproxy_feed_contents_v1; DROP TABLE IF EXISTS icalproxy_feeds_v1;`
	return db.exec(ctx, q)
}

type FeedRow struct {
	ContentsMD5          types.MD5Hash
	ContentsLastModified time.Time
}

func (db *DB) FetchFeedRow(ctx context.Context, uri *url.URL) (*FeedRow, error) {
	r := FeedRow{}
	const q = `SELECT contents_md5, contents_last_modified FROM icalproxy_feeds_v1 WHERE url = $1`
	err := db.conn.QueryRow(ctx, q, uri.String()).Scan(&r.ContentsMD5, &r.ContentsLastModified)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &r, nil
}

func (db *DB) FetchContentsAsFeed(ctx context.Context, uri *url.URL) (*feed.Feed, error) {
	r := feed.Feed{}
	var fetchHeaders json.RawMessage
	const q = `SELECT
	fetch_headers, fetch_status, checked_at, contents_md5, (CASE WHEN fetch_status >= 400 THEN fetch_error_body ELSE contents END)
FROM icalproxy_feeds_v1
JOIN icalproxy_feed_contents_v1 ON feed_id = icalproxy_feeds_v1.id
WHERE url = $1`
	err := db.conn.QueryRow(ctx, q, uri.String()).Scan(
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

type CommitFeedOptions struct {
	WebhookPending bool
}

func (db *DB) CommitFeed(ctx context.Context, feed *feed.Feed, opts *CommitFeedOptions) error {
	if opts == nil {
		opts = &CommitFeedOptions{}
	}
	// Truncate the second out, since http only knows about seconds
	fetchedTrunc := feed.FetchedAt.Truncate(time.Second)
	urlHost := types.NormalizeURLHostname(feed.Url).Reverse()

	if feed.HttpStatus >= 400 {
		const errQuery = `INSERT INTO icalproxy_feeds_v1 
(url, url_host_rev, checked_at, fetch_status, fetch_headers, fetch_error_body, contents_md5, contents_last_modified, contents_size)
VALUES ($1, $2, $3, $4, $5, $6, '', $7, 0)
ON CONFLICT (url) DO UPDATE SET
	url_host_rev=EXCLUDED.url_host_rev,
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
		if err := db.exec(ctx, errQuery, args...); err != nil {
			return internal.ErrWrap(err, "unable to upsert error feed")
		}
		return nil
	}
	const feedQuery = `INSERT INTO icalproxy_feeds_v1 
(url, url_host_rev, checked_at, fetch_status, fetch_headers, contents_md5, contents_last_modified, contents_size, fetch_error_body)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, '')
ON CONFLICT (url) DO UPDATE SET
	url_host_rev=EXCLUDED.url_host_rev,
	checked_at=EXCLUDED.checked_at,
	fetch_status=EXCLUDED.fetch_status,
	fetch_headers=EXCLUDED.fetch_headers,
	contents_md5=EXCLUDED.contents_md5,
	contents_last_modified=EXCLUDED.contents_last_modified,
	contents_size=EXCLUDED.contents_size,
	fetch_error_body='',
	webhook_pending=$9
RETURNING id`
	const contentsQuery = `INSERT INTO icalproxy_feed_contents_v1
(feed_id, contents)
VALUES ($1, $2)
ON CONFLICT (feed_id) DO UPDATE SET contents = EXCLUDED.contents`
	feedArgs := []any{
		feed.Url.String(),
		urlHost,
		fetchedTrunc,
		feed.HttpStatus,
		feed.HttpHeaders,
		feed.MD5,
		fetchedTrunc,
		len(feed.Body),
		opts.WebhookPending,
	}
	var insertedId int64
	if err := db.conn.QueryRow(ctx, feedQuery, feedArgs...).Scan(&insertedId); err != nil {
		return internal.ErrWrap(err, "unable to upsert feed")
	}
	if err := db.exec(ctx, contentsQuery, insertedId, feed.Body); err != nil {
		return internal.ErrWrap(err, "unable to upsert contents")
	}
	return nil
}

func (db *DB) CommitUnchanged(ctx context.Context, feed *feed.Feed) error {
	fetchedTrunc := feed.FetchedAt.Truncate(time.Second)
	const query = `UPDATE icalproxy_feeds_v1 SET checked_at = $1 WHERE url = $2`
	if err := db.exec(ctx, query, fetchedTrunc, feed.Url); err != nil {
		return internal.ErrWrap(err, "unable to update feed")
	}
	return nil
}
