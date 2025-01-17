package proxy_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"testing"
)

func TestDB(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "proxy package Suite")
}

var _ = Describe("proxy", func() {
	Describe("TTLFor", func() {
		It("returns the ttl for a configured hostname", func() {

		})
		It("returns the ttl for a configured more rooted hostname", func() {

		})
		It("returns the default ttl for no match", func() {

		})
	})

	Describe("Fetch", func() {
		It("requests the url and returns the result", func() {

		})
		It("errors for a non-2xx response", func() {

		})
	})
})
