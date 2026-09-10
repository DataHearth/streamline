package tvshow

import (
	"context"
	"errors"
	"os"
	"path/filepath"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/mediaevent"
	enttvshow "github.com/datahearth/streamline/ent/tvshow"
	dbmocks "github.com/datahearth/streamline/internal/db/mocks"
	"github.com/datahearth/streamline/internal/events"
	"github.com/datahearth/streamline/internal/testutil/dbtest"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stretchr/testify/mock"
)

var _ = Describe("RenameService", Label("unit", "series"), func() {
	// Test-only naming template (kept tight & deterministic). Production wires
	// config.Library.SeriesNaming.
	const naming = "{title}/Season {season}/{title} - S{season:2}E{episode:2}.{ext}"

	var (
		ctx     context.Context
		store   *dbmocks.MockStore
		storeMk *dbmocks.MockStore_Expecter
		svc     *RenameService
	)

	BeforeEach(func() {
		ctx = context.Background()
		store = dbmocks.NewMockStore(GinkgoT())
		storeMk = store.EXPECT()
		svc = NewRenameService(store, "/library/tv", naming)
	})

	It("maps NotFound to ErrSeriesNotFound on preview", func() {
		storeMk.FindTVShowByID(mock.Anything, uint32(99)).
			Return(nil, &ent.NotFoundError{}).Once()

		_, err := svc.Preview(ctx, 99)
		Expect(err).To(MatchError(ErrSeriesNotFound))
		Expect(err).To(MatchError(ContainSubstring("series 99")))
	})

	It("maps NotFound to ErrSeriesNotFound on apply", func() {
		storeMk.FindTVShowByID(mock.Anything, uint32(99)).
			Return(nil, &ent.NotFoundError{}).Once()

		_, err := svc.Apply(ctx, 99)
		Expect(err).To(MatchError(ErrSeriesNotFound))
		Expect(err).To(MatchError(ContainSubstring("series 99")))
	})

	It("wraps generic lookup errors without the not-found sentinel", func() {
		storeErr := errors.New("db down")
		storeMk.FindTVShowByID(mock.Anything, uint32(1)).
			Return(nil, storeErr).Once()

		_, err := svc.Preview(ctx, 1)
		Expect(err).To(MatchError(storeErr))
		Expect(err).To(MatchError(ContainSubstring("find series")))
		Expect(err).NotTo(MatchError(ErrSeriesNotFound))
	})

	It("returns an empty plan when the show has no episode files", func() {
		storeMk.FindTVShowByID(mock.Anything, uint32(1)).
			Return(&ent.TVShow{ID: 1, Title: "The Black Sea"}, nil).Once()

		plan, err := svc.Preview(ctx, 1)
		Expect(err).NotTo(HaveOccurred())
		Expect(plan.Operations).To(BeEmpty())
	})

	Describe("Apply's event recording", func() {
		var (
			client *ent.Client
			tmp    string
			showID uint32
		)

		BeforeEach(func() {
			client = dbtest.SetupTestDB(ctx)
			DeferCleanup(client.Close)
			events.Register(client)

			tmp = GinkgoT().TempDir()
			svc = NewRenameService(store, tmp, naming)

			row := client.TVShow.Create().
				SetTitle("The Wire").
				SetOriginalTitle("The Wire").
				SetYear(2002).
				SetTvdbID(1).
				SaveX(ctx)
			showID = row.ID
		})

		It(
			"records one series-scoped file_renamed event across every season touched",
			func() {
				src1 := filepath.Join(tmp, "ep1.mkv")
				src2 := filepath.Join(tmp, "ep2.mkv")
				Expect(os.WriteFile(src1, []byte("x"), 0o644)).To(Succeed())
				Expect(os.WriteFile(src2, []byte("x"), 0o644)).To(Succeed())

				show := &ent.TVShow{
					ID:     showID,
					Title:  "The Wire",
					Year:   2002,
					TvdbID: 1,
				}
				show.Edges.Seasons = []*ent.Season{
					{Number: 1, Edges: ent.SeasonEdges{Episodes: []*ent.Episode{
						{ID: 100, Number: 1, Edges: ent.EpisodeEdges{
							MediaFiles: []*ent.MediaFile{{ID: 10, Path: src1}},
						}},
					}}},
					{Number: 2, Edges: ent.SeasonEdges{Episodes: []*ent.Episode{
						{ID: 200, Number: 1, Edges: ent.EpisodeEdges{
							MediaFiles: []*ent.MediaFile{{ID: 20, Path: src2}},
						}},
					}}},
				}
				storeMk.FindTVShowByID(mock.Anything, showID).
					Return(show, nil).
					Once()
				storeMk.UpdateMediaFilePath(
					mock.Anything, uint32(10), mock.AnythingOfType("string"),
				).Return(nil).Once()
				storeMk.UpdateMediaFilePath(
					mock.Anything, uint32(20), mock.AnythingOfType("string"),
				).Return(nil).Once()

				plan, err := svc.Apply(ctx, showID)
				Expect(err).NotTo(HaveOccurred())
				Expect(plan.Operations).To(HaveLen(2))

				evs := client.MediaEvent.Query().
					Where(
						mediaevent.TypeEQ(mediaevent.Type(events.TypeFileRenamed)),
						mediaevent.HasTvShowWith(enttvshow.IDEQ(showID)),
					).
					AllX(ctx)
				Expect(evs).To(HaveLen(1))
				Expect(evs[0].Payload).To(HaveKeyWithValue("episodes", float64(2)))
				Expect(
					evs[0].Payload["seasons"],
				).To(ConsistOf(float64(1), float64(2)))
			},
		)

		It("records nothing when the plan has no operations", func() {
			storeMk.FindTVShowByID(mock.Anything, showID).
				Return(&ent.TVShow{ID: showID, Title: "The Wire"}, nil).Once()

			plan, err := svc.Apply(ctx, showID)
			Expect(err).NotTo(HaveOccurred())
			Expect(plan.Operations).To(BeEmpty())

			evs := client.MediaEvent.Query().
				Where(mediaevent.TypeEQ(mediaevent.Type(events.TypeFileRenamed))).
				AllX(ctx)
			Expect(evs).To(BeEmpty())
		})
	})
})
