package tvshow

import (
	"context"
	"errors"
	"time"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/episode"
	enttvshow "github.com/datahearth/streamline/ent/tvshow"
	"github.com/datahearth/streamline/internal/db"
	dbmocks "github.com/datahearth/streamline/internal/db/mocks"
	mockdownload "github.com/datahearth/streamline/internal/download/mocks"
	"github.com/datahearth/streamline/internal/indexer"
	"github.com/datahearth/streamline/internal/metadata"
	mockmeta "github.com/datahearth/streamline/internal/metadata/mocks"
	mockposters "github.com/datahearth/streamline/internal/posters/mocks"
	"github.com/datahearth/streamline/internal/testutil/configtest"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stretchr/testify/mock"
)

var _ = Describe("TVShow service", Label("unit", "series"), func() {
	var (
		ctx     context.Context
		storeMk *dbmocks.MockStore_Expecter
		metaMk  *mockmeta.MockTVProvider_Expecter
		postMk  *mockposters.MockManager_Expecter
		dlMk    *mockdownload.MockDownloader_Expecter
		svc     *Service
	)

	BeforeEach(func() {
		ctx = context.Background()
		store := dbmocks.NewMockStore(GinkgoT())
		storeMk = store.EXPECT()
		meta := mockmeta.NewMockTVProvider(GinkgoT())
		metaMk = meta.EXPECT()
		post := mockposters.NewMockManager(GinkgoT())
		postMk = post.EXPECT()
		dl := mockdownload.NewMockDownloader(GinkgoT())
		dlMk = dl.EXPECT()
		svc = NewService(store, meta, post, dl)
		configtest.Setup(map[string]any{})
	})

	It("fetches TVDB metadata and creates the show with a poster fetch", func() {
		air := time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC)
		metaMk.GetSeries(mock.Anything, uint32(123)).Return(&metadata.TVDetails{
			TVResult: metadata.TVResult{
				TVDBID:     123,
				Title:      "The Black Sea",
				Year:       2023,
				Network:    "Halcyon",
				PosterPath: "/p.jpg",
			},
			Status:  "continuing",
			Type:    metadata.SeriesStandard,
			Genres:  []string{"Drama"},
			Seasons: []metadata.SeasonInfo{{Number: 1}},
			Episodes: []metadata.EpisodeInfo{
				{SeasonNumber: 1, Number: 1, Title: "Pilot", AirDate: &air},
			},
		}, nil).Once()
		metaMk.GetSeriesCast(mock.Anything, uint32(123)).
			Return([]metadata.CastMember{{Name: "Ana Vidal", Character: "Iris"}}, nil).
			Once()

		storeMk.CreateTVShow(mock.Anything, mock.MatchedBy(func(p db.CreateTVShowParams) bool {
			return p.TvdbID == 123 && p.Title == "The Black Sea" &&
				len(p.Seasons) == 1 &&
				len(p.Seasons[0].Episodes) == 1 &&
				len(p.Cast) == 1 && p.Cast[0].Name == "Ana Vidal"
		})).
			Return(&ent.TVShow{ID: 7, Title: "The Black Sea", TvdbID: 123}, nil).
			Once()

		done := make(chan struct{})
		postMk.Fetch(mock.Anything, "tvshows", uint32(7), "https://artworks.thetvdb.com/p.jpg").
			RunAndReturn(func(_ context.Context, _ string, _ uint32, _ string) error { close(done); return nil }).
			Once()

		show, err := svc.Add(ctx, 123, "")
		Expect(err).NotTo(HaveOccurred())
		Expect(show.ID).To(Equal(uint32(7)))
		Eventually(done).Should(BeClosed())
	})

	It("adds the show anyway when the cast fetch fails", func() {
		metaMk.GetSeries(mock.Anything, uint32(123)).Return(&metadata.TVDetails{
			TVResult: metadata.TVResult{TVDBID: 123, Title: "The Black Sea"},
			Status:   "continuing",
			Type:     metadata.SeriesStandard,
		}, nil).Once()
		metaMk.GetSeriesCast(mock.Anything, uint32(123)).
			Return(nil, errors.New("tvdb down")).
			Once()
		storeMk.CreateTVShow(mock.Anything, mock.MatchedBy(
			func(p db.CreateTVShowParams) bool { return len(p.Cast) == 0 },
		)).
			Return(&ent.TVShow{ID: 7, TvdbID: 123}, nil).
			Once()

		show, err := svc.Add(ctx, 123, "")
		Expect(err).NotTo(HaveOccurred())
		Expect(show.ID).To(Equal(uint32(7)))
	})

	It("RefreshStale only visits shows past the refresh interval", func() {
		storeMk.ListTVShowsStaleSince(
			mock.Anything,
			mock.AnythingOfType("time.Time"),
		).
			RunAndReturn(func(_ context.Context, cutoff time.Time) ([]*ent.TVShow, error) {
				Expect(cutoff).To(BeTemporally(
					"~", time.Now().Add(-metadataMinRefreshInterval), time.Minute,
				))
				return nil, nil
			}).
			Once()

		Expect(svc.RefreshStale(ctx)).To(Succeed())
	})

	DescribeTable(
		"seedSeasons leaves specials unmonitored unless opted in",
		func(monitorSpecials, wantSpecialsMonitored bool) {
			configtest.Setup(
				map[string]any{"library.monitor_specials": monitorSpecials},
			)

			seasons := seedSeasons(&metadata.TVDetails{
				Seasons: []metadata.SeasonInfo{{Number: 0}, {Number: 1}},
			})

			Expect(seasons[0].Number).To(Equal(uint16(0)))
			Expect(seasons[0].Unmonitored).To(Equal(!wantSpecialsMonitored))
			Expect(seasons[1].Unmonitored).To(BeFalse())
		},
		Entry("off by default", false, false),
		Entry("opted in", true, true),
	)

	It("Add rejects the show when no quality profile resolves", func() {
		configtest.Setup(map[string]any{
			"quality_profiles":        []any{},
			"quality_default_profile": "",
		})

		_, err := svc.Add(ctx, 123, "")
		Expect(err).To(MatchError(ErrNoQualityProfile))
	})

	It("Update rejects a profile change when none resolves", func() {
		configtest.Setup(map[string]any{
			"quality_profiles":        []any{},
			"quality_default_profile": "",
		})
		qp := "gone"

		_, err := svc.Update(ctx, 7, UpdateParams{QualityProfile: &qp})
		Expect(err).To(MatchError(ErrNoQualityProfile))
	})

	It("List returns total and a page", func() {
		storeMk.CountTVShows(mock.Anything).Return(3, nil).Once()
		storeMk.ListTVShows(mock.Anything, uint32(0), uint32(20)).
			Return([]*ent.TVShow{{ID: 1}, {ID: 2}, {ID: 3}}, nil).Once()
		items, total, err := svc.List(ctx, 1, 20)
		Expect(err).NotTo(HaveOccurred())
		Expect(total).To(Equal(uint32(3)))
		Expect(items).To(HaveLen(3))
	})

	It("Counts aggregates show + wanted-episode totals", func() {
		storeMk.CountTVShows(mock.Anything).Return(2, nil).Once()
		storeMk.CountTVShowsByStatus(mock.Anything, enttvshow.SeriesStatusContinuing).
			Return(1, nil).
			Once()
		storeMk.CountTVShowsByStatus(mock.Anything, enttvshow.SeriesStatusEnded).
			Return(1, nil).Once()
		storeMk.CountWantedEpisodes(mock.Anything).Return(2, nil).Once()
		c, err := svc.Counts(ctx)
		Expect(err).NotTo(HaveOccurred())
		Expect(c.Total).To(Equal(2))
		Expect(c.Continuing).To(Equal(1))
		Expect(c.Ended).To(Equal(1))
		Expect(c.WantedEpisodes).To(Equal(2))
	})

	It("Update toggles show monitored and cascades to the tree", func() {
		t := true
		storeMk.CascadeShowMonitored(mock.Anything, uint32(7), true).
			Return(nil).Once()
		storeMk.UpdateTVShow(mock.Anything, uint32(7), mock.MatchedBy(func(p db.UpdateTVShowParams) bool {
			return p.Monitored != nil && *p.Monitored
		})).
			Return(&ent.TVShow{ID: 7}, nil).
			Once()
		_, err := svc.Update(ctx, 7, UpdateParams{Monitored: &t})
		Expect(err).NotTo(HaveOccurred())
	})

	It("Update overrides the series type", func() {
		anime := "anime"
		storeMk.UpdateTVShow(mock.Anything, uint32(7), mock.MatchedBy(func(p db.UpdateTVShowParams) bool {
			return p.Type != nil && *p.Type == enttvshow.TypeAnime
		})).
			Return(&ent.TVShow{ID: 7}, nil).
			Once()
		_, err := svc.Update(ctx, 7, UpdateParams{Type: &anime})
		Expect(err).NotTo(HaveOccurred())
	})

	It("Update rejects an unknown series type", func() {
		bogus := "cartoon"
		_, err := svc.Update(ctx, 7, UpdateParams{Type: &bogus})
		Expect(err).To(MatchError(ErrInvalidSeriesType))
	})

	It("Update applies the 'all' monitoring preset to seasons and episodes", func() {
		show := withWantedEpisodes(2)
		storeMk.FindTVShowByID(mock.Anything, uint32(1)).Return(show, nil).Twice()
		storeMk.SetEpisodeMonitored(mock.Anything, uint32(1), true).
			Return(nil).
			Once()
		storeMk.SetEpisodeMonitored(mock.Anything, uint32(2), true).
			Return(nil).
			Once()
		storeMk.SetSeasonMonitored(mock.Anything, uint32(1), true).Return(nil).Once()
		_, err := svc.Update(ctx, 1, UpdateParams{Preset: "all"})
		Expect(err).NotTo(HaveOccurred())
	})

	It("Update's 'pilot' preset monitors S01E01, not the first special", func() {
		show := &ent.TVShow{ID: 1, Edges: ent.TVShowEdges{Seasons: []*ent.Season{
			{ID: 10, Number: 0, Edges: ent.SeasonEdges{Episodes: []*ent.Episode{
				{ID: 100, Number: 1},
			}}},
			{ID: 11, Number: 1, Edges: ent.SeasonEdges{Episodes: []*ent.Episode{
				{ID: 101, Number: 1},
				{ID: 102, Number: 2},
			}}},
		}}}
		storeMk.FindTVShowByID(mock.Anything, uint32(1)).Return(show, nil).Twice()
		storeMk.SetEpisodeMonitored(mock.Anything, uint32(100), false).
			Return(nil).Once()
		storeMk.SetEpisodeMonitored(mock.Anything, uint32(101), true).
			Return(nil).Once()
		storeMk.SetEpisodeMonitored(mock.Anything, uint32(102), false).
			Return(nil).Once()
		storeMk.SetSeasonMonitored(mock.Anything, uint32(10), false).
			Return(nil).Once()
		storeMk.SetSeasonMonitored(mock.Anything, uint32(11), true).
			Return(nil).Once()

		_, err := svc.Update(ctx, 1, UpdateParams{Preset: "pilot"})
		Expect(err).NotTo(HaveOccurred())
	})

	It("SetSeasonMonitored cascades to the season's episodes", func() {
		storeMk.CascadeSeasonMonitored(mock.Anything, uint32(3), false).
			Return(nil).
			Once()
		Expect(svc.SetSeasonMonitored(ctx, 3, false)).To(Succeed())
	})

	It("Delete removes the show and evicts the cached poster", func() {
		storeMk.DeleteTVShow(mock.Anything, uint32(7)).Return(nil).Once()
		postMk.Remove("tvshows", uint32(7)).Return(nil).Once()
		Expect(svc.Delete(ctx, 7, DeleteOptions{})).To(Succeed())
	})

	It("RefreshOne re-pulls metadata and stamps refreshed_at", func() {
		storeMk.FindTVShowByID(mock.Anything, uint32(7)).
			Return(&ent.TVShow{ID: 7, TvdbID: 123}, nil).Twice()
		metaMk.GetSeries(mock.Anything, uint32(123)).
			Return(&metadata.TVDetails{TVResult: metadata.TVResult{TVDBID: 123, Title: "X"}}, nil).
			Once()
		metaMk.GetSeriesCast(mock.Anything, uint32(123)).Return(nil, nil).Once()
		storeMk.UpdateTVShowMetadata(mock.Anything, uint32(7), mock.Anything).
			Return(nil).
			Once()
		storeMk.ReconcileEpisodes(mock.Anything, uint32(7), mock.Anything).
			Return(nil, nil).
			Once()
		storeMk.SetTVShowRefreshedAt(mock.Anything, uint32(7), mock.Anything).
			Return(nil).
			Once()
		_, err := svc.RefreshOne(ctx, 7)
		Expect(err).NotTo(HaveOccurred())
	})

	Describe("DeriveSeasonViews", func() {
		now := time.Now()
		aired := now.Add(-24 * time.Hour)

		It("counts a monitored, aired, file-less episode as missing", func() {
			show := &ent.TVShow{Edges: ent.TVShowEdges{Seasons: []*ent.Season{
				{Number: 1, Edges: ent.SeasonEdges{Episodes: []*ent.Episode{
					{ID: 1, Number: 1, AirDate: aired, Monitored: true},
				}}},
			}}}

			v := DeriveSeasonViews(show, now)[0]
			Expect(v.Missing).To(Equal(1))
			Expect(v.Total).To(Equal(1))
		})

		It("leaves a file-less unmonitored episode out of every count", func() {
			show := &ent.TVShow{Edges: ent.TVShowEdges{Seasons: []*ent.Season{
				{Number: 0, Edges: ent.SeasonEdges{Episodes: []*ent.Episode{
					{ID: 1, Number: 1, AirDate: aired},
					{ID: 2, Number: 2, AirDate: now.Add(24 * time.Hour)},
					{ID: 3, Number: 3},
				}}},
			}}}

			v := DeriveSeasonViews(show, now)[0]
			Expect(v.Missing).To(BeZero())
			Expect(v.Available).To(BeZero())
			Expect(v.Unaired).To(BeZero())
			Expect(v.Total).To(BeZero())
		})

		It("counts a downloaded unmonitored episode as available", func() {
			show := &ent.TVShow{Edges: ent.TVShowEdges{Seasons: []*ent.Season{
				{Number: 0, Edges: ent.SeasonEdges{Episodes: []*ent.Episode{
					{ID: 1, Number: 1, AirDate: aired, Edges: ent.EpisodeEdges{
						MediaFiles: []*ent.MediaFile{{ID: 1}},
					}},
					{ID: 2, Number: 2, AirDate: aired},
				}}},
			}}}

			v := DeriveSeasonViews(show, now)[0]
			Expect(v.Available).To(Equal(1))
			Expect(v.Total).To(Equal(1))
			Expect(v.Missing).To(BeZero())
		})

		It(
			"keeps a season whole when monitored and unmonitored files mix",
			func() {
				show := &ent.TVShow{Edges: ent.TVShowEdges{Seasons: []*ent.Season{
					{Number: 1, Edges: ent.SeasonEdges{Episodes: []*ent.Episode{
						{
							ID: 1, Number: 1, AirDate: aired, Monitored: true,
							Edges: ent.EpisodeEdges{
								MediaFiles: []*ent.MediaFile{{ID: 1}},
							},
						},
						{ID: 2, Number: 2, AirDate: aired, Edges: ent.EpisodeEdges{
							MediaFiles: []*ent.MediaFile{{ID: 2}},
						}},
						{ID: 3, Number: 3, AirDate: aired},
					}}},
				}}}

				v := DeriveSeasonViews(show, now)[0]
				Expect(v.Available).To(Equal(2))
				Expect(v.Total).To(Equal(2))
				Expect(v.Missing).To(BeZero())
			},
		)
	})

	// The filter, sort and page all resolve in SQL now; what the service still
	// owns is defaulting the page window. The predicates themselves are covered
	// against a real database in the db suite (FilterTVShows).
	Describe("FilterList", func() {
		It("defaults page and limit and forwards the rest verbatim", func() {
			storeMk.FilterTVShows(mock.Anything, mock.MatchedBy(
				func(p db.FilterTVShowsParams) bool {
					return p.Status == "missing" && p.Offset == 0 && p.Limit == 20
				},
			)).Return([]*ent.TVShow{{ID: 1}}, map[uint32]db.EpisodeCounts{
				1: {Total: 3, Have: 1, Wanted: 2},
			}, uint32(1), nil).Once()

			items, counts, total, err := svc.FilterList(
				ctx, FilterParams{Status: "missing"},
			)
			Expect(err).NotTo(HaveOccurred())
			Expect(total).To(Equal(uint32(1)))
			Expect(items).To(HaveLen(1))
			Expect(counts[1].Wanted).To(Equal(uint32(2)))
		})

		It("turns page 3 into the matching offset", func() {
			storeMk.FilterTVShows(mock.Anything, mock.MatchedBy(
				func(p db.FilterTVShowsParams) bool {
					return p.Offset == 20 && p.Limit == 10
				},
			)).Return(nil, nil, uint32(0), nil).Once()

			_, _, _, err := svc.FilterList(ctx, FilterParams{Page: 3, Limit: 10})
			Expect(err).NotTo(HaveOccurred())
		})
	})

	Describe("DeleteEpisodeFile", func() {
		It(
			"deletes the file, reverts the episode, removes the torrent when asked",
			func() {
				storeMk.FindMediaFileByEpisodeID(mock.Anything, uint32(9)).
					Return(&ent.MediaFile{ID: 4, Path: "/lib/does-not-exist.mkv"}, nil).
					Once()
				storeMk.DeleteMediaFileAndRevertEpisode(mock.Anything, uint32(4), uint32(9)).
					Return(nil).
					Once()
				storeMk.LatestImportedRecordForEpisode(mock.Anything, uint32(9)).
					Return(&ent.DownloadRecord{TorrentHash: "H", DownloadClientName: "qb"}, nil).
					Once()
				dlMk.RemoveTorrent(mock.Anything, "qb", "H", false).
					Return(nil).
					Once()

				err := svc.DeleteEpisodeFile(
					ctx,
					9,
					DeleteFileOptions{RemoveTorrent: true},
				)
				Expect(err).NotTo(HaveOccurred())
			},
		)

		It("skips torrent removal when not requested", func() {
			storeMk.FindMediaFileByEpisodeID(mock.Anything, uint32(9)).
				Return(&ent.MediaFile{ID: 4, Path: "/lib/does-not-exist.mkv"}, nil).
				Once()
			storeMk.DeleteMediaFileAndRevertEpisode(mock.Anything, uint32(4), uint32(9)).
				Return(nil).
				Once()

			err := svc.DeleteEpisodeFile(
				ctx,
				9,
				DeleteFileOptions{RemoveTorrent: false},
			)
			Expect(err).NotTo(HaveOccurred())
		})

		It("errors when the episode has no media file", func() {
			storeMk.FindMediaFileByEpisodeID(mock.Anything, uint32(9)).
				Return(nil, &ent.NotFoundError{}).Once()

			err := svc.DeleteEpisodeFile(ctx, 9, DeleteFileOptions{})
			Expect(err).To(MatchError(ContainSubstring("no media file")))
		})
	})

	Describe("GrabSeasonRelease", func() {
		It("grabs against the first episode and marks wanted, aired ones", func() {
			past := time.Now().Add(-24 * time.Hour)
			future := time.Now().Add(720 * time.Hour)
			show := &ent.TVShow{ID: 3, Edges: ent.TVShowEdges{
				Seasons: []*ent.Season{{
					Number: 1,
					Edges: ent.SeasonEdges{Episodes: []*ent.Episode{
						{
							ID:      10,
							Number:  1,
							Status:  episode.StatusWanted,
							AirDate: past,
						},
						{
							ID:      11,
							Number:  2,
							Status:  episode.StatusAvailable,
							AirDate: past,
						},
						{
							ID:      12,
							Number:  3,
							Status:  episode.StatusWanted,
							AirDate: future,
						},
					}},
				}},
			}}
			storeMk.FindTVShowByID(mock.Anything, uint32(3)).Return(show, nil).Once()
			// Anchored on the season's first episode regardless of its status.
			dlMk.GrabEpisode(mock.Anything,
				mock.AnythingOfType("indexer.SearchResult"), uint32(10)).
				Return(&ent.DownloadRecord{ID: 1}, nil).Once()
			// Only the wanted+aired episode flips; available and future ones don't.
			storeMk.SetEpisodeStatus(mock.Anything, uint32(10), episode.StatusDownloading).
				Return(nil).
				Once()

			err := svc.GrabSeasonRelease(ctx, 3, 1,
				indexer.SearchResult{Title: "BB S01", Download: "magnet:x"}, false)
			Expect(err).NotTo(HaveOccurred())
		})

		It("flags the record for replacement when requested", func() {
			past := time.Now().Add(-24 * time.Hour)
			show := &ent.TVShow{ID: 3, Edges: ent.TVShowEdges{
				Seasons: []*ent.Season{{
					Number: 1,
					Edges: ent.SeasonEdges{Episodes: []*ent.Episode{{
						ID:      10,
						Number:  1,
						Status:  episode.StatusWanted,
						AirDate: past,
					}}},
				}},
			}}
			storeMk.FindTVShowByID(mock.Anything, uint32(3)).Return(show, nil).Once()
			dlMk.GrabEpisode(mock.Anything,
				mock.AnythingOfType("indexer.SearchResult"), uint32(10)).
				Return(&ent.DownloadRecord{ID: 7}, nil).Once()
			storeMk.MarkDownloadRecordReplaceExisting(mock.Anything, uint32(7)).
				Return(nil).Once()
			storeMk.SetEpisodeStatus(mock.Anything, uint32(10), episode.StatusDownloading).
				Return(nil).
				Once()

			err := svc.GrabSeasonRelease(ctx, 3, 1,
				indexer.SearchResult{Title: "BB S01", Download: "magnet:x"}, true)
			Expect(err).NotTo(HaveOccurred())
		})

		It("errors when the season has no episodes", func() {
			show := &ent.TVShow{ID: 3, Edges: ent.TVShowEdges{
				Seasons: []*ent.Season{{Number: 1, Edges: ent.SeasonEdges{
					Episodes: []*ent.Episode{{ID: 10}},
				}}},
			}}
			storeMk.FindTVShowByID(mock.Anything, uint32(3)).Return(show, nil).Once()

			err := svc.GrabSeasonRelease(ctx, 3, 5,
				indexer.SearchResult{Title: "x", Download: "y"}, false)
			Expect(err).To(MatchError(ContainSubstring("no episodes")))
		})
	})

	Describe("GrabSeriesRelease", func() {
		It(
			"grabs against the first episode and marks every wanted, aired one",
			func() {
				past := time.Now().Add(-24 * time.Hour)
				show := &ent.TVShow{ID: 3, Edges: ent.TVShowEdges{
					Seasons: []*ent.Season{
						{Number: 1, Edges: ent.SeasonEdges{Episodes: []*ent.Episode{
							{
								ID:      10,
								Number:  1,
								Status:  episode.StatusWanted,
								AirDate: past,
							},
						}}},
						{Number: 2, Edges: ent.SeasonEdges{Episodes: []*ent.Episode{
							{
								ID:      20,
								Number:  1,
								Status:  episode.StatusWanted,
								AirDate: past,
							},
						}}},
					},
				}}
				storeMk.FindTVShowByID(mock.Anything, uint32(3)).
					Return(show, nil).
					Once()
				dlMk.GrabEpisode(mock.Anything,
					mock.AnythingOfType("indexer.SearchResult"), uint32(10)).
					Return(&ent.DownloadRecord{ID: 1}, nil).Once()
				storeMk.SetEpisodeStatus(mock.Anything, uint32(10), episode.StatusDownloading).
					Return(nil).
					Once()
				storeMk.SetEpisodeStatus(mock.Anything, uint32(20), episode.StatusDownloading).
					Return(nil).
					Once()

				err := svc.GrabSeriesRelease(ctx, 3,
					indexer.SearchResult{Title: "BB Complete", Download: "magnet:y"},
					false)
				Expect(err).NotTo(HaveOccurred())
			},
		)
	})

	Describe("Reidentify", func() {
		newShow := func() *metadata.TVDetails {
			return &metadata.TVDetails{
				TVResult: metadata.TVResult{
					TVDBID: 999, Title: "Right Show", Year: 2019,
				},
				Status:  "continuing",
				Type:    metadata.SeriesStandard,
				Seasons: []metadata.SeasonInfo{{Number: 1, Name: "Season 1"}},
				Episodes: []metadata.EpisodeInfo{
					{SeasonNumber: 1, Number: 1, Title: "One"},
					{SeasonNumber: 1, Number: 2, Title: "Two"},
				},
			}
		}

		It("rejects a zero tvdb id before touching the store", func() {
			_, _, err := svc.Reidentify(ctx, 5, 0)
			Expect(err).To(MatchError(ErrInvalidTVDBID))
		})

		It("refuses when the target show is already in the library", func() {
			storeMk.FindTVShowByID(mock.Anything, uint32(5)).
				Return(&ent.TVShow{ID: 5, TvdbID: 111}, nil).Once()
			storeMk.FindTVShowByTVDBID(mock.Anything, uint32(999)).
				Return(&ent.TVShow{ID: 8, TvdbID: 999}, nil).Once()

			_, _, err := svc.Reidentify(ctx, 5, 999)
			Expect(err).To(MatchError(ErrSeriesExists))
		})

		It("leaves the series untouched when the TVDB fetch fails", func() {
			storeMk.FindTVShowByID(mock.Anything, uint32(5)).
				Return(&ent.TVShow{ID: 5, TvdbID: 111}, nil).Once()
			storeMk.FindTVShowByTVDBID(mock.Anything, uint32(999)).
				Return(nil, nil).Once()
			metaMk.GetSeries(mock.Anything, uint32(999)).
				Return(nil, errors.New("tvdb down")).Once()

			// No detach, no id write: the mock would fail the spec on any.
			_, _, err := svc.Reidentify(ctx, 5, 999)
			Expect(err).To(MatchError(ContainSubstring("tvdb get series")))
		})

		It(
			"detaches files before reconciling, then re-attaches them by S/E",
			func() {
				storeMk.FindTVShowByID(mock.Anything, uint32(5)).
					Return(&ent.TVShow{ID: 5, TvdbID: 111, Title: "Wrong Show"}, nil).
					Once()
				storeMk.FindTVShowByTVDBID(mock.Anything, uint32(999)).
					Return(nil, nil).Once()
				metaMk.GetSeries(mock.Anything, uint32(999)).
					Return(newShow(), nil).
					Once()

				s1 := &ent.Season{ID: 70, Number: 1}
				detached := []*ent.MediaFile{
					{ID: 1, Path: "/tv/Wrong/S01E01.mkv", Edges: ent.MediaFileEdges{
						Episode: &ent.Episode{
							ID:     11,
							Number: 1,
							Edges:  ent.EpisodeEdges{Season: s1},
						},
					}},
					{ID: 2, Path: "/tv/Wrong/S01E09.mkv", Edges: ent.MediaFileEdges{
						Episode: &ent.Episode{
							ID:     19,
							Number: 9,
							Edges:  ent.EpisodeEdges{Season: s1},
						},
					}},
				}
				storeMk.DetachEpisodeMediaFiles(mock.Anything, uint32(5)).
					Return(detached, nil).Once()
				storeMk.SetTVShowTVDBID(mock.Anything, uint32(5), uint32(999)).
					Return(nil).Once()
				metaMk.GetSeriesCast(mock.Anything, uint32(999)).
					Return(nil, nil).
					Once()
				storeMk.UpdateTVShowMetadata(
					mock.Anything,
					uint32(5),
					mock.AnythingOfType("db.UpdateTVShowMetadataParams"),
				).Return(nil).Once()
				storeMk.UpdateTVShow(
					mock.Anything,
					uint32(5),
					mock.AnythingOfType("db.UpdateTVShowParams"),
				).Return(&ent.TVShow{ID: 5}, nil).Once()
				storeMk.ReconcileEpisodes(
					mock.Anything, uint32(5), mock.AnythingOfType("[]db.SeasonSeed"),
				).Return(nil, nil).Once()

				// The rebuilt tree: S01E01 and S01E02 exist, E09 does not.
				rebuilt := &ent.TVShow{
					ID: 5, TvdbID: 999, Title: "Right Show",
					Edges: ent.TVShowEdges{Seasons: []*ent.Season{{
						ID: 90, Number: 1,
						Edges: ent.SeasonEdges{Episodes: []*ent.Episode{
							{ID: 901, Number: 1},
							{ID: 902, Number: 2},
						}},
					}}},
				}
				storeMk.FindTVShowByID(mock.Anything, uint32(5)).
					Return(rebuilt, nil).
					Once()
				storeMk.AttachMediaFileToEpisode(mock.Anything, uint32(1), uint32(901)).
					Return(nil).
					Once()
				storeMk.SetEpisodeStatus(mock.Anything, uint32(901), episode.StatusAvailable).
					Return(nil).
					Once()
				// E09 has no counterpart: the row goes, the file does not.
				storeMk.DeleteMediaFile(mock.Anything, uint32(2)).Return(nil).Once()
				storeMk.SetTVShowRefreshedAt(
					mock.Anything, uint32(5), mock.AnythingOfType("time.Time"),
				).Return(nil).Once()
				storeMk.FindTVShowByID(mock.Anything, uint32(5)).
					Return(rebuilt, nil).
					Once()

				show, unmatched, err := svc.Reidentify(ctx, 5, 999)
				Expect(err).ToNot(HaveOccurred())
				Expect(show.Title).To(Equal("Right Show"))
				Expect(unmatched).To(ConsistOf("/tv/Wrong/S01E09.mkv"))
			},
		)
	})
})

// withWantedEpisodes builds a show with a single season carrying n episodes.
func withWantedEpisodes(n int) *ent.TVShow {
	GinkgoHelper()
	eps := make([]*ent.Episode, 0, n)
	for i := range n {
		eps = append(eps, &ent.Episode{ID: uint32(i + 1)})
	}
	season := &ent.Season{ID: 1, Edges: ent.SeasonEdges{Episodes: eps}}
	return &ent.TVShow{ID: 1, Edges: ent.TVShowEdges{Seasons: []*ent.Season{season}}}
}
