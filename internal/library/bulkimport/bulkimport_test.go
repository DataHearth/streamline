package bulkimport

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stretchr/testify/mock"

	"github.com/datahearth/streamline/ent"
	entimportscanfile "github.com/datahearth/streamline/ent/importscanfile"
	dbmocks "github.com/datahearth/streamline/internal/db/mocks"
)

var _ = Describe("Service skeleton", Label("unit", "bulkimport"), func() {
	It("constructs without panic", func() {
		s := NewService(nil, nil, nil, nil, nil, nil, "/library")
		Expect(s).ToNot(BeNil())
	})
})

var _ = Describe("Service file decisions", Label("unit", "bulkimport"), func() {
	var (
		ctx   context.Context
		store *dbmocks.MockStore
		svc   *Service
	)

	BeforeEach(func() {
		ctx = context.Background()
		store = dbmocks.NewMockStore(GinkgoT())
		svc = NewService(store, nil, nil, nil, nil, nil, "/library")
	})

	Describe("UpdateFileDecision", func() {
		It("persists the decision then returns the file looked up by id", func() {
			tmdbID := uint32(27205)
			store.EXPECT().
				UpdateImportScanFileDecision(
					mock.Anything,
					uint32(12),
					entimportscanfile.DecisionAccept,
					&tmdbID,
				).
				Return(nil).
				Once()
			// The lookup must target the exact (scan, file) pair — not the
			// scan's first file — so a non-first file resolves correctly.
			store.EXPECT().
				FindImportScanFile(mock.Anything, uint32(3), uint32(12)).
				Return(&ent.ImportScanFile{ID: 12, DecisionTmdbID: tmdbID}, nil).
				Once()

			f, err := svc.UpdateFileDecision(
				ctx, 3, 12, entimportscanfile.DecisionAccept, &tmdbID,
			)
			Expect(err).ToNot(HaveOccurred())
			Expect(f.ID).To(Equal(uint32(12)))
			Expect(f.DecisionTmdbID).To(Equal(tmdbID))
		})

		It("propagates the store write error without looking up the file", func() {
			store.EXPECT().
				UpdateImportScanFileDecision(
					mock.Anything, uint32(12), entimportscanfile.DecisionSkip, (*uint32)(nil),
				).
				Return(context.DeadlineExceeded).
				Once()

			_, err := svc.UpdateFileDecision(
				ctx, 3, 12, entimportscanfile.DecisionSkip, nil,
			)
			Expect(err).To(MatchError(context.DeadlineExceeded))
		})
	})

	Describe("GetFile", func() {
		It("maps an ent not-found to ErrScanNotFound", func() {
			store.EXPECT().
				FindImportScanFile(mock.Anything, uint32(3), uint32(99)).
				Return(nil, &ent.NotFoundError{}).
				Once()

			_, err := svc.GetFile(ctx, 3, 99)
			Expect(err).To(MatchError(ErrScanNotFound))
		})
	})
})
