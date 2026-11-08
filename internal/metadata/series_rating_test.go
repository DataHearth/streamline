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
	tmdbID *uint32 // the TMDB id the decorator passed
}

func (s stubRatings) SeriesRating(
	_ context.Context,
	_, tmdbID uint32,
) (*SeriesRating, error) {
	if s.calls != nil {
		*s.calls++
	}
	if s.tmdbID != nil {
		*s.tmdbID = tmdbID
	}
	return s.rating, s.err
}

var _ = Describe("Series ratings", Label("unit", "metadata"), func() {
	Describe("TMDB.SeriesRating", func() {
		type route struct {
			status int
			body   any
		}
		// serve answers each path from routes and records the paths asked.
		serve := func(routes map[string]route) (*TMDB, *[]string) {
			var asked []string
			ts := httptest.NewServer(
				http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					Expect(
						r.Header.Get("Authorization"),
					).To(Equal("Bearer test-key"))
					asked = append(asked, r.URL.Path)
					if r.URL.Path == "/3/find/81189" {
						Expect(
							r.URL.Query().Get("external_source"),
						).To(Equal("tvdb_id"))
					}
					rt, ok := routes[r.URL.Path]
					if !ok {
						rt = route{
							http.StatusNotFound,
							map[string]any{"status_code": 34},
						}
					}
					w.WriteHeader(rt.status)
					_, _ = w.Write(jsonBytes(rt.body))
				}),
			)
			DeferCleanup(ts.Close)
			return newTestTMDB(ts.URL), &asked
		}
		breakingBad := map[string]any{
			"id":           1396,
			"vote_average": 8.9,
			"vote_count":   15234,
		}

		It("reads the series TVDB maps the show to, without a find", func() {
			c, asked := serve(map[string]route{"/3/tv/1396": {200, breakingBad}})
			r, err := c.SeriesRating(context.Background(), 81189, 1396)
			Expect(err).NotTo(HaveOccurred())
			Expect(
				r,
			).To(Equal(&SeriesRating{TMDBID: 1396, Rating: 8.9, VoteCount: 15234}))
			Expect(*asked).To(Equal([]string{"/3/tv/1396"}))
		})

		It("finds the series by TVDB id when TVDB names no TMDB id", func() {
			c, asked := serve(map[string]route{"/3/find/81189": {
				200,
				map[string]any{
					"movie_results": []any{
						map[string]any{"id": 1, "vote_average": 3.0},
					},
					"tv_results": []any{breakingBad},
					"tv_episode_results": []any{
						map[string]any{"id": 2, "vote_average": 9.9},
					},
				},
			}})
			r, err := c.SeriesRating(context.Background(), 81189, 0)
			Expect(err).NotTo(HaveOccurred())
			Expect(
				r,
			).To(Equal(&SeriesRating{TMDBID: 1396, Rating: 8.9, VoteCount: 15234}))
			Expect(*asked).To(Equal([]string{"/3/find/81189"}))
		})

		It("falls back to the find when TMDB no longer knows TVDB's id", func() {
			c, asked := serve(map[string]route{"/3/find/81189": {200, map[string]any{
				"tv_results": []any{breakingBad},
			}}})
			r, err := c.SeriesRating(context.Background(), 81189, 999)
			Expect(err).NotTo(HaveOccurred())
			Expect(r.TMDBID).To(Equal(uint32(1396)))
			Expect(*asked).To(Equal([]string{"/3/tv/999", "/3/find/81189"}))
		})

		It("answers nil without an error when TMDB knows no such series", func() {
			c, _ := serve(map[string]route{"/3/find/81189": {
				200,
				map[string]any{
					"tv_results": []any{},
					"tv_episode_results": []any{
						map[string]any{"id": 2, "vote_average": 9.9},
					},
				},
			}})
			r, err := c.SeriesRating(context.Background(), 81189, 0)
			Expect(err).NotTo(HaveOccurred())
			Expect(r).To(BeNil())
		})

		It("fails on any other non-200, without falling back", func() {
			c, asked := serve(map[string]route{
				"/3/tv/1396": {
					http.StatusUnauthorized,
					map[string]any{"status_code": 7},
				},
			})
			_, err := c.SeriesRating(context.Background(), 81189, 1396)
			Expect(err).To(MatchError(ContainSubstring("tmdb get series")))
			Expect(*asked).To(Equal([]string{"/3/tv/1396"}))

			c, _ = serve(map[string]route{
				"/3/find/81189": {http.StatusInternalServerError, map[string]any{}},
			})
			_, err = c.SeriesRating(context.Background(), 81189, 0)
			Expect(err).To(MatchError(ContainSubstring("tmdb find series")))
		})

		It("is configured only with a key", func() {
			Expect((&TMDB{apiKey: "k"}).Configured()).To(BeTrue())
			Expect((&TMDB{}).Configured()).To(BeFalse())
		})
	})

	Describe("WithSeriesRatings", func() {
		base := &TVDetails{TVDBID: 81189, Title: "Breaking Bad", TMDBID: 1396}

		It("returns the provider unchanged without a rating source", func() {
			tv := stubTV{details: base}
			Expect(WithSeriesRatings(tv, nil)).To(Equal(TVProvider(tv)))
		})

		It("asks by TVDB's TMDB id and fills the rating and the votes", func() {
			var asked uint32
			tv := WithSeriesRatings(stubTV{details: base}, stubRatings{
				rating: &SeriesRating{TMDBID: 1396, Rating: 8.9, VoteCount: 15234},
				tmdbID: &asked,
			})
			d, err := tv.GetSeries(context.Background(), 81189)
			Expect(err).NotTo(HaveOccurred())
			Expect(asked).To(Equal(uint32(1396)))
			Expect(d.Title).To(Equal("Breaking Bad"))
			Expect(d.RatingFetched).To(BeTrue())
			Expect(d.Rating).To(Equal(float32(8.9)))
			Expect(d.VoteCount).To(Equal(uint32(15234)))
			Expect(d.TMDBID).To(Equal(uint32(1396)))
		})

		It("fills the TMDB id TVDB did not have", func() {
			noTMDB := *base
			noTMDB.TMDBID = 0
			tv := WithSeriesRatings(stubTV{details: &noTMDB}, stubRatings{
				rating: &SeriesRating{TMDBID: 1396, Rating: 8.9, VoteCount: 15234},
			})
			d, err := tv.GetSeries(context.Background(), 81189)
			Expect(err).NotTo(HaveOccurred())
			Expect(d.TMDBID).To(Equal(uint32(1396)))
		})

		It(
			"marks an unknown series fetched with no rating, keeping TVDB's TMDB id",
			func() {
				tv := WithSeriesRatings(stubTV{details: base}, stubRatings{})
				d, err := tv.GetSeries(context.Background(), 81189)
				Expect(err).NotTo(HaveOccurred())
				Expect(d.RatingFetched).To(BeTrue())
				Expect(d.Rating).To(BeZero())
				Expect(d.TMDBID).To(Equal(uint32(1396)))
			},
		)

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
				Expect(d.RatingFailed).To(BeTrue())
				Expect(d.TMDBID).To(Equal(uint32(1396)))
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
