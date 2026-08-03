package web

import (
	"fmt"
	"strconv"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/e2e/fakes"
)

var _ = Describe("Movies page", Label("e2e"), func() {
	It("renders the empty-library state", func() {
		page := newSessionPage("/movies")
		Expect(page.MustElementR("h1", "^Movies$").MustText()).To(Equal("Movies"))
		Expect(page.MustElementR("p", "Your library is empty").MustText()).
			To(ContainSubstring("Your library is empty"))
		Expect(page.MustElement(`nav[aria-label="Movie status"]`).MustText()).
			To(ContainSubstring("All"))
	})

	// No search term is typed: the modal's TMDB query needs 2+ characters, and
	// this suite must never reach out to the network.
	It("opens and closes the add-movie modal from the toolbar", func() {
		page := newSessionPage("/movies")
		page.MustElementR("button", "Add movie").MustClick()

		modal := page.MustElement(`div[role=dialog]`)
		Expect(modal.MustElement("h2").MustText()).To(Equal("Add movie"))
		modal.MustElement(`input[aria-label="Search TMDB by title"]`)
		Expect(modal.MustText()).To(ContainSubstring("Quality profile"))

		modal.MustElement(`button[aria-label=Close]`).MustClick()
		modal.MustWaitInvisible()
	})

	It("filters to a status tab and clears back to the library view", func() {
		page := newSessionPage("/movies")
		page.MustElement(`nav[aria-label="Movie status"]`).
			MustElementR("button", "Wanted").MustClick()

		Expect(page.MustElementR("p", "No movies match this view").MustText()).
			To(ContainSubstring("No movies match this view"))
		expectPath(page, "/movies?status=wanted")

		page.MustElementR("button", "Clear filters").MustClick()
		Expect(page.MustElementR("p", "Your library is empty").MustText()).
			To(ContainSubstring("Your library is empty"))
		expectPath(page, "/movies")
	})
})

// Every metadata value asserted here comes from the in-process TMDB fake, so
// the specs pin the SPA's rendering of it rather than TMDB's catalog.
var _ = Describe("Movies page with a populated library", Label("e2e"), func() {
	It("renders the seeded movie in the library grid", func() {
		id := seedMovie()
		page := newSessionPage("/movies")

		card := page.MustElement(fmt.Sprintf("#poster-card-%d", id))
		Expect(card.MustText()).To(SatisfyAll(
			ContainSubstring(fakes.MovieTitle),
			ContainSubstring(strconv.Itoa(fakes.MovieYear)),
			ContainSubstring("Wanted"),
		))
		// The fake serves no poster_path, so nothing is ever cached and /posters
		// 404s. Poster.svelte answers that by dropping its <img> and re-adding
		// it on a backoff, so the absence is polled for rather than read once.
		Eventually(func() int {
			return len(page.MustElements(fmt.Sprintf("#poster-card-%d img", id)))
		}).
			WithTimeout(5 * time.Second).
			WithPolling(50 * time.Millisecond).
			Should(BeZero())
	})

	It("opens the movie detail page from the grid", func() {
		id := seedMovie()
		page := newSessionPage("/movies")

		page.MustElement(fmt.Sprintf("#poster-card-%d a", id)).MustClick()

		expectPath(page, fmt.Sprintf("/movies/%d", id))
		Expect(page.MustElement("h1#movie-title").MustText()).
			To(Equal(fakes.MovieTitle))
	})

	It("renders the canned TMDB metadata on the detail page", func() {
		id := seedMovie()
		page := newSessionPage(fmt.Sprintf("/movies/%d", id))

		// Waits out the loading skeleton: it renders a section of its own that
		// carries no aria-labelledby.
		hero := page.MustElement(`section[aria-labelledby="movie-title"]`)
		Expect(hero.MustElement("h1").MustText()).To(Equal(fakes.MovieTitle))
		Expect(hero.MustText()).To(SatisfyAll(
			ContainSubstring("Wanted"),
			ContainSubstring(strconv.Itoa(fakes.MovieYear)),
			ContainSubstring(fmt.Sprintf("%dm", fakes.MovieRuntime)),
			ContainSubstring(strconv.FormatFloat(fakes.MovieRating, 'f', 1, 64)),
			ContainSubstring(fakes.MovieGenre),
			ContainSubstring(fakes.MovieOverview),
		))
		// The hero holds two images of the same missing poster: the blurred
		// backdrop, a plain <img> that never retries, and the poster frame's
		// Poster.svelte, which drops itself on the 404. Settling at one is what
		// distinguishes the poster having gone from the hero never having
		// rendered it.
		Eventually(func() int { return len(hero.MustElements("img")) }).
			WithTimeout(5 * time.Second).
			WithPolling(50 * time.Millisecond).
			Should(Equal(1))

		synopsis := page.MustElement(`section[aria-labelledby="detail-synopsis"]`)
		Expect(synopsis.MustText()).To(SatisfyAll(
			ContainSubstring(fakes.MovieOverview),
			ContainSubstring(fakes.MovieCastName),
		))
	})
})
