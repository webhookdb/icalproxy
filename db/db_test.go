package db_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"testing"
)

func TestDB(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "db package Suite")
}

var _ = Describe("db", func() {
	Describe("Migrate", func() {
		It("creates the table and indices if they do not exist", func() {

		})
	})
	Describe("FetchConditionalRow", func() {
		It("returns the row if it exists", func() {

		})
		It("returns nil if the row does not exist", func() {

		})
	})
	Describe("FetchContentsAsFeed", func() {
		It("returns the row", func() {

		})
		It("errors if the row does not exist", func() {

		})
	})
	Describe("CommitFeed", func() {
		It("sets fields from the passed in feed", func() {

		})
	})
})
