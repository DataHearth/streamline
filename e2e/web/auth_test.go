package web

import (
	"github.com/go-rod/rod"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/e2e/apptest"
)

// auth.Limiter allows 5 /auth/login attempts per 15 minutes per IP and all
// specs share 127.0.0.1 — keep total login submissions in this suite under 5.
var _ = Describe("Auth flows", Label("e2e"), func() {
	It("redirects unauthenticated visitors to the login form", func() {
		page := newPage("/")
		page.MustElement(`input[type=email]`)
		page.MustElement(`input[type=password]`)
		Expect(page.MustInfo().URL).To(ContainSubstring("/login"))
	})

	It("logs the seed admin in and lands on the dashboard", func() {
		page := newPage("/login")
		loginAsAdmin(page)
		page.MustElement(`button[aria-label="Sign out"]`)
		Expect(page.MustInfo().URL).To(ContainSubstring("/dashboard"))
	})

	It("shows an error on a wrong password", func() {
		page := newPage("/login")
		page.MustElement(`input[type=email]`).MustInput(apptest.AdminEmail)
		page.MustElement(`input[type=password]`).MustInput("definitely-wrong")
		page.MustElementR("button", "Sign in").MustClick()
		// The SPA never renders the API's own message — it resolves the
		// `invalid_credentials` code to its own translated copy. Asserting the
		// specific line also catches a broken code lookup, which would fall
		// back to the generic 401 ("session has expired") and read as nonsense
		// on a login form.
		Expect(
			page.MustElement(`p[role=alert]`).MustText(),
		).To(ContainSubstring("Incorrect email or password"))
	})

	It("logs out back to the login page", func() {
		page := newPage("/login")
		loginAsAdmin(page)
		page.MustElement(`button[aria-label="Sign out"]`).MustClick()
		page.MustElement(`input[type=email]`)
		Expect(page.MustInfo().URL).To(ContainSubstring("/login"))
	})
})

func loginAsAdmin(page *rod.Page) {
	GinkgoHelper()
	page.MustElement(`input[type=email]`).MustInput(apptest.AdminEmail)
	page.MustElement(`input[type=password]`).MustInput(apptest.AdminPassword)
	page.MustElementR("button", "Sign in").MustClick()
}
