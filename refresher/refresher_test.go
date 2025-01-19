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
	"github.com/webhookdb/icalproxy/pgxt"
	"github.com/webhookdb/icalproxy/refresher"
	"github.com/webhookdb/icalproxy/types"
	"math/rand"
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
	var ag *appglobals.AppGlobals
	var origin *ghttp.Server

	BeforeEach(func() {
		ctx, _ = logctx.WithNullLogger(context.Background())
		ag = fp.Must(appglobals.New(ctx, fp.Must(config.LoadConfig())))
		Expect(db.TruncateLocal(ctx, ag.DB)).To(Succeed())
		origin = ghttp.NewServer()
	})

	AfterEach(func() {
		origin.Close()
	})

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
			Expect(db.CommitFeed(ag.DB, ctx,
				feed.New(
					fp.Must(url.Parse(origin.URL()+"/expired-ttl.ics")),
					make(map[string]string),
					200,
					[]byte("EXPIRED"),
					time.Now().Add(-5*time.Hour),
				))).To(Succeed())
			Expect(db.CommitFeed(ag.DB, ctx,
				feed.New(
					fp.Must(url.Parse(origin.URL()+"/recent-ttl.ics")),
					make(map[string]string),
					200,
					[]byte("RECENT"),
					time.Now().Add(-time.Hour),
				))).To(Succeed())

			Expect(refresher.New(ag).Run(ctx)).To(Succeed())

			row := fp.Must(db.FetchContentsAsFeed(ag.DB, ctx, fp.Must(url.Parse(origin.URL()+"/expired-ttl.ics"))))
			Expect(string(row.Body)).To(BeEquivalentTo("FETCHED"))
		})
		It("processes every row that needs it", func() {
			rowCnt := 1003 + rand.Intn(500)
			for i := 0; i < rowCnt; i++ {
				istr := strconv.Itoa(i)
				Expect(db.CommitFeed(ag.DB, ctx,
					feed.New(
						fp.Must(url.Parse(origin.URL()+"/feed-"+istr)),
						make(map[string]string),
						200,
						[]byte("FEED-"+istr),
						time.Now().Add(-5*time.Hour),
					))).To(Succeed())
				origin.RouteToHandler("GET", "/feed-"+istr, ghttp.RespondWith(200, "FETCHED-"+istr))
			}
			Expect(refresher.New(ag).Run(ctx)).To(Succeed())

			row2 := fp.Must(db.FetchContentsAsFeed(ag.DB, ctx, fp.Must(url.Parse(origin.URL()+"/feed-2"))))
			Expect(string(row2.Body)).To(BeEquivalentTo("FETCHED-2"))

			row1002 := fp.Must(db.FetchContentsAsFeed(ag.DB, ctx, fp.Must(url.Parse(origin.URL()+"/feed-1002"))))
			Expect(string(row1002.Body)).To(BeEquivalentTo("FETCHED-1002"))
		})
		It("commits rows that fail to fetch", func() {
			origin.AppendHandlers(
				ghttp.CombineHandlers(
					ghttp.VerifyRequest("GET", "/expired-ttl.ics", ""),
					ghttp.RespondWith(401, "errbody"),
				),
			)
			Expect(db.CommitFeed(ag.DB, ctx,
				feed.New(
					fp.Must(url.Parse(origin.URL()+"/expired-ttl.ics")),
					make(map[string]string),
					200,
					[]byte("EXPIRED"),
					time.Now().Add(-5*time.Hour),
				))).To(Succeed())

			Expect(refresher.New(ag).Run(ctx)).To(Succeed())

			row := fp.Must(db.FetchContentsAsFeed(ag.DB, ctx, fp.Must(url.Parse(origin.URL()+"/expired-ttl.ics"))))
			Expect(row).To(And(
				HaveField("HttpStatus", 401),
				HaveField("Body", BeEquivalentTo("errbody")),
			))
		})
		It("commits unchanged rows", func() {
			origin.AppendHandlers(
				ghttp.CombineHandlers(
					ghttp.VerifyRequest("GET", "/expired-ttl.ics", ""),
					ghttp.RespondWith(200, "SAMEBODY"),
				),
			)
			Expect(db.CommitFeed(ag.DB, ctx,
				feed.New(
					fp.Must(url.Parse(origin.URL()+"/expired-ttl.ics")),
					make(map[string]string),
					200,
					[]byte("SAMEBODY"),
					time.Now().Add(-5*time.Hour),
				))).To(Succeed())

			Expect(refresher.New(ag).Run(ctx)).To(Succeed())
			row := fp.Must(db.FetchContentsAsFeed(ag.DB, ctx, fp.Must(url.Parse(origin.URL()+"/expired-ttl.ics"))))
			Expect(row).To(And(
				HaveField("Body", BeEquivalentTo("SAMEBODY")),
				HaveField("FetchedAt", BeTemporally("~", time.Now(), time.Minute)),
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

			Expect(db.CommitFeed(ag.DB, ctx,
				feed.New(fp.Must(url.Parse("https://30min.localhost/15old")), hd, 200, []byte("ORIGINAL"), time.Now().Add(-15*time.Minute)),
			)).To(Succeed())
			Expect(db.CommitFeed(ag.DB, ctx,
				feed.New(fp.Must(url.Parse("https://30min.localhost/45old")), hd, 200, []byte("ORIGINAL"), time.Now().Add(-45*time.Minute)),
			)).To(Succeed())

			Expect(db.CommitFeed(ag.DB, ctx,
				feed.New(fp.Must(url.Parse("https://60min.localhost/45old")), hd, 200, []byte("ORIGINAL"), time.Now().Add(-45*time.Minute)),
			)).To(Succeed())
			Expect(db.CommitFeed(ag.DB, ctx,
				feed.New(fp.Must(url.Parse("https://60min.localhost/75old")), hd, 200, []byte("ORIGINAL"), time.Now().Add(-75*time.Minute)),
			)).To(Succeed())

			Expect(pgxt.WithTransaction(ctx, ag.DB, func(tx pgx.Tx) error {
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
