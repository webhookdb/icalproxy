package server_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"testing"
)

func TestServer(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "server package Suite")
}

var _ = Describe("server", func() {
	Describe("with a configured api key", func() {
		It("succeeds with a valid api key header in the request", func() {

		})
		It("errors with a missing auth header", func() {

		})
		It("errors with an invalid auth header", func() {

		})
	})

	Describe("GET /", func() {
		It("returns 422 for a missing url", func() {

		})
		It("returns 422 for an invalid url", func() {

		})
		It("returns 200 and caches the feed if it is not in the cache", func() {

		})
		Describe("with a cached feed", func() {
			It("returns 304 if the feed has not been modified and the caller passes if-none-match headers", func() {

			})
			It("returns 304 if the feed has not been modified and the caller passes if-modified-since header", func() {

			})
			It("serves from cache if the feed TTL has not expired", func() {

			})
			It("fetches from origin and serves from cache if the TTL has expired", func() {

			})
			It("fetches from origin and serves if there are critical issues like DB problems", func() {

			})
		})
	})
	Describe("HEAD /", func() {
		It("responds just as GET but with no body", func() {

		})
	})

})
