package feed_test

import (
	"context"
	"crypto/x509"
	"errors"
	"fmt"
	"github.com/lithictech/go-aperitif/v2/logctx"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/ghttp"
	"github.com/webhookdb/icalproxy/feed"
	"github.com/webhookdb/icalproxy/fp"
	"github.com/webhookdb/icalproxy/types"
	"net/http"
	"net/url"
	"testing"
	"time"
)

func TestFeed(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "feed package Suite")
}

var _ = Describe("feed", func() {
	var ctx context.Context

	BeforeEach(func() {
		ctx, _ = logctx.WithNullLogger(context.Background())
	})

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
					ghttp.VerifyHeaderKV("Accept", "text/calendar,*/*"),
					ghttp.RespondWith(200, "hi"),
				))
			feed, err := feed.Fetch(ctx, fp.Must(url.Parse(server.URL()+"/feed.ics")), nil)
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
			feed, err := feed.Fetch(ctx, fp.Must(url.Parse(server.URL()+"/feed.ics")), nil)
			Expect(err).ToNot(HaveOccurred())
			Expect(feed).To(And(
				HaveField("HttpStatus", 403),
				HaveField("Body", BeEquivalentTo("hi")),
			))
		})
		It("returns the feed in the case of a url error (http timeout, etc)", func() {
			server.AppendHandlers(
				ghttp.CombineHandlers(
					ghttp.VerifyRequest("GET", "/feed.ics", ""),
					func(w http.ResponseWriter, r *http.Request) {
						time.Sleep(1 * time.Second)
						w.WriteHeader(500)
					},
				),
			)
			timeoutCtx, cancel := context.WithTimeout(ctx, 0)
			defer cancel()
			feed, err := feed.Fetch(timeoutCtx, fp.Must(url.Parse(server.URL()+"/feed.ics")), nil)
			Expect(err).ToNot(HaveOccurred())
			Expect(feed).To(And(
				HaveField("HttpStatus", 599),
				HaveField("Body", ContainSubstring("context deadline exceeded")),
			))
		})
		It("returns the feed in the case of a certificate error", func() {
			certErr := x509.SystemRootsError{Err: errors.New("bad cert")}
			Expect(feed.WithHttpClient(&erroringHttpClient{Err: certErr}, func() error {
				feed, err := feed.Fetch(ctx, fp.Must(url.Parse(server.URL()+"/feed.ics")), nil)
				Expect(err).ToNot(HaveOccurred())
				Expect(feed).To(And(
					HaveField("HttpStatus", 599),
					HaveField("Body", ContainSubstring("failed to load system roots")),
				))
				return nil
			})).To(Succeed())

			wrappedErr := fmt.Errorf("wrapped: %w", certErr)
			Expect(feed.WithHttpClient(&erroringHttpClient{Err: wrappedErr}, func() error {
				feed, err := feed.Fetch(ctx, fp.Must(url.Parse(server.URL()+"/feed.ics")), nil)
				Expect(err).ToNot(HaveOccurred())
				Expect(feed).To(And(
					HaveField("HttpStatus", 599),
					HaveField("Body", ContainSubstring("failed to load system roots")),
				))
				return nil
			})).To(Succeed())
		})
		It("returns the feed as an error in the case of an error or timeout reading the body of a success", func() {
			cancelCtx, cancel := context.WithCancel(ctx)
			server.AppendHandlers(
				ghttp.CombineHandlers(
					ghttp.VerifyRequest("GET", "/feed.ics", ""),
					func(w http.ResponseWriter, r *http.Request) {
						// Tell the server the body is coming
						w.WriteHeader(200)
						// We need to cancel the context after the body starts getting read;
						// if we cancel it immediately we'll time out the GET itself so won't test the body read.
						// So start writing the body, enough to fill buffers, then cancel the context.
						for i := 0; i < 1000_000; i++ {
							_, _ = w.Write([]byte("1"))
						}
						cancel()
					},
				))
			feed, err := feed.Fetch(cancelCtx, fp.Must(url.Parse(server.URL()+"/feed.ics")), nil)
			Expect(err).ToNot(HaveOccurred())
			Expect(feed).To(And(
				HaveField("HttpStatus", 599),
				HaveField("Body", ContainSubstring("error reading body: context canceled")),
			))
		})
		It("returns the feed error in the case of an error or timeout reading the body of an error", func() {
			cancelCtx, cancel := context.WithCancel(ctx)
			server.AppendHandlers(
				ghttp.CombineHandlers(
					ghttp.VerifyRequest("GET", "/feed.ics", ""),
					func(w http.ResponseWriter, r *http.Request) {
						// See previous test for explanation, this uses an error status code to check we don't lose it.
						w.WriteHeader(400)
						for i := 0; i < 1000_000; i++ {
							_, _ = w.Write([]byte("1"))
						}
						cancel()
					},
				))
			feed, err := feed.Fetch(cancelCtx, fp.Must(url.Parse(server.URL()+"/feed.ics")), nil)
			Expect(err).ToNot(HaveOccurred())
			Expect(feed).To(And(
				HaveField("HttpStatus", 400),
				HaveField("Body", ContainSubstring("error reading body: context canceled")),
			))
		})
		Describe("with previous fetch http headers", func() {
			// http.Format ignores TZ so make sure we force UTC
			nowFmt := time.Now().UTC().Format(http.TimeFormat)

			It("passes If-None-Match if an Etag is present", func() {
				server.AppendHandlers(
					ghttp.CombineHandlers(
						ghttp.VerifyRequest("GET", "/feed.ics", ""),
						ghttp.VerifyHeaderKV("If-None-Match", `"abcd"`),
						ghttp.RespondWith(200, "hi"),
					))
				feed, err := feed.Fetch(ctx, fp.Must(url.Parse(server.URL()+"/feed.ics")), map[string]string{"Etag": `"abcd"`})
				Expect(err).ToNot(HaveOccurred())
				Expect(feed).To(HaveField("HttpStatus", 200))
			})
			It("passes If-Modified-Since if a Last-Modified is present", func() {
				server.AppendHandlers(
					ghttp.CombineHandlers(
						ghttp.VerifyRequest("GET", "/feed.ics", ""),
						ghttp.VerifyHeaderKV("If-Modified-Since", `Tue, 22 Feb 2022 22:00:00 GMT`),
						ghttp.RespondWith(200, "hi"),
					))
				feed, err := feed.Fetch(ctx, fp.Must(url.Parse(server.URL()+"/feed.ics")), map[string]string{"Last-Modified": `Tue, 22 Feb 2022 22:00:00 GMT`})
				Expect(err).ToNot(HaveOccurred())
				Expect(feed).To(HaveField("HttpStatus", 200))
			})
			It("returns a NotModified error and the feed on 304", func() {
				server.AppendHandlers(
					ghttp.CombineHandlers(
						ghttp.VerifyRequest("GET", "/feed.ics", ""),
						ghttp.RespondWith(304, ""),
					))
				fd, err := feed.Fetch(ctx, fp.Must(url.Parse(server.URL()+"/feed.ics")), map[string]string{"Etag": `xyz`})
				Expect(err).To(BeIdenticalTo(feed.ErrNotModified))
				Expect(fd).To(HaveField("FetchedAt", BeTemporally("~", time.Now(), time.Minute)))
			})
			Describe("with a Date and Cache-Control header", func() {
				It("makes the request if more than max-age has elapsed since the last request date", func() {
					server.AppendHandlers(
						ghttp.CombineHandlers(
							ghttp.VerifyRequest("GET", "/feed.ics", ""),
							ghttp.RespondWith(200, "hi"),
						))
					feed, err := feed.Fetch(ctx, fp.Must(url.Parse(server.URL()+"/feed.ics")), map[string]string{
						"Date":          nowFmt,
						"Cache-Control": `max-age=0`,
					})
					Expect(err).ToNot(HaveOccurred())
					Expect(feed).To(HaveField("HttpStatus", 200))
				})
				It("makes the request if the Date header is invalid", func() {
					server.AppendHandlers(
						ghttp.CombineHandlers(
							ghttp.VerifyRequest("GET", "/feed.ics", ""),
							ghttp.RespondWith(200, "hi"),
						))
					feed, err := feed.Fetch(ctx, fp.Must(url.Parse(server.URL()+"/feed.ics")), map[string]string{
						"Date":          "not valid",
						"Cache-Control": "max-age=1000",
					})
					Expect(err).ToNot(HaveOccurred())
					Expect(feed).To(HaveField("HttpStatus", 200))
				})
				It("makes the request if the Cache-Control header is invalid", func() {
					server.AppendHandlers(
						ghttp.CombineHandlers(
							ghttp.VerifyRequest("GET", "/feed.ics", ""),
							ghttp.RespondWith(200, "hi"),
						))
					feed, err := feed.Fetch(ctx, fp.Must(url.Parse(server.URL()+"/feed.ics")), map[string]string{
						"Date":          nowFmt,
						"Cache-Control": "not sure what is up here",
					})
					Expect(err).ToNot(HaveOccurred())
					Expect(feed).To(HaveField("HttpStatus", 200))
				})
				It("returns NotModified if the last request date plus max-age is after now", func() {
					fd, err := feed.Fetch(ctx, fp.Must(url.Parse(server.URL()+"/feed.ics")), map[string]string{
						"Date":          nowFmt,
						"Cache-Control": `max-age=100`,
					})
					Expect(err).To(BeIdenticalTo(feed.ErrNotModified))
					Expect(fd).To(HaveField("FetchedAt", BeTemporally("~", time.Now(), time.Minute)))
				})
				It("uses a predefined max-age to avoid servers that give bad values", func() {
					server.AppendHandlers(
						ghttp.CombineHandlers(
							ghttp.VerifyRequest("GET", "/feed.ics", ""),
							ghttp.RespondWith(200, "hi"),
						))
					feed, err := feed.Fetch(ctx, fp.Must(url.Parse(server.URL()+"/feed.ics")), map[string]string{
						"Date":          time.Now().Add(-25 * time.Hour).UTC().Format(http.TimeFormat),
						"Cache-Control": `max-age=9999999999999`,
					})
					Expect(err).ToNot(HaveOccurred())
					Expect(feed).To(HaveField("HttpStatus", 200))
				})
			})
		})
	})
})

type erroringHttpClient struct {
	Err error
}

func (e *erroringHttpClient) Do(*http.Request) (*http.Response, error) {
	return nil, e.Err
}
