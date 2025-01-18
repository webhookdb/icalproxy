package feed_test

import (
	"context"
	"github.com/lithictech/go-aperitif/v2/logctx"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/ghttp"
	"github.com/webhookdb/icalproxy/feed"
	"github.com/webhookdb/icalproxy/fp"
	"github.com/webhookdb/icalproxy/types"
	"net/url"
	"testing"
	"time"
)

func TestFeed(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "feed package Suite")
}

var _ = Describe("feed", func() {
	ctx, _ := logctx.WithNullLogger(context.Background())

	Describe("TTLFor", func() {
		ttlmap := map[types.NormalizedHostname]types.TTL{
			"WEBHOOKDBCOM":    types.TTL(time.Minute * 15),
			"SUBWEBHOOKDBCOM": types.TTL(time.Minute * 10),
			"INFREQUENTCOM":   types.TTL(time.Hour * 20),
		}
		It("returns the ttl for a configured hostname", func() {
			Expect(feed.TTLFor(fp.Must(url.Parse("https://webhookdb.com/feed.ics")), ttlmap)).To(BeEquivalentTo(time.Minute * 15))
			Expect(feed.TTLFor(fp.Must(url.Parse("https://otherthing.webhookdb.com/feed.ics")), ttlmap)).To(BeEquivalentTo(time.Minute * 15))
		})
		It("returns the minimum TTL for all matching configured hosts", func() {
			Expect(feed.TTLFor(fp.Must(url.Parse("https://sub.webhookdb.com/feed.ics")), ttlmap)).To(BeEquivalentTo(time.Minute * 10))
		})
		It("returns the default ttl for no match, or a TTL higher than default", func() {
			Expect(feed.TTLFor(fp.Must(url.Parse("https://sub.lithic.tech/feed.ics")), ttlmap)).To(Equal(feed.DefaultTTL))
			Expect(feed.TTLFor(fp.Must(url.Parse("https://infrequent.com/feed.ics")), ttlmap)).To(Equal(feed.DefaultTTL))
		})
	})

	Describe("Fetch", func() {
		var server *ghttp.Server
		BeforeEach(func() {
			server = ghttp.NewServer()
		})
		AfterEach(func() {
			server.Close()
		})
		It("requests the url and returns the result", func() {
			server.AppendHandlers(
				ghttp.CombineHandlers(
					ghttp.VerifyRequest("GET", "/feed.ics", ""),
					ghttp.RespondWith(200, "hi"),
				),
			)
			feed, err := feed.Fetch(ctx, fp.Must(url.Parse(server.URL()+"/feed.ics")))
			Expect(err).ToNot(HaveOccurred())
			Expect(feed).To(And(
				HaveField("HttpStatus", 200),
				HaveField("Body", BeEquivalentTo("hi")),
				HaveField("MD5", BeEquivalentTo("49f68a5c8493ec2c0bf489821c21fc3b")),
			))
		})
		It("returns the feed in the case of an http error", func() {
			server.AppendHandlers(
				ghttp.CombineHandlers(
					ghttp.VerifyRequest("GET", "/feed.ics", ""),
					ghttp.RespondWith(403, "hi"),
				),
			)
			feed, err := feed.Fetch(ctx, fp.Must(url.Parse(server.URL()+"/feed.ics")))
			Expect(err).ToNot(HaveOccurred())
			Expect(feed).To(And(
				HaveField("HttpStatus", 403),
				HaveField("Body", BeEquivalentTo("hi")),
			))
		})
	})
})
