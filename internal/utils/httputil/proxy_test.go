package httputil

import (
	"net/http"
	"net/http/httptest"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/internal/testutil/configtest"
)

var _ = Describe("ClientIPResolver", Label("unit"), func() {
	// resolve runs one request through the resolver and reports the client IP
	// the rest of the chain would see.
	resolve := func(remoteAddr string, xff ...string) string {
		GinkgoHelper()
		var seen string
		h := ClientIPResolver()(
			http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
				seen = ClientIPString(r)
			}),
		)
		req := httptest.NewRequest("GET", "/x", nil)
		req.RemoteAddr = remoteAddr
		for _, v := range xff {
			req.Header.Add("X-Forwarded-For", v)
		}
		h.ServeHTTP(httptest.NewRecorder(), req)
		return seen
	}

	trustProxies := func(cidrs ...string) {
		GinkgoHelper()
		configtest.Setup(map[string]any{
			"server": map[string]any{"trusted_proxies": cidrs},
		})
	}

	Context("with no trusted proxies configured", func() {
		BeforeEach(func() { trustProxies() })

		It("uses the peer address", func() {
			Expect(resolve("203.0.113.9:1234")).To(Equal("203.0.113.9"))
		})

		It("ignores X-Forwarded-For from a direct client", func() {
			Expect(resolve("203.0.113.9:1234", "10.0.0.1")).
				To(Equal("203.0.113.9"))
		})
	})

	Context("with a trusted proxy in front", func() {
		BeforeEach(func() { trustProxies("10.1.0.0/16") })

		It("takes the forwarded address for a trusted peer", func() {
			Expect(resolve("10.1.0.5:9000", "203.0.113.9")).
				To(Equal("203.0.113.9"))
		})

		It("still ignores the header from an untrusted peer", func() {
			Expect(resolve("203.0.113.9:1234", "127.0.0.1")).
				To(Equal("203.0.113.9"))
		})

		It("skips trusted-proxy hops and takes the rightmost client entry", func() {
			Expect(resolve("10.1.0.5:9000", "1.2.3.4, 203.0.113.9, 10.1.0.6")).
				To(Equal("203.0.113.9"))
		})

		It("merges repeated headers right to left", func() {
			Expect(resolve("10.1.0.5:9000", "1.2.3.4", "203.0.113.9")).
				To(Equal("203.0.113.9"))
		})

		It("falls back to the peer when the chain holds only proxies", func() {
			Expect(resolve("10.1.0.5:9000", "10.1.0.6")).To(Equal("10.1.0.5"))
		})

		It("falls back to the peer when the header is absent", func() {
			Expect(resolve("10.1.0.5:9000")).To(Equal("10.1.0.5"))
		})

		It("fails closed on an unparseable entry", func() {
			Expect(resolve("10.1.0.5:9000", "203.0.113.9, not-an-ip")).
				To(Equal("10.1.0.5"))
		})

		It("cannot be aliased past the trust check by v4-mapped notation", func() {
			Expect(resolve("10.1.0.5:9000", "203.0.113.9, ::ffff:10.1.0.6")).
				To(Equal("203.0.113.9"))
		})
	})

	Context("with an IPv6 trusted proxy", func() {
		BeforeEach(func() { trustProxies("2001:db8::/32") })

		It("takes the forwarded address for a trusted peer", func() {
			Expect(resolve("[2001:db8::1]:9000", "203.0.113.9")).
				To(Equal("203.0.113.9"))
		})
	})
})

var _ = Describe("TrustedPeer", Label("unit"), func() {
	request := func(remoteAddr string) *http.Request {
		req := httptest.NewRequest("GET", "/x", nil)
		req.RemoteAddr = remoteAddr
		return req
	}

	It("is false when nothing is configured", func() {
		configtest.Setup()
		Expect(TrustedPeer(request("10.1.0.5:9000"))).To(BeFalse())
	})

	It("is true only for peers inside a configured range", func() {
		configtest.Setup(map[string]any{
			"server": map[string]any{
				"trusted_proxies": []string{"10.1.0.0/16"},
			},
		})
		Expect(TrustedPeer(request("10.1.0.5:9000"))).To(BeTrue())
		Expect(TrustedPeer(request("10.2.0.5:9000"))).To(BeFalse())
	})
})
