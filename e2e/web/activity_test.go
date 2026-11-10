package web

import (
	"context"
	"fmt"
	"net/http"

	"github.com/go-rod/rod"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/ent/downloadrecord"
	entmovie "github.com/datahearth/streamline/ent/movie"
	"github.com/datahearth/streamline/internal/db"
)

// A download record has an address, /activity?download=<id>, and the queue can
// be filtered on a title. One seeded library serves every spec: a movie on the
// queue, a failed record older than the history's first page, and a show with
// downloads in two seasons.
var _ = Describe("Activity links", Label("e2e"), Ordered, func() {
	// The history's page size (PAGE in routes/activity/index.svelte).
	const historyPage = 50

	var queued, old, showID uint32

	BeforeAll(func() {
		ctx := context.Background()
		record := func(p db.CreateDownloadRecordParams) uint32 {
			GinkgoHelper()
			p.Size = 1 << 30
			rec, err := app.Store.CreateDownloadRecord(ctx, p)
			Expect(err).NotTo(HaveOccurred())
			return rec.ID
		}
		movie := func(title string, tmdbID uint32) uint32 {
			GinkgoHelper()
			m, err := app.Store.CreateMovie(ctx, db.CreateMovieParams{
				Title: title, OriginalTitle: title, Year: 2024,
				TmdbID: tmdbID, Status: entmovie.StatusWanted,
			})
			Expect(err).NotTo(HaveOccurred())
			// Deleting a title cascades its download records away.
			DeferCleanup(func() {
				Expect(app.Store.DeleteMovie(ctx, m.ID)).To(Succeed())
			})
			return m.ID
		}

		queued = record(db.CreateDownloadRecordParams{
			Title:   "Queued.Flick.2024.1080p-GRP",
			Status:  downloadrecord.StatusDownloading,
			MovieID: movie("Queued Flick", 880001),
		})

		// Born failed rather than completed: a record born completed is
		// bookkeeping and writes no event, and the feed has to link this one.
		oldMovie := movie("Old Flick", 880002)
		old = record(db.CreateDownloadRecordParams{
			Title:         "Old.Flick.2024.1080p-GRP",
			Status:        downloadrecord.StatusFailed,
			MovieID:       oldMovie,
			FailureReason: "no seeders",
		})
		// The history is newest first, so a full page of newer records puts
		// the one above out of the first page's reach.
		for i := range historyPage {
			record(db.CreateDownloadRecordParams{
				Title:   fmt.Sprintf("Filler.%02d.2024.1080p-GRP", i),
				Status:  downloadrecord.StatusCompleted,
				MovieID: oldMovie,
			})
		}

		show, err := app.Store.CreateTVShow(ctx, db.CreateTVShowParams{
			Title: "Linked Show", Year: 2024, TvdbID: 880003,
			Seasons: []db.SeasonSeed{
				{Number: 1, Episodes: []db.EpisodeSeed{
					{Number: 1, Title: "One"}, {Number: 2, Title: "Two"},
				}},
				{Number: 2, Episodes: []db.EpisodeSeed{
					{Number: 1, Title: "Three"},
				}},
			},
		})
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() {
			Expect(app.Store.DeleteTVShow(ctx, show.ID)).To(Succeed())
		})
		showID = show.ID
		for _, season := range show.Edges.Seasons {
			for _, ep := range season.Edges.Episodes {
				record(db.CreateDownloadRecordParams{
					Title: fmt.Sprintf(
						"Linked.Show.S%02dE%02d.1080p-GRP",
						season.Number, ep.Number,
					),
					Status:    downloadrecord.StatusDownloading,
					EpisodeID: ep.ID,
				})
			}
		}
	})

	openRow := func(id uint32) string {
		return fmt.Sprintf(`tr[data-download="%d"][aria-expanded=true]`, id)
	}
	download := func(id uint32) string {
		return fmt.Sprintf("/activity?download=%d", id)
	}
	pressedView := func(page *rod.Page) string {
		GinkgoHelper()
		return visibleElement(page, `fieldset[aria-label="Activity view"] `+
			`button[aria-pressed=true]`).MustText()
	}
	rowCount := func(page *rod.Page) func() int {
		return func() int {
			return len(visibleElements(page, `tr[data-download]`))
		}
	}
	// A link to the page from itself: Routify's patched pushState routes it
	// the way a click on an <a> does.
	follow := func(page *rod.Page, path string) {
		GinkgoHelper()
		page.MustEval(fmt.Sprintf(`() => history.pushState(null, "", %q)`, path))
	}

	It("opens a queue record from its address and keeps it over a reload", func() {
		page := newSessionPage(download(queued))
		visibleElement(page, openRow(queued))
		Expect(pressedView(page)).To(HavePrefix("Queue"))
		expectPath(page, download(queued))

		page.MustReload()
		visibleElement(page, openRow(queued))
		expectPath(page, download(queued))

		// The id is in the URL only while the detail is open.
		visibleElement(page, openRow(queued)).MustClick()
		expectPath(page, "/activity")
		Expect(visibleElements(page, openRow(queued))).To(BeEmpty())
	})

	It("reads a history record on its own when no loaded page holds it", func() {
		var first struct {
			Items []struct {
				ID uint32 `json:"id"`
			} `json:"items"`
		}
		Expect(apiDo(
			http.MethodGet,
			fmt.Sprintf("/api/v1/activity/history?limit=%d", historyPage),
			nil, &first,
		)).To(Equal(http.StatusOK))
		Expect(first.Items).To(HaveLen(historyPage))
		for _, it := range first.Items {
			Expect(it.ID).NotTo(Equal(old), "the record must be past page one")
		}

		page := newSessionPage(download(old))
		visibleElement(page, openRow(old))
		Expect(pressedView(page)).To(HavePrefix("History"))
		expectPath(page, download(old))
		Expect(rowCount(page)()).To(Equal(historyPage + 1))
		// Read once, by the request made for this record alone.
		single := fmt.Sprintf("/api/v1/activity/history/%d", old)
		fetches := func() int {
			return page.MustEval(`(path) => performance.getEntriesByType("resource")
				.filter((e) => new URL(e.name).pathname === path).length`, single).Int()
		}
		Expect(fetches()).To(Equal(1))

		// The page that holds the record replaces the copy read alone.
		page.MustEval(
			`() => document.querySelector("tbody tr:last-child").scrollIntoView()`,
		)
		Eventually(func() int {
			return len(page.MustElements(fmt.Sprintf(`tr[data-download="%d"]`, old)))
		}).Should(Equal(1))
		// It was listed first while it was read alone; the oldest record's
		// place is last.
		Eventually(func() string {
			return page.MustEval(`() => Array.from(
				document.querySelectorAll("tr[data-download]"),
			).at(-1).dataset.download`).Str()
		}).Should(Equal(fmt.Sprint(old)))
		Expect(rowCount(page)()).To(Equal(historyPage + 1))
		visibleElement(page, openRow(old))
		Expect(fetches()).To(Equal(1))
	})

	It("says a record is gone and drops it from the URL", func() {
		page := newSessionPage("/activity?download=999999")
		expectToast(page, "no longer in Activity")
		expectPath(page, "/activity")
		Expect(pressedView(page)).To(HavePrefix("Queue"))
	})

	It(
		"opens a download from the events feed, and Back returns to the feed",
		func() {
			page := newSessionPage("/activity?view=events")
			chip := func(id uint32) *rod.Element {
				GinkgoHelper()
				return visibleElement(
					page, fmt.Sprintf(`a[data-download-link="%d"]`, id),
				)
			}

			// Into the history: the link names no view, the page switches to the
			// one holding the record, and the feed's own entry survives.
			chip(old).MustClick()
			visibleElement(page, openRow(old))
			Expect(pressedView(page)).To(HavePrefix("History"))
			expectPath(page, download(old))

			page.MustEval(`() => history.back()`)
			expectPath(page, "/activity?view=events")
			Expect(pressedView(page)).To(HavePrefix("Events"))

			// And onto the queue.
			chip(queued).MustClick()
			visibleElement(page, openRow(queued))
			Expect(pressedView(page)).To(HavePrefix("Queue"))
			expectPath(page, download(queued))

			page.MustEval(`() => history.back()`)
			expectPath(page, "/activity?view=events")
			chip(queued)
		},
	)

	It("filters the queue on a title, and clears the filter", func() {
		const filterChip = `button[data-title-filter]`
		season := func(n int) string {
			return fmt.Sprintf("/activity?series=%d&season=%d", showID, n)
		}

		page := newSessionPage(season(1))
		Expect(visibleElement(page, filterChip).MustText()).
			To(ContainSubstring("Linked Show · Season 1"))
		Eventually(rowCount(page)).Should(Equal(2))
		expectPath(page, season(1))

		// The chip's own ✕.
		visibleElement(page, filterChip).MustClick()
		Eventually(rowCount(page)).Should(Equal(4))
		expectPath(page, "/activity")
		Expect(visibleElements(page, filterChip)).To(BeEmpty())

		// The whole show; the filter is the queue's, so History drops it.
		follow(page, fmt.Sprintf("/activity?series=%d", showID))
		Eventually(func() string {
			return visibleElement(page, filterChip).MustText()
		}).Should(ContainSubstring("Linked Show"))
		Eventually(rowCount(page)).Should(Equal(3))
		page.MustElementR(`button[aria-pressed]`, "^History").MustClick()
		expectPath(page, "/activity?view=history")
		page.MustElementR(`button[aria-pressed]`, "^Queue").MustClick()
		expectPath(page, "/activity")
		Eventually(rowCount(page)).Should(Equal(4))

		// The strip's clear takes the title with the status chips.
		follow(page, season(2))
		Eventually(rowCount(page)).Should(Equal(1))
		page.MustElementR(`button[aria-pressed]`, "^downloading").MustClick()
		expectPath(page, fmt.Sprintf(
			"/activity?status=downloading&series=%d&season=2", showID,
		))
		visibleElement(page, `button[aria-label="Clear status filters"]`).MustClick()
		expectPath(page, "/activity")

		// Below lg the search sits behind a magnifier, which lights for the
		// search alone, and the sheet it opens resets the title filter too.
		follow(page, season(2))
		Eventually(rowCount(page)).Should(Equal(1))
		page.MustSetViewport(900, 900, 1, false)
		page.MustElementR(`button[aria-pressed]`, "^downloading").MustClick()
		magnifier := visibleElement(page, `button[aria-label="Search and sort"]`)
		Expect(magnifier.MustElements("span")).To(BeEmpty())
		magnifier.MustClick()
		field := visibleElement(page, `[role=dialog] input[type=search]`)
		field.MustInput("zzz")
		Eventually(func() int {
			return len(magnifier.MustElements("span"))
		}).Should(Equal(1))
		page.MustElementR(`[role=dialog] button`, "^Reset$").MustClick()
		expectPath(page, "/activity")
		Expect(visibleElements(page, filterChip)).To(BeEmpty())
	})
})
