package web

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Account profile", Label("e2e"), func() {
	It("updates the display name from the profile card", func() {
		const edited = "E2E Web Admin"
		original := displayName()

		page := newSessionPage("/account")
		card := page.MustElementX(`//h3[text()="Profile"]/ancestor::section[1]`)
		card.MustElementR("button", "^Edit$").MustClick()

		modal := page.MustElement(`div[role=dialog]`)
		Expect(modal.MustElement("h2").MustText()).To(Equal("Edit profile"))
		modal.MustElement(`input[name=display_name]`).
			MustSelectAllText().
			MustInput(edited)
		modal.MustElementR("button", "Save changes").MustClick()
		DeferCleanup(func() { setDisplayName(original) })

		expectToast(page, "Profile updated")
		modal.MustWaitInvisible()
		Expect(card.MustText()).To(ContainSubstring(edited))
		Expect(
			page.MustElement(`aside[aria-label="Primary navigation"]`).MustText(),
		).
			To(ContainSubstring(edited))
	})
})
