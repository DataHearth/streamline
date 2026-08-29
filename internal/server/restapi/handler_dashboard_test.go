package restapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/stretchr/testify/mock"

	"github.com/datahearth/streamline/ent"
	entepisode "github.com/datahearth/streamline/ent/episode"
	"github.com/datahearth/streamline/ent/mediaevent"
	entmovie "github.com/datahearth/streamline/ent/movie"
	"github.com/datahearth/streamline/internal/db"
)

var _ = Describe(
	"Handler: Dashboard",
	Label("unit", "server", "dashboard"),
	func() {
		var app *apiKeyApp

		BeforeEach(func() {
			app = newAPIKeyApp()
		})

		Describe("GET /api/v1/activity", func() {
			It("returns events newest-first with the eager-loaded movie", func() {
				m := &ent.Movie{
					ID: 1, Title: "Anora", Year: 2024, TmdbID: 1064213,
				}
				now := time.Now()
				e1 := &ent.MediaEvent{
					ID: 1, Type: mediaevent.TypeGrabbed, CreateTime: now,
				}
				e1.Edges.Movie = m
				e2 := &ent.MediaEvent{
					ID:         2,
					Type:       mediaevent.TypeImported,
					CreateTime: now.Add(time.Second),
				}
				e2.Edges.Movie = m

				app.store.EXPECT().
					RecentActivity(mock.Anything, mock.AnythingOfType("db.ActivityFilter")).
					Return(&db.ActivityResult{
						Events: []*ent.MediaEvent{e2, e1},
					}, nil).
					Once()

				resp, err := http.Get(app.srv.URL + "/api/v1/activity?limit=10")
				Expect(err).NotTo(HaveOccurred())
				defer resp.Body.Close()
				Expect(resp.StatusCode).To(Equal(http.StatusOK))

				var body ActivityList
				Expect(
					json.NewDecoder(resp.Body).Decode(&body),
				).To(Succeed())
				Expect(body.Events).To(HaveLen(2))
				Expect(body.Events[0].Type).To(Equal(ActivityEventType("imported")))
				Expect(body.Events[0].Movie.Title).To(Equal("Anora"))
			})

			It("clamps a limit above the documented maximum", func() {
				app.store.EXPECT().
					RecentActivity(mock.Anything, mock.MatchedBy(func(f db.ActivityFilter) bool {
						return f.Limit == activityMaxLimit
					})).
					Return(&db.ActivityResult{}, nil).
					Once()

				resp, err := http.Get(
					app.srv.URL + "/api/v1/activity?limit=2147483647",
				)
				Expect(err).NotTo(HaveOccurred())
				defer resp.Body.Close()
				Expect(resp.StatusCode).To(Equal(http.StatusOK))
			})

			It(
				"forwards series_id, so a series feed is not the whole library",
				func() {
					app.store.EXPECT().
						RecentActivity(mock.Anything, mock.MatchedBy(func(f db.ActivityFilter) bool {
							return f.SeriesID != nil && *f.SeriesID == 118
						})).
						Return(&db.ActivityResult{}, nil).
						Once()

					resp, err := http.Get(
						app.srv.URL + "/api/v1/activity?series_id=118",
					)
					Expect(err).NotTo(HaveOccurred())
					defer resp.Body.Close()
					Expect(resp.StatusCode).To(Equal(http.StatusOK))
				},
			)

			It("400s on a malformed cursor", func() {
				app.store.EXPECT().
					RecentActivity(mock.Anything, mock.AnythingOfType("db.ActivityFilter")).
					Return(nil, fmt.Errorf("recent activity: decode cursor: bad input")).
					Once()

				resp, err := http.Get(
					app.srv.URL + "/api/v1/activity?cursor=not-base64!",
				)
				Expect(err).NotTo(HaveOccurred())
				defer resp.Body.Close()
				Expect(resp.StatusCode).To(Equal(http.StatusBadRequest))
			})
		})

		Describe("GET /api/v1/calendar/upcoming", func() {
			It("returns wanted movies + upcoming episodes in [from,to)", func() {
				now := time.Now().UTC()
				future := now.Add(3 * 24 * time.Hour)

				app.store.EXPECT().
					UpcomingReleases(mock.Anything, mock.AnythingOfType("time.Time"), mock.AnythingOfType("time.Time")).
					Return([]*ent.Movie{
						{
							ID:                 10,
							Title:              "Coming Soon",
							Year:               2025,
							TmdbID:             101,
							Status:             entmovie.StatusWanted,
							DigitalReleaseDate: &future,
						},
					}, nil).
					Once()

				ep := &ent.Episode{
					ID:        5,
					Number:    3,
					AirDate:   future,
					Monitored: true,
				}
				season := &ent.Season{Number: 2}
				season.Edges.TvShow = &ent.TVShow{ID: 7, Title: "Show"}
				ep.Edges.Season = season
				app.store.EXPECT().
					ListUpcomingEpisodes(mock.Anything, mock.AnythingOfType("time.Time"), mock.AnythingOfType("time.Time")).
					Return([]*ent.Episode{ep}, nil).
					Once()

				q := url.Values{}
				q.Set("from", now.Format(time.RFC3339))
				q.Set("to", now.Add(7*24*time.Hour).Format(time.RFC3339))
				resp, err := http.Get(
					app.srv.URL + "/api/v1/calendar/upcoming?" + q.Encode(),
				)
				Expect(err).NotTo(HaveOccurred())
				defer resp.Body.Close()
				Expect(resp.StatusCode).To(Equal(http.StatusOK))

				var body UpcomingList
				Expect(
					json.NewDecoder(resp.Body).Decode(&body),
				).To(Succeed())
				Expect(body.Movies).To(HaveLen(1))
				Expect(body.Movies[0].Title).To(Equal("Coming Soon"))
				Expect(body.Episodes).To(HaveLen(1))
				Expect(body.Episodes[0].SeriesTitle).To(Equal("Show"))
				Expect(body.Episodes[0].Season).To(Equal(uint16(2)))
				Expect(body.Episodes[0].Episode).To(Equal(uint16(3)))
				// No file and an air date still ahead — the calendar pill must
				// read "unaired", not the stored enum value.
				Expect(body.Episodes[0].Status).To(Equal(EpisodeStatusUnaired))
			})

			It("reports an already-downloaded episode by its real status", func() {
				now := time.Now().UTC()
				future := now.Add(3 * 24 * time.Hour)

				app.store.EXPECT().
					UpcomingReleases(mock.Anything, mock.AnythingOfType("time.Time"), mock.AnythingOfType("time.Time")).
					Return([]*ent.Movie{}, nil).
					Once()

				ep := &ent.Episode{
					ID:        5,
					Number:    3,
					AirDate:   future,
					Monitored: true,
					Status:    entepisode.StatusDownloading,
				}
				season := &ent.Season{Number: 2}
				season.Edges.TvShow = &ent.TVShow{ID: 7, Title: "Show"}
				ep.Edges.Season = season
				ep.Edges.MediaFiles = []*ent.MediaFile{{ID: 1}}
				app.store.EXPECT().
					ListUpcomingEpisodes(mock.Anything, mock.AnythingOfType("time.Time"), mock.AnythingOfType("time.Time")).
					Return([]*ent.Episode{ep}, nil).
					Once()

				q := url.Values{}
				q.Set("from", now.Format(time.RFC3339))
				q.Set("to", now.Add(7*24*time.Hour).Format(time.RFC3339))
				resp, err := http.Get(
					app.srv.URL + "/api/v1/calendar/upcoming?" + q.Encode(),
				)
				Expect(err).NotTo(HaveOccurred())
				defer resp.Body.Close()

				var body UpcomingList
				Expect(json.NewDecoder(resp.Body).Decode(&body)).To(Succeed())
				Expect(body.Episodes).To(HaveLen(1))
				Expect(body.Episodes[0].Status).
					To(Equal(EpisodeStatusDownloading))
			})

			It("400s when from is after to", func() {
				now := time.Now().UTC()
				q := url.Values{}
				q.Set("from", now.Format(time.RFC3339))
				q.Set("to", now.Add(-time.Hour).Format(time.RFC3339))
				resp, err := http.Get(
					app.srv.URL + "/api/v1/calendar/upcoming?" + q.Encode(),
				)
				Expect(err).NotTo(HaveOccurred())
				defer resp.Body.Close()
				Expect(resp.StatusCode).To(Equal(http.StatusBadRequest))
			})
		})
	},
)
