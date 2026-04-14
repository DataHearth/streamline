package restapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/stretchr/testify/mock"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/internal/indexer"
	"github.com/datahearth/streamline/internal/media/tvshow"
	"github.com/datahearth/streamline/internal/metadata"
)

var _ = Describe("Handler: Series", Label("unit", "server", "series"), func() {
	var app *apiKeyApp

	BeforeEach(func() {
		app = newAPIKeyApp()
	})

	Describe("ListSeries", func() {
		It("returns a paginated list", func() {
			app.tvshows.EXPECT().
				FilterList(mock.Anything, mock.AnythingOfType("tvshow.FilterParams")).
				Return([]*ent.TVShow{{ID: 1, Title: "X", Year: 2020, TvdbID: 9}}, uint32(1), nil).
				Once()

			resp := app.do(
				app.req(
					http.MethodGet,
					"/api/v1/series?page=1&limit=20",
					app.adminKey,
					nil,
				),
			)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusOK))

			var body struct {
				Total int `json:"total"`
				Items []struct {
					Title string `json:"title"`
				} `json:"items"`
			}
			Expect(json.NewDecoder(resp.Body).Decode(&body)).To(Succeed())
			Expect(body.Total).To(Equal(1))
			Expect(body.Items).To(HaveLen(1))
			Expect(body.Items[0].Title).To(Equal("X"))
		})

		It("500s when the service errors", func() {
			app.tvshows.EXPECT().
				FilterList(mock.Anything, mock.AnythingOfType("tvshow.FilterParams")).
				Return(nil, uint32(0), errors.New("db down")).Once()

			resp := app.do(
				app.req(http.MethodGet, "/api/v1/series", app.adminKey, nil),
			)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusInternalServerError))
		})
	})

	Describe("GetSeries", func() {
		It("404s when the series is missing", func() {
			app.tvshows.EXPECT().Get(mock.Anything, uint32(42)).
				Return(nil, errors.New("tv show 42 not found")).Once()

			resp := app.do(
				app.req(http.MethodGet, "/api/v1/series/42", app.adminKey, nil),
			)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusNotFound))
		})

		It("attaches live cast from TVDB", func() {
			app.tvshows.EXPECT().Get(mock.Anything, uint32(1)).
				Return(&ent.TVShow{ID: 1, Title: "Breaking Bad", Year: 2008, TvdbID: 81189}, nil).
				Once()
			app.metadataTV.EXPECT().GetSeriesCast(mock.Anything, uint32(81189)).
				Return([]metadata.CastMember{{Name: "Bryan Cranston", Character: "Walter White"}}, nil).
				Once()

			resp := app.do(
				app.req(http.MethodGet, "/api/v1/series/1", app.adminKey, nil),
			)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusOK))

			var body struct {
				Cast []struct {
					Name      string `json:"name"`
					Character string `json:"character"`
				} `json:"cast"`
			}
			Expect(json.NewDecoder(resp.Body).Decode(&body)).To(Succeed())
			Expect(body.Cast).To(HaveLen(1))
			Expect(body.Cast[0].Name).To(Equal("Bryan Cranston"))
			Expect(body.Cast[0].Character).To(Equal("Walter White"))
		})

		It("still returns 200 when the cast fetch fails", func() {
			app.tvshows.EXPECT().Get(mock.Anything, uint32(1)).
				Return(&ent.TVShow{ID: 1, Title: "Breaking Bad", Year: 2008, TvdbID: 81189}, nil).
				Once()
			app.metadataTV.EXPECT().GetSeriesCast(mock.Anything, uint32(81189)).
				Return(nil, errors.New("tvdb down")).Once()

			resp := app.do(
				app.req(http.MethodGet, "/api/v1/series/1", app.adminKey, nil),
			)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusOK))
		})
	})

	Describe("GetSeriesCounts", func() {
		It("maps the service counts", func() {
			app.tvshows.EXPECT().Counts(mock.Anything).
				Return(tvshow.Counts{Total: 5, Continuing: 3, Ended: 2, WantedEpisodes: 7}, nil).
				Once()

			resp := app.do(
				app.req(http.MethodGet, "/api/v1/series/counts", app.adminKey, nil),
			)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusOK))

			var body struct {
				Total          int `json:"total"`
				Continuing     int `json:"continuing"`
				Ended          int `json:"ended"`
				WantedEpisodes int `json:"wanted_episodes"`
			}
			Expect(json.NewDecoder(resp.Body).Decode(&body)).To(Succeed())
			Expect(body.Total).To(Equal(5))
			Expect(body.Continuing).To(Equal(3))
			Expect(body.Ended).To(Equal(2))
			Expect(body.WantedEpisodes).To(Equal(7))
		})
	})

	Describe("LookupSeries", func() {
		It("flags already-added results", func() {
			app.metadataTV.EXPECT().SearchSeries(mock.Anything, "black sea").
				Return([]metadata.TVResult{
					{TVDBID: 9, Title: "The Black Sea", Year: 2024, Network: "HBO"},
				}, nil).Once()
			app.store.EXPECT().FindTVShowByTVDBID(mock.Anything, uint32(9)).
				Return(&ent.TVShow{ID: 1, TvdbID: 9}, nil).Once()

			resp := app.do(
				app.req(
					http.MethodGet,
					"/api/v1/series/lookup?query=black+sea",
					app.adminKey,
					nil,
				),
			)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusOK))

			var body struct {
				Items []struct {
					TvdbID       uint32 `json:"tvdb_id"`
					AlreadyAdded bool   `json:"already_added"`
				} `json:"items"`
			}
			Expect(json.NewDecoder(resp.Body).Decode(&body)).To(Succeed())
			Expect(body.Items).To(HaveLen(1))
			Expect(body.Items[0].TvdbID).To(Equal(uint32(9)))
			Expect(body.Items[0].AlreadyAdded).To(BeTrue())
		})
	})

	Describe("DeleteEpisodeFile", func() {
		It("deletes an episode file and returns 204", func() {
			app.tvshows.EXPECT().
				DeleteEpisodeFile(mock.Anything, uint32(9),
					tvshow.DeleteFileOptions{RemoveTorrent: true}).
				Return(nil).Once()

			req := app.req(http.MethodDelete,
				"/api/v1/series/3/episodes/9/file",
				app.adminKey, strings.NewReader(`{"remove_torrent":true}`))
			req.Header.Set("Content-Type", "application/json")
			resp := app.do(req)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusNoContent))
		})

		It("maps a service error to 404", func() {
			app.tvshows.EXPECT().
				DeleteEpisodeFile(mock.Anything, uint32(9),
					tvshow.DeleteFileOptions{RemoveTorrent: false}).
				Return(errors.New("episode 9 has no media file")).Once()

			req := app.req(http.MethodDelete,
				"/api/v1/series/3/episodes/9/file", app.adminKey, nil)
			resp := app.do(req)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusNotFound))
		})
	})

	Describe("BrowseSeasonReleases", func() {
		It("returns ranked season-pack results", func() {
			app.tvshows.EXPECT().Get(mock.Anything, uint32(3)).
				Return(&ent.TVShow{ID: 3, Title: "Breaking Bad", TvdbID: 81189}, nil).
				Once()
			app.indexers.EXPECT().
				SearchSeason(mock.Anything, []string{"Breaking Bad"},
					uint32(81189), uint16(1)).
				Return([]indexer.SearchResult{
					{Title: "BB S01 1080p", Download: "magnet:x", Seeders: 20},
				}, nil).Once()

			resp := app.do(app.req(http.MethodPost,
				"/api/v1/series/3/seasons/1/search", app.adminKey, nil))
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusOK))

			var body struct {
				Items []struct {
					Title string `json:"title"`
				} `json:"items"`
			}
			Expect(json.NewDecoder(resp.Body).Decode(&body)).To(Succeed())
			Expect(body.Items).To(HaveLen(1))
			Expect(body.Items[0].Title).To(Equal("BB S01 1080p"))
		})

		It("404s when the series is missing", func() {
			app.tvshows.EXPECT().Get(mock.Anything, uint32(3)).
				Return(nil, errors.New("tv show 3 not found")).Once()

			resp := app.do(app.req(http.MethodPost,
				"/api/v1/series/3/seasons/1/search", app.adminKey, nil))
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusNotFound))
		})
	})

	Describe("GrabSeasonRelease", func() {
		It("grabs a season pack and returns 202", func() {
			app.tvshows.EXPECT().Get(mock.Anything, uint32(3)).
				Return(&ent.TVShow{ID: 3}, nil).Once()
			app.tvshows.EXPECT().
				GrabSeasonRelease(mock.Anything, uint32(3), uint16(1),
					mock.AnythingOfType("indexer.SearchResult"), false).
				Return(nil).Once()

			req := app.req(
				http.MethodPost,
				"/api/v1/series/3/seasons/1/grab",
				app.adminKey,
				strings.NewReader(
					`{"title":"BB S01","download_url":"magnet:x","size":1,"seeders":1}`,
				),
			)
			req.Header.Set("Content-Type", "application/json")
			resp := app.do(req)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusAccepted))
		})

		It("forwards replace_existing=true to the service", func() {
			app.tvshows.EXPECT().Get(mock.Anything, uint32(3)).
				Return(&ent.TVShow{ID: 3}, nil).Once()
			app.tvshows.EXPECT().
				GrabSeasonRelease(mock.Anything, uint32(3), uint16(1),
					mock.AnythingOfType("indexer.SearchResult"), true).
				Return(nil).Once()

			req := app.req(
				http.MethodPost,
				"/api/v1/series/3/seasons/1/grab",
				app.adminKey,
				strings.NewReader(
					`{"title":"BB S01","download_url":"magnet:x","size":1,"seeders":1,"replace_existing":true}`,
				),
			)
			req.Header.Set("Content-Type", "application/json")
			resp := app.do(req)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusAccepted))
		})

		It("422s when title/download_url are missing", func() {
			app.tvshows.EXPECT().Get(mock.Anything, uint32(3)).
				Return(&ent.TVShow{ID: 3}, nil).Once()

			req := app.req(
				http.MethodPost,
				"/api/v1/series/3/seasons/1/grab",
				app.adminKey,
				strings.NewReader(
					`{"title":"","download_url":"","size":0,"seeders":0}`,
				),
			)
			req.Header.Set("Content-Type", "application/json")
			resp := app.do(req)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusUnprocessableEntity))
		})
	})

	Describe("BrowseSeriesReleases", func() {
		It("returns ranked whole-series results", func() {
			app.tvshows.EXPECT().Get(mock.Anything, uint32(3)).
				Return(&ent.TVShow{ID: 3, Title: "Breaking Bad", TvdbID: 81189}, nil).
				Once()
			app.indexers.EXPECT().
				SearchSeries(mock.Anything, []string{"Breaking Bad"}, uint32(81189)).
				Return([]indexer.SearchResult{
					{Title: "BB Complete 1080p", Download: "magnet:y", Seeders: 42},
				}, nil).Once()

			resp := app.do(app.req(http.MethodPost,
				"/api/v1/series/3/browse", app.adminKey, nil))
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusOK))

			var body struct {
				Items []struct {
					Title string `json:"title"`
				} `json:"items"`
			}
			Expect(json.NewDecoder(resp.Body).Decode(&body)).To(Succeed())
			Expect(body.Items).To(HaveLen(1))
			Expect(body.Items[0].Title).To(Equal("BB Complete 1080p"))
		})
	})

	Describe("GrabSeriesRelease", func() {
		It("grabs a whole-series pack and returns 202", func() {
			app.tvshows.EXPECT().Get(mock.Anything, uint32(3)).
				Return(&ent.TVShow{ID: 3}, nil).Once()
			app.tvshows.EXPECT().
				GrabSeriesRelease(mock.Anything, uint32(3),
					mock.AnythingOfType("indexer.SearchResult"), false).
				Return(nil).Once()

			req := app.req(
				http.MethodPost,
				"/api/v1/series/3/grab",
				app.adminKey,
				strings.NewReader(
					`{"title":"BB Complete","download_url":"magnet:y","size":1,"seeders":1}`,
				),
			)
			req.Header.Set("Content-Type", "application/json")
			resp := app.do(req)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusAccepted))
		})
	})
})
