package web

import (
	"net/http"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Account preferences", Label("e2e"), func() {
	It("turns title logos off for the account", func() {
		DeferCleanup(func() {
			Expect(apiDo(http.MethodPatch, "/api/v1/auth/me", map[string]any{
				"title_logos": true,
			}, nil)).To(Equal(http.StatusOK))
		})

		page := newSessionPage("/account")
		card := page.MustElementX(`//h3[text()="Title logos"]/ancestor::section[1]`)
		Expect(card.MustElementR("button", "^On$").MustAttribute("aria-pressed")).
			To(HaveValue(Equal("true")))

		card.MustElementR("button", "^Off$").MustClick()
		Eventually(titleLogos).WithTimeout(5 * time.Second).Should(BeFalse())

		// Another browser of the same account reads the stored choice.
		page = newSessionPage("/account")
		card = page.MustElementX(`//h3[text()="Title logos"]/ancestor::section[1]`)
		Expect(card.MustElementR("button", "^Off$").MustAttribute("aria-pressed")).
			To(HaveValue(Equal("true")))
	})
})

func titleLogos() bool {
	GinkgoHelper()
	var me struct {
		TitleLogos bool `json:"title_logos"`
	}
	Expect(apiDo(http.MethodGet, "/api/v1/auth/me", nil, &me)).
		To(Equal(http.StatusOK))
	return me.TitleLogos
}
