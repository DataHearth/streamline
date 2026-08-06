// Package fakes hosts in-process HTTP fakes for external services the e2e
// suites must never reach for real.
package fakes

import (
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"net/http/httptest"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// TMDB serves the subset of the TMDB v3 API the metadata client consumes.
// One canned movie is enough: the e2e specs assert plumbing, not catalog
// breadth.
type TMDB struct {
	URL string
}

// The canned movie, exported so specs can assert what the fake — and only the
// fake — serves.
const (
	MovieTMDBID      = 550
	MovieTitle       = "Fight Club"
	MovieOverview    = "A ticking-time-bomb insomniac."
	MovieReleaseDate = "1999-10-15"
	MovieYear        = 1999
	MovieRuntime     = 139
	MovieRating      = 8.4
	MovieGenre       = "Drama"
	MovieCastName    = "Brad Pitt"
	MoviePosterPath  = "/fake-poster.jpg"
)

var (
	// movieDetail deliberately carries no poster_path: metadata.PosterURL pins
	// the poster host to image.tmdb.org with no override seam, so a non-empty
	// path would send movie.Add's background poster fetch to the real CDN.
	movieDetail = map[string]any{
		"id":             MovieTMDBID,
		"title":          MovieTitle,
		"original_title": MovieTitle,
		"overview":       MovieOverview,
		"release_date":   MovieReleaseDate,
		"runtime":        MovieRuntime,
		"vote_average":   MovieRating,
		"vote_count":     27000,
		"status":         "Released",
		"imdb_id":        "tt0137523",
		"genres":         []any{map[string]any{"id": 18, "name": MovieGenre}},
	}

	movieCredits = map[string]any{
		"cast": []any{
			map[string]any{
				"id":        287,
				"name":      MovieCastName,
				"character": "Tyler Durden",
				"order":     0,
			},
		},
	}

	movieSummary = map[string]any{
		"id":             MovieTMDBID,
		"title":          MovieTitle,
		"original_title": MovieTitle,
		"overview":       MovieOverview,
		"release_date":   MovieReleaseDate,
		"poster_path":    MoviePosterPath,
	}
)

func NewTMDB() *TMDB {
	GinkgoHelper()

	detail := marshal(movieDetail)
	credited := maps.Clone(movieDetail)
	credited["credits"] = movieCredits
	detailWithCredits := marshal(credited)
	found := marshal(searchPage([]any{movieSummary}))
	empty := marshal(searchPage([]any{}))

	mux := http.NewServeMux()
	mux.HandleFunc(
		"GET /3/search/movie",
		func(w http.ResponseWriter, r *http.Request) {
			defer GinkgoRecover()
			body := empty
			if matchesTitle(r.URL.Query().Get("query")) {
				body = found
			}
			writeJSON(w, body)
		},
	)
	mux.HandleFunc(
		fmt.Sprintf("GET /3/movie/%d", MovieTMDBID),
		func(w http.ResponseWriter, r *http.Request) {
			defer GinkgoRecover()
			body := detail
			if strings.Contains(
				r.URL.Query().Get("append_to_response"),
				"credits",
			) {
				body = detailWithCredits
			}
			writeJSON(w, body)
		},
	)
	mux.Handle(
		fmt.Sprintf("GET /3/movie/%d/release_dates", MovieTMDBID),
		jsonHandler(marshal(map[string]any{
			"id":      MovieTMDBID,
			"results": []any{},
		})),
	)
	mux.Handle(
		fmt.Sprintf("GET /3/movie/%d/recommendations", MovieTMDBID),
		jsonHandler(marshal(searchPage([]any{}))),
	)

	srv := httptest.NewServer(mux)
	DeferCleanup(srv.Close)
	return &TMDB{URL: srv.URL}
}

func matchesTitle(query string) bool {
	if query == "" {
		return false
	}
	return strings.Contains(
		strings.ToLower(MovieTitle),
		strings.ToLower(query),
	)
}

func searchPage(results []any) map[string]any {
	return map[string]any{
		"page":          1,
		"results":       results,
		"total_pages":   1,
		"total_results": len(results),
	}
}

func marshal(payload any) []byte {
	GinkgoHelper()
	body, err := json.Marshal(payload)
	Expect(err).NotTo(HaveOccurred())
	return body
}

func jsonHandler(body []byte) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		defer GinkgoRecover()
		writeJSON(w, body)
	})
}

func writeJSON(w http.ResponseWriter, body []byte) {
	w.Header().Set("Content-Type", "application/json")
	_, err := w.Write(body)
	Expect(err).NotTo(HaveOccurred())
}
