package web

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Movies page", Label("e2e"), func() {
	It("renders the empty-library state", func() {
		page := newSessionPage("/movies")
		Expect(page.MustElementR("h1", "^Movies$").MustText()).To(Equal("Movies"))
		Expect(page.MustElementR("p", "Your library is empty").MustText()).
			To(ContainSubstring("Your library is empty"))
		Expect(page.MustElement(`nav[aria-label="Movie status"]`).MustText()).
			To(ContainSubstring("All"))
	})

	// No search term is typed: the modal's TMDB query needs 2+ characters, and
	// this suite must never reach out to the network.
	It("opens and closes the add-movie modal from the toolbar", func() {
		page := newSessionPage("/movies")
		page.MustElementR("button", "Add movie").MustClick()

		modal := page.MustElement(`div[role=dialog]`)
		Expect(modal.MustElement("h2").MustText()).To(Equal("Add movie"))
		modal.MustElement(`input[aria-label="Search TMDB by title"]`)
		Expect(modal.MustText()).To(ContainSubstring("Quality profile"))

		modal.MustElement(`button[aria-label=Close]`).MustClick()
		modal.MustWaitInvisible()
	})

	It("filters to a status tab and clears back to the library view", func() {
		page := newSessionPage("/movies")
		page.MustElement(`nav[aria-label="Movie status"]`).
			MustElementR("button", "Wanted").MustClick()

		Expect(page.MustElementR("p", "No movies match this view").MustText()).
			To(ContainSubstring("No movies match this view"))
		expectPath(page, "/movies?status=wanted")

		page.MustElementR("button", "Clear filters").MustClick()
		Expect(page.MustElementR("p", "Your library is empty").MustText()).
			To(ContainSubstring("Your library is empty"))
		expectPath(page, "/movies")
	})
})
