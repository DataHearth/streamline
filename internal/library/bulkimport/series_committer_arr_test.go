package bulkimport

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stretchr/testify/mock"

	"github.com/datahearth/streamline/ent"
	entimportscan "github.com/datahearth/streamline/ent/importscan"
	entimportscanshow "github.com/datahearth/streamline/ent/importscanshow"
	"github.com/datahearth/streamline/ent/schema"
	"github.com/datahearth/streamline/internal/db"
	dbmocks "github.com/datahearth/streamline/internal/db/mocks"
	"github.com/datahearth/streamline/internal/media/tvshow"
	tvmocks "github.com/datahearth/streamline/internal/media/tvshow/mocks"
)

func showTree() *ent.TVShow {
	return &ent.TVShow{
		ID: 1,
		Edges: ent.TVShowEdges{Seasons: []*ent.Season{
			{
				ID: 100, Number: 1,
				Edges: ent.SeasonEdges{Episodes: []*ent.Episode{
					{ID: 11, Number: 1}, {ID: 12, Number: 2},
				}},
			},
			{
				ID: 200, Number: 2,
				Edges: ent.SeasonEdges{Episodes: []*ent.Episode{
					{ID: 21, Number: 1},
				}},
			},
		}},
	}
}

var _ = Describe("planFromSource", Label("unit", "bulkimport"), func() {
	It("resolves by number without touching the filename", func() {
		plan, skipped, shared := planFromSource(
			showTree(),
			[]schema.SourceEpisodeFile{
				{
					Season:  1,
					Episode: 1,
					Path:    "/srv/tv/Show/completely-unparseable.mkv",
				},
			},
		)

		Expect(skipped).To(BeZero())
		Expect(shared).To(BeZero())
		Expect(plan).To(HaveLen(1))
		Expect(plan[0].episode.ID).To(Equal(uint32(11)))
		Expect(plan[0].season).To(Equal(uint16(1)))
		Expect(plan[0].path).
			To(Equal("/srv/tv/Show/completely-unparseable.mkv"))
	})

	It("skips an entry with no matching episode row and counts it", func() {
		plan, skipped, _ := planFromSource(showTree(), []schema.SourceEpisodeFile{
			{Season: 1, Episode: 1, Path: "/a.mkv"},
			{Season: 9, Episode: 9, Path: "/b.mkv"},
		})

		Expect(plan).To(HaveLen(1))
		Expect(skipped).To(Equal(1))
	})

	It("binds a multi-episode file to its first episode only", func() {
		// Sonarr embeds one S01E01E02 file under both episodes it covers.
		plan, skipped, shared := planFromSource(
			showTree(),
			[]schema.SourceEpisodeFile{
				{Season: 1, Episode: 1, Path: "/srv/tv/Show/S01E01E02.mkv"},
				{Season: 1, Episode: 2, Path: "/srv/tv/Show/S01E01E02.mkv"},
				{Season: 2, Episode: 1, Path: "/srv/tv/Show/S02E01.mkv"},
			},
		)

		Expect(skipped).To(BeZero())
		Expect(shared).To(Equal(1))
		Expect(plan).To(HaveLen(2))
		Expect(plan[0].episode.ID).To(Equal(uint32(11)))
		Expect(plan[1].episode.ID).To(Equal(uint32(21)))
	})
})

