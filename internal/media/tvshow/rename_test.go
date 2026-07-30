package tvshow

import (
	"context"
	"errors"

	"github.com/datahearth/streamline/ent"
	dbmocks "github.com/datahearth/streamline/internal/db/mocks"
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
		storeMk *dbmocks.MockStore_Expecter
		svc     *RenameService
	)

	BeforeEach(func() {
		ctx = context.Background()
		store := dbmocks.NewMockStore(GinkgoT())
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
})
