package auth

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/internal/testutil/configtest"
)

var _ = Describe("Cookie", Label("unit", "auth"), func() {
	It("SetSession emits httpOnly, SameSite=Lax, Secure when TLS", func() {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/", nil)
		r.TLS = &tls.ConnectionState{}

		SetSession(w, r, "token", time.Hour)

		got := w.Result().Cookies()
		Expect(got).To(HaveLen(1))
		c := got[0]
		Expect(c.Name).To(Equal("streamline_session"))
		Expect(c.Value).To(Equal("token"))
		Expect(c.HttpOnly).To(BeTrue())
		Expect(c.SameSite).To(Equal(http.SameSiteLaxMode))
		Expect(c.Secure).To(BeTrue())
		Expect(c.MaxAge).To(Equal(3600))
	})

	It("SetSession honors X-Forwarded-Proto=https from a trusted proxy", func() {
		configtest.Setup(map[string]any{
			"server": map[string]any{"trusted_proxies": []string{"192.0.2.1/32"}},
		})
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/", nil) // RemoteAddr 192.0.2.1:1234
		r.Header.Set("X-Forwarded-Proto", "https")

		SetSession(w, r, "token", time.Hour)
		Expect(w.Result().Cookies()[0].Secure).To(BeTrue())
	})

	// Every spelling of "the browser spoke https to me" a mainstream proxy
	// emits. Each entry used to leave the session JWT without Secure, because
	// this path exact-matched X-Forwarded-Proto while HSTS understood the lot.
	// No public https URL is configured, so nothing else can mask a regression.
	DescribeTable("SetSession marks Secure for every forwarded TLS spelling",
		func(header, value string) {
			configtest.Setup(map[string]any{
				"server": map[string]any{
					"trusted_proxies": []string{"192.0.2.1/32"},
				},
			})
			w := httptest.NewRecorder()
			r := httptest.NewRequest("GET", "/", nil)
			r.Header.Set(header, value)

			SetSession(w, r, "token", time.Hour)
			Expect(w.Result().Cookies()[0].Secure).To(BeTrue())
		},
		Entry("X-Forwarded-Ssl", "X-Forwarded-Ssl", "on"),
		Entry("RFC 7239 Forwarded", "Forwarded", "proto=https"),
		Entry("quoted RFC 7239 proto", "Forwarded", `proto="https";by=lb`),
		Entry("uppercased scheme", "X-Forwarded-Proto", "HTTPS"),
		Entry("appended chain", "X-Forwarded-Proto", "https,http"),
	)

	// The one behavior that moved in the fail-closed direction: the scheme is
	// now read first-signal-wins, as HSTS already read it, so the standard
	// header settles it even when a stale X-Forwarded-Proto disagrees.
	It(
		"SetSession lets Forwarded: proto=http beat a stale X-Forwarded-Proto",
		func() {
			configtest.Setup(map[string]any{
				"server": map[string]any{
					"trusted_proxies": []string{"192.0.2.1/32"},
				},
			})
			w := httptest.NewRecorder()
			r := httptest.NewRequest("GET", "/", nil)
			r.Header.Set("Forwarded", "proto=http")
			r.Header.Set("X-Forwarded-Proto", "https")

			SetSession(w, r, "token", time.Hour)
			Expect(w.Result().Cookies()[0].Secure).To(BeFalse())
		},
	)

	It("SetSession ignores X-Forwarded-Proto from an untrusted peer", func() {
		// Off a configured proxy the header is attacker-supplied, so it must
		// not be able to flip the flag either way.
		configtest.Setup()
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/", nil)
		r.Header.Set("X-Forwarded-Proto", "https")

		SetSession(w, r, "token", time.Hour)
		Expect(w.Result().Cookies()[0].Secure).To(BeFalse())
	})

	It("SetSession marks Secure when a public https URL is configured", func() {
		// The case that actually bites: a TLS-terminating proxy that forwards
		// no X-Forwarded-Proto at all would otherwise emit the JWT unprotected.
		configtest.Setup()
		GinkgoT().Setenv("STREAMLINE_PUBLIC_URL", "https://media.example")
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/", nil)

		SetSession(w, r, "token", time.Hour)
		Expect(w.Result().Cookies()[0].Secure).To(BeTrue())
	})

	It("SetSession leaves Secure=false for plain HTTP dev", func() {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/", nil)

		SetSession(w, r, "token", time.Hour)
		Expect(w.Result().Cookies()[0].Secure).To(BeFalse())
	})

	It("ClearSession marks Secure off a forwarded TLS spelling too", func() {
		configtest.Setup(map[string]any{
			"server": map[string]any{"trusted_proxies": []string{"192.0.2.1/32"}},
		})
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/", nil)
		r.Header.Set("X-Forwarded-Ssl", "on")

		ClearSession(w, r)
		Expect(w.Result().Cookies()[0].Secure).To(BeTrue())
	})

	It("ClearSession emits MaxAge=-1 with empty value", func() {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/", nil)

		ClearSession(w, r)

		c := w.Result().Cookies()[0]
		Expect(c.Value).To(Equal(""))
		Expect(c.MaxAge).To(Equal(-1))
	})
})
