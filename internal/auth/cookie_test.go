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

	It("ClearSession emits MaxAge=-1 with empty value", func() {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/", nil)

		ClearSession(w, r)

		c := w.Result().Cookies()[0]
		Expect(c.Value).To(Equal(""))
		Expect(c.MaxAge).To(Equal(-1))
	})
})
