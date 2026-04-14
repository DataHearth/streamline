package tvshow

import (
	"context"
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

		storeMk.CreateTVShow(mock.Anything, mock.MatchedBy(func(p db.CreateTVShowParams) bool {
			return p.TvdbID == 123 && p.Title == "The Black Sea" &&
				len(p.Seasons) == 1 &&
				len(p.Seasons[0].Episodes) == 1
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
		storeMk.ListWantedEpisodes(mock.Anything).Return([]*ent.TVShow{
			withWantedEpisodes(2),
		}, nil).Once()
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

	It("SetSeasonMonitored cascades to the season's episodes", func() {
		storeMk.CascadeSeasonMonitored(mock.Anything, uint32(3), false).
			Return(nil).
			Once()
		Expect(svc.SetSeasonMonitored(ctx, 3, false)).To(Succeed())
	})

	It("Delete removes the show", func() {
		storeMk.DeleteTVShow(mock.Anything, uint32(7)).Return(nil).Once()
		Expect(svc.Delete(ctx, 7, DeleteOptions{})).To(Succeed())
	})

	It("RefreshOne re-pulls metadata and stamps refreshed_at", func() {
		storeMk.FindTVShowByID(mock.Anything, uint32(7)).
			Return(&ent.TVShow{ID: 7, TvdbID: 123}, nil).Twice()
		metaMk.GetSeries(mock.Anything, uint32(123)).
			Return(&metadata.TVDetails{TVResult: metadata.TVResult{TVDBID: 123, Title: "X"}}, nil).
			Once()
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
				dlMk.RemoveTorrent(mock.Anything, "qb", "H").Return(nil).Once()

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
