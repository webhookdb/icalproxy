package db_test

import (
	"context"
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
	Describe("FetchConditionalRow", func() {
		It("returns the row if it exists", func() {
			_, err := ag.DB.Exec(ctx, `INSERT INTO icalproxy_feeds_v1(url, url_host, checked_at, contents, contents_md5, contents_last_modified, contents_size)
VALUES ('https://localhost/feed', 'LOCALHOST', now(), 'vevent', 'abc123', now(), 5)`)
			Expect(err).ToNot(HaveOccurred())
			r, err := db.FetchConditionalRow(ag.DB, ctx, fp.Must(url.Parse("https://localhost/feed")))
			Expect(err).ToNot(HaveOccurred())
			Expect(r.ContentsMD5).To(BeEquivalentTo("abc123"))
		})
		It("returns nil if the row does not exist", func() {
			r, err := db.FetchConditionalRow(ag.DB, ctx, fp.Must(url.Parse("https://localhost/feed")))
			Expect(err).ToNot(HaveOccurred())
			Expect(r).To(BeNil())
		})
	})
	Describe("FetchContentsAsFeed", func() {
		It("returns the row", func() {
			_, err := ag.DB.Exec(ctx, `INSERT INTO icalproxy_feeds_v1(url, url_host, checked_at, contents, contents_md5, contents_last_modified, contents_size)
VALUES ('https://localhost/feed', 'LOCALHOST', now(), 'vevent', 'abc123', now(), 5)`)
			Expect(err).ToNot(HaveOccurred())
			r, err := db.FetchContentsAsFeed(ag.DB, ctx, fp.Must(url.Parse("https://localhost/feed")))
			Expect(err).ToNot(HaveOccurred())
			Expect(r.MD5).To(BeEquivalentTo("abc123"))
		})
		It("errors if the row does not exist", func() {
			_, err := db.FetchContentsAsFeed(ag.DB, ctx, fp.Must(url.Parse("https://localhost/feed")))
			Expect(err).To(MatchError(ContainSubstring("no rows in result set")))
		})
	})
	Describe("CommitFeed", func() {
		It("sets fields from the passed in feed", func() {
			t := time.Date(2020, 1, 1, 0, 0, 0, 999999, time.UTC)
			Expect(db.CommitFeed(ag.DB, ctx, fp.Must(url.Parse("https://localhost/feed")), &feed.Feed{
				Body:      []byte("hello"),
				MD5:       "abc123",
				FetchedAt: t,
			})).To(Succeed())
			var checkedAt time.Time
			var md5 string
			Expect(ag.DB.QueryRow(ctx, `SELECT contents_md5, checked_at FROM icalproxy_feeds_v1`).Scan(&md5, &checkedAt)).To(Succeed())
			Expect(checkedAt.UTC().Format(time.RFC3339Nano)).To(Equal("2020-01-01T00:00:00Z"))
			Expect(md5).To(BeEquivalentTo("abc123"))
		})
		It("upserts fields", func() {
			Expect(db.CommitFeed(ag.DB, ctx, fp.Must(url.Parse("https://localhost/feed")), &feed.Feed{
				Body:      []byte("hello"),
				MD5:       "call1",
				FetchedAt: time.Now(),
			})).To(Succeed())
			Expect(db.CommitFeed(ag.DB, ctx, fp.Must(url.Parse("https://localhost/feed")), &feed.Feed{
				Body:      []byte("hello"),
				MD5:       "call2",
				FetchedAt: time.Now(),
			})).To(Succeed())
			var md5 string
			Expect(ag.DB.QueryRow(ctx, `SELECT contents_md5 FROM icalproxy_feeds_v1`).Scan(&md5)).To(Succeed())
			Expect(md5).To(BeEquivalentTo("call2"))
		})
	})
})
