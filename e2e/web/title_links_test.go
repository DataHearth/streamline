package web

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/go-rod/rod"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/ent/downloadrecord"
	"github.com/datahearth/streamline/ent/episode"
	entmovie "github.com/datahearth/streamline/ent/movie"
	"github.com/datahearth/streamline/internal/db"
)

// titleLibrary is what the title pages point at their downloads from: a movie
// with one download, and a show whose first season waits on a download per
// episode and whose second waits on one pack.
type titleLibrary struct {
	movie, movieDownload uint32
	show                 uint32
	// Season 1: two episodes downloading on their own, a third available.
	single [2]uint32
	// Season 2: one pack over both episodes, anchored on the first.
	pack        uint32
	packEpisode uint32
}

// seedTitleLibrary seeds it and registers its removal: the empty-library specs
// share this app.
func seedTitleLibrary() titleLibrary {
	GinkgoHelper()
	ctx := context.Background()
	var lib titleLibrary
	record := func(p db.CreateDownloadRecordParams) uint32 {
		GinkgoHelper()
		p.Size = 1 << 30
		p.Status = downloadrecord.StatusDownloading
		rec, err := app.Store.CreateDownloadRecord(ctx, p)
		Expect(err).NotTo(HaveOccurred())
		return rec.ID
	}

	movie, err := app.Store.CreateMovie(ctx, db.CreateMovieParams{
		Title: "Pointed Flick", OriginalTitle: "Pointed Flick", Year: 2024,
		TmdbID: 881001, Status: entmovie.StatusDownloading,
	})
	Expect(err).NotTo(HaveOccurred())
	// Deleting a title cascades its download records away.
	DeferCleanup(func() {
		Expect(app.Store.DeleteMovie(ctx, movie.ID)).To(Succeed())
	})
	lib.movie = movie.ID
	lib.movieDownload = record(db.CreateDownloadRecordParams{
		Title: "Pointed.Flick.2024.1080p-GRP", MovieID: movie.ID,
	})

	// An episode with no air date reads as unaired, whatever its status.
	aired := time.Now().AddDate(0, -1, 0)
	show, err := app.Store.CreateTVShow(ctx, db.CreateTVShowParams{
		Title: "Pointed Show", Year: 2024, TvdbID: 881002,
		Seasons: []db.SeasonSeed{
			{Number: 1, Episodes: []db.EpisodeSeed{
				{Number: 1, Title: "One", AirDate: &aired},
				{Number: 2, Title: "Two", AirDate: &aired},
				{Number: 3, Title: "Held", AirDate: &aired},
			}},
			{Number: 2, Episodes: []db.EpisodeSeed{
				{Number: 1, Title: "Three", AirDate: &aired},
				{Number: 2, Title: "Four", AirDate: &aired},
			}},
		},
	})
	Expect(err).NotTo(HaveOccurred())
	DeferCleanup(func() {
		Expect(app.Store.DeleteTVShow(ctx, show.ID)).To(Succeed())
	})
	lib.show = show.ID

	status := func(id uint32, s episode.Status) {
		GinkgoHelper()
		Expect(app.Store.SetEpisodeStatus(ctx, id, s)).To(Succeed())
	}
	for _, season := range show.Edges.Seasons {
		ids := map[uint16]uint32{}
		for _, ep := range season.Edges.Episodes {
			ids[ep.Number] = ep.ID
		}
		if season.Number == 2 {
			status(ids[1], episode.StatusDownloading)
			status(ids[2], episode.StatusDownloading)
			lib.packEpisode = ids[2]
			lib.pack = record(db.CreateDownloadRecordParams{
				Title:      "Pointed.Show.S02.1080p-GRP",
				EpisodeID:  ids[1],
				EpisodeIDs: []uint32{ids[1], ids[2]},
			})
			continue
		}
		status(ids[3], episode.StatusAvailable)
		for i, n := range []uint16{1, 2} {
			status(ids[n], episode.StatusDownloading)
			lib.single[i] = record(db.CreateDownloadRecordParams{
				Title:     fmt.Sprintf("Pointed.Show.S01E%02d.1080p-GRP", n),
				EpisodeID: ids[n],
			})
		}
	}
	awaitQueued(lib.movieDownload)
	return lib
}

