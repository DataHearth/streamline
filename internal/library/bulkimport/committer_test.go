package bulkimport

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stretchr/testify/mock"

	"github.com/datahearth/streamline/ent"
	entimportscan "github.com/datahearth/streamline/ent/importscan"
	entimportscanfile "github.com/datahearth/streamline/ent/importscanfile"
	entmediafile "github.com/datahearth/streamline/ent/mediafile"
	entmovie "github.com/datahearth/streamline/ent/movie"
	"github.com/datahearth/streamline/internal/db"
	dbmocks "github.com/datahearth/streamline/internal/db/mocks"
)

var _ = Describe("Service.Commit validation", Label("unit", "bulkimport"), func() {
	var (
		ctx   context.Context
		store *dbmocks.MockStore
		svc   *Service
	)

	BeforeEach(func() {
		ctx = context.Background()
		store = dbmocks.NewMockStore(GinkgoT())
		svc = NewService(store, nil, nil, nil, nil, nil, "/lib")
	})

	It("rejects when scan is not in awaiting_review status", func() {
		store.EXPECT().FindImportScan(mock.Anything, uint32(1)).
			Return(&ent.ImportScan{ID: 1, Status: entimportscan.StatusRunning}, nil).
			Once()
		err := svc.Commit(ctx, 1)
		Expect(err).To(MatchError(ErrScanNotReviewable))
	})

	It("returns ErrScanNotFound when scan does not exist", func() {
		store.EXPECT().FindImportScan(mock.Anything, uint32(2)).
			Return(nil, &ent.NotFoundError{}).Once()
		err := svc.Commit(ctx, 2)
		Expect(err).To(MatchError(ErrScanNotFound))
	})

	It("flips status to committing then dispatches runCommit", func() {
		store.EXPECT().FindImportScan(mock.Anything, uint32(3)).
			Return(&ent.ImportScan{ID: 3, Status: entimportscan.StatusAwaitingReview, Mode: entimportscan.ModeInPlace}, nil).
			Once()
		store.EXPECT().
			UpdateImportScanStatus(mock.Anything, uint32(3), entimportscan.StatusCommitting, mock.Anything).
			Return(nil).
			Once()
		// Async goroutine: allow any subsequent calls.
		store.EXPECT().
			ListImportScanFilesForCommit(mock.Anything, uint32(3)).
			Return(nil, nil).
			Maybe()
		store.EXPECT().
			UpdateImportScanStatus(mock.Anything, uint32(3), entimportscan.StatusCompleted, mock.Anything).
			Return(nil).
			Maybe()

		Expect(svc.Commit(ctx, 3)).To(Succeed())
	})
})

var _ = Describe("Service.commitAttach", Label("unit", "bulkimport"), func() {
	var (
		ctx   context.Context
		store *dbmocks.MockStore
		svc   *Service
	)

	BeforeEach(func() {
		ctx = context.Background()
		store = dbmocks.NewMockStore(GinkgoT())
		svc = NewService(store, nil, nil, nil, nil, nil, "/lib")
	})

	It(
		"creates the media file with Source=wizard and marks the movie available",
		func() {
			f := &ent.ImportScanFile{
				ID:                 7,
				ExistingMovieID:    42,
				SourcePath:         "/import/Movie.mkv",
				Size:               1_500_000_000,
				ParsedQuality:      "1080p",
				ParsedReleaseGroup: "X",
			}

			store.EXPECT().
				CreateMediaFile(mock.Anything, db.CreateMediaFileParams{
					MovieID:      42,
					Path:         "/import/Movie.mkv",
					Size:         1_500_000_000,
					Quality:      "1080p",
					ReleaseGroup: "X",
					Source:       entmediafile.SourceWizard,
				}).
				Return(&ent.MediaFile{}, nil).
				Once()
			store.EXPECT().
				UpdateMovieStatus(mock.Anything, uint32(42), entmovie.StatusAvailable).
				Return(nil).
				Once()

			outcome, msg, movieID := svc.commitAttach(ctx, f)
			Expect(outcome).To(Equal(entimportscanfile.OutcomeAttached))
			Expect(msg).To(BeEmpty())
			Expect(movieID).To(Equal(uint32(42)))
		},
	)
})
