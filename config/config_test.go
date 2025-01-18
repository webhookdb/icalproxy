package config_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/webhookdb/icalproxy/config"
	"github.com/webhookdb/icalproxy/types"
	"testing"
	"time"
)

func TestConfig(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "config package Suite")
}

var _ = Describe("config", func() {
	Describe("BuildTTLMap", func() {
		It("builds the ttl map as specified from the environment", func() {
			e := []string{
				"EXAMPLEORG=10m",
				"ICAL_TTL_WEBHOOKDBCOM=15m",
				"ICAL_TTL_sub.webhookdb.com=20m",
			}
			m, err := config.BuildTTLMap(e)
			Expect(err).NotTo(HaveOccurred())
			Expect(m).To(And(
				HaveKeyWithValue(types.NormalizedHostname("WEBHOOKDBCOM"), types.TTL(15*time.Minute)),
				HaveKeyWithValue(types.NormalizedHostname("SUBWEBHOOKDBCOM"), types.TTL(20*time.Minute)),
			))
		})
	})
})
