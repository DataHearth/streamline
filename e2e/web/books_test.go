package web

import (
	"context"
	"fmt"
	"net/http"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/internal/db"
)

// seedSeries writes a series of hydrated volumes straight into the store: the
// suite runs without a Hardcover key, so the add flow cannot reach one.
func seedSeries(hardcoverID uint32, slug string, volumes int) uint32 {
	GinkgoHelper()
	now := time.Now()
	seeds := make([]db.BookSeed, 0, volumes)
	for i := range volumes {
		position := float64(i + 1)
		hc := hardcoverID*100 + uint32(i) + 1
		seeds = append(seeds, db.BookSeed{
			HardcoverID:       hc,
			Title:             fmt.Sprintf("Volume %d", i+1),
			AuthorName:        "Isaac Asimov",
			Kind:              "novel",
			PreferredLanguage: "en",
			SeriesPosition:    &position,
			EbookMonitored:    true,
			RefreshedAt:       &now,
			Editions: []db.EditionSeed{{
				HardcoverID: hc*10 + 1,
				Language:    "en",
				Title:       fmt.Sprintf("Volume %d", i+1),
				Publisher:   "Spectra",
				Format:      "ebook",
				Popularity:  1,
			}},
		})
	}
	s, _, err := app.Store.CreateSeries(context.Background(), db.CreateSeriesParams{
		HardcoverID:     hardcoverID,
		Slug:            slug,
		Title:           "Foundation",
		AuthorName:      "Isaac Asimov",
		Kind:            "novel",
		Monitor:         "all",
		EditionLanguage: "en",
		RefreshedAt:     now,
		Volumes:         seeds,
	})
	Expect(err).NotTo(HaveOccurred())
	DeferCleanup(func() {
		Expect(apiDo(
			http.MethodDelete,
			fmt.Sprintf("/api/v1/books/series/%d", s.ID),
			nil,
			nil,
		)).To(Equal(http.StatusNoContent))
	})
	return s.ID
}

var _ = Describe("Book series covers", Label("e2e"), func() {
	It("fans the first three volumes on the shelf card and the series page", func() {
		id := seedSeries(5450, "foundation", 4)

		shelf := newSessionPage("/books")
		card := visibleElement(shelf, fmt.Sprintf(`a[href="/books/series/%d"]`, id))
		Expect(
			card.MustElements("[data-series-cover] [data-cover-sheet]"),
		).To(HaveLen(3))

		page := newSessionPage(fmt.Sprintf("/books/series/%d", id))
		hero := visibleElement(page, "[data-series-cover]")
		Expect(hero.MustElements("[data-cover-sheet]")).To(HaveLen(3))
		link := visibleElement(
			page,
			`a[href="https://hardcover.app/series/foundation"]`,
		)
		Expect(link.MustText()).To(ContainSubstring("5450"))
	})

	It(
		"draws one cover and no Hardcover link for a one-volume series without a slug",
		func() {
			id := seedSeries(39538, "", 1)

			page := newSessionPage(fmt.Sprintf("/books/series/%d", id))
			hero := visibleElement(page, "[data-series-cover]")
			Expect(hero.MustElements("[data-cover-sheet]")).To(HaveLen(1))
			Expect(
				page.MustElements(`a[href^="https://hardcover.app/series/"]`),
			).To(BeEmpty())
		},
	)
})
