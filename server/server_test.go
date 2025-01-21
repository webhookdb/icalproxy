package server_test

import (
	"context"
	"github.com/labstack/echo/v4"
	"github.com/lithictech/go-aperitif/v2/api"
	. "github.com/lithictech/go-aperitif/v2/api/echoapitest"
	. "github.com/lithictech/go-aperitif/v2/apitest"
	"github.com/lithictech/go-aperitif/v2/logctx"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/ghttp"
	. "github.com/rgalanakis/golangal"
	"github.com/webhookdb/icalproxy/appglobals"
	"github.com/webhookdb/icalproxy/config"
	"github.com/webhookdb/icalproxy/db"
	"github.com/webhookdb/icalproxy/feed"
	"github.com/webhookdb/icalproxy/fp"
	"github.com/webhookdb/icalproxy/icalproxytest"
	"github.com/webhookdb/icalproxy/server"
	"github.com/webhookdb/icalproxy/types"
	"net/http"
	"net/url"
	"testing"
	"time"
)

func TestServer(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "server package Suite")
}

var _ = Describe("server", func() {
	ctx, _ := logctx.WithNullLogger(context.Background())
	var e *echo.Echo
	var ag *appglobals.AppGlobals
	var origin *ghttp.Server
	var originFeedUri *url.URL
	var originFeedUrl string
	var serverRequestUrl string

	BeforeEach(func() {
		ag = fp.Must(appglobals.New(ctx, fp.Must(config.LoadConfig())))
		Expect(icalproxytest.TruncateLocal(ctx, ag.DB)).To(Succeed())
		e = api.New(api.Config{Logger: logctx.Logger(ctx)})

		origin = ghttp.NewServer()
		originFeedUrl = origin.URL() + "/feed.ics"
		serverRequestUrl = "/?url=" + url.QueryEscape(originFeedUrl)
		originFeedUri = fp.Must(url.Parse(originFeedUrl))
	})

	AfterEach(func() {
		origin.Close()
	})

	Describe("with a configured api key", func() {
		BeforeEach(func() {
			ag.Config.ApiKey = "sekret"
			Expect(server.Register(ctx, e, ag)).To(Succeed())
		})

		It("succeeds with a valid api key header in the request", func() {
			origin.AppendHandlers(
				ghttp.CombineHandlers(
					ghttp.VerifyRequest("GET", "/feed.ics", ""),
					ghttp.RespondWith(200, "VEVENT"),
				),
			)
			req := NewRequest("GET", serverRequestUrl, nil)
			req.Header.Add("Authorization", "Apikey sekret")
			rr := Serve(e, req)
			Expect(rr).To(HaveResponseCode(200))
			Expect(rr.Body.String()).To(Equal("VEVENT"))
		})
		It("succeeds with basic auth with the api key as the password", func() {
			origin.AppendHandlers(
				ghttp.CombineHandlers(
					ghttp.VerifyRequest("GET", "/feed.ics", ""),
					ghttp.RespondWith(200, "VEVENT"),
				),
			)
			req := NewRequest("GET", serverRequestUrl, nil)
			req.SetBasicAuth("", "sekret")
			rr := Serve(e, req)
			Expect(rr).To(HaveResponseCode(200))
			Expect(rr.Body.String()).To(Equal("VEVENT"))
		})
		It("errors with a missing auth header", func() {
			req := NewRequest("GET", serverRequestUrl, nil)
			rr := Serve(e, req)
			Expect(rr).To(HaveResponseCode(401))
		})
		It("errors with an invalid auth header", func() {
			req := NewRequest("GET", serverRequestUrl, nil)
			req.Header.Add("Authorization", "Apikey badsekret")
			rr := Serve(e, req)
			Expect(rr).To(HaveResponseCode(401))
		})
	})

	Describe("GET /", func() {
		BeforeEach(func() {
			Expect(server.Register(ctx, e, ag)).To(Succeed())
		})

		It("returns 400 for a missing url", func() {
			req := NewRequest("GET", "/", nil)
			rr := Serve(e, req)
			Expect(rr).To(HaveResponseCode(400))
		})
		It("returns 400 for an invalid url", func() {
			req := NewRequest("GET", "/?url="+url.QueryEscape("https://a.co:m/x:y/z"), nil)
			rr := Serve(e, req)
			Expect(rr).To(HaveResponseCode(400))
		})
		It("returns 200 with headers, and caches the feed if it is not in the cache", func() {
			origin.AppendHandlers(
				ghttp.CombineHandlers(
					ghttp.VerifyRequest("GET", "/feed.ics", ""),
					ghttp.RespondWith(200, "VEVENT"),
				),
			)
			req := NewRequest("GET", serverRequestUrl, nil)
			rr := Serve(e, req)
			Expect(rr).To(HaveResponseCode(200))
			Expect(rr.Body.String()).To(Equal("VEVENT"))
			Expect(feed.HeadersToMap(rr.Header())).To(And(
				HaveKeyWithValue("Content-Type", "text/calendar"),
				HaveKeyWithValue("Content-Length", "6"),
				HaveKey("Last-Modified"),
				HaveKeyWithValue("Etag", "a2ec0c77b7bea23455185bcc75535bf7"),
			))

			row := fp.Must(db.New(ag.DB).FetchFeedRow(ctx, originFeedUri))
			Expect(row.ContentsMD5).To(BeEquivalentTo("a2ec0c77b7bea23455185bcc75535bf7"))
		})
		It("returns a 421 with the origin error if the fetch errors", func() {
			origin.AppendHandlers(
				ghttp.CombineHandlers(
					ghttp.VerifyRequest("GET", "/feed.ics", ""),
					ghttp.RespondWith(403, "nope", map[string][]string{"Content-Type": {"application/custom"}}),
				),
			)
			req := NewRequest("GET", serverRequestUrl, nil)
			rr := Serve(e, req)
			Expect(rr).To(HaveResponseCode(421))
			Expect(rr.Body.String()).To(Equal("nope"))
			Expect(feed.HeadersToMap(rr.Header())).To(And(
				HaveKeyWithValue("Content-Type", "application/custom"),
				HaveKeyWithValue("Ical-Proxy-Origin-Error", "403"),
			))
		})
		Describe("with a cached feed", func() {
			BeforeEach(func() {
				Expect(db.New(ag.DB).CommitFeed(ctx, feed.New(
					originFeedUri,
					make(map[string]string),
					200,
					[]byte("VEVENT"),
					time.Now(),
				), nil)).To(Succeed())
			})
			It("returns 304 if the feed has not been modified and the caller passes if-none-match headers", func() {
				req := NewRequest("GET", serverRequestUrl, nil)
				req.Header.Add("If-None-Match", "a2ec0c77b7bea23455185bcc75535bf7")
				rr := Serve(e, req)
				Expect(rr).To(HaveResponseCode(304))
			})
			It("returns 200 if the if-none-match header fails validation", func() {
				req := NewRequest("GET", serverRequestUrl, nil)
				req.Header.Add("If-None-Match", "failsmatch")
				rr := Serve(e, req)
				Expect(rr).To(HaveResponseCode(200))
			})
			It("returns 304 if the feed has not been modified and the caller passes if-modified-since header", func() {
				req := NewRequest("GET", serverRequestUrl, nil)
				req.Header.Add("If-Modified-Since", types.FormatHttpTime(time.Now()))
				rr := Serve(e, req)
				Expect(rr).To(HaveResponseCode(304))
			})
			It("returns 200 if the if-modified-since header fails validation", func() {
				req := NewRequest("GET", serverRequestUrl, nil)
				req.Header.Add("If-Modified-Since", types.FormatHttpTime(time.Now().Add(-time.Hour*20)))
				rr := Serve(e, req)
				Expect(rr).To(HaveResponseCode(200))
			})
			It("serves from cache if the feed TTL has not expired", func() {
				req := NewRequest("GET", serverRequestUrl, nil)
				rr := Serve(e, req)
				Expect(rr).To(HaveResponseCode(200))
			})
			It("fetches from origin and serves from cache if the TTL has expired", func() {
				Expect(db.New(ag.DB).CommitFeed(ctx, feed.New(
					originFeedUri,
					make(map[string]string),
					200,
					[]byte("VERSION1"),
					time.Now().Add(-5*time.Hour),
				), nil)).To(Succeed())
				origin.AppendHandlers(
					ghttp.CombineHandlers(
						ghttp.VerifyRequest("GET", "/feed.ics", ""),
						ghttp.RespondWith(200, "VERSION2"),
					),
				)
				req := NewRequest("GET", serverRequestUrl, nil)
				rr := Serve(e, req)
				Expect(rr).To(HaveResponseCode(200))
				Expect(rr.Body.String()).To(Equal("VERSION2"))

				row := fp.Must(db.New(ag.DB).FetchFeedRow(ctx, originFeedUri))
				Expect(row.ContentsMD5).To(BeEquivalentTo("e09e7582b0849d4b27f9af87ae6703ea"))
			})
			It("fetches from origin and serves if there are critical issues like DB problems", func() {
				origin.AppendHandlers(
					ghttp.CombineHandlers(
						ghttp.VerifyRequest("GET", "/feed.ics", ""),
						ghttp.RespondWith(200, "FETCHED"),
					),
				)
				ag.DB.Close() // Cause a DB error
				req := NewRequest("GET", serverRequestUrl, nil)
				rr := Serve(e, req)
				Expect(rr).To(HaveResponseCode(200))
				Expect(rr.Body.String()).To(Equal("FETCHED"))
			})
			It("returns the origin error if the cached feed was an error", func() {
				Expect(db.New(ag.DB).CommitFeed(ctx, feed.New(
					originFeedUri,
					map[string]string{"Content-Type": "application/custom"},
					403,
					[]byte("nope"),
					time.Now(),
				), nil)).To(Succeed())
				req := NewRequest("GET", serverRequestUrl, nil)
				rr := Serve(e, req)
				Expect(rr).To(HaveResponseCode(421))
				Expect(rr.Body.String()).To(Equal("nope"))
				Expect(feed.HeadersToMap(rr.Header())).To(And(
					HaveKeyWithValue("Content-Type", "application/custom"),
					HaveKeyWithValue("Ical-Proxy-Origin-Error", "403"),
				))
			})
			It("returns the cached feed if there was an expired TTL but the origin returned NotModified on fetch", func() {
				Expect(db.New(ag.DB).CommitFeed(ctx, feed.New(
					originFeedUri,
					make(map[string]string),
					200,
					[]byte("VERSION1"),
					time.Now().Add(-5*time.Hour),
				), nil)).To(Succeed())
				origin.AppendHandlers(
					ghttp.CombineHandlers(
						ghttp.VerifyRequest("GET", "/feed.ics", ""),
						ghttp.RespondWith(304, ""),
					),
				)
				req := NewRequest("GET", serverRequestUrl, nil)
				rr := Serve(e, req)
				Expect(rr).To(HaveResponseCode(200))
				Expect(rr.Body.String()).To(Equal("VERSION1"))
			})
		})
		Describe("when the database is down", func() {
			It("calls and returns from the origin", func() {
				origin.AppendHandlers(
					ghttp.CombineHandlers(
						ghttp.VerifyRequest("GET", "/feed.ics", ""),
						ghttp.RespondWith(403, "nope", map[string][]string{"Content-Type": {"application/custom"}}),
					),
				)
				ag.DB.Close()
				req := NewRequest("GET", serverRequestUrl, nil)
				rr := Serve(e, req)
				Expect(rr).To(HaveResponseCode(421))
				Expect(rr.Body.String()).To(Equal("nope"))
				Expect(feed.HeadersToMap(rr.Header())).To(And(
					HaveKeyWithValue("Content-Type", "application/custom"),
					HaveKeyWithValue("Ical-Proxy-Origin-Error", "403"),
					HaveKeyWithValue("Ical-Proxy-Fallback", "true"),
				))
			})
			It("uses a configured maximum timeout", func() {
				ag.Config.RequestMaxTimeout = 0
				origin.AppendHandlers(
					ghttp.CombineHandlers(
						ghttp.VerifyRequest("GET", "/feed.ics", ""),
						func(w http.ResponseWriter, r *http.Request) {
							time.Sleep(time.Second)
						},
					))
				ag.DB.Close()
				req := NewRequest("GET", serverRequestUrl, nil)
				rr := Serve(e, req)
				Expect(rr).To(HaveResponseCode(421))
				Expect(rr.Body.String()).To(ContainSubstring("context deadline exceeded"))
				Expect(feed.HeadersToMap(rr.Header())).To(And(
					HaveKeyWithValue("Content-Type", "text/plain"),
					HaveKeyWithValue("Ical-Proxy-Origin-Error", "599"),
					HaveKeyWithValue("Ical-Proxy-Fallback", "true"),
				))
			})
		})
		Describe("when the origin request times out", func() {
			BeforeEach(func() {
				ag.Config.RequestTimeout = 0
			})
			It("treats it like a normal error (421)", func() {
				origin.AppendHandlers(
					ghttp.CombineHandlers(
						ghttp.VerifyRequest("GET", "/feed.ics", ""),
						func(w http.ResponseWriter, req *http.Request) {
							time.Sleep(time.Second)
						},
					),
				)
				req := NewRequest("GET", serverRequestUrl, nil)
				rr := Serve(e, req)
				Expect(rr).To(HaveResponseCode(421))
				Expect(rr.Body.String()).To(HaveSuffix(`/feed.ics": context deadline exceeded`))
				Expect(feed.HeadersToMap(rr.Header())).To(And(
					HaveKeyWithValue("Content-Type", "text/plain"),
					HaveKeyWithValue("Ical-Proxy-Origin-Error", "599"),
				))
			})
		})
	})
	Describe("HEAD /", func() {
		BeforeEach(func() {
			Expect(server.Register(ctx, e, ag)).To(Succeed())
		})

		It("responds just as GET but with no body", func() {
			origin.AppendHandlers(
				ghttp.CombineHandlers(
					ghttp.VerifyRequest("GET", "/feed.ics", ""),
					ghttp.RespondWith(200, "FETCHED"),
				),
			)
			req := NewRequest("HEAD", serverRequestUrl, nil)
			rr := Serve(e, req)
			Expect(rr).To(HaveResponseCode(200))
			Expect(rr.Body.Len()).To(Equal(0))
		})
	})
	Describe("GET /stats", func() {
		BeforeEach(func() {
			Expect(server.Register(ctx, e, ag)).To(Succeed())
		})

		It("returns row latency", func() {
			req := NewRequest("GET", "/stats", nil)
			rr := Serve(e, req)
			Expect(rr).To(HaveResponseCode(200))
			Expect(MustUnmarshalFrom(rr.Body)).To(And(
				HaveKey("db_count_latency"),
				HaveKeyWithValue("pending_refresh_count", BeEquivalentTo(0)),
				HaveKeyWithValue("pending_webhooks", BeEquivalentTo(0)),
			))
		})
	})
	Describe("GET /favicon.ico", func() {
		BeforeEach(func() {
			Expect(server.Register(ctx, e, ag)).To(Succeed())
		})

		It("returns the image", func() {
			req := NewRequest("GET", "/favicon.ico", nil)
			rr := Serve(e, req)
			Expect(rr).To(HaveResponseCode(200))
			Expect(rr.Body).To(MatchLen(BeNumerically(">", 1000)))
		})
	})
})
