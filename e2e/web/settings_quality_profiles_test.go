package web

import (
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// rowSelector matches a profile row's own button, whose accessible name is the
// only per-row unique hook on the page.
func rowSelector(name string) string {
	return fmt.Sprintf(`button[aria-label="Edit %s"]`, name)
}

// deleteSelector reaches the row's trash button, which shares its aria-label
// with every other row's, via the uniquely named sibling button.
func deleteSelector(name string) string {
	return fmt.Sprintf(
		`//button[@aria-label="Edit %s"]/following-sibling::div`+
			`/button[@aria-label="Delete profile"]`,
		name,
	)
}

var _ = Describe("Settings quality profiles", Label("e2e"), func() {
	It("lists the seeded default profile", func() {
		page := newSessionPage("/settings/quality-profiles")
		Expect(page.MustElementR("h1", "Quality profiles").MustText()).
			To(Equal("Quality profiles"))
		Expect(page.MustElement(rowSelector("default")).MustText()).
			To(ContainSubstring("default"))
	})

	It("creates, edits and deletes a profile through the UI", func() {
		const name = "e2e-web-qp"
		page := newSessionPage("/settings/quality-profiles")
		page.MustElementR("h1", "Quality profiles")

		By("creating it from the form")
		page.MustElementR("button", "Add profile").MustClick()
		form := page.MustElement(`div[role=dialog]`)
		Expect(form.MustElement("h2").MustText()).To(Equal("Add quality profile"))
		form.MustElement(`input[name=name]`).MustInput(name)
		form.MustElement(`button[type=submit]`).MustClick()
		DeferCleanup(func() { deleteQualityProfile(name) })

		expectToast(page, "Profile created")
		row := page.MustElement(rowSelector(name))
		// The status pill is CSS-uppercased, so innerText reads "UPGRADES ON".
		Expect(row.MustText()).To(MatchRegexp(`(?i)upgrades on`))
		Expect(row.MustText()).To(ContainSubstring("1080p"))

		By("raising its preferred resolution")
		page = page.CancelTimeout().Timeout(pageBudget)
		page.MustElement(rowSelector(name)).MustClick()
		// Modal.svelte portals every dialog to <body> and fades the closed one
		// out over 180ms, so two div[role=dialog] nodes coexist while a second
		// modal opens on the same page — and querySelector would bind the
		// closing one. Matching on content picks the right node and waits the
		// transition out. Every lookup inside it must stay scoped to `form`
		// (relative ".//" XPath included) for the same reason.
		form = page.MustElementR(`div[role=dialog]`, "Edit quality profile")
		Expect(form.MustElement(`input[name=name]`).MustText()).To(Equal(name))
		form.MustElementX(
			`.//span[text()="Preferred resolution"]/following-sibling::div/button`,
		).MustClick()
		page.MustElementR(`[role=listbox] button[role=option]`, "^2160p$").
			MustClick()
		form.MustElement(`button[type=submit]`).MustClick()

		expectToast(page, "Profile updated")
		// Polling form: the row is already on screen, so a one-shot MustText
		// would race the list refetch that carries the new resolution.
		page.MustElementR(rowSelector(name), "2160p")

		By("deleting it through the confirmation dialog")
		page = page.CancelTimeout().Timeout(pageBudget)
		row = page.MustElement(rowSelector(name))
		page.MustElementX(deleteSelector(name)).MustClick()
		confirm := page.MustElementR(`div[role=dialog]`, "Delete quality profile")
		Expect(confirm.MustText()).To(ContainSubstring(name))
		confirm.MustElementR("button", "^Delete$").MustClick()

		expectToast(page, "Profile deleted")
		row.MustWaitInvisible()
	})

	It("keeps the profile when the delete dialog is cancelled", func() {
		const name = "e2e-web-qp-keep"
		seedQualityProfile(name)

		page := newSessionPage("/settings/quality-profiles")
		row := page.MustElement(rowSelector(name))
		page.MustElementX(deleteSelector(name)).MustClick()

		confirm := page.MustElement(`div[role=dialog]`)
		confirm.MustElementR("button", "^Cancel$").MustClick()
		confirm.MustWaitInvisible()

		Expect(row.MustText()).To(ContainSubstring(name))
	})
})
