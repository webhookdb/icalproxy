package notifier_test

import (
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5"
	"github.com/lithictech/go-aperitif/v2/logctx"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/ghttp"
	"github.com/webhookdb/icalproxy/appglobals"
	"github.com/webhookdb/icalproxy/config"
	"github.com/webhookdb/icalproxy/db"
	"github.com/webhookdb/icalproxy/feed"
	"github.com/webhookdb/icalproxy/feedstorage/fakefeedstorage"
	"github.com/webhookdb/icalproxy/fp"
	. "github.com/webhookdb/icalproxy/icalproxytest"
	"github.com/webhookdb/icalproxy/notifier"
	"net/http"
	"net/url"
	"strconv"
	"testing"
	"time"
)

func TestNotifier(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "notifier package Suite")
}

var _ = Describe("notifier", func() {
	var ctx context.Context
	var ag *appglobals.AppGlobals
	var fs *fakefeedstorage.FakeFeedStorage
	var webhookSrv *ghttp.Server

	BeforeEach(func() {
		ctx, _ = logctx.WithNullLogger(context.Background())
		ag = fp.Must(appglobals.New(ctx, fp.Must(config.LoadConfig())))
		Expect(TruncateLocal(ctx, ag.DB)).To(Succeed())
		webhookSrv = ghttp.NewServer()
		fs = fakefeedstorage.New()
	})

	AfterEach(func() {
		webhookSrv.Close()
	})

	Describe("StartScheduler", func() {
		It("starts a routine that can be canceled", func() {
			ctx, cancel := context.WithCancel(ctx)
			notifier.StartScheduler(ctx, notifier.New(ag))
			cancel()
		})
	})
	Describe("Run", func() {
		It("notifies the configured service for all rows needing a webhook", func() {
			ag.Config.WebhookUrl = webhookSrv.URL() + "/wh"
			// Set up 125 rows so we make 2 webhook requests
			for i := 0; i < 125; i++ {
				u := "https://notifiertest.localhost/feed-" + strconv.Itoa(i)
				Expect(db.New(ag.DB).CommitFeed(ctx,
					fs,
					feed.New(
						fp.Must(url.Parse(u)),
						make(map[string]string),
						200,
						[]byte("FEED"),
						time.Now(),
					), &db.CommitFeedOptions{WebhookPendingOnInsert: true})).To(Succeed())
			}
			// Webhook is not pending, so this gets no http handler
			Expect(db.New(ag.DB).CommitFeed(ctx,
				fs,
				feed.New(
					fp.Must(url.Parse("https://notifiertest.localhost/feed-10000")),
					make(map[string]string),
					200,
					[]byte("FEED"),
					time.Now(),
				), nil)).To(Succeed())
			// first request will have 100 rows
			webhookSrv.AppendHandlers(
				ghttp.CombineHandlers(
					ghttp.VerifyRequest("POST", "/wh", ""),
					func(w http.ResponseWriter, req *http.Request) {
						Expect(req.Header).ToNot(HaveKey("Authorization"))
						var b map[string]any
						Expect(json.NewDecoder(req.Body).Decode(&b)).To(Succeed())
						Expect(b).To(HaveKeyWithValue("urls", HaveLen(100)))
					},
					ghttp.RespondWith(200, ""),
				),
			)
			// second will have 25 rows
			webhookSrv.AppendHandlers(
				ghttp.CombineHandlers(
					ghttp.VerifyRequest("POST", "/wh", ""),
					func(w http.ResponseWriter, req *http.Request) {
						var b map[string]any
						Expect(json.NewDecoder(req.Body).Decode(&b)).To(Succeed())
						Expect(b).To(HaveKeyWithValue("urls", HaveLen(25)))
					},
					ghttp.RespondWith(200, ""),
				),
			)

			Expect(notifier.New(ag).Run(ctx)).To(Succeed())

			row := fp.Must(pgx.CollectExactlyOneRow[FeedRow](
				fp.Must(ag.DB.Query(ctx, `SELECT * FROM icalproxy_feeds_v2 WHERE url = 'https://notifiertest.localhost/feed-5'`)),
				pgx.RowToStructByName[FeedRow],
			))
			Expect(row).To(HaveField("WebhookPending", false))
		})
		It("errors if the webhook call fails", func() {
			ag.Config.WebhookUrl = webhookSrv.URL() + "/wh"
			Expect(db.New(ag.DB).CommitFeed(ctx,
				fs,
				feed.New(
					fp.Must(url.Parse("https://notifiertest.localhost/feed")),
					make(map[string]string),
					200,
					[]byte("FEED"),
					time.Now(),
				), &db.CommitFeedOptions{WebhookPendingOnInsert: true})).To(Succeed())
			webhookSrv.AppendHandlers(
				ghttp.CombineHandlers(
					ghttp.VerifyRequest("POST", "/wh", ""),
					ghttp.RespondWith(503, ""),
				),
			)

			Expect(notifier.New(ag).Run(ctx)).To(MatchError(ContainSubstring("error sending webhook: 503")))

			row := fp.Must(pgx.CollectExactlyOneRow[FeedRow](
				fp.Must(ag.DB.Query(ctx, `SELECT * FROM icalproxy_feeds_v2 WHERE url = 'https://notifiertest.localhost/feed'`)),
				pgx.RowToStructByName[FeedRow],
			))
			Expect(row).To(HaveField("WebhookPending", true))
		})

		It("includes the api key header if configured", func() {
			ag.Config.WebhookUrl = webhookSrv.URL() + "/wh"
			ag.Config.ApiKey = "sekret"
			Expect(db.New(ag.DB).CommitFeed(ctx,
				fs,
				feed.New(
					fp.Must(url.Parse("https://notifiertest.localhost/feed")),
					make(map[string]string),
					200,
					[]byte("FEED"),
					time.Now(),
				), &db.CommitFeedOptions{WebhookPendingOnInsert: true})).To(Succeed())

			webhookSrv.AppendHandlers(
				ghttp.CombineHandlers(
					ghttp.VerifyRequest("POST", "/wh", ""),
					ghttp.VerifyHeaderKV("Authorization", "Apikey sekret"),
					ghttp.RespondWith(200, ""),
				),
			)
			Expect(notifier.New(ag).Run(ctx)).To(Succeed())
		})
	})
})
