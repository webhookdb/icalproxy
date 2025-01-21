package refresher_test

import (
	"context"
	"github.com/jackc/pgx/v5"
	"github.com/lithictech/go-aperitif/v2/logctx"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/ghttp"
	"github.com/webhookdb/icalproxy/appglobals"
	"github.com/webhookdb/icalproxy/config"
	"github.com/webhookdb/icalproxy/db"
	"github.com/webhookdb/icalproxy/feed"
	"github.com/webhookdb/icalproxy/fp"
	. "github.com/webhookdb/icalproxy/icalproxytest"
	"github.com/webhookdb/icalproxy/pgxt"
	"github.com/webhookdb/icalproxy/refresher"
	"github.com/webhookdb/icalproxy/types"
	"math/rand"
	"net/http"
	"net/url"
	"strconv"
	"testing"
	"time"
)

func TestRefresher(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "refresher package Suite")
}

var _ = Describe("refresher", func() {
	var ctx context.Context
	var hook *logctx.Hook
	var ag *appglobals.AppGlobals
	var origin *ghttp.Server
	var d *db.DB

	BeforeEach(func() {
		ctx, hook = logctx.WithNullLogger(context.Background())
		ag = fp.Must(appglobals.New(ctx, fp.Must(config.LoadConfig())))
		Expect(TruncateLocal(ctx, ag.DB)).To(Succeed())
		origin = ghttp.NewServer()
		d = db.New(ag.DB)
	})

	AfterEach(func() {
		origin.Close()
	})

	expiredFeed := func(tail string) *feed.Feed {
		return feed.New(
			fp.Must(url.Parse(origin.URL()+tail)),
			make(map[string]string),
			200,
			[]byte("EXPIRED"),
			time.Now().Add(-5*time.Hour),
		)
	}

	Describe("StartScheduler", func() {
		It("starts a routine that can be canceled", func() {
			ctx, cancel := context.WithCancel(ctx)
			refresher.StartScheduler(ctx, refresher.New(ag))
			cancel()
		})
	})
	Describe("Run", func() {
		It("refreshes all feeds that need it", func() {
			origin.AppendHandlers(
				ghttp.CombineHandlers(
					ghttp.VerifyRequest("GET", "/expired-ttl.ics", ""),
					ghttp.RespondWith(200, "FETCHED"),
				),
			)
			Expect(d.CommitFeed(ctx,
				feed.New(
					fp.Must(url.Parse(origin.URL()+"/expired-ttl.ics")),
					make(map[string]string),
					200,
					[]byte("EXPIRED"),
					time.Now().Add(-5*time.Hour),
				), nil)).To(Succeed())
			Expect(d.CommitFeed(ctx,
				feed.New(
					fp.Must(url.Parse(origin.URL()+"/recent-ttl.ics")),
					make(map[string]string),
					200,
					[]byte("RECENT"),
					time.Now().Add(-time.Hour),
				), nil)).To(Succeed())

			Expect(refresher.New(ag).Run(ctx)).To(Succeed())

			row := fp.Must(pgx.CollectExactlyOneRow[FeedRow](
				fp.Must(ag.DB.Query(ctx, `SELECT * FROM icalproxy_feeds_v1 WHERE url = $1`, origin.URL()+"/expired-ttl.ics")),
				pgx.RowToStructByName[FeedRow],
			))
			Expect(row).To(And(
				HaveField("ContentsMD5", MustMD5("FETCHED")),
				HaveField("WebhookPending", false),
			))
		})
		It("sets changed feeds as pending a webhook if configured", func() {
			ag.Config.WebhookUrl = "https://fake"
			Expect(d.CommitFeed(ctx,
				feed.New(
					fp.Must(url.Parse(origin.URL()+"/changed.ics")),
					make(map[string]string),
					200,
					[]byte("CHANGED-ORIGINAL"),
					time.Now().Add(-5*time.Hour),
				), nil)).To(Succeed())
			origin.RouteToHandler("GET", "/changed.ics",
				ghttp.RespondWith(200, "CHANGED-DIFF"),
			)
			Expect(d.CommitFeed(ctx,
				feed.New(
					fp.Must(url.Parse(origin.URL()+"/unchanged.ics")),
					make(map[string]string),
					200,
					[]byte("UNCHANGED"),
					time.Now().Add(-5*time.Hour),
				), nil)).To(Succeed())
			origin.RouteToHandler("GET", "/unchanged.ics",
				ghttp.RespondWith(200, "UNCHANGED"),
			)
			Expect(d.CommitFeed(ctx,
				feed.New(
					fp.Must(url.Parse(origin.URL()+"/erroring.ics")),
					make(map[string]string),
					200,
					[]byte("ERRORING-ORIG"),
					time.Now().Add(-5*time.Hour),
				), nil)).To(Succeed())
			origin.RouteToHandler("GET", "/erroring.ics",
				ghttp.RespondWith(503, "ERRORING-FETCHED"),
			)

			Expect(refresher.New(ag).Run(ctx)).To(Succeed())

			rows := fp.Must(pgx.CollectRows[FeedRow](
				fp.Must(ag.DB.Query(ctx, `SELECT * FROM icalproxy_feeds_v1 WHERE starts_with(url, $1)`, origin.URL())),
				pgx.RowToStructByName[FeedRow],
			))
			Expect(rows).To(And(
				ContainElement(And(
					HaveField("Url", HaveSuffix("erroring.ics")),
					HaveField("WebhookPending", false),
				)),
				ContainElement(And(
					HaveField("Url", HaveSuffix("unchanged.ics")),
					HaveField("WebhookPending", false),
				)),
				ContainElement(And(
					HaveField("Url", HaveSuffix("changed.ics")),
					HaveField("WebhookPending", true),
				)),
			))
		})
		It("can work for large sets, without races or page issues", func() {
			rowCnt := 1003 + rand.Intn(500)
			for i := 0; i < rowCnt; i++ {
				istr := strconv.Itoa(i)
				Expect(d.CommitFeed(ctx, expiredFeed("/feed-"+istr), nil)).To(Succeed())
				origin.RouteToHandler("GET", "/feed-"+istr, ghttp.RespondWith(200, "FETCHED-"+istr))
			}
			Expect(refresher.New(ag).Run(ctx)).To(Succeed())

			row2 := fp.Must(d.FetchContentsAsFeed(ctx, fp.Must(url.Parse(origin.URL()+"/feed-2"))))
			Expect(string(row2.Body)).To(BeEquivalentTo("FETCHED-2"))

			row1002 := fp.Must(d.FetchContentsAsFeed(ctx, fp.Must(url.Parse(origin.URL()+"/feed-1002"))))
			Expect(string(row1002.Body)).To(BeEquivalentTo("FETCHED-1002"))
		})
		It("commits rows that fail to fetch", func() {
			origin.AppendHandlers(
				ghttp.CombineHandlers(
					ghttp.VerifyRequest("GET", "/expired-ttl.ics", ""),
					ghttp.RespondWith(401, "errbody"),
				),
			)
			Expect(d.CommitFeed(ctx, expiredFeed("/expired-ttl.ics"), nil)).To(Succeed())

			Expect(refresher.New(ag).Run(ctx)).To(Succeed())

			row := fp.Must(pgx.CollectExactlyOneRow[FeedRow](
				fp.Must(ag.DB.Query(ctx, `SELECT * FROM icalproxy_feeds_v1 WHERE url = $1`, origin.URL()+"/expired-ttl.ics")),
				pgx.RowToStructByName[FeedRow],
			))
			Expect(row).To(And(
				HaveField("FetchStatus", 401),
				HaveField("FetchErrorBody", BeEquivalentTo("errbody")),
				HaveField("WebhookPending", false),
			))
		})
		It("marks repeated fetch failures as unchanged (compares bodies)", func() {
			Expect(d.CommitFeed(ctx, expiredFeed("/feed.ics"), nil)).To(Succeed())
			origin.AppendHandlers(
				ghttp.CombineHandlers(
					ghttp.VerifyRequest("GET", "/feed.ics", ""),
					ghttp.RespondWith(404, ""),
				),
			)
			origin.AppendHandlers(
				ghttp.CombineHandlers(
					ghttp.VerifyRequest("GET", "/feed.ics", ""),
					ghttp.RespondWith(404, ""),
				),
			)
			Expect(refresher.New(ag).Run(ctx)).To(Succeed())
			// Set this to need to be checked again
			_, err := ag.DB.Exec(ctx, "UPDATE icalproxy_feeds_v1 SET checked_at=$1 WHERE url=$2", time.Now().Add(-5*time.Hour), origin.URL()+"/feed.ics")
			Expect(err).ToNot(HaveOccurred())
			Expect(refresher.New(ag).Run(ctx)).To(Succeed())
			messages := fp.Map(hook.Records(), func(r logctx.HookRecord) string { return r.Record.Message })
			Expect(messages).To(ContainElement("feed_change_committed"))
			Expect(messages).To(ContainElement("feed_unchanged"))
		})
		It("commits rows that timeout (fail with a url.Error from HttpClient.Do)", func() {
			ag.Config.RefreshTimeout = 0
			origin.AppendHandlers(
				ghttp.CombineHandlers(
					ghttp.VerifyRequest("GET", "/expired-ttl.ics", ""),
					func(w http.ResponseWriter, r *http.Request) {
						time.Sleep(1 * time.Second)
						w.WriteHeader(500)
					},
				),
			)
			Expect(d.CommitFeed(ctx, expiredFeed("/expired-ttl.ics"), nil)).To(Succeed())

			Expect(refresher.New(ag).Run(ctx)).To(Succeed())

			row := fp.Must(pgx.CollectExactlyOneRow[FeedRow](
				fp.Must(ag.DB.Query(ctx, `SELECT * FROM icalproxy_feeds_v1 WHERE url = $1`, origin.URL()+"/expired-ttl.ics")),
				pgx.RowToStructByName[FeedRow],
			))
			Expect(row).To(And(
				HaveField("FetchStatus", 599),
				HaveField("FetchErrorBody", ContainSubstring("context deadline exceeded")),
			))
		})
		It("commits unchanged rows", func() {
			origin.AppendHandlers(
				ghttp.CombineHandlers(
					ghttp.VerifyRequest("GET", "/expired-ttl.ics", ""),
					ghttp.RespondWith(200, "SAMEBODY"),
				),
			)
			Expect(d.CommitFeed(ctx,
				feed.New(
					fp.Must(url.Parse(origin.URL()+"/expired-ttl.ics")),
					make(map[string]string),
					200,
					[]byte("SAMEBODY"),
					time.Now().Add(-5*time.Hour),
				), &db.CommitFeedOptions{WebhookPending: false})).To(Succeed())

			Expect(refresher.New(ag).Run(ctx)).To(Succeed())
			row := fp.Must(pgx.CollectExactlyOneRow[FeedRow](
				fp.Must(ag.DB.Query(ctx, `SELECT * FROM icalproxy_feeds_v1 WHERE url = $1`, origin.URL()+"/expired-ttl.ics")),
				pgx.RowToStructByName[FeedRow],
			))
			Expect(row).To(And(
				HaveField("ContentsMD5", MustMD5("SAMEBODY")),
				HaveField("CheckedAt", BeTemporally("~", time.Now(), time.Minute)),
				// Make sure this doesn't get set back to true when there is no change
				HaveField("WebhookPending", false),
			))
		})
		It("commits rows where Fetch returns ErrNotModified", func() {
			origin.AppendHandlers(
				ghttp.CombineHandlers(
					ghttp.VerifyRequest("GET", "/expired-ttl.ics", ""),
					ghttp.RespondWith(304, ""),
				),
			)
			Expect(d.CommitFeed(ctx,
				feed.New(
					fp.Must(url.Parse(origin.URL()+"/expired-ttl.ics")),
					make(map[string]string),
					200,
					[]byte("SAMEBODY"),
					time.Now().Add(-5*time.Hour),
				), &db.CommitFeedOptions{WebhookPending: false})).To(Succeed())

			Expect(refresher.New(ag).Run(ctx)).To(Succeed())
			row := fp.Must(pgx.CollectExactlyOneRow[FeedRow](
				fp.Must(ag.DB.Query(ctx, `SELECT * FROM icalproxy_feeds_v1 WHERE url = $1`, origin.URL()+"/expired-ttl.ics")),
				pgx.RowToStructByName[FeedRow],
			))
			Expect(row).To(And(
				HaveField("ContentsMD5", MustMD5("SAMEBODY")),
				HaveField("CheckedAt", BeTemporally("~", time.Now(), time.Minute)),
				// Make sure this doesn't get set back to true when there is no change
				HaveField("WebhookPending", false),
			))
		})
	})
	Describe("SelectRowsToProcess", func() {
		It("selects rows that have not been checked since the TTL for their host", func() {
			// Set up 2 custom domains, with 30 and 60 minute TTLs.
			// Then create two feeds for each domain, with recent and expired TTLs.
			// Assert we fetched the expired ones. Note that this checks the per-host expiration
			// because for example the 45 minute feeds are expired for a 30 minute TTL but live for a 60 minute TTL.

			ag.Config.IcalTTLMap["30MINLOCALHOST"] = types.TTL(30 * time.Minute)
			ag.Config.IcalTTLMap["60MINLOCALHOST"] = types.TTL(60 * time.Minute)
			hd := make(map[string]string)

			Expect(d.CommitFeed(ctx,
				feed.New(fp.Must(url.Parse("https://30min.localhost/15old")), hd, 200, []byte("ORIGINAL"), time.Now().Add(-15*time.Minute)), nil,
			)).To(Succeed())
			Expect(d.CommitFeed(ctx,
				feed.New(fp.Must(url.Parse("https://30min.localhost/45old")), hd, 200, []byte("ORIGINAL"), time.Now().Add(-45*time.Minute)), nil,
			)).To(Succeed())

			Expect(d.CommitFeed(ctx,
				feed.New(fp.Must(url.Parse("https://60min.localhost/45old")), hd, 200, []byte("ORIGINAL"), time.Now().Add(-45*time.Minute)), nil,
			)).To(Succeed())
			Expect(d.CommitFeed(ctx,
				feed.New(fp.Must(url.Parse("https://60min.localhost/75old")), hd, 200, []byte("ORIGINAL"), time.Now().Add(-75*time.Minute)), nil,
			)).To(Succeed())

			Expect(pgxt.WithTransaction(ctx, d.Conn(), func(tx pgx.Tx) error {
				rows, err := refresher.New(ag).SelectRowsToProcess(ctx, tx)
				Expect(err).ToNot(HaveOccurred())
				Expect(rows).To(ConsistOf(
					HaveField("Url", "https://30min.localhost/45old"),
					HaveField("Url", "https://60min.localhost/75old"),
				))
				return nil
			})).To(Succeed())
		})
		It("uses indices for its query", func() {
			// Test the actual query, we want to make sure we don't accidentally regress on performance
			// since this is a really important query to keep fast.
			ag.Config.IcalTTLMap["EXAMPLEORG"] = types.TTL(time.Minute)
			expl, err := refresher.New(ag).ExplainSelectQuery(ctx)
			Expect(err).NotTo(HaveOccurred())
			// Limit  (cost=12.54..16.57 rows=1 width=63) (actual time=0.025..0.025 rows=0 loops=1)
			//  ->  LockRows  (cost=12.54..16.57 rows=1 width=63) (actual time=0.024..0.024 rows=0 loops=1)
			//        ->  Bitmap Heap Scan on icalproxy_feeds_v1  (cost=12.54..16.56 rows=1 width=63) (actual time=0.023..0.024 rows=0 loops=1)
			//              Recheck Cond: (starts_with(url_host_rev, 'GROELPMAXE'::text) OR (checked_at < ('2025-01-19 00:26:55+00'::timestamp with time zone - '02:00:00'::interval)))
			//              Filter: ((starts_with(url_host_rev, 'GROELPMAXE'::text) AND (checked_at < ('2025-01-19 00:26:55+00'::timestamp with time zone - '00:01:00'::interval))) OR (checked_at < ('2025-01-19 00:26:55+00'::timestamp with time zone - '02:00:00'::interval)))
			//              ->  BitmapOr  (cost=12.54..12.54 rows=1 width=0) (actual time=0.021..0.021 rows=0 loops=1)
			//                    ->  Bitmap Index Scan on icalproxy_feeds_v1_url_host_rev_idx  (cost=0.00..8.27 rows=1 width=0) (actual time=0.018..0.018 rows=0 loops=1)
			//                          Index Cond: ((url_host_rev >= 'GROELPMAXE'::text) AND (url_host_rev < 'GROELPMAXF'::text))
			//                    ->  Bitmap Index Scan on icalproxy_feeds_v1_checked_at_idx  (cost=0.00..4.27 rows=1 width=0) (actual time=0.002..0.002 rows=0 loops=1)
			//                          Index Cond: (checked_at < ('2025-01-19 00:26:55+00'::timestamp with time zone - '02:00:00'::interval))
			// Planning Time: 0.868 ms
			// Execution Time: 0.551 ms
			Expect(expl).To(ContainSubstring("Index Cond: ((url_host_rev >= 'GROELPMAXE'::text) AND (url_host_rev < 'GROELPMAXF'::text))"))
			Expect(expl).To(ContainSubstring("Index Cond: (checked_at < "))
			Expect(expl).To(ContainSubstring("LockRows"))
		})
	})
})
