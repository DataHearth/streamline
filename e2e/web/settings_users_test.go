package web

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/e2e/apptest"
)

// invitesSection scopes lookups to the Invites card: the users page renders a
// second `input[name=email]` in its (closed) new-user dialog.
const invitesSection = `//h2[text()="Invites"]/ancestor::section[1]`

var _ = Describe("Settings users", Label("e2e"), func() {
	It("shows the seed admin in the users list", func() {
		page := newSessionPage("/settings/users")
		Expect(page.MustElementR("h1", "^Users$").MustText()).To(Equal("Users"))

		row := page.MustElementR("tbody tr", apptest.AdminEmail)
		Expect(row.MustText()).To(ContainSubstring("admin"))
		// The self-marker pill is CSS-uppercased, so innerText reads "YOU".
		Expect(row.MustText()).To(MatchRegexp(`(?i)\byou\b`))
	})

	It("opens and cancels the new-user dialog", func() {
		page := newSessionPage("/settings/users")
		page.MustElementR("button", "New user").MustClick()

		dialog := page.MustElement(`div[role=dialog]`)
		Expect(dialog.MustElement("h2").MustText()).To(Equal("New user"))
		dialog.MustElement(`input[name=email]`)
		dialog.MustElement(`input[name=password]`)
		Expect(dialog.MustText()).To(ContainSubstring("Display name"))

		dialog.MustElementR("button", "^Cancel$").MustClick()
		dialog.MustWaitInvisible()
	})

	It("creates an invite and reveals its token once", func() {
		const invitee = "e2e-web-invitee@streamline.local"
		page := newSessionPage("/settings/users")
		invites := page.MustElementX(invitesSection)
		invites.MustElement(`input[name=email]`).MustInput(invitee)
		invites.MustElementR("button", "Create invite").MustClick()
		DeferCleanup(func() { revokeInvitesFor(invitee) })

		expectToast(page, "Invite created")
		Expect(invites.MustText()).To(ContainSubstring("copy now"))

		codes := invites.MustElements("code")
		Expect(codes).To(HaveLen(2))
		url, token := codes[0].MustText(), codes[1].MustText()
		Expect(token).NotTo(BeEmpty())
		Expect(url).To(ContainSubstring("/register?token=" + token))

		// The role pill is CSS-uppercased, so innerText reads "MEMBER".
		Expect(invites.MustElementR("li", invitee).MustText()).
			To(MatchRegexp(`(?i)member`))
	})
})
