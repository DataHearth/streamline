package httputil

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/internal/testutil/configtest"
)

// forwardedTrustedCIDR is the range these specs put in server.trusted_proxies;
// the default RemoteAddr httptest.NewRequest hands out sits inside it, and
// forwardedUntrustedPeer sits outside.
const (
	forwardedTrustedCIDR   = "192.0.2.0/24"
	forwardedUntrustedPeer = "198.51.100.7:5555"
)

var _ = Describe("ForwardedProto", Label("unit"), func() {
	// trustProxies configures a non-empty server.trusted_proxies. The
	// untrusted-peer specs need this as much as the trusted ones: left at the
	// empty default, TrustedPeer returns false from its len == 0 early return
	// and the range check below it never runs, so a regression that trusted
	// every peer once any proxy was configured would stay invisible.
	trustProxies := func() {
		GinkgoHelper()
		configtest.Setup(map[string]any{
			"server": map[string]any{
				"trusted_proxies": []string{forwardedTrustedCIDR},
			},
		})
	}

	request := func(headers map[string]string) *http.Request {
		r := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
		for k, v := range headers {
			r.Header.Set(k, v)
		}
		return r
	}

	BeforeEach(func() { configtest.Setup() })

	// Every spelling of "the browser spoke https to me" that a mainstream
	// proxy emits. An exact match on X-Forwarded-Proto reads only the first.
	DescribeTable("reads https off a trusted proxy",
		func(header, value string) {
			trustProxies()

			Expect(ForwardedProto(request(map[string]string{
				header: value,
			}))).To(Equal("https"))
		},
		Entry("X-Forwarded-Proto", "X-Forwarded-Proto", "https"),
		Entry("uppercased scheme", "X-Forwarded-Proto", "HTTPS"),
		Entry("padded value", "X-Forwarded-Proto", " https "),
		Entry("appended chain", "X-Forwarded-Proto", "https,http"),
		Entry("X-Forwarded-Ssl", "X-Forwarded-Ssl", "on"),
		Entry("RFC 7239 Forwarded", "Forwarded", "for=192.0.2.7;proto=https"),
		Entry("quoted RFC 7239 proto", "Forwarded", `proto="https";by=lb`),
	)

	// RFC 7239 is the standard and settles the scheme on its own, so a stale
	// X-Forwarded-* left by an earlier hop must not override it.
	It("lets Forwarded: proto=http win over a stale X-Forwarded-Proto", func() {
		trustProxies()

		Expect(ForwardedProto(request(map[string]string{
			"Forwarded":         "proto=http",
			"X-Forwarded-Proto": "https",
		}))).To(Equal("http"))
	})

	It("takes the leftmost hop of an appended chain", func() {
		trustProxies()

		Expect(ForwardedProto(request(map[string]string{
			"X-Forwarded-Proto": "http,https",
		}))).To(Equal("http"))
	})

	DescribeTable("ignores every spelling from an untrusted peer",
		func(header, value string) {
			trustProxies()
			r := request(map[string]string{header: value})
			r.RemoteAddr = forwardedUntrustedPeer

			Expect(ForwardedProto(r)).To(BeEmpty())
		},
		Entry("X-Forwarded-Proto", "X-Forwarded-Proto", "https"),
		Entry("uppercased scheme", "X-Forwarded-Proto", "HTTPS"),
		Entry("appended chain", "X-Forwarded-Proto", "https,http"),
		Entry("X-Forwarded-Ssl", "X-Forwarded-Ssl", "on"),
		Entry("Forwarded", "Forwarded", "proto=https"),
	)

	It("ignores them when no proxy is configured to be trusted", func() {
		Expect(ForwardedProto(request(map[string]string{
			"X-Forwarded-Proto": "https",
			"X-Forwarded-Ssl":   "on",
			"Forwarded":         "proto=https",
		}))).To(BeEmpty())
	})

	It("reports nothing when a trusted proxy forwards no scheme", func() {
		trustProxies()

		Expect(ForwardedProto(request(nil))).To(BeEmpty())
	})
})

var _ = Describe("ServedOverTLS", Label("unit"), func() {
	BeforeEach(func() { configtest.Setup() })

	It("is true when the request itself arrived over TLS", func() {
		r := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
		r.RemoteAddr = forwardedUntrustedPeer
		r.TLS = &tls.ConnectionState{}
		r.Header.Set("X-Forwarded-Proto", "http")

		Expect(ServedOverTLS(r)).To(BeTrue())
	})

	It("is true for a trusted proxy reporting X-Forwarded-Ssl: on", func() {
		configtest.Setup(map[string]any{
			"server": map[string]any{
				"trusted_proxies": []string{forwardedTrustedCIDR},
			},
		})
		r := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
		r.Header.Set("X-Forwarded-Ssl", "on")

		Expect(ServedOverTLS(r)).To(BeTrue())
	})

	It("is false on a plain http request with no proxy configured", func() {
		r := httptest.NewRequest(http.MethodGet, "/dashboard", nil)

		Expect(ServedOverTLS(r)).To(BeFalse())
	})

	// The public URL is the session cookie's fallback, not this helper's: HSTS
	// must never pin a host for a year off configuration.
	It("ignores a configured https public URL", func() {
		GinkgoT().Setenv("STREAMLINE_PUBLIC_URL", "https://media.example")
		r := httptest.NewRequest(http.MethodGet, "/dashboard", nil)

		Expect(ServedOverTLS(r)).To(BeFalse())
	})
})
