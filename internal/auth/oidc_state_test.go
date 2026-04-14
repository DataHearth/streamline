package auth

import (
	"net/http"
	"net/http/httptest"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("OIDC State Helpers", Label("unit", "auth"), func() {
	Describe("SetTransientCookie", func() {
		It("sets httpOnly cookie scoped to /auth/oidc/ with 10-min TTL", func() {
			By("Creating response recorder and request")
			w := httptest.NewRecorder()
			r := httptest.NewRequest(http.MethodGet, "/auth/oidc/test/start", nil)

			By("Setting transient cookie")
			SetTransientCookie(w, r, "_oidc_state", "test-state-value")

			By("Asserting cookie properties")
			cookies := w.Result().Cookies()
			Expect(cookies).To(HaveLen(1))

			c := cookies[0]
			Expect(c.Name).To(Equal("_oidc_state"))
			Expect(c.Value).To(Equal("test-state-value"))
			Expect(c.Path).To(Equal("/auth/oidc/"))
			Expect(c.HttpOnly).To(BeTrue())
			Expect(c.SameSite).To(Equal(http.SameSiteLaxMode))
			Expect(c.MaxAge).To(Equal(600))
		})

		It("sets Secure flag when request has X-Forwarded-Proto https", func() {
			w := httptest.NewRecorder()
			r := httptest.NewRequest(http.MethodGet, "/auth/oidc/test/start", nil)
			r.Header.Set("X-Forwarded-Proto", "https")

			SetTransientCookie(w, r, "_oidc_state", "val")

			cookies := w.Result().Cookies()
			Expect(cookies).To(HaveLen(1))
			Expect(cookies[0].Secure).To(BeTrue())
		})
	})

	Describe("ReadTransientCookies", func() {
		It("reads existing cookies by name", func() {
			r := httptest.NewRequest(http.MethodGet, "/auth/oidc/test/callback", nil)
			r.AddCookie(&http.Cookie{Name: "_oidc_state", Value: "state123"})
			r.AddCookie(&http.Cookie{Name: "_oidc_nonce", Value: "nonce456"})

			result := ReadTransientCookies(r, "_oidc_state", "_oidc_nonce")

			Expect(result["_oidc_state"]).To(Equal("state123"))
			Expect(result["_oidc_nonce"]).To(Equal("nonce456"))
		})

		It("returns empty string for missing cookies", func() {
			r := httptest.NewRequest(http.MethodGet, "/", nil)

			result := ReadTransientCookies(r, "_oidc_state", "_oidc_nonce")

			Expect(result["_oidc_state"]).To(BeEmpty())
			Expect(result["_oidc_nonce"]).To(BeEmpty())
		})
	})

	Describe("ClearTransientCookies", func() {
		It("expires named cookies with MaxAge=-1", func() {
			w := httptest.NewRecorder()
			r := httptest.NewRequest(http.MethodGet, "/auth/oidc/test/callback", nil)

			ClearTransientCookies(w, r, "_oidc_state", "_oidc_nonce")

			cookies := w.Result().Cookies()
			Expect(cookies).To(HaveLen(2))
			for _, c := range cookies {
				Expect(c.MaxAge).To(Equal(-1))
				Expect(c.Value).To(BeEmpty())
				Expect(c.Path).To(Equal("/auth/oidc/"))
			}
		})
	})
})
