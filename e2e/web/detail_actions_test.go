package web

import (
	"fmt"
	"time"

	"github.com/go-rod/rod"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// heroActions are the visible buttons of a detail hero's action row, each named
// by its accessible name, with its vertical centre, how far the row overflows
// and the labels the row cuts short.
type heroActions struct {
	Names    []string  `json:"names"`
	Centres  []float64 `json:"centres"`
	Overflow float64   `json:"overflow"`
	Clipped  []string  `json:"clipped"`
}

func readHeroActions(page *rod.Page, titleID string) heroActions {
	GinkgoHelper()
	hero := page.MustElement(fmt.Sprintf(`section[aria-labelledby="%s"]`, titleID))
	row := hero.MustElement("fieldset")
	var got heroActions
	Expect(row.MustEval(`() => {
		const shown = [...this.querySelectorAll("button")].filter((b) => b.offsetWidth > 0);
		const box = this.getBoundingClientRect();
		return {
			names: shown.map((b) => b.getAttribute("aria-label") || b.innerText.trim()),
			centres: shown.map((b) => {
				const r = b.getBoundingClientRect();
				return r.top + r.height / 2;
			}),
			overflow: Math.max(...shown.map((b) => b.getBoundingClientRect().right)) - box.right,
			clipped: [...this.querySelectorAll("span.truncate")]
				.filter((l) => l.scrollWidth > l.clientWidth)
				.map((l) => l.innerText),
		};
	}`).Unmarshal(&got)).To(Succeed())
	return got
}

// expectOneLine checks the row from md up: the narrowest layout, a tablet, the
// narrowest one with the full sidebar, a desktop, and the two widths where the
// row reaches the 512px its labels need.
func expectOneLine(page *rod.Page, titleID string) {
	GinkgoHelper()
	for _, width := range []int{768, 820, 896, 1024, 1132, 1440} {
		page.MustSetViewport(width, 900, 1, false)
		page.MustEval(`() => document.fonts.ready.then(() => true)`)
		var got heroActions
		Eventually(func(g Gomega) {
			got = readHeroActions(page, titleID)
			g.Expect(got.Names).NotTo(BeEmpty())
			for _, c := range got.Centres {
				g.Expect(c).
					To(BeNumerically("~", got.Centres[0], 1), "%dpx: %+v", width, got)
			}
			g.Expect(got.Overflow).
				To(BeNumerically("<=", 0.5), "%dpx: %+v", width, got)
			g.Expect(got.Clipped).To(BeEmpty(), "%dpx: %+v", width, got)
		}).WithTimeout(2 * time.Second).WithPolling(100 * time.Millisecond).Should(Succeed())
	}
}

func openInLocale(locale, path string, setup func(*rod.Page)) *rod.Page {
	GinkgoHelper()
	page := newSessionPage("/")
	page.MustEvalOnNewDocument(fmt.Sprintf(
		`localStorage.setItem("PARAGLIDE_LOCALE", %q)`, locale))
	setup(page)
	page.MustNavigate(baseURL + path)
	return page
}

// The suite has no series provider, so the detail response is the page's only
// source for the show.
func stubSeries(page *rod.Page) {
	GinkgoHelper()
	router := page.HijackRequests()
	router.MustAdd("*/api/v1/series/1", func(h *rod.Hijack) {
		h.Response.SetHeader("Content-Type", "application/json")
		h.Response.SetBody(`{
			"id": 1, "title": "Stub Show", "year": 2007, "series_status": "ended",
			"type": "standard", "monitored": true, "tvdb_id": 1,
			"total_seasons": 1, "have_episodes": 0, "total_episodes": 1,
			"wanted_episodes": 1, "seasons": [], "cast": [], "crew": []
		}`)
	})
	go router.Run()
	DeferCleanup(func() { Expect(router.Stop()).To(Succeed()) })
}

var _ = Describe("Detail hero actions", Label("e2e"), func() {
	DescribeTable("stay on one line on the movie page",
		func(locale string) {
			id := seedMovie()
			page := openInLocale(
				locale,
				fmt.Sprintf("/movies/%d", id),
				func(*rod.Page) {},
			)
			expectOneLine(page, "movie-title")
		},
		Entry("in English", "en"),
		Entry("in French", "fr"),
	)

	DescribeTable("stay on one line on the series page",
		func(locale string) {
			page := openInLocale(locale, "/series/1", stubSeries)
			expectOneLine(page, "series-title")
		},
		Entry("in English", "en"),
		Entry("in French", "fr"),
	)

	It("come in one order: monitor, play, search, menu", func() {
		id := seedMovie()
		movie := openInLocale(
			"en",
			fmt.Sprintf("/movies/%d", id),
			func(*rod.Page) {},
		)
		series := openInLocale("en", "/series/1", stubSeries)

		m := readHeroActions(movie, "movie-title").Names
		s := readHeroActions(series, "series-title").Names
		Expect(m).To(HaveLen(4))
		Expect(s).To(HaveLen(5))
		Expect(m[0]).To(ContainSubstring("onitor"))
		Expect(m[1:3]).To(Equal([]string{"Play on", "Manual search"}))
		// The series bookmark carries its presets chevron beside it.
		Expect(s[0]).To(ContainSubstring("onitor"))
		Expect(s[2:4]).To(Equal([]string{"Play on", "Manual search"}))
		Expect(m[3]).To(Equal(s[4]))
	})
})