var _ = Describe("commitShow — migrated show", Label("unit", "bulkimport"), func() {
	It("still applies the source's monitoring when no file could be adopted",
		func() {
			store := dbmocks.NewMockStore(GinkgoT())
			shows := tvmocks.NewMockManager(GinkgoT())
			svc := &Service{
				store: store, seriesAdder: shows, seriesPath: GinkgoT().TempDir(),
			}
			existing := uint32(1)
			store.EXPECT().FindTVShowByID(mock.Anything, uint32(1)).
				Return(showTree(), nil).Once()
			// The source had the show unmonitored; left at its pre-commit
			// flags it would be searched in full.
			shows.EXPECT().Update(mock.Anything, uint32(1),
				mock.MatchedBy(func(p tvshow.UpdateParams) bool {
					return p.Monitored != nil && !*p.Monitored
				})).
				Return(&ent.TVShow{ID: 1}, nil).Once()

			outcome, msg, id := svc.commitShow(context.Background(),
				&ent.ImportScan{
					ID:     1,
					Kind:   entimportscan.KindSeries,
					Mode:   entimportscan.ModeInPlace,
					Source: entimportscan.SourceSonarr,
				},
				&ent.ImportScanShow{
					ExistingTvshowID: &existing,
					Monitored:        false,
					SourceFiles: []schema.SourceEpisodeFile{
						{Season: 1, Episode: 1, Path: "/nowhere/S01E01.mkv"},
					},
				})

			Expect(outcome).To(Equal(entimportscanshow.OutcomeFailed))
			Expect(msg).To(ContainSubstring("none of the 1 files"))
			Expect(id).To(Equal(uint32(1)))
		})

	It("adds a re-pointed title-only row bare, without the source's flags",
		func() {
			store := dbmocks.NewMockStore(GinkgoT())
			shows := tvmocks.NewMockManager(GinkgoT())
			svc := &Service{store: store, seriesAdder: shows}
			source, picked := uint32(1), uint32(2)
			store.EXPECT().FindTVShowByTVDBID(mock.Anything, picked).
				Return(&ent.TVShow{ID: 1}, nil).Once()
			store.EXPECT().FindTVShowByID(mock.Anything, uint32(1)).
				Return(showTree(), nil).Once()
			// No Update: the source's monitoring names another series. No
			// folder listing either — the folder does not exist, and there is
			// no file for the half-match guard to weigh.

			outcome, msg, id := svc.commitShow(context.Background(),
				&ent.ImportScan{
					ID:     1,
					Kind:   entimportscan.KindSeries,
					Mode:   entimportscan.ModeInPlace,
					Source: entimportscan.SourceSonarr,
				},
				&ent.ImportScanShow{
					FolderPath:     "/nowhere/Show",
					TvdbID:         &source,
					DecisionTvdbID: &picked,
					Monitored:      false,
				})

			Expect(outcome).To(Equal(entimportscanshow.OutcomeAttached))
			Expect(msg).To(BeEmpty())
			Expect(id).To(Equal(uint32(1)))
		})
})

var _ = Describe("rematched", Label("unit", "bulkimport"), func() {
	ids := func(n uint32) *uint32 { return &n }

	It("is true only for a new row pointed at another show than the source's",
		func() {
			Expect(rematched(&ent.ImportScanShow{
				TvdbID: ids(1), DecisionTvdbID: ids(2),
			})).To(BeTrue())
			Expect(rematched(&ent.ImportScanShow{
				TvdbID: ids(1), DecisionTvdbID: ids(1),
			})).To(BeFalse())
			Expect(rematched(&ent.ImportScanShow{TvdbID: ids(1)})).To(BeFalse())
			// An existing row resolves through ExistingTvshowID and ignores
			// the pick.
			Expect(rematched(&ent.ImportScanShow{
				TvdbID: ids(1), DecisionTvdbID: ids(2), ExistingTvshowID: ids(9),
			})).To(BeFalse())
		})
})

var _ = Describe("applyShowState", Label("unit", "bulkimport"), func() {
	It(
		"sets the show flag once, then only the seasons and episodes that differ",
		func() {
			store := dbmocks.NewMockStore(GinkgoT())
			shows := tvmocks.NewMockManager(GinkgoT())
			svc := &Service{store: store, seriesAdder: shows}

			shows.EXPECT().Update(mock.Anything, uint32(1),
				mock.MatchedBy(func(p tvshow.UpdateParams) bool {
					return p.Monitored != nil && *p.Monitored &&
						p.QualityProfile != nil && *p.QualityProfile == "HD" &&
						p.Type == nil
				})).
				Return(&ent.TVShow{ID: 1}, nil).Once()
			// Season 1 matches the show flag and is left to the show's cascade.
			store.EXPECT().CascadeSeasonMonitored(mock.Anything, uint32(200), false).
				Return(db.SeasonCascade{}, nil).Once()
			store.EXPECT().SetEpisodeMonitored(mock.Anything, uint32(12), false).
				Return(nil).Once()

			msg := svc.applyShowState(context.Background(), showTree(),
				&ent.ImportScanShow{
					Monitored: true,
					Monitoring: schema.ShowMonitoring{
						Seasons: map[uint16]bool{1: true, 2: false},
						Episodes: []schema.EpisodeFlag{
							{Season: 1, Episode: 2, Monitored: false},
							{Season: 7, Episode: 1, Monitored: true},
						},
					},
				}, "HD")

			Expect(msg).To(BeEmpty())
		},
	)
})
