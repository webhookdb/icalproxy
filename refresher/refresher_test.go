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
	"github.com/webhookdb/icalproxy/fp"
	"github.com/webhookdb/icalproxy/pgxt"
	"github.com/webhookdb/icalproxy/proxy"
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
				fp.Must(url.Parse(origin.URL()+"/expired-ttl.ics")),
				proxy.NewFeed(
					[]byte("EXPIRED"),
					time.Now().Add(-5*time.Hour),
				))).To(Succeed())
			Expect(db.CommitFeed(ag.DB, ctx,
				fp.Must(url.Parse(origin.URL()+"/recent-ttl.ics")),
				proxy.NewFeed(
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
					fp.Must(url.Parse(origin.URL()+"/feed-"+istr)),
					proxy.NewFeed(
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
	})
	Describe("SelectRowsToProcess", func() {
		It("selects rows that have not been checked since the TTL for their host", func() {
			// Set up 2 custom domains, with 30 and 60 minute TTLs.
			// Then create two feeds for each domain, with recent and expired TTLs.
			// Assert we fetched the expired ones. Note that this checks the per-host expiration
			// because for example the 45 minute feeds are expired for a 30 minute TTL but live for a 60 minute TTL.

			ag.Config.IcalTTLMap["30MINCOM"] = types.TTL(30 * time.Minute)
			ag.Config.IcalTTLMap["60MINCOM"] = types.TTL(60 * time.Minute)

			Expect(db.CommitFeed(ag.DB, ctx,
				fp.Must(url.Parse("https://30min.com/15old")),
				proxy.NewFeed([]byte("ORIGINAL"), time.Now().Add(-15*time.Minute)),
			)).To(Succeed())
			Expect(db.CommitFeed(ag.DB, ctx,
				fp.Must(url.Parse("https://30min.com/45old")),
				proxy.NewFeed([]byte("ORIGINAL"), time.Now().Add(-45*time.Minute)),
			)).To(Succeed())

			Expect(db.CommitFeed(ag.DB, ctx,
				fp.Must(url.Parse("https://60min.com/45old")),
				proxy.NewFeed([]byte("ORIGINAL"), time.Now().Add(-45*time.Minute)),
			)).To(Succeed())
			Expect(db.CommitFeed(ag.DB, ctx,
				fp.Must(url.Parse("https://60min.com/75old")),
				proxy.NewFeed([]byte("ORIGINAL"), time.Now().Add(-75*time.Minute)),
			)).To(Succeed())

			Expect(pgxt.WithTransaction(ctx, ag.DB, func(tx pgx.Tx) error {
				rows, err := refresher.New(ag).SelectRowsToProcess(ctx, tx)
				Expect(err).ToNot(HaveOccurred())
				Expect(rows).To(ConsistOf(
					HaveField("Url", "https://30min.com/45old"),
					HaveField("Url", "https://60min.com/75old"),
				))
				return nil
			})).To(Succeed())

		})
	})
})
