package middleware

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/internal/metadata"
	"github.com/datahearth/streamline/internal/testutil/configtest"
)

// trustedProxyCIDR is the range specs put in server.trusted_proxies; the serve
// helper's default peer sits inside it and untrustedPeer sits outside.
const (
	trustedProxyCIDR = "203.0.113.0/24"
	untrustedPeer    = "198.51.100.7:5555"
)

// directiveSources returns the source list of one CSP directive, or fails the
// spec when the policy carries no such directive.
func directiveSources(csp, name string) []string {
	GinkgoHelper()
	for directive := range strings.SplitSeq(csp, ";") {
		fields := strings.Fields(directive)
		if len(fields) > 0 && fields[0] == name {
			return fields[1:]
		}
	}
	Fail("no " + name + " directive in: " + csp)
	return nil
}

// originOf reduces a metadata artwork URL to the scheme://host form a CSP
// source expression names it by.
func originOf(raw string) string {
	GinkgoHelper()
	u, err := url.Parse(raw)
	Expect(err).ToNot(HaveOccurred())
	Expect(u.Host).ToNot(BeEmpty(), raw)
	return u.Scheme + "://" + u.Host
}

var _ = Describe("SecurityHeaders middleware", Label("unit", "server"), func() {
	// serve runs one request through the middleware and hands back the
	// response, so each spec asserts on headers a real handler produced.
	serve := func(mutate func(*http.Request)) *httptest.ResponseRecorder {
		GinkgoHelper()
		h := SecurityHeaders(
			http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			}),
		)
		req := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
		req.RemoteAddr = "203.0.113.9:5555"
		if mutate != nil {
			mutate(req)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}

	// trustProxies configures a non-empty server.trusted_proxies. Specs about
	// an *untrusted* peer need this as much as the trusted ones do: left at
	// the empty default, TrustedPeer returns false from its len == 0 early
	// return and the range check below it never runs.
	trustProxies := func() {
		GinkgoHelper()
		configtest.Setup(map[string]any{
			"server": map[string]any{
				"trusted_proxies": []string{trustedProxyCIDR},
			},
		})
	}

	BeforeEach(func() { configtest.Setup() })

	Describe("Content-Security-Policy", func() {
		It("frames the app out and confines it to same-origin sources", func() {
			csp := serve(nil).Header().Get("Content-Security-Policy")

			Expect(csp).To(SatisfyAll(
				ContainSubstring("default-src 'self'"),
				ContainSubstring("frame-ancestors 'none'"),
				ContainSubstring("object-src 'none'"),
				ContainSubstring("base-uri 'self'"),
				ContainSubstring("form-action 'self'"),
				ContainSubstring("connect-src 'self'"),
			))
		})

		It("keeps script-src free of inline and eval escapes", func() {
			csp := serve(nil).Header().Get("Content-Security-Policy")

			Expect(csp).To(ContainSubstring("script-src 'self'"))
			Expect(csp).ToNot(ContainSubstring("script-src 'self' 'unsafe"))
			Expect(csp).ToNot(ContainSubstring("'unsafe-eval'"))
		})

		// The one concession the SPA and the Scalar docs bundle both force.
		// Pinned so widening it to scripts has to be a deliberate edit rather
		// than a passing thought.
		It("allows inline styles and no wildcard or plain-http source", func() {
			csp := serve(nil).Header().Get("Content-Security-Policy")

			Expect(csp).To(ContainSubstring("style-src 'self' 'unsafe-inline'"))
			Expect(csp).ToNot(ContainSubstring("http://"))
			Expect(csp).ToNot(ContainSubstring("*"))
		})

		// The SPA renders TMDB and TVDB artwork off those hosts directly, so
		// img-src 'self' alone blanks every result in the add-media pickers —
		// silently, behind the onerror placeholder.
		//
		// The expected hosts are derived from internal/metadata's own URL
		// builders rather than spelled out a second time. metadataImageCDNs is
		// a hand-copy of two string literals owned by another package, and
		// nothing else pins the two together: a provider that starts serving
		// artwork off a further CDN would have every image from it blocked,
		// with the failure visible only in a browser console. Deriving them
		// turns that into a red spec here.
		It("allows exactly the hosts metadata builds artwork URLs on", func() {
			csp := serve(nil).Header().Get("Content-Security-Policy")

			Expect(directiveSources(csp, "img-src")).To(ConsistOf(
				"'self'",
				originOf(metadata.PosterURL("/poster.jpg", "w500")),
				originOf(metadata.TVDBArtworkURL("/banners/series.jpg")),
			))
		})

		// Confining the hosts to img-src is the other half: neither may leak
		// into script-src, connect-src or any other directive.
		It("carries the artwork CDNs in no other directive", func() {
			csp := serve(nil).Header().Get("Content-Security-Policy")

			for _, host := range []string{
				"https://image.tmdb.org", "https://artworks.thetvdb.com",
			} {
				var carrying []string
				for directive := range strings.SplitSeq(csp, ";") {
					if strings.Contains(directive, host) {
						carrying = append(
							carrying, strings.Fields(directive)[0],
						)
					}
				}
				Expect(carrying).To(
					Equal([]string{"img-src"}),
					"%s must appear in img-src and nowhere else", host,
				)
			}
		})
	})

	It("sets the clickjacking, sniffing and referrer headers", func() {
		got := serve(nil).Header()

		Expect(got.Get("X-Frame-Options")).To(Equal("DENY"))
		Expect(got.Get("X-Content-Type-Options")).To(Equal("nosniff"))
		Expect(got.Get("Referrer-Policy")).To(Equal("same-origin"))
	})

	It("stamps the headers on a response the handler never wrote", func() {
		h := SecurityHeaders(
			http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				http.Redirect(w, r, "/login", http.StatusFound)
			}),
		)
		req := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)

		Expect(rec.Code).To(Equal(http.StatusFound))
		Expect(rec.Header().Get("Content-Security-Policy")).ToNot(BeEmpty())
		Expect(rec.Header().Get("X-Frame-Options")).To(Equal("DENY"))
	})

	Describe("Strict-Transport-Security", func() {
		It("is absent on plain http, so a LAN install is never stranded", func() {
			Expect(
				serve(nil).Header().Get("Strict-Transport-Security"),
			).To(BeEmpty())
		})

		It("is sent when the request itself arrived over TLS", func() {
			got := serve(func(r *http.Request) {
				r.TLS = &tls.ConnectionState{}
			})

			Expect(got.Header().Get("Strict-Transport-Security")).
				To(Equal("max-age=31536000"))
		})

		// Every spelling of "the browser spoke https to me" that a mainstream
		// proxy emits. An exact match on X-Forwarded-Proto reads only the
		// first, so the rest would silently leave HSTS off forever.
		DescribeTable("is sent when a trusted proxy reports it terminated TLS",
			func(header, value string) {
				trustProxies()

				got := serve(func(r *http.Request) {
					r.Header.Set(header, value)
				})

				Expect(got.Header().Get("Strict-Transport-Security")).
					To(Equal("max-age=31536000"))
			},
			Entry("X-Forwarded-Proto", "X-Forwarded-Proto", "https"),
			Entry("uppercased scheme", "X-Forwarded-Proto", "HTTPS"),
			Entry("padded value", "X-Forwarded-Proto", " https "),
			Entry("appended chain", "X-Forwarded-Proto", "https,http"),
			Entry("X-Forwarded-Ssl", "X-Forwarded-Ssl", "on"),
			Entry("RFC 7239 Forwarded", "Forwarded", "for=192.0.2.7;proto=https"),
			Entry("quoted RFC 7239 proto", "Forwarded", `proto="https";by=lb`),
		)

		// RFC 7239 is the standard and settles the scheme on its own. Letting a
		// stale X-Forwarded-* override it would mean the least current signal
		// gets to arm a year-long pin.
		It("lets Forwarded: proto=http win over a stale X-Forwarded-Proto", func() {
			trustProxies()

			got := serve(func(r *http.Request) {
				r.Header.Set("Forwarded", "proto=http")
				r.Header.Set("X-Forwarded-Proto", "https")
			})

			Expect(
				got.Header().Get("Strict-Transport-Security"),
			).To(BeEmpty())
		})

		// Off a trusted proxy the headers are attacker-supplied, and honouring
		// them would let any client pin the host to https for a year.
		//
		// The peer sits outside a *configured, non-empty* trusted_proxies on
		// purpose, because that is the only arrangement that reaches the
		// range check. Leaving the list at its empty default sends
		// TrustedPeer home from its len == 0 early return, and a regression
		// that trusted every peer the moment any proxy was configured would
		// keep the whole table green.
		DescribeTable("ignores forwarded scheme headers from an untrusted peer",
			func(header, value string) {
				trustProxies()

				got := serve(func(r *http.Request) {
					r.RemoteAddr = untrustedPeer
					r.Header.Set(header, value)
				})

				Expect(
					got.Header().Get("Strict-Transport-Security"),
				).To(BeEmpty())
			},
			Entry("X-Forwarded-Proto", "X-Forwarded-Proto", "https"),
			Entry("uppercased scheme", "X-Forwarded-Proto", "HTTPS"),
			Entry("appended chain", "X-Forwarded-Proto", "https,http"),
			Entry("X-Forwarded-Ssl", "X-Forwarded-Ssl", "on"),
			Entry("Forwarded", "Forwarded", "proto=https"),
		)

		// The default deployment: no proxy is trusted at all, so the same
		// headers are ignored one branch earlier, before any peer address is
		// even parsed.
		It("ignores them when no proxy is configured to be trusted", func() {
			got := serve(func(r *http.Request) {
				r.Header.Set("X-Forwarded-Proto", "https")
			})

			Expect(
				got.Header().Get("Strict-Transport-Security"),
			).To(BeEmpty())
		})

		It("does not claim subdomains or ask for preload", func() {
			got := serve(func(r *http.Request) {
				r.TLS = &tls.ConnectionState{}
			})
			hsts := got.Header().Get("Strict-Transport-Security")

			Expect(hsts).ToNot(ContainSubstring("includeSubDomains"))
			Expect(hsts).ToNot(ContainSubstring("preload"))
		})
	})
})
