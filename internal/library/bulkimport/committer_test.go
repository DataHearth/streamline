package bulkimport

import (
	"context"
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stretchr/testify/mock"

	"github.com/datahearth/streamline/ent"
	entimportscan "github.com/datahearth/streamline/ent/importscan"
	entimportscanfile "github.com/datahearth/streamline/ent/importscanfile"
	entmediafile "github.com/datahearth/streamline/ent/mediafile"
	entmovie "github.com/datahearth/streamline/ent/movie"
	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/db"
	dbmocks "github.com/datahearth/streamline/internal/db/mocks"
	"github.com/datahearth/streamline/internal/library"
	"github.com/datahearth/streamline/internal/media/movie"
	"github.com/datahearth/streamline/internal/metadata"
	metamocks "github.com/datahearth/streamline/internal/metadata/mocks"
	"github.com/datahearth/streamline/internal/testutil/configtest"
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
		svc = NewService(store, nil, nil, nil, nil, nil, "/lib", "/lib-tv")
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

	inPlaceScan := &ent.ImportScan{ID: 1, Mode: entimportscan.ModeInPlace}

	BeforeEach(func() {
		ctx = context.Background()
		store = dbmocks.NewMockStore(GinkgoT())
		svc = NewService(store, nil, nil, nil, nil, nil, "/lib", "/lib-tv")
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
				ListMediaFilesByMovieID(mock.Anything, uint32(42)).
				Return(nil, nil).
				Once()
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

			outcome, msg, movieID := svc.commitAttach(ctx, inPlaceScan, f)
			Expect(outcome).To(Equal(entimportscanfile.OutcomeAttached))
			Expect(msg).To(BeEmpty())
			Expect(movieID).To(Equal(uint32(42)))
		},
	)

	It("replaces the movie's current file before attaching", func() {
		old := filepath.Join(GinkgoT().TempDir(), "old.mkv")
		Expect(os.WriteFile(old, []byte("old"), 0o644)).To(Succeed())
		f := &ent.ImportScanFile{
			ID:              7,
			ExistingMovieID: 42,
			SourcePath:      "/import/Movie.mkv",
			Size:            1_500_000_000,
		}

		store.EXPECT().
			ListMediaFilesByMovieID(mock.Anything, uint32(42)).
			Return([]*ent.MediaFile{{ID: 9, Path: old}}, nil).
			Once()
		store.EXPECT().
			DeleteMediaFile(mock.Anything, uint32(9)).
			Return(nil).
			Once()
		store.EXPECT().
			CreateMediaFile(mock.Anything, mock.MatchedBy(func(p db.CreateMediaFileParams) bool {
				return p.MovieID == 42 && p.Path == "/import/Movie.mkv"
			})).
			Return(&ent.MediaFile{}, nil).
			Once()
		store.EXPECT().
			UpdateMovieStatus(mock.Anything, uint32(42), entmovie.StatusAvailable).
			Return(nil).
			Once()

		outcome, _, movieID := svc.commitAttach(ctx, inPlaceScan, f)
		Expect(outcome).To(Equal(entimportscanfile.OutcomeAttached))
		Expect(movieID).To(Equal(uint32(42)))
		Expect(old).NotTo(BeAnExistingFile())
	})

	It("is a no-op when the scanned path is already the movie's file", func() {
		f := &ent.ImportScanFile{
			ID:              7,
			ExistingMovieID: 42,
			SourcePath:      "/import/Movie.mkv",
		}

		store.EXPECT().
			ListMediaFilesByMovieID(mock.Anything, uint32(42)).
			Return([]*ent.MediaFile{{ID: 9, Path: "/import/Movie.mkv"}}, nil).
			Once()

		outcome, msg, movieID := svc.commitAttach(ctx, inPlaceScan, f)
		Expect(outcome).To(Equal(entimportscanfile.OutcomeAttached))
		Expect(msg).To(BeEmpty())
		Expect(movieID).To(Equal(uint32(42)))
	})
})

