package db_test

import (
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5"
	"github.com/lithictech/go-aperitif/v2/logctx"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/webhookdb/icalproxy/appglobals"
	"github.com/webhookdb/icalproxy/config"
	"github.com/webhookdb/icalproxy/db"
	"github.com/webhookdb/icalproxy/feed"
	"github.com/webhookdb/icalproxy/fp"
	"net/url"
	"testing"
	"time"
)

func TestDB(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "db package Suite")
}

var _ = Describe("db", func() {
	ctx, _ := logctx.WithNullLogger(context.Background())

	var ag *appglobals.AppGlobals

	BeforeEach(func() {
		ag = fp.Must(appglobals.New(ctx, fp.Must(config.LoadConfig())))
		Expect(db.TruncateLocal(ctx, ag.DB)).To(Succeed())
	})

	Describe("Migrate", func() {
		It("creates the table and indices if they do not exist", func() {
			Expect(db.Migrate(ctx, ag.DB)).To(Succeed())
			Expect(db.Migrate(ctx, ag.DB)).To(Succeed())
		})
	})
	Describe("FetchFeedRow", func() {
		It("returns the row if it exists", func() {
			Expect(db.CommitFeed(ag.DB, ctx, &feed.Feed{
				Url:         fp.Must(url.Parse("https://localhost/feed")),
				HttpHeaders: make(map[string]string),
				HttpStatus:  200,
				Body:        []byte("hello"),
				MD5:         "abc123",
				FetchedAt:   time.Now(),
			})).To(Succeed())
			r, err := db.FetchFeedRow(ag.DB, ctx, fp.Must(url.Parse("https://localhost/feed")))
			Expect(err).ToNot(HaveOccurred())
			Expect(r.ContentsMD5).To(BeEquivalentTo("abc123"))
		})
		It("returns nil if the row does not exist", func() {
			r, err := db.FetchFeedRow(ag.DB, ctx, fp.Must(url.Parse("https://localhost/feed")))
			Expect(err).ToNot(HaveOccurred())
			Expect(r).To(BeNil())
		})
	})
	Describe("FetchContentsAsFeed", func() {
		It("returns the row", func() {
			Expect(db.CommitFeed(ag.DB, ctx, &feed.Feed{
				Url:         fp.Must(url.Parse("https://localhost/feed")),
				HttpHeaders: make(map[string]string),
				HttpStatus:  200,
				Body:        []byte("hello"),
				MD5:         "abc123",
				FetchedAt:   time.Now(),
			})).To(Succeed())
			r, err := db.FetchContentsAsFeed(ag.DB, ctx, fp.Must(url.Parse("https://localhost/feed")))
			Expect(err).ToNot(HaveOccurred())
			Expect(r.MD5).To(BeEquivalentTo("abc123"))
		})
		It("errors if the row does not exist", func() {
			_, err := db.FetchContentsAsFeed(ag.DB, ctx, fp.Must(url.Parse("https://localhost/feed")))
			Expect(err).To(MatchError(ContainSubstring("no rows in result set")))
		})
		It("errors if only the content row does not exist", func() {
			_, err := ag.DB.Exec(ctx, `INSERT INTO icalproxy_feeds_v1(url, url_host_rev, checked_at, contents_md5, contents_last_modified, contents_size, fetch_status, fetch_headers)
VALUES ('https://localhost/feed', 'TSOHLACOL', now(), 'abc123', now(), 5, 200, '{}')`)
			Expect(err).ToNot(HaveOccurred())
			_, err = db.FetchContentsAsFeed(ag.DB, ctx, fp.Must(url.Parse("https://localhost/feed")))
			Expect(err).To(MatchError(ContainSubstring("no rows in result set")))
		})
	})
	Describe("CommitFeed", func() {
		It("inserts and upserts fields from the passed in feed", func() {
			t := time.Date(2020, 1, 1, 0, 0, 0, 999999, time.UTC)
			// Make sure the fetch time is truncated to the nearest second, since that is what HTTP supports.
			tTrunc := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
			Expect(db.CommitFeed(ag.DB, ctx, &feed.Feed{
				Url:         fp.Must(url.Parse("https://localhost/feed")),
				HttpHeaders: map[string]string{"X": "1"},
				HttpStatus:  200,
				Body:        []byte("version1"),
				MD5:         "version1hash",
				FetchedAt:   t,
			})).To(Succeed())
			rowv1 := fp.Must(pgx.CollectExactlyOneRow[feedRow](
				fp.Must(ag.DB.Query(ctx, `SELECT * FROM icalproxy_feeds_v1 WHERE url = 'https://localhost/feed'`)),
				pgx.RowToStructByName[feedRow],
			))
			Expect(rowv1).To(And(
				HaveField("Url", "https://localhost/feed"),
				HaveField("UrlHostRev", "TSOHLACOL"),
				HaveField("CheckedAt", BeTemporally("==", tTrunc)),
				HaveField("ContentsMD5", "version1hash"),
				HaveField("ContentsLastModified", BeTemporally("==", tTrunc)),
				HaveField("ContentsSize", 8),
				HaveField("FetchStatus", 200),
				HaveField("FetchHeaders", BeEquivalentTo(`{"X": "1"}`)),
				HaveField("FetchErrorBody", BeEmpty()),
			))

			// Update and check all fields
			t2 := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
			Expect(db.CommitFeed(ag.DB, ctx, &feed.Feed{
				Url:         fp.Must(url.Parse("https://localhost/feed")),
				HttpHeaders: map[string]string{"X": "11"},
				HttpStatus:  201,
				Body:        []byte("version2X"),
				MD5:         "version2hash",
				FetchedAt:   t2,
			})).To(Succeed())
			rowv2 := fp.Must(pgx.CollectExactlyOneRow[feedRow](
				fp.Must(ag.DB.Query(ctx, `SELECT * FROM icalproxy_feeds_v1 WHERE url = 'https://localhost/feed'`)),
				pgx.RowToStructByName[feedRow],
			))
			Expect(rowv2).To(And(
				HaveField("Url", "https://localhost/feed"),
				HaveField("UrlHostRev", "TSOHLACOL"),
				HaveField("CheckedAt", BeTemporally("==", t2)),
				HaveField("ContentsMD5", "version2hash"),
				HaveField("ContentsLastModified", BeTemporally("==", t2)),
				HaveField("ContentsSize", 9),
				HaveField("FetchStatus", 201),
				HaveField("FetchHeaders", BeEquivalentTo(`{"X": "11"}`)),
				HaveField("FetchErrorBody", BeEmpty()),
			))
		})
		It("inserts and upserts field from an error response", func() {
			t := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
			Expect(db.CommitFeed(ag.DB, ctx, &feed.Feed{
				Url:         fp.Must(url.Parse("https://localhost/feed")),
				HttpHeaders: map[string]string{"X": "1"},
				HttpStatus:  400,
				Body:        []byte("someerror"),
				MD5:         "version1hash",
				FetchedAt:   t,
			})).To(Succeed())
			rowv1 := fp.Must(pgx.CollectExactlyOneRow[feedRow](
				fp.Must(ag.DB.Query(ctx, `SELECT * FROM icalproxy_feeds_v1 WHERE url = 'https://localhost/feed'`)),
				pgx.RowToStructByName[feedRow],
			))
			Expect(rowv1).To(And(
				HaveField("Url", "https://localhost/feed"),
				HaveField("UrlHostRev", "TSOHLACOL"),
				HaveField("CheckedAt", BeTemporally("==", t)),
				HaveField("ContentsMD5", ""),
				HaveField("ContentsLastModified", BeTemporally("==", t)),
				HaveField("ContentsSize", 0),
				HaveField("FetchStatus", 400),
				HaveField("FetchHeaders", BeEquivalentTo(`{"X": "1"}`)),
				HaveField("FetchErrorBody", BeEquivalentTo("someerror")),
			))

			// Update and check all fields
			t2 := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
			Expect(db.CommitFeed(ag.DB, ctx, &feed.Feed{
				Url:         fp.Must(url.Parse("https://localhost/feed")),
				HttpHeaders: map[string]string{"X": "11"},
				HttpStatus:  401,
				Body:        []byte("error2"),
				MD5:         "version2hash",
				FetchedAt:   t2,
			})).To(Succeed())
			rowv2 := fp.Must(pgx.CollectExactlyOneRow[feedRow](
				fp.Must(ag.DB.Query(ctx, `SELECT * FROM icalproxy_feeds_v1 WHERE url = 'https://localhost/feed'`)),
				pgx.RowToStructByName[feedRow],
			))
			Expect(rowv2).To(And(
				HaveField("Url", "https://localhost/feed"),
				HaveField("UrlHostRev", "TSOHLACOL"),
				HaveField("CheckedAt", BeTemporally("==", t2)),
				HaveField("ContentsMD5", ""),
				// last modified does NOT get updated
				HaveField("ContentsLastModified", BeTemporally("==", t)),
				HaveField("ContentsSize", 0),
				HaveField("FetchStatus", 401),
				HaveField("FetchHeaders", BeEquivalentTo(`{"X": "11"}`)),
				HaveField("FetchErrorBody", BeEquivalentTo("error2")),
			))
		})
		It("will clear error fields on a successful fetch", func() {
			t := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
			t2 := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
			Expect(db.CommitFeed(ag.DB, ctx, &feed.Feed{
				Url:         fp.Must(url.Parse("https://localhost/feed")),
				HttpHeaders: map[string]string{"X": "1"},
				HttpStatus:  400,
				Body:        []byte("someerror"),
				MD5:         "version1hash",
				FetchedAt:   t,
			})).To(Succeed())
			Expect(db.CommitFeed(ag.DB, ctx, &feed.Feed{
				Url:         fp.Must(url.Parse("https://localhost/feed")),
				HttpHeaders: map[string]string{"X": "11"},
				HttpStatus:  201,
				Body:        []byte("version2X"),
				MD5:         "version2hash",
				FetchedAt:   t2,
			})).To(Succeed())

			row := fp.Must(pgx.CollectExactlyOneRow[feedRow](
				fp.Must(ag.DB.Query(ctx, `SELECT * FROM icalproxy_feeds_v1 WHERE url = 'https://localhost/feed'`)),
				pgx.RowToStructByName[feedRow],
			))
			Expect(row).To(And(
				HaveField("Url", "https://localhost/feed"),
				HaveField("UrlHostRev", "TSOHLACOL"),
				HaveField("CheckedAt", BeTemporally("==", t2)),
				HaveField("ContentsMD5", "version2hash"),
				HaveField("ContentsLastModified", BeTemporally("==", t2)),
				HaveField("ContentsSize", 9),
				HaveField("FetchStatus", 201),
				HaveField("FetchHeaders", BeEquivalentTo(`{"X": "11"}`)),
				HaveField("FetchErrorBody", BeEmpty()),
			))
		})
		It("will clear success fields on an error fetch", func() {
			t := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
			t2 := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
			Expect(db.CommitFeed(ag.DB, ctx, &feed.Feed{
				Url:         fp.Must(url.Parse("https://localhost/feed")),
				HttpHeaders: map[string]string{"X": "1"},
				HttpStatus:  200,
				Body:        []byte("version1"),
				MD5:         "version1hash",
				FetchedAt:   t,
			})).To(Succeed())
			Expect(db.CommitFeed(ag.DB, ctx, &feed.Feed{
				Url:         fp.Must(url.Parse("https://localhost/feed")),
				HttpHeaders: map[string]string{"X": "11"},
				HttpStatus:  401,
				Body:        []byte("error2"),
				MD5:         "version2hash",
				FetchedAt:   t2,
			})).To(Succeed())
			row := fp.Must(pgx.CollectExactlyOneRow[feedRow](
				fp.Must(ag.DB.Query(ctx, `SELECT * FROM icalproxy_feeds_v1 WHERE url = 'https://localhost/feed'`)),
				pgx.RowToStructByName[feedRow],
			))
			Expect(row).To(And(
				HaveField("Url", "https://localhost/feed"),
				HaveField("UrlHostRev", "TSOHLACOL"),
				HaveField("CheckedAt", BeTemporally("==", t2)),
				HaveField("ContentsMD5", "version1hash"),
				HaveField("ContentsLastModified", BeTemporally("==", t)),
				HaveField("ContentsSize", 8),
				HaveField("FetchStatus", 401),
				HaveField("FetchHeaders", BeEquivalentTo(`{"X": "11"}`)),
				HaveField("FetchErrorBody", BeEquivalentTo("error2")),
			))
		})
	})
	Describe("CommitUnchanged", func() {
		It("bumps the checked_at time", func() {
			t := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
			fd := &feed.Feed{
				Url:         fp.Must(url.Parse("https://localhost/feed")),
				HttpHeaders: map[string]string{"X": "1"},
				HttpStatus:  200,
				Body:        []byte("version1"),
				MD5:         "version1hash",
				FetchedAt:   time.Now().Add(-time.Hour * 99999),
			}
			Expect(db.CommitFeed(ag.DB, ctx, fd)).To(Succeed())

			fd.FetchedAt = t
			Expect(db.CommitUnchanged(ag.DB, ctx, fd)).To(Succeed())
			row := fp.Must(pgx.CollectExactlyOneRow[feedRow](
				fp.Must(ag.DB.Query(ctx, `SELECT * FROM icalproxy_feeds_v1 WHERE url = 'https://localhost/feed'`)),
				pgx.RowToStructByName[feedRow],
			))
			Expect(row).To(And(
				HaveField("CheckedAt", BeTemporally("==", t)),
				HaveField("ContentsMD5", "version1hash"),
			))
		})
	})
})

type feedRow struct {
	Id                   int64
	Url                  string
	UrlHostRev           string
	CheckedAt            time.Time
	ContentsMD5          string
	ContentsLastModified time.Time
	ContentsSize         int
	FetchStatus          int
	FetchHeaders         json.RawMessage
	FetchErrorBody       []byte
}
