package config_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"testing"
)

func TestConfig(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "config package Suite")
}

var _ = Describe("config", func() {
	Describe("BuildTTLMap", func() {
		It("builds the ttl map as specified from the environment", func() {

		})
	})
})
