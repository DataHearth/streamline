package bulkimport

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stretchr/testify/mock"

	"github.com/datahearth/streamline/ent"
	entimportscanfile "github.com/datahearth/streamline/ent/importscanfile"
	"github.com/datahearth/streamline/internal/db"
	dbmocks "github.com/datahearth/streamline/internal/db/mocks"
)

var _ = Describe("Service skeleton", Label("unit", "bulkimport"), func() {
	It("constructs without panic", func() {
		s := NewService(nil, nil, nil, nil, nil, "/library")
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
		svc = NewService(store, nil, nil, nil, nil, "/library")
	})

	Describe("UpdateFileDecision", func() {
		It("persists the decision then returns the file looked up by id", func() {
			tmdbID := uint32(27205)
			store.EXPECT().
				UpdateImportScanFileDecision(
					mock.Anything,
					uint32(3),
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

		// The store rejects an unknown file id and a file id owned by another
		// scan the same way, so one spec covers both. No FindImportScanFile
		// expectation: the strict mock fails the spec if a read-back is
		// attempted after the write reported a miss.
		It(
			"maps a (scan, file) pair the store rejects to ErrScanFileNotFound",
			func() {
				store.EXPECT().
					UpdateImportScanFileDecision(
						mock.Anything, uint32(3), uint32(99),
						entimportscanfile.DecisionSkip, (*uint32)(nil),
					).
					Return(db.ErrImportScanFileNotFound).
					Once()

				_, err := svc.UpdateFileDecision(
					ctx, 3, 99, entimportscanfile.DecisionSkip, nil,
				)
				Expect(err).To(MatchError(ErrScanFileNotFound))
			},
		)

		It("propagates the store write error without looking up the file", func() {
			store.EXPECT().
				UpdateImportScanFileDecision(
					mock.Anything, uint32(3), uint32(12),
					entimportscanfile.DecisionSkip, (*uint32)(nil),
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
		It("maps an ent not-found to ErrScanFileNotFound", func() {
			store.EXPECT().
				FindImportScanFile(mock.Anything, uint32(3), uint32(99)).
				Return(nil, &ent.NotFoundError{}).
				Once()

			_, err := svc.GetFile(ctx, 3, 99)
			Expect(err).To(MatchError(ErrScanFileNotFound))
		})
	})

	Describe("Files", func() {
		It("maps an unknown scan id to ErrScanNotFound", func() {
			store.EXPECT().
				FindImportScan(mock.Anything, uint32(404)).
				Return(nil, &ent.NotFoundError{}).
				Once()

			_, _, err := svc.Files(ctx, FilesParams{ScanID: 404})
			Expect(err).To(MatchError(ErrScanNotFound))
		})

		It("returns an empty page for a known scan with no files", func() {
			store.EXPECT().
				FindImportScan(mock.Anything, uint32(3)).
				Return(&ent.ImportScan{ID: 3}, nil).
				Once()
			store.EXPECT().
				FilterImportScanFiles(mock.Anything, mock.MatchedBy(
					func(p db.FilterImportScanFilesParams) bool {
						return p.ScanID == 3
					},
				)).
				Return([]*ent.ImportScanFile{}, uint32(0), nil).
				Once()

			items, total, err := svc.Files(ctx, FilesParams{ScanID: 3, Limit: 50})
			Expect(err).ToNot(HaveOccurred())
			Expect(items).To(BeEmpty())
			Expect(total).To(BeZero())
		})
	})
})
