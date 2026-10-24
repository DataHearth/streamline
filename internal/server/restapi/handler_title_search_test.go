package restapi

import (
	"encoding/json"
	"net/http"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/stretchr/testify/mock"

	"github.com/datahearth/streamline/ent"
	entalbum "github.com/datahearth/streamline/ent/album"
	"github.com/datahearth/streamline/ent/tvshow"
	"github.com/datahearth/streamline/internal/db"
)

var _ = Describe(
	"Handler: title search",
	Label("unit", "server", "search"),
	func() {
		var app *apiKeyApp

		BeforeEach(func() {
			app = newAPIKeyApp()
		})

		get := func(path string, out any) int {
			GinkgoHelper()
			resp := app.do(app.req(http.MethodGet, path, app.adminKey, nil))
			defer resp.Body.Close()
			Expect(json.NewDecoder(resp.Body).Decode(out)).To(Succeed())
			return resp.StatusCode
		}

		Describe("ListMusicAlbums", func() {
			It("rejects a limit outside [1, 100] with a JSON 400", func() {
				var body Error
				Expect(get("/api/v1/music/albums?limit=101", &body)).
					To(Equal(http.StatusBadRequest))
				Expect(body.Message).To(Equal("limit must be between 1 and 100"))
			})

			It("passes the query through and names each album's artist", func() {
				released := time.Date(1971, 9, 9, 0, 0, 0, 0, time.UTC)
				app.store.EXPECT().
					SearchAlbums(mock.Anything, db.TitleSearchParams{
						Query: "imagine", Limit: 5, Offset: 10,
					}).
					Return([]*ent.Album{
						{
							ID:          41,
							Title:       "Imagine",
							Type:        entalbum.TypeAlbum,
							ReleaseDate: &released,
							Edges: ent.AlbumEdges{
								Artist: &ent.Artist{ID: 7, Name: "John Lennon"},
							},
						},
						{ID: 42, Title: "Imagine (Demo)", Type: entalbum.TypeSingle},
					}, 2, nil).
					Once()

				var body AlbumSearchList
				Expect(
					get(
						"/api/v1/music/albums?query=imagine&limit=5&offset=10",
						&body,
					),
				).
					To(Equal(http.StatusOK))
				Expect(body.Total).To(Equal(uint32(2)))
				Expect(body.Limit).To(Equal(uint16(5)))
				Expect(body.Offset).To(Equal(uint32(10)))
				Expect(body.Items).To(HaveLen(2))
				Expect(body.Items[0].Id).To(Equal(uint32(41)))
				Expect(body.Items[0].ArtistId).To(Equal(uint32(7)))
				Expect(body.Items[0].ArtistName).To(Equal("John Lennon"))
				Expect(body.Items[0].ReleaseDate).NotTo(BeNil())
				Expect(body.Items[0].ReleaseDate.Time.Year()).To(Equal(1971))
				// An undated album leaves release_date out rather than
				// claiming the zero date.
				Expect(body.Items[1].ReleaseDate).To(BeNil())
				Expect(body.Items[1].Type).To(Equal(MusicAlbumTypeSingle))
			})
		})

		Describe("ListEpisodes", func() {
			It("rejects an explicit limit=0 with a JSON 400", func() {
				var body Error
				Expect(get("/api/v1/episodes?limit=0", &body)).
					To(Equal(http.StatusBadRequest))
				Expect(body.Message).To(Equal("limit must be between 1 and 100"))
			})

			It("names each episode's show and season, and drops an orphan", func() {
				aired := time.Date(2024, 11, 17, 0, 0, 0, 0, time.UTC)
				show := &ent.TVShow{
					ID:    19,
					Title: "Dune: Prophecy",
					Type:  tvshow.TypeStandard,
				}
				app.store.EXPECT().
					SearchEpisodes(mock.Anything, db.TitleSearchParams{
						Query: "hidden", Limit: 20,
					}).
					Return([]*ent.Episode{
						{
							ID:      301,
							Number:  1,
							Title:   "The Hidden Hand",
							AirDate: aired,
							Edges: ent.EpisodeEdges{Season: &ent.Season{
								Number: 1,
								Edges:  ent.SeasonEdges{TvShow: show},
							}},
						},
						{ID: 302, Number: 2, Title: "Hidden Away"},
					}, 2, nil).
					Once()

				var body EpisodeSearchList
				Expect(get("/api/v1/episodes?query=hidden", &body)).
					To(Equal(http.StatusOK))
				Expect(body.Total).To(Equal(uint32(2)))
				Expect(body.Items).To(HaveLen(1))
				hit := body.Items[0]
				Expect(hit.Id).To(Equal(uint32(301)))
				Expect(hit.SeriesId).To(Equal(uint32(19)))
				Expect(hit.SeriesTitle).To(Equal("Dune: Prophecy"))
				Expect(hit.SeriesType).To(Equal(EpisodeSearchHitSeriesTypeStandard))
				Expect(hit.Season).To(Equal(uint16(1)))
				Expect(hit.Episode).To(Equal(uint16(1)))
				Expect(hit.AirDate).To(HaveValue(BeTemporally("==", aired)))
			})
		})
	},
)
