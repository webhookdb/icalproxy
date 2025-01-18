package types_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/webhookdb/icalproxy/types"
	"testing"
)

func TestTypes(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "types package Suite")
}

var _ = Describe("types", func() {
	Describe("NormalizedHostname", func() {
		Describe("Reverse", func() {
			It("reverses the hostname", func() {
				Expect(types.NormalizedHostname("abc.com").Reverse()).To(BeEquivalentTo("moc.cba"))
			})
		})
	})
})
