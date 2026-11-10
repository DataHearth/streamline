package web

import (
	"context"
	"fmt"
	"strings"

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

	It("carries the activity view, its chips and the search, view first", func() {
		page := newSessionPage("/activity?view=history&status=failed,bogus&q=x")
		expectPath(page, "/activity?view=history&status=failed&q=x")
		Expect(
			visibleElement(page, `button[aria-pressed=true]`).MustText(),
		).To(HavePrefix("History"))

		// A switch clears the chips, and the URL follows.
		visibleElement(page, `button[aria-pressed=false]`)
		page.MustElementR(`button[aria-pressed]`, "^Queue").MustClick()
		expectPath(page, "/activity?q=x")
	})

	It("opens requests on the tab and kind a link names", func() {
		page := newSessionPage("/requests?tab=all&kind=movies")
		expectPath(page, "/requests?tab=all&kind=movies")
		Expect(
			visibleElement(page, `nav button[aria-current=page]`).MustText(),
		).To(HavePrefix("All"))
	})

	It("keeps the users filter and sort in the URL", func() {
		page := newSessionPage("/settings/users?role=admin&sort=name")
		expectPath(page, "/settings/users?role=admin&sort=name")
		Expect(
			visibleElement(page, `th[aria-sort=ascending]`).MustText(),
		).To(ContainSubstring("USER"))

		visibleElement(page, `th[aria-sort=ascending] button`).MustClick()
		expectPath(page, "/settings/users?role=admin&sort=name&order=desc")
	})

	It("keeps the calendar's month and kind in the URL", func() {
		page := newSessionPage("/calendar?month=2026-01&kind=movies")
		page.MustElementR("h2", "January 2026")
		expectPath(page, "/calendar?kind=movies&month=2026-01")

		visibleElement(page, `button[aria-label="Next month"]`).MustClick()
		page.MustElementR("h2", "February 2026")
		expectPath(page, "/calendar?kind=movies&month=2026-02")

		page = newSessionPage("/calendar?month=2026-13")
		expectPath(page, "/calendar")
	})

	// The specs below arrive by a link inside the app. Routify sets the active
	// route first and writes history from a timer after it, so the page's own
	// write-back has a window in which the address bar is still the URL left.
	historyLength := func(page *rod.Page) int {
		GinkgoHelper()
		return page.MustEval(`() => history.length`).Int()
	}
	const moviesSort = `() => localStorage.setItem("streamline:movies:sort", "year-desc")`

	It("settles a list reached by a link, for its URL and the back link", func() {
		id := seedMovie()
		page := newSessionPage("/movies?status=failed")
		expectPath(page, "/movies?status=failed")
		page.MustEval(moviesSort)

		visibleElement(page, `a[href="/series"]`).MustClick()
		expectPath(page, "/series")
		visibleElement(page, `a[href="/movies"]`).MustClick()
		// The remembered sort is the page's state: the bare link is rewritten.
		expectPath(page, "/movies?sort=year&order=desc")

		// The detail page's back link returns to the list as it was left, not
		// to the filter of an earlier visit.
		visibleElement(page, fmt.Sprintf(`a[href="/movies/%d"]`, id)).MustClick()
		expectPath(page, fmt.Sprintf("/movies/%d", id))
		Eventually(func() string {
			return *visibleElement(page, `section.hero a`).MustAttribute("href")
		}).Should(Equal("/movies?sort=year&order=desc"))
	})

	It("keeps Back when a link to the same page is rewritten", func() {
		seedMovie()
		page := newSessionPage("/movies")
		visibleElement(page, `a[href="/movies"]`)
		page.MustEval(moviesSort)
		page.MustNavigate(baseURL + "/movies?status=failed")
		expectPath(page, "/movies?status=failed&sort=year&order=desc")
		before := historyLength(page)

		// The sidebar's bare /movies is not how the page spells its state.
		visibleElement(page, `a[href="/movies"]`).MustClick()
		expectPath(page, "/movies?sort=year&order=desc")
		Expect(historyLength(page)).To(Equal(before + 1))

		page.MustEval(`() => history.back()`)
		expectPath(page, "/movies?status=failed&sort=year&order=desc")
	})

	It("keeps Back when the URL a same-page link leaves has a hash", func() {
		seedMovie()
		page := newSessionPage("/movies?status=failed")
		expectPath(page, "/movies?status=failed")
		// The skip link is off screen until focused, so it is clicked in page.
		page.MustElement(`a[href="#main"]`).MustEval(`() => this.click()`)
		expectPath(page, "/movies?status=failed#main")
		before := historyLength(page)

		visibleElement(page, `a[href="/movies"]`).MustClick()
		Eventually(func() string {
			return strings.TrimPrefix(page.MustInfo().URL, baseURL)
		}).Should(Equal("/movies"))
		Expect(historyLength(page)).To(Equal(before + 1))

		page.MustEval(`() => history.back()`)
		expectPath(page, "/movies?status=failed#main")
	})

	// A show with specials and two seasons, one episode each; returns its path
	// and the id of season 1's episode. Specials are what season 0 selects, and
	// season 2 is the one the page picks by default.
	seedShow := func() (string, uint32) {
		GinkgoHelper()
		ctx := context.Background()
		show := app.DB.TVShow.Create().
			SetTitle("Query State").SetYear(2005).SetTvdbID(990134).
			SaveX(ctx)
		DeferCleanup(func() {
			app.DB.TVShow.DeleteOneID(show.ID).ExecX(context.Background())
		})
		var target uint32
		for n := range uint16(3) {
			season := app.DB.Season.Create().
				SetNumber(n).SetTvShow(show).SaveX(ctx)
			ep := app.DB.Episode.Create().
				SetNumber(1).SetTitle(fmt.Sprintf("Part %d", n)).
				SetSeason(season).SaveX(ctx)
			if n == 1 {
				target = ep.ID
			}
		}
		return fmt.Sprintf("/series/%d", show.ID), target
	}

	It("opens an episode link naming no season on the episode's season", func() {
		path, episode := seedShow()
		page := newSessionPage(fmt.Sprintf("%s?episode=%d", path, episode))
		expectPath(
			page,
			fmt.Sprintf("%s?tab=episodes&season=1&episode=%d", path, episode),
		)
		Expect(
			visibleElement(page, `button[aria-current=true]`).MustText(),
		).To(MatchRegexp(`(?i)^season 0?1\b`))
	})

	It("closes the episode sheet a link opened when Back leaves it", func() {
		path, episode := seedShow()
		page := newSessionPage(path + "?tab=history")
		expectPath(page, path+"?tab=history")

		// What picking the episode in the global search does from this page.
		link := fmt.Sprintf("%s?tab=episodes&season=1&episode=%d", path, episode)
		page.MustEval(fmt.Sprintf(`() => history.pushState(null, "", %q)`, link))
		visibleElement(page, `[role=dialog]`)
		expectPath(page, link)

		page.MustEval(`() => history.back()`)
		expectPath(page, path+"?tab=history")
		Eventually(func() int {
			return len(visibleElements(page, `[role=dialog]`))
		}).Should(BeZero())
		// The entry left is not rewritten to carry the episode.
		Consistently(func() string { return page.MustInfo().URL }, "500ms").
			Should(HaveSuffix(path + "?tab=history"))
	})
})
