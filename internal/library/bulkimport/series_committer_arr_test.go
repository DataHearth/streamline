package bulkimport

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stretchr/testify/mock"

	"github.com/datahearth/streamline/ent"
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
		plan, skipped := planFromSource(showTree(), []schema.SourceEpisodeFile{
			{Season: 1, Episode: 1, Path: "/srv/tv/Show/completely-unparseable.mkv"},
		})

		Expect(skipped).To(BeZero())
		Expect(plan).To(HaveLen(1))
		Expect(plan[0].episode.ID).To(Equal(uint32(11)))
		Expect(plan[0].season).To(Equal(uint16(1)))
		Expect(plan[0].path).
			To(Equal("/srv/tv/Show/completely-unparseable.mkv"))
	})

	It("skips an entry with no matching episode row and counts it", func() {
		plan, skipped := planFromSource(showTree(), []schema.SourceEpisodeFile{
			{Season: 1, Episode: 1, Path: "/a.mkv"},
			{Season: 9, Episode: 9, Path: "/b.mkv"},
		})

		Expect(plan).To(HaveLen(1))
		Expect(skipped).To(Equal(1))
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
