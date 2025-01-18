package db_test

import (
	"context"
	"github.com/lithictech/go-aperitif/v2/logctx"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/webhookdb/icalproxy/appglobals"
	"github.com/webhookdb/icalproxy/config"
	"github.com/webhookdb/icalproxy/db"
	"github.com/webhookdb/icalproxy/proxy"
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

	var cfg config.Config
	var ag *appglobals.AppGlobals

	BeforeEach(func() {
		var err error
		cfg, err = config.LoadConfig()
		Expect(err).ToNot(HaveOccurred())
		ag, err = appglobals.New(ctx, cfg)
		Expect(err).ToNot(HaveOccurred())
	})

	AfterEach(func() {
		Expect(db.Truncate(ctx, ag.DB)).To(Succeed())
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
VALUES ('https://a.b.c', 'ABC', now(), 'vevent', 'abc123', now(), 5)`)
			Expect(err).ToNot(HaveOccurred())
			r, err := db.FetchConditionalRow(ag.DB, ctx, newUrl("https://a.b.c"))
			Expect(err).ToNot(HaveOccurred())
			Expect(r.ContentsMD5).To(BeEquivalentTo("abc123"))
		})
		It("returns nil if the row does not exist", func() {
			r, err := db.FetchConditionalRow(ag.DB, ctx, newUrl("https://a.b.c"))
			Expect(err).ToNot(HaveOccurred())
			Expect(r).To(BeNil())
		})
	})
	Describe("FetchContentsAsFeed", func() {
		It("returns the row", func() {
			_, err := ag.DB.Exec(ctx, `INSERT INTO icalproxy_feeds_v1(url, url_host, checked_at, contents, contents_md5, contents_last_modified, contents_size)
VALUES ('https://a.b.c', 'ABC', now(), 'vevent', 'abc123', now(), 5)`)
			Expect(err).ToNot(HaveOccurred())
			r, err := db.FetchContentsAsFeed(ag.DB, ctx, newUrl("https://a.b.c"))
			Expect(err).ToNot(HaveOccurred())
			Expect(r.MD5).To(BeEquivalentTo("abc123"))
		})
		It("errors if the row does not exist", func() {
			_, err := db.FetchContentsAsFeed(ag.DB, ctx, newUrl("https://a.b.c"))
			Expect(err).To(MatchError(ContainSubstring("no rows in result set")))
		})
	})
	Describe("CommitFeed", func() {
		It("sets fields from the passed in feed", func() {
			t := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
			Expect(db.CommitFeed(ag.DB, ctx, newUrl("https://a.b.c"), &proxy.Feed{
				Body:      []byte("hello"),
				MD5:       "abc123",
				FetchedAt: t,
			})).To(Succeed())
			var checkedAt time.Time
			var md5 string
			Expect(ag.DB.QueryRow(ctx, `SELECT contents_md5, checked_at FROM icalproxy_feeds_v1`).Scan(&md5, &checkedAt)).To(Succeed())
			Expect(checkedAt).To(BeTemporally("~", t))
			Expect(md5).To(BeEquivalentTo("abc123"))
		})
		It("upserts fields", func() {
			Expect(db.CommitFeed(ag.DB, ctx, newUrl("https://a.b.c"), &proxy.Feed{
				Body:      []byte("hello"),
				MD5:       "call1",
				FetchedAt: time.Now(),
			})).To(Succeed())
			Expect(db.CommitFeed(ag.DB, ctx, newUrl("https://a.b.c"), &proxy.Feed{
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

func newUrl(s string) *url.URL {
	u, err := url.Parse(s)
	Expect(err).ToNot(HaveOccurred())
	return u
}
