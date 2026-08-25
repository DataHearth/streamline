package restapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/stretchr/testify/mock"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/schema"
	"github.com/datahearth/streamline/internal/db"
	"github.com/datahearth/streamline/internal/indexer"
	"github.com/datahearth/streamline/internal/library"
	"github.com/datahearth/streamline/internal/media/tvshow"
	"github.com/datahearth/streamline/internal/metadata"
	"github.com/datahearth/streamline/internal/testutil/configtest"
)

var _ = Describe("Handler: Series", Label("unit", "server", "series"), func() {
	var app *apiKeyApp

	BeforeEach(func() {
		app = newAPIKeyApp()
	})

	Describe("ListSeries", func() {
		It("clamps a limit above the documented maximum", func() {
			app.tvshows.EXPECT().
				FilterList(mock.Anything, mock.MatchedBy(func(p tvshow.FilterParams) bool {
					return p.Limit == seriesMaxLimit
				})).
				Return([]*ent.TVShow{}, nil, uint32(0), nil).
				Once()

			resp := app.do(app.req(
				http.MethodGet,
				"/api/v1/series?limit=65535",
				app.adminKey,
				nil,
			))
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusOK))
		})

		It("returns a paginated list", func() {
			app.tvshows.EXPECT().
				FilterList(mock.Anything, mock.AnythingOfType("tvshow.FilterParams")).
				Return(
					[]*ent.TVShow{{ID: 1, Title: "X", Year: 2020, TvdbID: 9}},
					map[uint32]db.EpisodeCounts{1: {Total: 10, Have: 4, Wanted: 6}},
					uint32(1),
					nil,
				).
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
					Title          string  `json:"title"`
					HaveEpisodes   *uint32 `json:"have_episodes"`
					WantedEpisodes *uint32 `json:"wanted_episodes"`
					Seasons        *[]any  `json:"seasons"`
				} `json:"items"`
			}
			Expect(json.NewDecoder(resp.Body).Decode(&body)).To(Succeed())
			Expect(body.Total).To(Equal(1))
			Expect(body.Items).To(HaveLen(1))
			Expect(body.Items[0].Title).To(Equal("X"))
			// The rollup comes from SQL; the tree it used to be derived from
			// is deliberately absent from a list row.
			Expect(*body.Items[0].HaveEpisodes).To(Equal(uint32(4)))
			Expect(*body.Items[0].WantedEpisodes).To(Equal(uint32(6)))
			Expect(body.Items[0].Seasons).To(BeNil())
		})

		It("500s when the service errors", func() {
			app.tvshows.EXPECT().
				FilterList(mock.Anything, mock.AnythingOfType("tvshow.FilterParams")).
				Return(nil, nil, uint32(0), errors.New("db down")).Once()

			resp := app.do(
				app.req(http.MethodGet, "/api/v1/series", app.adminKey, nil),
			)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusInternalServerError))
		})
	})

	Describe("AddSeries", func() {
		It("409s when no quality profile resolves", func() {
			app.tvshows.EXPECT().Add(mock.Anything, uint32(9), "gone").
				Return(nil, tvshow.ErrNoQualityProfile).Once()

			req := app.req(
				http.MethodPost,
				"/api/v1/series",
				app.adminKey,
				strings.NewReader(`{"tvdb_id":9,"quality_profile":"gone"}`),
			)
			req.Header.Set("Content-Type", "application/json")
			resp := app.do(req)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusConflict))
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

		// The strict TVDB mock carries no GetSeriesCast expectation in either
		// spec: the detail view must serve cast from the stored row.
		It("attaches the cast stored on the show", func() {
			app.tvshows.EXPECT().Get(mock.Anything, uint32(1)).
				Return(&ent.TVShow{
					ID: 1, Title: "Breaking Bad", Year: 2008, TvdbID: 81189,
					Cast: []schema.CastMember{
						{Name: "Bryan Cranston", Character: "Walter White"},
					},
				}, nil).
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

		It("omits cast when the show has none stored", func() {
			app.tvshows.EXPECT().Get(mock.Anything, uint32(1)).
				Return(&ent.TVShow{ID: 1, Title: "Breaking Bad", Year: 2008, TvdbID: 81189}, nil).
				Once()

			resp := app.do(
				app.req(http.MethodGet, "/api/v1/series/1", app.adminKey, nil),
			)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusOK))

			var body map[string]any
			Expect(json.NewDecoder(resp.Body).Decode(&body)).To(Succeed())
			Expect(body).NotTo(HaveKey("cast"))
		})

		It("scores each episode's file against the series profile", func() {
			configtest.Setup(map[string]any{
				"quality_profiles": []map[string]any{{
					"name":                 "default",
					"min_resolution":       "720p",
					"preferred_resolution": "1080p",
					"formats": []map[string]any{
						{"name": "remux", "score": 50},
						{"name": "x265", "score": 10},
					},
				}},
				"quality_default_profile": "default",
			})
			ep := &ent.Episode{
				ID:        7,
				Number:    1,
				Status:    "available",
				Monitored: true,
			}
			ep.Edges.MediaFiles = []*ent.MediaFile{{
				ID: 3, Size: 4_000_000_000,
				Path: "/tv/Breaking Bad/Season 01/" +
					"Breaking.Bad.S01E01.1080p.BluRay.Remux.x265-GRP.mkv",
			}}
			season := &ent.Season{ID: 2, Number: 1, Monitored: true}
			season.Edges.Episodes = []*ent.Episode{ep}
			show := &ent.TVShow{
				ID: 1, Title: "Breaking Bad", Year: 2008, TvdbID: 81189,
				QualityProfile: "default",
			}
			show.Edges.Seasons = []*ent.Season{season}
			app.tvshows.EXPECT().Get(mock.Anything, uint32(1)).
				Return(show, nil).Once()

			resp := app.do(
				app.req(http.MethodGet, "/api/v1/series/1", app.adminKey, nil),
			)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusOK))

			var body struct {
				Seasons []struct {
					Episodes []struct {
						FileScore *int `json:"file_score"`
					} `json:"episodes"`
				} `json:"seasons"`
			}
			Expect(json.NewDecoder(resp.Body).Decode(&body)).To(Succeed())
			Expect(body.Seasons).To(HaveLen(1))
			Expect(body.Seasons[0].Episodes).To(HaveLen(1))
			Expect(body.Seasons[0].Episodes[0].FileScore).
				To(HaveValue(Equal(60)))
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

	Describe("browse-release scoring", func() {
		results := []indexer.SearchResult{
			{Title: "BB.S01.1080p.WEB-DL.x264", Download: "magnet:a", Seeders: 30},
			{
				Title:    "BB.S01.1080p.BluRay.Remux.x265",
				Download: "magnet:b",
				Seeders:  4,
			},
			{Title: "BB.S01.480p.WEB-DL.x264", Download: "magnet:c", Seeders: 99},
		}
		show := &ent.TVShow{
			ID:             3,
			Title:          "Breaking Bad",
			TvdbID:         81189,
			QualityProfile: "default",
		}

		BeforeEach(func() {
			configtest.Setup(map[string]any{
				"quality_profiles": []map[string]any{
					{
						"name":                 "default",
						"min_resolution":       "720p",
						"preferred_resolution": "1080p",
						"formats": []map[string]any{
							{"name": "remux", "score": 50},
							{"name": "x265", "score": 10},
						},
					},
				},
				"quality_default_profile": "default",
			})
		})

		// expectScored asserts the shared annotation contract: best-first
		// order, matched formats on the winner, and the out-of-band release
		// still listed with its reason.
		expectScored := func(resp *http.Response) {
			GinkgoHelper()
			Expect(resp.StatusCode).To(Equal(http.StatusOK))
			var body SearchResultList
			Expect(json.NewDecoder(resp.Body).Decode(&body)).To(Succeed())
			Expect(body.Items).To(HaveLen(3))

			Expect(body.Items[0].Title).
				To(Equal("BB.S01.1080p.BluRay.Remux.x265"))
			Expect(body.Items[0].Score).To(HaveValue(Equal(60)))
			Expect(body.Items[0].MatchedFormats).
				To(HaveValue(ConsistOf("remux", "x265")))
			Expect(body.Items[0].Rejected).To(HaveValue(BeFalse()))

			Expect(body.Items[1].Title).To(Equal("BB.S01.1080p.WEB-DL.x264"))
			Expect(body.Items[1].Score).To(HaveValue(Equal(0)))

			Expect(body.Items[2].Title).To(Equal("BB.S01.480p.WEB-DL.x264"))
			Expect(body.Items[2].Rejected).To(HaveValue(BeTrue()))
			Expect(body.Items[2].RejectReason).
				To(HaveValue(ContainSubstring("resolution")))
		}

		It("annotates whole-series results", func() {
			app.tvshows.EXPECT().Get(mock.Anything, uint32(3)).
				Return(show, nil).Once()
			app.indexers.EXPECT().
				SearchSeries(mock.Anything, []string{"Breaking Bad"},
					uint32(81189)).
				Return(results, nil).Once()

			resp := app.do(app.req(http.MethodPost,
				"/api/v1/series/3/browse", app.adminKey, nil))
			defer resp.Body.Close()
			expectScored(resp)
		})

		It("annotates season-pack results", func() {
			app.tvshows.EXPECT().Get(mock.Anything, uint32(3)).
				Return(show, nil).Once()
			app.indexers.EXPECT().
				SearchSeason(mock.Anything, []string{"Breaking Bad"},
					uint32(81189), uint16(1)).
				Return(results, nil).Once()

			resp := app.do(app.req(http.MethodPost,
				"/api/v1/series/3/seasons/1/search", app.adminKey, nil))
			defer resp.Body.Close()
			expectScored(resp)
		})

		It("annotates episode results", func() {
			withTree := *show
			withTree.Edges.Seasons = []*ent.Season{
				{
					ID:     10,
					Number: 1,
					Edges: ent.SeasonEdges{
						Episodes: []*ent.Episode{{ID: 100, Number: 2}},
					},
				},
			}
			app.tvshows.EXPECT().Get(mock.Anything, uint32(3)).
				Return(&withTree, nil).Once()
			app.indexers.EXPECT().
				SearchEpisode(mock.Anything, []string{"Breaking Bad"},
					uint32(81189), uint16(1), uint16(2)).
				Return(results, nil).Once()

			resp := app.do(app.req(http.MethodPost,
				"/api/v1/series/3/episodes/100/search", app.adminKey, nil))
			defer resp.Body.Close()
			expectScored(resp)
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

	Describe("RenameSeriesFiles", func() {
		notFound := fmt.Errorf("series 999: %w", tvshow.ErrSeriesNotFound)

		It("returns 404 when the previewed series does not exist", func() {
			app.seriesRenamer.EXPECT().
				Preview(mock.Anything, uint32(999)).
				Return(library.RenamePlan{}, notFound).
				Once()

			resp := app.do(app.req(http.MethodPost,
				"/api/v1/series/999/rename?preview=true", app.adminKey, nil))
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusNotFound))

			var body map[string]any
			Expect(json.NewDecoder(resp.Body).Decode(&body)).To(Succeed())
			Expect(body["message"]).To(Equal("series not found"))
		})

		It("returns 404 when the applied series does not exist", func() {
			app.seriesRenamer.EXPECT().
				Apply(mock.Anything, uint32(999)).
				Return(library.RenamePlan{}, notFound).
				Once()

			resp := app.do(app.req(http.MethodPost,
				"/api/v1/series/999/rename", app.adminKey, nil))
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusNotFound))
		})

		It("returns 500 when the rename itself fails", func() {
			app.seriesRenamer.EXPECT().
				Apply(mock.Anything, uint32(3)).
				Return(library.RenamePlan{}, errors.New("disk full")).
				Once()

			resp := app.do(app.req(http.MethodPost,
				"/api/v1/series/3/rename", app.adminKey, nil))
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusInternalServerError))
		})

		It("returns the plan on success", func() {
			app.seriesRenamer.EXPECT().
				Preview(mock.Anything, uint32(3)).
				Return(library.RenamePlan{
					Operations: []library.RenameOperation{{
						MediaFileID: 11,
						From:        "/library/tv/old.mkv",
						To:          "/library/tv/Show/Season 1/Show - S01E01.mkv",
					}},
				}, nil).
				Once()

			resp := app.do(app.req(http.MethodPost,
				"/api/v1/series/3/rename?preview=true", app.adminKey, nil))
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusOK))

			var plan SeriesRenamePlan
			Expect(json.NewDecoder(resp.Body).Decode(&plan)).To(Succeed())
			Expect(plan.SeriesId).To(Equal(uint32(3)))
			Expect(plan.Operations).To(HaveLen(1))
			Expect(plan.Operations[0].MediaFileId).To(Equal(uint32(11)))
		})
	})
})
