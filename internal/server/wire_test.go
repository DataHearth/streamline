package server

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/internal/testutil/configtest"
)

var _ = Describe("apiFailureLimiter", Label("unit", "server", "auth"), func() {
	// spend records n failures for one address and reports whether the next
	// one is still allowed — the question the middleware asks.
	spend := func(n int) bool {
		GinkgoHelper()
		l := apiFailureLimiter()
		if l == nil {
			return true
		}
		for range n {
			l.Allow("203.0.113.5")
		}
		ok, _ := l.Allow("203.0.113.5")
		return ok
	}

	It("meters at 20 by default", func() {
		configtest.Setup(nil)
		Expect(spend(19)).To(BeTrue())
		Expect(spend(20)).To(BeFalse())
	})

	It("honours the hidden override", func() {
		configtest.Setup(map[string]any{
			"auth": map[string]any{"api_failure_limit": "2"},
		})
		Expect(spend(1)).To(BeTrue())
		Expect(spend(2)).To(BeFalse())
	})

	It("meters nothing at 0", func() {
		configtest.Setup(map[string]any{
			"auth": map[string]any{"api_failure_limit": "0"},
		})
		Expect(apiFailureLimiter()).To(BeNil())
	})

	It("keeps the default when the override will not parse", func() {
		configtest.Setup(map[string]any{
			"auth": map[string]any{"api_failure_limit": "lots"},
		})
		Expect(spend(20)).To(BeFalse())
	})
})
