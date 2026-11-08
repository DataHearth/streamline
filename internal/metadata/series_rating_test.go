package metadata

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// stubTV answers GetSeries with a fixed record; the other methods are never
// reached through ratedTV.
type stubTV struct {
	TVProvider
	details *TVDetails
	err     error
}

func (s stubTV) GetSeries(context.Context, uint32) (*TVDetails, error) {
	if s.err != nil {
		return nil, s.err
	}
	d := *s.details
	return &d, nil
}

type stubRatings struct {
	rating *SeriesRating
	err    error
	calls  *int
}

func (s stubRatings) SeriesRating(context.Context, uint32) (*SeriesRating, error) {
	if s.calls != nil {
		*s.calls++
	}
	return s.rating, s.err
}

var _ = Describe("Series ratings", Label("unit", "metadata"), func() {
	Describe("TMDB.SeriesRating", func() {
		serve := func(status int, body any) *TMDB {
			ts := httptest.NewServer(
				http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					Expect(r.URL.Path).To(Equal("/3/find/81189"))
					Expect(r.URL.Query().Get("external_source")).To(Equal("tvdb_id"))
					Expect(
						r.Header.Get("Authorization"),
					).To(Equal("Bearer test-key"))
					w.WriteHeader(status)
					_, _ = w.Write(jsonBytes(body))
				}),
			)
			DeferCleanup(ts.Close)
			return newTestTMDB(ts.URL)
		}

		It("reads the first TV result's vote average, votes and id", func() {
			c := serve(http.StatusOK, map[string]any{
				"movie_results": []any{map[string]any{"id": 1, "vote_average": 3.0}},
				"tv_results": []any{
					map[string]any{
						"id":           1396,
						"vote_average": 8.9,
						"vote_count":   15234,
					},
				},
				"tv_episode_results": []any{
					map[string]any{"id": 2, "vote_average": 9.9},
				},
			})
			r, err := c.SeriesRating(context.Background(), 81189)
			Expect(err).NotTo(HaveOccurred())
			Expect(
				r,
			).To(Equal(&SeriesRating{TMDBID: 1396, Rating: 8.9, VoteCount: 15234}))
		})

		It("answers nil without an error when TMDB knows no such series", func() {
			c := serve(http.StatusOK, map[string]any{
				"tv_results": []any{},
				"tv_episode_results": []any{
					map[string]any{"id": 2, "vote_average": 9.9},
				},
			})
			r, err := c.SeriesRating(context.Background(), 81189)
			Expect(err).NotTo(HaveOccurred())
			Expect(r).To(BeNil())
		})

		It("fails on a non-200", func() {
			c := serve(http.StatusUnauthorized, map[string]any{"status_code": 7})
			_, err := c.SeriesRating(context.Background(), 81189)
			Expect(err).To(MatchError(ContainSubstring("tmdb find series")))
		})

		It("is configured only with a key", func() {
			Expect((&TMDB{apiKey: "k"}).Configured()).To(BeTrue())
			Expect((&TMDB{}).Configured()).To(BeFalse())
		})
	})

	Describe("WithSeriesRatings", func() {
		base := &TVDetails{TVDBID: 81189, Title: "Breaking Bad"}

		It("returns the provider unchanged without a rating source", func() {
			tv := stubTV{details: base}
			Expect(WithSeriesRatings(tv, nil)).To(Equal(TVProvider(tv)))
		})

		It("fills the rating, the votes and the TMDB id", func() {
			tv := WithSeriesRatings(stubTV{details: base}, stubRatings{
				rating: &SeriesRating{TMDBID: 1396, Rating: 8.9, VoteCount: 15234},
			})
			d, err := tv.GetSeries(context.Background(), 81189)
			Expect(err).NotTo(HaveOccurred())
			Expect(d.Title).To(Equal("Breaking Bad"))
			Expect(d.RatingFetched).To(BeTrue())
			Expect(d.Rating).To(Equal(float32(8.9)))
			Expect(d.VoteCount).To(Equal(uint32(15234)))
			Expect(d.TMDBID).To(Equal(uint32(1396)))
		})

		It("marks an unknown series fetched with no rating", func() {
			tv := WithSeriesRatings(stubTV{details: base}, stubRatings{})
			d, err := tv.GetSeries(context.Background(), 81189)
			Expect(err).NotTo(HaveOccurred())
			Expect(d.RatingFetched).To(BeTrue())
			Expect(d.Rating).To(BeZero())
			Expect(d.TMDBID).To(BeZero())
		})

		It(
			"still answers when the rating cannot be fetched, marked not fetched",
			func() {
				tv := WithSeriesRatings(
					stubTV{details: base},
					stubRatings{err: errors.New("down")},
				)
				d, err := tv.GetSeries(context.Background(), 81189)
				Expect(err).NotTo(HaveOccurred())
				Expect(d.Title).To(Equal("Breaking Bad"))
				Expect(d.RatingFetched).To(BeFalse())
			},
		)

		It("does not ask for a rating when TVDB fails", func() {
			calls := 0
			tv := WithSeriesRatings(
				stubTV{err: errors.New("tvdb down")},
				stubRatings{calls: &calls},
			)
			_, err := tv.GetSeries(context.Background(), 81189)
			Expect(err).To(MatchError("tvdb down"))
			Expect(calls).To(BeZero())
		})
	})
})
