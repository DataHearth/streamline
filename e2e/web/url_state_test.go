package web

import (
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/go-rod/rod"
)

// A page's filters, sort and tab live in its query string (#134). A link from
// before a parameter was renamed still opens the view it named, and the page
// rewrites it to the current name.
var _ = Describe("Query-string state", Label("e2e"), func() {
	selectedTab := func(page *rod.Page) string {
		GinkgoHelper()
		return visibleElement(page, `[role=tab][aria-selected=true]`).MustText()
	}

	It("reads quality profiles' old ?media= and writes ?kind=", func() {
		page := newSessionPage("/settings/quality-profiles?media=music")
		expectPath(page, "/settings/quality-profiles?kind=music")
		Expect(selectedTab(page)).To(HavePrefix("Music"))

		page.MustElementR(`[role=tab]`, "^Books").MustClick()
		expectPath(page, "/settings/quality-profiles?kind=books")
		page.MustElementR(`[role=tab]`, "^Video").MustClick()
		expectPath(page, "/settings/quality-profiles")
	})

	It("reads music's old ?sort=name and gives every sort a direction", func() {
		page := newSessionPage("/music?sort=name")
		expectPath(page, "/music")
		visibleElement(page, `button[aria-label="Sort: Name A→Z"]`)

		page = newSessionPage("/music?sort=recent")
		expectPath(page, "/music?sort=recent")
		visibleElement(page, `button[aria-label="Sort: Recently added"]`)

		page = newSessionPage("/music?sort=title&order=desc")
		expectPath(page, "/music?order=desc")
		visibleElement(page, `button[aria-label="Sort: Name Z→A"]`)
	})

	It("reads books' old ?sort=added and keeps the direction it is given", func() {
		page := newSessionPage("/books?sort=added&order=asc")
		expectPath(page, "/books?order=asc")
		visibleElement(page, `button[aria-label="Sort: Oldest added"]`)
	})

	It("keeps a movie's tab in the URL and follows it back", func() {
		id := seedMovie()
		path := fmt.Sprintf("/movies/%d", id)
		active := func(page *rod.Page) string {
			GinkgoHelper()
			return visibleElement(page, `button[aria-current=page]`).MustText()
		}

		page := newSessionPage(path + "?tab=cast")
		Expect(active(page)).To(Equal("Cast"))

		page.MustElementR("button", "^History$").MustClick()
		expectPath(page, path+"?tab=history")

		page.MustEval(fmt.Sprintf(`() => history.pushState(null, "", %q)`, path))
		Eventually(func() string { return active(page) }).Should(Equal("Overview"))

		page.MustEval(`() => history.back()`)
		Eventually(func() string { return active(page) }).Should(Equal("History"))
		expectPath(page, path+"?tab=history")
	})
})
