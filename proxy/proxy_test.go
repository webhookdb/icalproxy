package proxy_test

import (
	"context"
	"github.com/lithictech/go-aperitif/v2/logctx"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/ghttp"
	"github.com/webhookdb/icalproxy/proxy"
	"github.com/webhookdb/icalproxy/types"
	"net/url"
	"testing"
	"time"
)

func TestDB(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "proxy package Suite")
}

var _ = Describe("proxy", func() {
	ctx, _ := logctx.WithNullLogger(context.Background())

	Describe("TTLFor", func() {
		ttlmap := map[types.NormalizedHostname]types.TTL{
			"WEBHOOKDBCOM":    types.TTL(time.Minute * 15),
			"SUBWEBHOOKDBCOM": types.TTL(time.Minute * 10),
			"INFREQUENTCOM":   types.TTL(time.Hour * 20),
		}
		It("returns the ttl for a configured hostname", func() {
			Expect(proxy.TTLFor(newUrl("https://webhookdb.com/feed.ics"), ttlmap)).To(BeEquivalentTo(time.Minute * 15))
			Expect(proxy.TTLFor(newUrl("https://otherthing.webhookdb.com/feed.ics"), ttlmap)).To(BeEquivalentTo(time.Minute * 15))
		})
		It("returns the minimum TTL for all matching configured hosts", func() {
			Expect(proxy.TTLFor(newUrl("https://sub.webhookdb.com/feed.ics"), ttlmap)).To(BeEquivalentTo(time.Minute * 10))
		})
		It("returns the default ttl for no match, or a TTL higher than default", func() {
			Expect(proxy.TTLFor(newUrl("https://sub.lithic.tech/feed.ics"), ttlmap)).To(Equal(proxy.DefaultTTL))
			Expect(proxy.TTLFor(newUrl("https://infrequent.com/feed.ics"), ttlmap)).To(Equal(proxy.DefaultTTL))
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
			feed, err := proxy.Fetch(ctx, newUrl(server.URL()+"/feed.ics"))
			Expect(err).ToNot(HaveOccurred())
			Expect(feed.Body).To(BeEquivalentTo("hi"))
			Expect(feed.MD5).To(BeEquivalentTo("49f68a5c8493ec2c0bf489821c21fc3b"))
		})
		It("errors for a non-2xx response", func() {
			server.AppendHandlers(
				ghttp.CombineHandlers(
					ghttp.VerifyRequest("GET", "/feed.ics", ""),
					ghttp.RespondWith(403, "hi"),
				),
			)
			_, err := proxy.Fetch(ctx, newUrl(server.URL()+"/feed.ics"))
			Expect(err).To(BeAssignableToTypeOf(&proxy.OriginError{}))
			Expect(err.(*proxy.OriginError)).To(And(
				HaveField("StatusCode", 403),
				HaveField("Body", BeEquivalentTo("hi")),
			))
		})
	})
})

func newUrl(s string) *url.URL {
	u, err := url.Parse(s)
	Expect(err).ToNot(HaveOccurred())
	return u
}