var _ = Describe(
	"Service.commitAttach in rename mode",
	Label("unit", "bulkimport"),
	func() {
		var (
			ctx    context.Context
			store  *dbmocks.MockStore
			svc    *Service
			libDir string
			srcDir string
		)

		BeforeEach(func() {
			ctx = context.Background()
			store = dbmocks.NewMockStore(GinkgoT())
			base := GinkgoT().TempDir()
			libDir = filepath.Join(base, "lib")
			srcDir = filepath.Join(base, "src", "Fight Club (1999)")
			Expect(os.MkdirAll(libDir, 0o755)).To(Succeed())
			Expect(os.MkdirAll(srcDir, 0o755)).To(Succeed())
			svc = NewService(store, nil, nil, library.NewImportService(
				&config.LibraryConfig{
					MoviePath:   libDir,
					MovieNaming: "{title} ({year})/{title}.{ext}",
					ImportMode:  "hardlink",
				},
			), nil, nil, libDir, libDir)
		})

		It(
			"relocates an existing movie's file instead of adopting it in place",
			func() {
				src := filepath.Join(srcDir, "Fight Club - 1999 .mkv")
				Expect(
					os.WriteFile(src, make([]byte, 60*1024*1024), 0o644),
				).To(Succeed())
				scan := &ent.ImportScan{
					ID:         1,
					Mode:       entimportscan.ModeRename,
					ImportMode: entimportscan.ImportModeHardlink,
				}
				f := &ent.ImportScanFile{ID: 7, ExistingMovieID: 42, SourcePath: src}

				store.EXPECT().ListMediaFilesByMovieID(mock.Anything, uint32(42)).
					Return(nil, nil).Once()
				store.EXPECT().FindMovieByID(mock.Anything, uint32(42)).
					Return(&ent.Movie{ID: 42, Title: "Fight Club", Year: 1999}, nil).
					Once()
				var recorded db.CreateMediaFileParams
				store.EXPECT().CreateMediaFile(mock.Anything, mock.Anything).
					Run(func(_ context.Context, p db.CreateMediaFileParams) {
						recorded = p
					}).Return(&ent.MediaFile{}, nil).Once()
				store.EXPECT().
					UpdateMovieStatus(mock.Anything, uint32(42), entmovie.StatusAvailable).
					Return(nil).Once()

				outcome, msg, movieID := svc.commitAttach(ctx, scan, f)
				Expect(msg).To(BeEmpty())
				Expect(outcome).To(Equal(entimportscanfile.OutcomeAttached))
				Expect(movieID).To(Equal(uint32(42)))
				Expect(recorded.Path).To(HavePrefix(libDir))
				Expect(recorded.Path).To(BeAnExistingFile())
			},
		)
	},
)

var _ = Describe("Service.addOrFindMovie", Label("unit", "bulkimport"), func() {
	var (
		ctx   context.Context
		store *dbmocks.MockStore
		meta  *metamocks.MockProvider
		svc   *Service
	)

	const tmdbID = uint32(49948)

	BeforeEach(func() {
		configtest.Setup(map[string]any{
			"quality_profiles": []map[string]any{{
				"name": "hd", "preferred_resolution": "1080p",
				"min_resolution": "720p",
			}},
			"quality_default_profile": "hd",
		})
		ctx = context.Background()
		store = dbmocks.NewMockStore(GinkgoT())
		meta = metamocks.NewMockProvider(GinkgoT())
		svc = NewService(store, meta, nil, nil,
			movie.NewService(store, meta, nil, nil), nil, "/lib", "/lib-tv")
	})

	It("returns the existing row when another scan added the movie first", func() {
		meta.EXPECT().GetMovie(mock.Anything, tmdbID).
			Return(&metadata.MovieDetails{
				MovieResult: metadata.MovieResult{
					TMDBID: tmdbID, Title: "Fantasia 2000", Year: 2000,
				},
			}, nil).Once()
		store.EXPECT().CreateMovie(mock.Anything, mock.Anything).
			Return(nil, &ent.ConstraintError{}).Once()
		store.EXPECT().FindMovieByTMDBID(mock.Anything, tmdbID).
			Return(&ent.Movie{ID: 3, TmdbID: tmdbID}, nil).Once()

		m, err := svc.addOrFindMovie(ctx, tmdbID)
		Expect(err).NotTo(HaveOccurred())
		Expect(m.ID).To(Equal(uint32(3)))
	})
})