// A title's page leads to the download behind what it shows: the status
// itself is the link, and only while a download is in flight.
var _ = Describe("Title pages link their downloads", Label("e2e"), Ordered, func() {
	var lib titleLibrary

	BeforeAll(func() { lib = seedTitleLibrary() })

	download := func(id uint32) string {
		return fmt.Sprintf(`a[href="/activity?download=%d"]`, id)
	}
	const anyDownload = `a[href^="/activity?"]`
	// The episode table's row for a code, and the sheet its Details opens.
	row := func(page *rod.Page, code string) *rod.Element {
		GinkgoHelper()
		return page.MustElementR("tbody tr", code)
	}
	openSheet := func(page *rod.Page, code string) *rod.Element {
		GinkgoHelper()
		visibleElement(
			page, fmt.Sprintf(`button[aria-label="Details for %s"]`, code),
		).MustClick()
		return visibleElement(page, `[role=dialog]`)
	}

	It("links a downloading movie's status, on the poster and in Library", func() {
		page := newSessionPage(fmt.Sprintf("/movies/%d", lib.movie))
		// One on the poster's pill, one on the Library panel's Status row.
		Eventually(func() int {
			return len(visibleElements(page, download(lib.movieDownload)))
		}).WithTimeout(5 * time.Second).Should(Equal(2))
		for _, a := range visibleElements(page, download(lib.movieDownload)) {
			Expect(a.MustText()).To(ContainSubstring("Downloading"))
		}

		visibleElements(page, download(lib.movieDownload))[0].MustClick()
		expectPath(page, fmt.Sprintf("/activity?download=%d", lib.movieDownload))
		visibleElement(page, fmt.Sprintf(
			`tr[data-download="%d"][aria-expanded=true]`, lib.movieDownload,
		))
	})

	It("links a season's count, its episodes and their sheet, and nothing "+
		"that has no download", func() {
		// The latest season is the one selected: the pack's.
		page := newSessionPage(fmt.Sprintf("/series/%d?tab=episodes", lib.show))
		pack := download(lib.pack)

		By("a season pack: its count and every episode it covers")
		// The second episode is matched through the record's episode links
		// alone. The phone accordion's twins are in the DOM too, hidden at this
		// width.
		Eventually(func() int { return len(visibleElements(page, pack)) }).
			WithTimeout(5 * time.Second).Should(Equal(3))
		Expect(visibleElements(page, pack)[0].MustText()).
			To(ContainSubstring("2 downloading"))
		for _, code := range []string{"S02E01", "S02E02"} {
			Expect(row(page, code).MustElement(pack).MustText()).
				To(ContainSubstring("Downloading"))
		}
		sheet := openSheet(page, "S02E02")
		Expect(sheet.MustElement(pack).MustText()).
			To(ContainSubstring("Downloading"))
		sheet.MustElement(`button[aria-label=Close]`).MustClick()
		sheet.MustWaitInvisible()

		By("a season waiting on several downloads: the filtered queue")
		// The strip's button, not its hidden twin in the phone accordion.
		for _, b := range visibleElements(page, "button") {
			if strings.HasPrefix(b.MustText(), "Season 01") {
				b.MustClick()
				break
			}
		}
		filtered := fmt.Sprintf("/activity?series=%d&season=1", lib.show)
		count := visibleElement(page, fmt.Sprintf(`a[href="%s"]`, filtered))
		Expect(count.MustText()).To(ContainSubstring("2 downloading"))
		// Each episode still opens its own download.
		for i, code := range []string{"S01E01", "S01E02"} {
			row(page, code).MustElement(download(lib.single[i]))
		}

		By("an available episode: a status and no link, in its row and its sheet")
		held := row(page, "S01E03")
		Expect(held.MustText()).To(ContainSubstring("Available"))
		Expect(held.MustElements(anyDownload)).To(BeEmpty())
		sheet = openSheet(page, "S01E03")
		Expect(sheet.MustText()).To(ContainSubstring("Available"))
		// The sheet reads the queue on its own; give a late link time to show.
		Consistently(func() int { return len(sheet.MustElements(anyDownload)) }).
			WithTimeout(500 * time.Millisecond).Should(BeZero())
		sheet.MustElement(`button[aria-label=Close]`).MustClick()
		sheet.MustWaitInvisible()

		count.MustClick()
		expectPath(page, filtered)
		Expect(visibleElement(page, `button[data-title-filter]`).MustText()).
			To(ContainSubstring("Pointed Show · Season 1"))
		Eventually(func() int {
			return len(visibleElements(page, `tr[data-download]`))
		}).Should(Equal(2))
	})
})
