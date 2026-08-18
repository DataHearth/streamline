package web

import (
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/e2e/apptest"
)

var _ = Describe("Shell navigation", Label("e2e"), func() {
	It("renders the app shell for an authenticated admin", func() {
		page := newSessionPage("/")
		sidebar := visibleElement(page, `aside[aria-label="Primary navigation"]`)
		Expect(page.MustElementR("h1", "^Dashboard$").MustText()).
			To(Equal("Dashboard"))
		// The identity block shows the display name when one is set, so assert
		// on the role line — it doesn't move when a profile spec edits the name.
		Expect(sidebar.MustElement(`a[aria-label="Account settings"]`).MustText()).
			To(MatchRegexp(`(?i)admin`))
		// Settings is admin-only, so its presence proves the claims hydrated.
		sidebar.MustElement(`a[href="/settings"]`)
		page.MustElement(`button[aria-label="Sign out"]`)
	})

	It("walks the sidebar through every primary section", func() {
		page := newSessionPage("/")
		page.MustElement(`aside[aria-label="Primary navigation"]`)
		// The Activity group is a collapsed disclosure until one of its routes is
		// current, so its links aren't in the DOM to click yet.
		page.MustElement(
			`aside[aria-label="Primary navigation"] button[aria-expanded="false"]`,
		).MustClick()

		for _, section := range []struct{ href, heading string }{
			{"/movies", "Movies"},
			{"/series", "Series"},
			{"/activity", "Queue & History"},
			{"/activity/torrents", "Torrents"},
			{"/calendar", "Calendar"},
			{"/requests", "Requests"},
			{"/", "Dashboard"},
		} {
			By("navigating to " + section.href)
			// Per-hop budget: six hops each allowing expectPath its own 5s of
			// polling would otherwise overrun the whole-spec deadline, turning a
			// path mismatch into an unhelpful context-deadline error.
			page = page.CancelTimeout().Timeout(pageBudget)
			page.MustElement(fmt.Sprintf(
				`aside[aria-label="Primary navigation"] a[href=%q]`, section.href,
			)).MustClick()
			Expect(page.MustElementR("h1", "^"+section.heading+"$").MustText()).
				To(Equal(section.heading))
			expectPath(page, section.href)
			// The highlight has to follow the route. A nav that derives "current"
			// from a non-reactive source keeps lighting up the page it was left
			// on, so assert exactly one link claims it and that it's this one.
			// Scoped to the visible sidebar: the tablet rail is a second copy of
			// the same nav and marks its own current link.
			current := visibleElements(
				page,
				`aside[aria-label="Primary navigation"] a[aria-current="page"]`,
			)
			Expect(current).To(HaveLen(1))
			Expect(*current[0].MustAttribute("href")).To(Equal(section.href))
		}
	})

	It("opens settings from the sidebar and lands on general", func() {
		page := newSessionPage("/")
		page.MustElement(
			`aside[aria-label="Primary navigation"] a[href="/settings"]`,
		).MustClick()
		Expect(page.MustElementR("h1", "^General$").MustText()).To(Equal("General"))
		Expect(page.MustElement(`nav[aria-label=Breadcrumb]`).MustText()).
			To(ContainSubstring("Settings"))
		Expect(page.MustElement(`aside[aria-label="Settings sections"]`).MustText()).
			To(ContainSubstring("Quality profiles"))
		expectPath(page, "/settings/general")
	})

	It("opens the account page from the sidebar identity link", func() {
		page := newSessionPage("/")
		page.MustElement(`a[aria-label="Account settings"]`).MustClick()
		Expect(page.MustElementR("h3", "^Profile$").MustText()).To(Equal("Profile"))
		Expect(page.MustElement(`main`).MustText()).
			To(ContainSubstring(apptest.AdminEmail))
		expectPath(page, "/account")
	})

	It("renders the client-side 404 for an unknown path", func() {
		page := newSessionPage("/no-such-page")
		Expect(page.MustElementR("h1", "Off the grid").MustText()).
			To(ContainSubstring("Off the grid"))
		body := page.MustElement(`main`).MustText()
		Expect(body).To(ContainSubstring("/no-such-page"))
		Expect(body).To(ContainSubstring("Back to dashboard"))
	})
})
