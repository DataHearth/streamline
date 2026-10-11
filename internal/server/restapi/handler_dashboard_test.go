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
	entalbum "github.com/datahearth/streamline/ent/album"
	entbook "github.com/datahearth/streamline/ent/book"
	entepisode "github.com/datahearth/streamline/ent/episode"
	"github.com/datahearth/streamline/ent/mediaevent"
	entmediafile "github.com/datahearth/streamline/ent/mediafile"
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

			It("rejects a limit above the documented maximum", func() {
				resp, err := http.Get(
					app.srv.URL + "/api/v1/activity?limit=2147483647",
				)
				Expect(err).NotTo(HaveOccurred())
				defer resp.Body.Close()
				Expect(resp.StatusCode).To(Equal(http.StatusBadRequest))
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

				app.store.EXPECT().
					ListUpcomingAlbums(mock.Anything, mock.AnythingOfType("time.Time"), mock.AnythingOfType("time.Time")).
					Return([]*ent.Album{}, nil).
					Once()

				app.store.EXPECT().
					ListUpcomingBooks(mock.Anything, mock.AnythingOfType("time.Time"), mock.AnythingOfType("time.Time")).
					Return([]*ent.Book{}, nil).
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

				app.store.EXPECT().
					ListUpcomingAlbums(mock.Anything, mock.AnythingOfType("time.Time"), mock.AnythingOfType("time.Time")).
					Return([]*ent.Album{}, nil).
					Once()

				app.store.EXPECT().
					ListUpcomingBooks(mock.Anything, mock.AnythingOfType("time.Time"), mock.AnythingOfType("time.Time")).
					Return([]*ent.Book{}, nil).
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

			It("returns monitored albums with their artist", func() {
				now := time.Now().UTC()
				future := now.Add(3 * 24 * time.Hour)

				app.store.EXPECT().
					UpcomingReleases(mock.Anything, mock.AnythingOfType("time.Time"), mock.AnythingOfType("time.Time")).
					Return([]*ent.Movie{}, nil).
					Once()
				app.store.EXPECT().
					ListUpcomingEpisodes(mock.Anything, mock.AnythingOfType("time.Time"), mock.AnythingOfType("time.Time")).
					Return([]*ent.Episode{}, nil).
					Once()
				al := &ent.Album{
					ID:          3,
					Title:       "Nevermind",
					ReleaseDate: &future,
					Monitored:   true,
				}
				al.Edges.Artist = &ent.Artist{ID: 7, Name: "Nirvana"}
				app.store.EXPECT().
					ListUpcomingAlbums(mock.Anything, mock.AnythingOfType("time.Time"), mock.AnythingOfType("time.Time")).
					Return([]*ent.Album{al}, nil).
					Once()
				app.store.EXPECT().
					ListUpcomingBooks(mock.Anything, mock.AnythingOfType("time.Time"), mock.AnythingOfType("time.Time")).
					Return([]*ent.Book{}, nil).
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
				Expect(json.NewDecoder(resp.Body).Decode(&body)).To(Succeed())
				Expect(body.Movies).NotTo(BeNil())
				Expect(body.Episodes).NotTo(BeNil())
				Expect(body.Albums).To(HaveLen(1))
				Expect(body.Albums[0].Id).To(Equal(uint32(3)))
				Expect(body.Albums[0].Title).To(Equal("Nevermind"))
				Expect(body.Albums[0].ArtistId).To(Equal(uint32(7)))
				Expect(body.Albums[0].ArtistName).To(Equal("Nirvana"))
			})

			It("returns monitored books with author, series and status", func() {
				now := time.Now().UTC()
				future := now.Add(3 * 24 * time.Hour)

				app.store.EXPECT().
					UpcomingReleases(mock.Anything, mock.AnythingOfType("time.Time"), mock.AnythingOfType("time.Time")).
					Return([]*ent.Movie{}, nil).
					Once()
				app.store.EXPECT().
					ListUpcomingEpisodes(mock.Anything, mock.AnythingOfType("time.Time"), mock.AnythingOfType("time.Time")).
					Return([]*ent.Episode{}, nil).
					Once()
				app.store.EXPECT().
					ListUpcomingAlbums(mock.Anything, mock.AnythingOfType("time.Time"), mock.AnythingOfType("time.Time")).
					Return([]*ent.Album{}, nil).
					Once()
				bk := &ent.Book{
					ID:             4,
					Title:          "Dune",
					AuthorName:     "Frank Herbert",
					ReleaseDate:    &future,
					EbookMonitored: true,
					EbookStatus:    "paused",
				}
				pos := 1.0
				bk.SeriesPosition = &pos
				bk.Edges.Series = &ent.BookSeries{ID: 9, Title: "Dune Chronicles"}
				app.store.EXPECT().
					ListUpcomingBooks(mock.Anything, mock.AnythingOfType("time.Time"), mock.AnythingOfType("time.Time")).
					Return([]*ent.Book{bk}, nil).
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
				Expect(json.NewDecoder(resp.Body).Decode(&body)).To(Succeed())
				Expect(body.Books).To(HaveLen(1))
				Expect(body.Books[0].Id).To(Equal(uint32(4)))
				Expect(body.Books[0].Title).To(Equal("Dune"))
				Expect(body.Books[0].Author).To(Equal("Frank Herbert"))
				Expect(body.Books[0].Status).To(Equal(UpcomingBookStatusPaused))
				Expect(*body.Books[0].SeriesId).To(Equal(uint32(9)))
				Expect(*body.Books[0].SeriesTitle).To(Equal("Dune Chronicles"))
				Expect(*body.Books[0].Position).To(BeNumerically("==", 1))
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

		Describe("GET /api/v1/library/recent", func() {
			It("renders each kind's own fields and asks for 24 by default", func() {
				at := time.Date(2026, 10, 11, 9, 0, 0, 0, time.UTC)
				vol := 3.5
				app.store.EXPECT().
					RecentArrivals(mock.Anything, 24).
					Return([]db.Arrival{
						{
							Kind:  db.ArrivalMovie,
							At:    at,
							ID:    1,
							Title: "Northern Halo",
							Year:  2024,
							Path:  "/m/Northern.Halo.2024.2160p.BluRay.x265-GRP.mkv",
						},
						{
							Kind:  db.ArrivalSeries,
							At:    at,
							ID:    2,
							Title: "The Long Field",
							Addition: &db.Addition{
								At:       at,
								Seasons:  []uint16{1},
								Episodes: []uint16{4},
								Count:    1,
							},
						},
						{
							Kind:        db.ArrivalAlbum,
							At:          at,
							ID:          3,
							Title:       "The Long Quiet",
							ArtistID:    9,
							ArtistName:  "Northbound Static",
							ReleaseType: entalbum.TypeEp,
						},
						{
							Kind:     db.ArrivalBook,
							At:       at,
							ID:       4,
							Title:    "Les Archives du sel",
							Author:   "Iris Calder",
							BookKind: entbook.KindBd,
							Format:   entmediafile.BookKindEbook,
							SeriesID: 8,
							Volume:   &vol,
						},
					}, nil).
					Once()

				resp, err := http.Get(app.srv.URL + "/api/v1/library/recent")
				Expect(err).NotTo(HaveOccurred())
				defer resp.Body.Close()
				Expect(resp.StatusCode).To(Equal(http.StatusOK))

				var body LibraryArrivals
				Expect(json.NewDecoder(resp.Body).Decode(&body)).To(Succeed())
				Expect(body.Items).To(HaveLen(4))
				movie, show, release, vol3 := body.Items[0], body.Items[1], body.Items[2], body.Items[3]
				Expect(movie.Resolution).To(HaveValue(Equal("2160p")))
				Expect(movie.Year).To(HaveValue(Equal(uint16(2024))))
				Expect(show.Addition).NotTo(BeNil())
				Expect(show.Addition.Episodes).To(HaveValue(Equal([]uint16{4})))
				Expect(show.Year).To(BeNil())
				Expect(release.ArtistName).To(HaveValue(Equal("Northbound Static")))
				Expect(
					release.ReleaseType,
				).To(HaveValue(Equal(LibraryArrivalReleaseTypeEp)))
				Expect(vol3.Format).To(HaveValue(Equal(LibraryArrivalFormatEbook)))
				Expect(vol3.SeriesId).To(HaveValue(Equal(uint32(8))))
				Expect(vol3.Volume).To(HaveValue(Equal(3.5)))
				Expect(vol3.Resolution).To(BeNil())
			})

			DescribeTable("rejects a limit outside [1, 100]",
				func(limit string) {
					resp, err := http.Get(
						app.srv.URL + "/api/v1/library/recent?limit=" + limit,
					)
					Expect(err).NotTo(HaveOccurred())
					defer resp.Body.Close()
					Expect(resp.StatusCode).To(Equal(http.StatusBadRequest))
				},
				Entry("zero", "0"),
				Entry("above the ceiling", "101"),
			)
		})
	},
)
