package refresher_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"testing"
)

func TestRefresher(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "refresher package Suite")
}

var _ = Describe("refresher", func() {
	Describe("StartScheduler", func() {
		It("starts a routine that can be canceled", func() {

		})
	})
	Describe("Run", func() {
		It("refreshes all feeds that need it", func() {

		})
	})
	Describe("SelectRowsToProcess", func() {
		It("selects rows that have not been checked since the TTL for their host", func() {

		})
	})
})
