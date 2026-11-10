package bulkimport

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"sync"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stretchr/testify/mock"

	"github.com/datahearth/streamline/ent"
	entimportscan "github.com/datahearth/streamline/ent/importscan"
	entimportscanbook "github.com/datahearth/streamline/ent/importscanbook"
	entimportscanfile "github.com/datahearth/streamline/ent/importscanfile"
	entmediafile "github.com/datahearth/streamline/ent/mediafile"
	entmovie "github.com/datahearth/streamline/ent/movie"
	"github.com/datahearth/streamline/internal/db"
	dbmocks "github.com/datahearth/streamline/internal/db/mocks"
	"github.com/datahearth/streamline/internal/library"
	"github.com/datahearth/streamline/internal/library/roothealth"
	"github.com/datahearth/streamline/internal/media/book"
	"github.com/datahearth/streamline/internal/media/movie"
	msmocks "github.com/datahearth/streamline/internal/mediaserver/mocks"
	"github.com/datahearth/streamline/internal/metadata"
	metamocks "github.com/datahearth/streamline/internal/metadata/mocks"
	"github.com/datahearth/streamline/internal/testutil/configtest"
	"github.com/datahearth/streamline/internal/testutil/roottest"
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
		svc = NewService(
			store,
			nil,
			nil,
			nil,
			nil,
			nil,
			nil,
			"/lib",
			"/lib-tv",
			nil,
			nil,
			nil,
			nil,
		)
	})

	It("rejects when scan is not in awaiting_review status", func() {
		store.EXPECT().FindImportScan(mock.Anything, uint32(1)).
			Return(&ent.ImportScan{ID: 1, Status: entimportscan.StatusRunning}, nil).
			Once()
		err := svc.Commit(ctx, 1)
		Expect(err).To(MatchError(ErrScanNotReviewable))
	})

	It("refuses a book scan when Hardcover is not configured", func() {
		store.EXPECT().FindImportScan(mock.Anything, uint32(4)).
			Return(&ent.ImportScan{
				ID:     4,
				Kind:   entimportscan.KindBook,
				Status: entimportscan.StatusAwaitingReview,
			}, nil).Once()

		Expect(svc.Commit(ctx, 4)).To(MatchError(book.ErrNotConfigured))
	})

	It(
		"refuses a commit into an offline root and leaves the scan reviewable",
		func() {
			configtest.Setup()
			roottest.Offline(db.RootMusic)
			store.EXPECT().FindImportScan(mock.Anything, uint32(5)).
				Return(&ent.ImportScan{
					ID:     5,
					Kind:   entimportscan.KindMusic,
					Status: entimportscan.StatusAwaitingReview,
				}, nil).Once()

			_, refused := errors.AsType[*roothealth.OfflineError](svc.Commit(ctx, 5))

			Expect(refused).To(BeTrue())
		},
	)

	Describe("the roots a book commit needs", func() {
		roots := func(slots ...entimportscanbook.Slot) []db.LibraryRoot {
			GinkgoHelper()
			rows := make([]*ent.ImportScanBook, len(slots))
			for i, slot := range slots {
				rows[i] = &ent.ImportScanBook{Slot: slot}
			}
			store.EXPECT().ListImportScanBooksForCommit(mock.Anything, uint32(6)).
				Return(rows, nil).Once()
			got, err := svc.scanRoots(
				ctx,
				&ent.ImportScan{ID: 6, Kind: entimportscan.KindBook},
			)
			Expect(err).NotTo(HaveOccurred())
			return got
		}

		It("needs only the audiobook root for audiobook rows", func() {
			Expect(
				roots(
					entimportscanbook.SlotAudiobook,
					entimportscanbook.SlotAudiobook,
				),
			).
				To(ConsistOf(db.RootAudiobooks))
		})

		It("needs both roots for a mixed scan", func() {
			Expect(
				roots(entimportscanbook.SlotEbook, entimportscanbook.SlotAudiobook),
			).
				To(ConsistOf(db.RootEbooks, db.RootAudiobooks))
		})
	})

	It("returns ErrScanNotFound when scan does not exist", func() {
		store.EXPECT().FindImportScan(mock.Anything, uint32(2)).
			Return(nil, &ent.NotFoundError{}).Once()
		err := svc.Commit(ctx, 2)
		Expect(err).To(MatchError(ErrScanNotFound))
	})

	It("flips status to committing then dispatches runCommit", func() {
		store.EXPECT().FindImportScan(mock.Anything, uint32(3)).
			Return(&ent.ImportScan{ID: 3, Kind: entimportscan.KindMovie, Status: entimportscan.StatusAwaitingReview, Mode: entimportscan.ModeInPlace}, nil).
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

var _ = Describe("Service.runCommit", Label("unit", "bulkimport"), func() {
	var (
		ctx   context.Context
		store *dbmocks.MockStore
		ms    *msmocks.MockRefresher
		svc   *Service
		scan  *ent.ImportScan
	)

	BeforeEach(func() {
		ctx = context.Background()
		store = dbmocks.NewMockStore(GinkgoT())
		ms = msmocks.NewMockRefresher(GinkgoT())
		svc = NewService(
			store,
			nil,
			nil,
			nil,
			nil,
			nil,
			ms,
			"/lib",
			"/lib-tv",
			nil,
			nil,
			nil,
			nil,
		)
		scan = &ent.ImportScan{ID: 3, Mode: entimportscan.ModeInPlace}
		store.EXPECT().
			UpdateImportScanStatus(mock.Anything, uint32(3), entimportscan.StatusCompleted, mock.Anything).
			Return(nil).
			Once()
	})

	It("rescans the media servers once something committed", func() {
		store.EXPECT().ListImportScanFilesForCommit(mock.Anything, uint32(3)).
			Return([]*ent.ImportScanFile{{
				ID:              1,
				Classification:  entimportscanfile.ClassificationExisting,
				ExistingMovieID: 5,
				SourcePath:      "/lib/Movie (2020)/Movie.mkv",
			}}, nil).
			Once()
		store.EXPECT().ListMediaFilesByMovieID(mock.Anything, uint32(5)).
			Return([]*ent.MediaFile{{Path: "/lib/Movie (2020)/Movie.mkv"}}, nil).
			Once()
		store.EXPECT().
			UpdateImportScanFileOutcome(mock.Anything, uint32(1), entimportscanfile.OutcomeAttached, mock.Anything).
			Return(nil).
			Once()
		refreshed := make(chan struct{})
		ms.EXPECT().RefreshAll(mock.Anything, "movie", "/lib").
			Run(func(context.Context, string, string, ...string) { close(refreshed) }).
			Return(nil).Once()

		svc.runCommit(ctx, scan)
		Eventually(refreshed).Should(BeClosed())
	})

	It("leaves the media servers alone when nothing committed", func() {
		store.EXPECT().ListImportScanFilesForCommit(mock.Anything, uint32(3)).
			Return(nil, nil).
			Once()
		refreshed := make(chan struct{})
		ms.EXPECT().RefreshAll(mock.Anything, mock.Anything, mock.Anything).
			Run(func(context.Context, string, string, ...string) { close(refreshed) }).
			Return(nil).Maybe()

		svc.runCommit(ctx, scan)
		Consistently(refreshed).ShouldNot(BeClosed())
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
		svc = NewService(
			store,
			nil,
			nil,
			nil,
			nil,
			nil,
			nil,
			"/lib",
			"/lib-tv",
			nil,
			nil,
			nil,
			nil,
		)
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
				FindMovieByID(mock.Anything, uint32(42)).
				Return(&ent.Movie{ID: 42}, nil).
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
		dir := GinkgoT().TempDir()
		old := filepath.Join(dir, "old.mkv")
		Expect(os.WriteFile(old, []byte("old"), 0o644)).To(Succeed())
		src := filepath.Join(dir, "Movie.mkv")
		Expect(os.WriteFile(src, []byte("new"), 0o644)).To(Succeed())
		f := &ent.ImportScanFile{
			ID:              7,
			ExistingMovieID: 42,
			SourcePath:      src,
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
			FindMovieByID(mock.Anything, uint32(42)).
			Return(&ent.Movie{ID: 42}, nil).
			Once()
		store.EXPECT().
			CreateMediaFile(mock.Anything, mock.MatchedBy(func(p db.CreateMediaFileParams) bool {
				return p.MovieID == 42 && p.Path == src
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
		Expect(src).To(BeAnExistingFile())
	})

	It("keeps the tracked file and its row when the scanned file is gone", func() {
		dir := GinkgoT().TempDir()
		old := filepath.Join(dir, "old.mkv")
		Expect(os.WriteFile(old, []byte("old"), 0o644)).To(Succeed())
		f := &ent.ImportScanFile{
			ID:              7,
			ExistingMovieID: 42,
			// Scanned, then removed before the commit ran.
			SourcePath: filepath.Join(dir, "Movie.mkv"),
		}

		// No create and no delete: the mock fails the spec on either.
		store.EXPECT().ListMediaFilesByMovieID(mock.Anything, uint32(42)).
			Return([]*ent.MediaFile{{ID: 9, Path: old}}, nil).Once()

		outcome, msg, _ := svc.commitAttach(ctx, inPlaceScan, f)

		Expect(outcome).To(Equal(entimportscanfile.OutcomeFailed))
		Expect(msg).To(ContainSubstring("stat source"))
		Expect(old).To(BeAnExistingFile())
	})

	It("removes only the files whose rows it deleted", func() {
		dir := GinkgoT().TempDir()
		first := filepath.Join(dir, "first.mkv")
		second := filepath.Join(dir, "second.mkv")
		src := filepath.Join(dir, "Movie.mkv")
		for _, p := range []string{first, second, src} {
			Expect(os.WriteFile(p, []byte(p), 0o644)).To(Succeed())
		}
		f := &ent.ImportScanFile{ID: 7, ExistingMovieID: 42, SourcePath: src}

		store.EXPECT().ListMediaFilesByMovieID(mock.Anything, uint32(42)).
			Return([]*ent.MediaFile{
				{ID: 9, Path: first}, {ID: 10, Path: second},
			}, nil).Once()
		store.EXPECT().FindMovieByID(mock.Anything, uint32(42)).
			Return(&ent.Movie{ID: 42}, nil).Once()
		store.EXPECT().CreateMediaFile(mock.Anything, mock.Anything).
			Return(&ent.MediaFile{}, nil).Once()
		store.EXPECT().DeleteMediaFile(mock.Anything, uint32(9)).
			Return(nil).Once()
		store.EXPECT().DeleteMediaFile(mock.Anything, uint32(10)).
			Return(errors.New("database is locked")).Once()

		outcome, _, _ := svc.commitAttach(ctx, inPlaceScan, f)

		Expect(outcome).To(Equal(entimportscanfile.OutcomeFailed))
		Expect(first).NotTo(BeAnExistingFile())
		Expect(second).To(BeAnExistingFile())
	})

	It("keeps the tracked file when the create fails", func() {
		dir := GinkgoT().TempDir()
		old := filepath.Join(dir, "old.mkv")
		Expect(os.WriteFile(old, []byte("old"), 0o644)).To(Succeed())
		src := filepath.Join(dir, "Movie.mkv")
		Expect(os.WriteFile(src, []byte("new"), 0o644)).To(Succeed())
		f := &ent.ImportScanFile{ID: 7, ExistingMovieID: 42, SourcePath: src}

		store.EXPECT().ListMediaFilesByMovieID(mock.Anything, uint32(42)).
			Return([]*ent.MediaFile{{ID: 9, Path: old}}, nil).Once()
		store.EXPECT().FindMovieByID(mock.Anything, uint32(42)).
			Return(&ent.Movie{ID: 42}, nil).Once()
		store.EXPECT().CreateMediaFile(mock.Anything, mock.Anything).
			Return(nil, errors.New("disk I/O error")).Once()

		outcome, _, _ := svc.commitAttach(ctx, inPlaceScan, f)

		Expect(outcome).To(Equal(entimportscanfile.OutcomeFailed))
		Expect(old).To(BeAnExistingFile())
	})

	It(
		"is a no-op when the scanned path reaches the movie's file through a symlink",
		func() {
			base := GinkgoT().TempDir()
			realRoot := filepath.Join(base, "real")
			Expect(os.MkdirAll(realRoot, 0o755)).To(Succeed())
			Expect(os.WriteFile(
				filepath.Join(realRoot, "Movie.mkv"), []byte("movie"), 0o644,
			)).To(Succeed())
			link := filepath.Join(base, "library")
			Expect(os.Symlink(realRoot, link)).To(Succeed())
			// The row keeps the configured root's spelling; the scanner
			// resolves symlinks before it stores a path.
			tracked := filepath.Join(link, "Movie.mkv")
			f := &ent.ImportScanFile{
				ID:              7,
				ExistingMovieID: 42,
				SourcePath:      filepath.Join(realRoot, "Movie.mkv"),
			}

			store.EXPECT().ListMediaFilesByMovieID(mock.Anything, uint32(42)).
				Return([]*ent.MediaFile{{ID: 9, Path: tracked}}, nil).Once()

			outcome, msg, movieID := svc.commitAttach(ctx, inPlaceScan, f)

			Expect(outcome).To(Equal(entimportscanfile.OutcomeAttached))
			Expect(msg).To(BeEmpty())
			Expect(movieID).To(Equal(uint32(42)))
			Expect(tracked).To(BeAnExistingFile())
		},
	)

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

	It(
		"queues a transcode job when the movie's profile is transcode-eligible",
		func() {
			configtest.Setup(map[string]any{
				"transcoding": map[string]any{"enabled": true},
				"quality_profiles": []map[string]any{{
					"name": "hd", "preferred_resolution": "1080p",
					"min_resolution": "720p",
					"transcode": map[string]any{
						"to": map[string]any{
							"container": "mkv", "video_codec": "hevc",
							"preset": "medium", "audio_codec": "aac",
						},
					},
				}},
				"movie_quality_default_profile":  "hd",
				"series_quality_default_profile": "hd",
			})
			f := &ent.ImportScanFile{
				ID: 7, ExistingMovieID: 42, SourcePath: "/import/Movie.mkv",
				Size: 1_500_000_000,
			}

			store.EXPECT().
				ListMediaFilesByMovieID(mock.Anything, uint32(42)).
				Return(nil, nil).
				Once()
			store.EXPECT().
				FindMovieByID(mock.Anything, uint32(42)).
				Return(&ent.Movie{ID: 42, QualityProfile: "hd"}, nil).
				Once()
			store.EXPECT().
				CreateMediaFile(mock.Anything, mock.MatchedBy(func(p db.CreateMediaFileParams) bool {
					return p.QueueTranscode
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
		},
	)
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
			configtest.Setup(map[string]any{
				"library": map[string]any{
					"movie_path":   libDir,
					"movie_naming": "{title} ({year})/{title}.{ext}",
					"import_mode":  "hardlink",
				},
			})
			svc = NewService(
				store,
				nil,
				nil,
				library.NewImportService(),
				nil,
				nil,
				nil,
				libDir,
				libDir,
				nil, nil, nil, nil,
			)
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

		renameScan := &ent.ImportScan{
			ID:         1,
			Mode:       entimportscan.ModeRename,
			ImportMode: entimportscan.ImportModeHardlink,
		}
		video := func(path, content string) {
			GinkgoHelper()
			Expect(os.MkdirAll(filepath.Dir(path), 0o755)).To(Succeed())
			body := make([]byte, 60*1024*1024)
			copy(body, content)
			Expect(os.WriteFile(path, body, 0o644)).To(Succeed())
		}
		head := func(path string, n int) string {
			GinkgoHelper()
			body, err := os.ReadFile(path)
			Expect(err).NotTo(HaveOccurred())
			return string(body[:n])
		}

		It("keeps the tracked file and its row when the import fails", func() {
			old := filepath.Join(libDir, "Fight Club (1999) [old]", "Fight Club.mkv")
			video(old, "old")
			// Another file already sits where the scanned one would land.
			video(
				filepath.Join(libDir, "Fight Club (1999)", "Fight Club.mkv"),
				"foreign",
			)
			src := filepath.Join(srcDir, "Fight Club 1999 1080p.mkv")
			video(src, "new")
			f := &ent.ImportScanFile{ID: 7, ExistingMovieID: 42, SourcePath: src}

			// No DeleteMediaFile and no CreateMediaFile: the mock fails the
			// spec on either.
			store.EXPECT().ListMediaFilesByMovieID(mock.Anything, uint32(42)).
				Return([]*ent.MediaFile{{ID: 9, Path: old}}, nil).Once()
			store.EXPECT().FindMovieByID(mock.Anything, uint32(42)).
				Return(&ent.Movie{ID: 42, Title: "Fight Club", Year: 1999}, nil).
				Once()

			outcome, msg, _ := svc.commitAttach(ctx, renameScan, f)

			Expect(outcome).To(Equal(entimportscanfile.OutcomeFailed))
			Expect(msg).To(ContainSubstring("import movie"))
			Expect(head(old, 3)).To(Equal("old"))
			Expect(old + replacedSuffix).NotTo(BeAnExistingFile())
		})

		It("replaces a tracked file sitting at the destination", func() {
			dest := filepath.Join(libDir, "Fight Club (1999)", "Fight Club.mkv")
			video(dest, "old")
			src := filepath.Join(srcDir, "Fight Club 1999 1080p.mkv")
			video(src, "new")
			f := &ent.ImportScanFile{ID: 7, ExistingMovieID: 42, SourcePath: src}

			store.EXPECT().ListMediaFilesByMovieID(mock.Anything, uint32(42)).
				Return([]*ent.MediaFile{{ID: 9, Path: dest}}, nil).Once()
			store.EXPECT().FindMovieByID(mock.Anything, uint32(42)).
				Return(&ent.Movie{ID: 42, Title: "Fight Club", Year: 1999}, nil).
				Once()
			created := false
			store.EXPECT().CreateMediaFile(mock.Anything, mock.Anything).
				Run(func(context.Context, db.CreateMediaFileParams) {
					created = true
				}).Return(&ent.MediaFile{}, nil).Once()
			store.EXPECT().DeleteMediaFile(mock.Anything, uint32(9)).
				Run(func(context.Context, uint32) {
					// The old row goes only once the new one exists.
					Expect(created).To(BeTrue())
				}).Return(nil).Once()
			store.EXPECT().
				UpdateMovieStatus(mock.Anything, uint32(42), entmovie.StatusAvailable).
				Return(nil).Once()

			outcome, msg, _ := svc.commitAttach(ctx, renameScan, f)

			Expect(msg).To(BeEmpty())
			Expect(outcome).To(Equal(entimportscanfile.OutcomeAttached))
			Expect(head(dest, 3)).To(Equal("new"))
			Expect(dest + replacedSuffix).NotTo(BeAnExistingFile())
		})

		It("drops the set-aside file of each row it deleted and puts back the rest",
			func() {
				first := filepath.Join(
					libDir,
					"Fight Club (1999) [a]",
					"Fight Club.mkv",
				)
				second := filepath.Join(
					libDir,
					"Fight Club (1999) [b]",
					"Fight Club.mkv",
				)
				video(first, "one")
				video(second, "two")
				src := filepath.Join(srcDir, "Fight Club 1999 1080p.mkv")
				video(src, "new")
				f := &ent.ImportScanFile{ID: 7, ExistingMovieID: 42, SourcePath: src}

				store.EXPECT().ListMediaFilesByMovieID(mock.Anything, uint32(42)).
					Return([]*ent.MediaFile{
						{ID: 9, Path: first}, {ID: 10, Path: second},
					}, nil).Once()
				store.EXPECT().FindMovieByID(mock.Anything, uint32(42)).
					Return(&ent.Movie{ID: 42, Title: "Fight Club", Year: 1999}, nil).
					Once()
				store.EXPECT().CreateMediaFile(mock.Anything, mock.Anything).
					Return(&ent.MediaFile{}, nil).Once()
				store.EXPECT().DeleteMediaFile(mock.Anything, uint32(9)).
					Return(nil).Once()
				store.EXPECT().DeleteMediaFile(mock.Anything, uint32(10)).
					Return(errors.New("database is locked")).Once()

				outcome, _, _ := svc.commitAttach(ctx, renameScan, f)

				Expect(outcome).To(Equal(entimportscanfile.OutcomeFailed))
				Expect(first).NotTo(BeAnExistingFile())
				Expect(first + replacedSuffix).NotTo(BeAnExistingFile())
				Expect(head(second, 3)).To(Equal("two"))
				Expect(second + replacedSuffix).NotTo(BeAnExistingFile())
			},
		)

		It("replaces around a tracked file it cannot set aside", func() {
			old := filepath.Join(libDir, "Fight Club (1999) [old]", "Fight Club.mkv")
			video(old, "old")
			// What an interrupted replacement left: never renamed over.
			Expect(os.WriteFile(old+replacedSuffix, []byte("kept"), 0o644)).
				To(Succeed())
			src := filepath.Join(srcDir, "Fight Club 1999 1080p.mkv")
			video(src, "new")
			f := &ent.ImportScanFile{ID: 7, ExistingMovieID: 42, SourcePath: src}

			store.EXPECT().ListMediaFilesByMovieID(mock.Anything, uint32(42)).
				Return([]*ent.MediaFile{{ID: 9, Path: old}}, nil).Once()
			store.EXPECT().FindMovieByID(mock.Anything, uint32(42)).
				Return(&ent.Movie{ID: 42, Title: "Fight Club", Year: 1999}, nil).
				Once()
			var recorded db.CreateMediaFileParams
			store.EXPECT().CreateMediaFile(mock.Anything, mock.Anything).
				Run(func(_ context.Context, p db.CreateMediaFileParams) {
					recorded = p
				}).Return(&ent.MediaFile{}, nil).Once()
			store.EXPECT().DeleteMediaFile(mock.Anything, uint32(9)).
				Return(nil).Once()
			store.EXPECT().
				UpdateMovieStatus(mock.Anything, uint32(42), entmovie.StatusAvailable).
				Return(nil).Once()

			outcome, msg, _ := svc.commitAttach(ctx, renameScan, f)

			Expect(msg).To(BeEmpty())
			Expect(outcome).To(Equal(entimportscanfile.OutcomeAttached))
			Expect(head(recorded.Path, 3)).To(Equal("new"))
			Expect(old).NotTo(BeAnExistingFile())
			Expect(head(old+replacedSuffix, 4)).To(Equal("kept"))
		})

		It("commits each movie of a flat folder holding several", func() {
			flat := filepath.Dir(srcDir)
			first := filepath.Join(flat, "Fight Club 1999 1080p.mkv")
			second := filepath.Join(flat, "Se7en 1995 1080p.mkv")
			video(first, "one")
			video(second, "two")

			for id, tc := range map[uint32]struct {
				src   string
				movie *ent.Movie
			}{
				42: {first, &ent.Movie{ID: 42, Title: "Fight Club", Year: 1999}},
				43: {second, &ent.Movie{ID: 43, Title: "Se7en", Year: 1995}},
			} {
				store.EXPECT().ListMediaFilesByMovieID(mock.Anything, id).
					Return(nil, nil).Once()
				store.EXPECT().FindMovieByID(mock.Anything, id).
					Return(tc.movie, nil).Once()
				var recorded db.CreateMediaFileParams
				store.EXPECT().CreateMediaFile(mock.Anything, mock.Anything).
					Run(func(_ context.Context, p db.CreateMediaFileParams) {
						recorded = p
					}).Return(&ent.MediaFile{}, nil).Once()
				store.EXPECT().
					UpdateMovieStatus(mock.Anything, id, entmovie.StatusAvailable).
					Return(nil).Once()

				outcome, msg, _ := svc.commitAttach(
					ctx,
					renameScan,
					&ent.ImportScanFile{
						ID: id, ExistingMovieID: id, SourcePath: tc.src,
					},
				)

				Expect(msg).To(BeEmpty())
				Expect(outcome).To(Equal(entimportscanfile.OutcomeAttached))
				Expect(recorded.Path).To(ContainSubstring(tc.movie.Title))
				Expect(recorded.Path).To(BeAnExistingFile())
			}
		})

		// runCommit commits several files at once, and the default naming
		// renders two same-resolution copies of a movie to one destination.
		DescribeTable(
			"commits two scanned copies of one tracked movie at the same time",
			func(mode entimportscan.ImportMode) {
				scan := &ent.ImportScan{
					ID: 1, Mode: entimportscan.ModeRename, ImportMode: mode,
				}
				sparse := func(path, content string) {
					GinkgoHelper()
					Expect(os.MkdirAll(filepath.Dir(path), 0o755)).To(Succeed())
					Expect(os.WriteFile(path, []byte(content), 0o644)).To(Succeed())
					Expect(os.Truncate(path, 60*1024*1024)).To(Succeed())
				}
				dest := filepath.Join(libDir, "Fight Club (1999)", "Fight Club.mkv")
				srcs := []string{
					filepath.Join(srcDir, "A", "Fight Club 1999 1080p.mkv"),
					filepath.Join(srcDir, "B", "Fight Club 1999 2160p.mkv"),
				}

				// A store that keeps its rows: each commit has to see what
				// the other one wrote.
				var mu sync.Mutex
				var rows map[uint32]string
				var nextID uint32
				store.EXPECT().ListMediaFilesByMovieID(mock.Anything, uint32(42)).
					RunAndReturn(func(context.Context, uint32) ([]*ent.MediaFile, error) {
						mu.Lock()
						defer mu.Unlock()
						out := make([]*ent.MediaFile, 0, len(rows))
						for id, p := range rows {
							out = append(out, &ent.MediaFile{ID: id, Path: p})
						}
						return out, nil
					})
				store.EXPECT().FindMovieByID(mock.Anything, uint32(42)).
					Return(&ent.Movie{ID: 42, Title: "Fight Club", Year: 1999}, nil)
				store.EXPECT().CreateMediaFile(mock.Anything, mock.Anything).
					RunAndReturn(func(
						_ context.Context, p db.CreateMediaFileParams,
					) (*ent.MediaFile, error) {
						mu.Lock()
						defer mu.Unlock()
						nextID++
						rows[nextID] = p.Path
						return &ent.MediaFile{ID: nextID}, nil
					})
				store.EXPECT().DeleteMediaFile(mock.Anything, mock.Anything).
					RunAndReturn(func(_ context.Context, id uint32) error {
						mu.Lock()
						defer mu.Unlock()
						if _, ok := rows[id]; !ok {
							return errors.New("media_file not found")
						}
						delete(rows, id)
						return nil
					})
				store.EXPECT().
					UpdateMovieStatus(mock.Anything, uint32(42), entmovie.StatusAvailable).
					Return(nil)

				for range 20 {
					sparse(dest, "old")
					sparse(srcs[0], "aaa")
					sparse(srcs[1], "bbb")
					rows, nextID = map[uint32]string{1: dest}, 1

					var wg sync.WaitGroup
					msgs := make([]string, len(srcs))
					for n, src := range srcs {
						wg.Go(func() {
							defer GinkgoRecover()
							_, msgs[n], _ = svc.commitAttach(
								ctx,
								scan,
								&ent.ImportScanFile{
									ID: uint32(
										n + 1,
									),
									ExistingMovieID: 42,
									SourcePath:      src,
								},
							)
						})
					}
					wg.Wait()

					// The later commit replaced the earlier one's file: one
					// row, one file, and it is one of the two scanned.
					Expect(msgs).To(HaveEach(BeEmpty()))
					Expect(rows).To(HaveLen(1))
					Expect(slices.Collect(maps.Values(rows))).To(ConsistOf(dest))
					Expect(head(dest, 3)).To(BeElementOf("aaa", "bbb"))
					Expect(dest + replacedSuffix).NotTo(BeAnExistingFile())
					if mode == entimportscan.ImportModeHardlink {
						Expect(head(srcs[0], 3)).To(Equal("aaa"))
						Expect(head(srcs[1], 3)).To(Equal("bbb"))
					}
					Expect(os.Remove(dest)).To(Succeed())
				}
			},
			Entry("hardlinking", entimportscan.ImportModeHardlink),
			Entry("moving", entimportscan.ImportModeMove),
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
			"movie_quality_default_profile":  "hd",
			"series_quality_default_profile": "hd",
		})
		ctx = context.Background()
		store = dbmocks.NewMockStore(GinkgoT())
		// movie.Service enriches cast after every add; nothing to enrich here.
		store.EXPECT().PeopleNeedingDetails(
			mock.Anything, mock.Anything, mock.Anything,
		).Return(nil, nil).Maybe()
		meta = metamocks.NewMockProvider(GinkgoT())
		svc = NewService(
			store,
			meta,
			nil,
			nil,
			movie.NewService(
				store,
				meta,
				nil,
				nil,
				nil,
			),
			nil,
			nil,
			"/lib",
			"/lib-tv",
			nil, nil, nil, nil,
		)
	})

	It("returns the existing row when another scan added the movie first", func() {
		meta.EXPECT().GetMovie(mock.Anything, tmdbID).
			Return(&metadata.MovieDetails{
				TMDBID: tmdbID, Title: "Fantasia 2000", Year: 2000,
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

var _ = Describe("Service.commitAdoptInPlace", Label("unit", "bulkimport"), func() {
	var (
		ctx   context.Context
		store *dbmocks.MockStore
		meta  *metamocks.MockProvider
		svc   *Service
	)

	const tmdbID = uint32(55001)

	BeforeEach(func() {
		ctx = context.Background()
		store = dbmocks.NewMockStore(GinkgoT())
		// movie.Service enriches cast after every add; nothing to enrich here.
		store.EXPECT().PeopleNeedingDetails(
			mock.Anything, mock.Anything, mock.Anything,
		).Return(nil, nil).Maybe()
		meta = metamocks.NewMockProvider(GinkgoT())
	})

	It(
		"queues a transcode job when the created movie's profile is transcode-eligible",
		func() {
			configtest.Setup(map[string]any{
				"transcoding": map[string]any{"enabled": true},
				// tmdb_region defaults to "FR", which makes Add fetch a digital
				// release date — irrelevant here and not stubbed on the mock.
				"metadata": map[string]any{"tmdb_region": ""},
				"quality_profiles": []map[string]any{{
					"name": "hd", "preferred_resolution": "1080p",
					"min_resolution": "720p",
					"transcode": map[string]any{
						"to": map[string]any{
							"container": "mkv", "video_codec": "hevc",
							"preset": "medium", "audio_codec": "aac",
						},
					},
				}},
				"movie_quality_default_profile":  "hd",
				"series_quality_default_profile": "hd",
			})
			svc = NewService(
				store,
				meta,
				nil,
				nil,
				movie.NewService(
					store,
					meta,
					nil,
					nil,
					nil,
				),
				nil,
				nil,
				"/lib",
				"/lib-tv",
				nil, nil, nil, nil,
			)
			f := &ent.ImportScanFile{
				ID: 7, SourcePath: "/import/Movie.mkv", Size: 1_500_000_000,
				ParsedQuality: "1080p", ParsedReleaseGroup: "X",
			}

			meta.EXPECT().GetMovie(mock.Anything, tmdbID).
				Return(&metadata.MovieDetails{
					TMDBID: tmdbID, Title: "Fantasia 2000", Year: 2000,
				}, nil).Once()
			// Add is called with an empty quality profile (resolved later at
			// read time), so the created row's QualityProfile is empty here too
			// — config.TranscodeEligible("") is what falls back to the default.
			store.EXPECT().CreateMovie(mock.Anything, mock.Anything).
				Return(&ent.Movie{ID: 42}, nil).Once()
			store.EXPECT().
				CreateMediaFile(mock.Anything, mock.MatchedBy(func(p db.CreateMediaFileParams) bool {
					return p.QueueTranscode
				})).
				Return(&ent.MediaFile{}, nil).
				Once()
			store.EXPECT().
				UpdateMovieStatus(mock.Anything, uint32(42), entmovie.StatusAvailable).
				Return(nil).
				Once()

			outcome, msg, movieID := svc.commitAdoptInPlace(ctx, f, tmdbID)
			Expect(msg).To(BeEmpty())
			Expect(outcome).To(Equal(entimportscanfile.OutcomeCreated))
			Expect(movieID).To(Equal(uint32(42)))
		},
	)

	It("does not queue a transcode job when transcoding is disabled", func() {
		configtest.Setup(map[string]any{
			"metadata": map[string]any{"tmdb_region": ""},
			"quality_profiles": []map[string]any{{
				"name": "hd", "preferred_resolution": "1080p",
				"min_resolution": "720p",
			}},
			"movie_quality_default_profile":  "hd",
			"series_quality_default_profile": "hd",
		})
		svc = NewService(
			store,
			meta,
			nil,
			nil,
			movie.NewService(
				store,
				meta,
				nil,
				nil,
				nil,
			),
			nil,
			nil,
			"/lib",
			"/lib-tv",
			nil, nil, nil, nil,
		)
		f := &ent.ImportScanFile{
			ID: 8, SourcePath: "/import/Movie2.mkv", Size: 1_500_000_000,
		}

		meta.EXPECT().GetMovie(mock.Anything, tmdbID).
			Return(&metadata.MovieDetails{
				TMDBID: tmdbID, Title: "Fantasia 2000", Year: 2000,
			}, nil).Once()
		store.EXPECT().CreateMovie(mock.Anything, mock.Anything).
			Return(&ent.Movie{ID: 43}, nil).Once()
		store.EXPECT().
			CreateMediaFile(mock.Anything, mock.MatchedBy(func(p db.CreateMediaFileParams) bool {
				return !p.QueueTranscode
			})).
			Return(&ent.MediaFile{}, nil).
			Once()
		store.EXPECT().
			UpdateMovieStatus(mock.Anything, uint32(43), entmovie.StatusAvailable).
			Return(nil).
			Once()

		outcome, _, movieID := svc.commitAdoptInPlace(ctx, f, tmdbID)
		Expect(outcome).To(Equal(entimportscanfile.OutcomeCreated))
		Expect(movieID).To(Equal(uint32(43)))
	})
})

var _ = Describe("Service.commitRename", Label("unit", "bulkimport"), func() {
	var (
		ctx    context.Context
		store  *dbmocks.MockStore
		meta   *metamocks.MockProvider
		svc    *Service
		libDir string
		srcDir string
	)

	const tmdbID = uint32(55002)

	BeforeEach(func() {
		ctx = context.Background()
		store = dbmocks.NewMockStore(GinkgoT())
		// movie.Service enriches cast after every add; nothing to enrich here.
		store.EXPECT().PeopleNeedingDetails(
			mock.Anything, mock.Anything, mock.Anything,
		).Return(nil, nil).Maybe()
		meta = metamocks.NewMockProvider(GinkgoT())
		base := GinkgoT().TempDir()
		libDir = filepath.Join(base, "lib")
		srcDir = filepath.Join(base, "src")
		Expect(os.MkdirAll(libDir, 0o755)).To(Succeed())
		Expect(os.MkdirAll(srcDir, 0o755)).To(Succeed())
	})

	It(
		"queues a transcode job when the created movie's profile is transcode-eligible",
		func() {
			configtest.Setup(map[string]any{
				"library": map[string]any{
					"movie_path":   libDir,
					"movie_naming": "{title} ({year})/{title}.{ext}",
					"import_mode":  "hardlink",
				},
				"transcoding": map[string]any{"enabled": true},
				"metadata":    map[string]any{"tmdb_region": ""},
				"quality_profiles": []map[string]any{{
					"name": "hd", "preferred_resolution": "1080p",
					"min_resolution": "720p",
					"transcode": map[string]any{
						"to": map[string]any{
							"container": "mkv", "video_codec": "hevc",
							"preset": "medium", "audio_codec": "aac",
						},
					},
				}},
				"movie_quality_default_profile":  "hd",
				"series_quality_default_profile": "hd",
			})
			svc = NewService(
				store, meta, nil, library.NewImportService(),
				movie.NewService(store, meta, nil, nil, nil), nil, nil,
				libDir, libDir,
				nil, nil, nil, nil,
			)

			src := filepath.Join(srcDir, "Fight Club - 1999.mkv")
			Expect(
				os.WriteFile(src, make([]byte, 60*1024*1024), 0o644),
			).To(Succeed())
			scan := &ent.ImportScan{
				ID:         1,
				Mode:       entimportscan.ModeRename,
				ImportMode: entimportscan.ImportModeHardlink,
			}
			f := &ent.ImportScanFile{ID: 7, SourcePath: src}

			meta.EXPECT().GetMovie(mock.Anything, tmdbID).
				Return(&metadata.MovieDetails{
					TMDBID: tmdbID, Title: "Fight Club", Year: 1999,
				}, nil).Once()
			store.EXPECT().CreateMovie(mock.Anything, mock.Anything).
				Return(&ent.Movie{ID: 42, Title: "Fight Club", Year: 1999}, nil).
				Once()
			store.EXPECT().
				CreateMediaFile(mock.Anything, mock.MatchedBy(func(p db.CreateMediaFileParams) bool {
					return p.QueueTranscode
				})).
				Return(&ent.MediaFile{}, nil).
				Once()
			store.EXPECT().
				UpdateMovieStatus(mock.Anything, uint32(42), entmovie.StatusAvailable).
				Return(nil).
				Once()

			outcome, msg, movieID := svc.commitRename(ctx, scan, f, tmdbID)
			Expect(msg).To(BeEmpty())
			Expect(outcome).To(Equal(entimportscanfile.OutcomeCreated))
			Expect(movieID).To(Equal(uint32(42)))
		},
	)

	It("commits each new movie of a flat folder holding several", func() {
		configtest.Setup(map[string]any{
			"library": map[string]any{
				"movie_path":   libDir,
				"movie_naming": "{title} ({year})/{title}.{ext}",
				"import_mode":  "hardlink",
			},
			"metadata": map[string]any{"tmdb_region": ""},
		})
		svc = NewService(
			store, meta, nil, library.NewImportService(),
			movie.NewService(store, meta, nil, nil, nil), nil, nil,
			libDir, libDir,
			nil, nil, nil, nil,
		)
		scan := &ent.ImportScan{
			ID:         1,
			Mode:       entimportscan.ModeRename,
			ImportMode: entimportscan.ImportModeHardlink,
		}
		movies := []*ent.Movie{
			{ID: 42, Title: "Fight Club", Year: 1999},
			{ID: 43, Title: "Se7en", Year: 1995},
		}
		srcs := make([]string, len(movies))
		for i, m := range movies {
			srcs[i] = filepath.Join(
				srcDir,
				fmt.Sprintf("%s - %d.mkv", m.Title, m.Year),
			)
			Expect(os.WriteFile(srcs[i], nil, 0o644)).To(Succeed())
			Expect(os.Truncate(srcs[i], 60*1024*1024)).To(Succeed())
		}

		for i, m := range movies {
			id := tmdbID + uint32(i)
			meta.EXPECT().GetMovie(mock.Anything, id).
				Return(&metadata.MovieDetails{
					TMDBID: id, Title: m.Title, Year: m.Year,
				}, nil).Once()
			store.EXPECT().CreateMovie(mock.Anything, mock.Anything).
				Return(m, nil).Once()
			var recorded db.CreateMediaFileParams
			store.EXPECT().CreateMediaFile(mock.Anything, mock.Anything).
				Run(func(_ context.Context, p db.CreateMediaFileParams) {
					recorded = p
				}).Return(&ent.MediaFile{}, nil).Once()
			store.EXPECT().
				UpdateMovieStatus(mock.Anything, m.ID, entmovie.StatusAvailable).
				Return(nil).Once()

			outcome, msg, movieID := svc.commitRename(
				ctx,
				scan,
				&ent.ImportScanFile{ID: uint32(i + 1), SourcePath: srcs[i]},
				id,
			)

			Expect(msg).To(BeEmpty())
			Expect(outcome).To(Equal(entimportscanfile.OutcomeCreated))
			Expect(movieID).To(Equal(m.ID))
			Expect(recorded.Path).To(Equal(
				filepath.Join(
					libDir,
					fmt.Sprintf("%s (%d)", m.Title, m.Year),
					m.Title+".mkv",
				),
			))
			Expect(recorded.Path).To(BeAnExistingFile())
		}
	})
})

var _ = Describe("CommitOptions", Label("unit", "bulkimport"), func() {
	It("leaves adopted files alone unless the commit asks for tags", func() {
		on, off := true, false
		Expect(CommitOptions{}.writeTags()).To(BeFalse(),
			"a commit with no body never rewrites a collection's files")
		Expect(CommitOptions{WriteTags: &off}.writeTags()).To(BeFalse())
		Expect(CommitOptions{WriteTags: &on}.writeTags()).To(BeTrue())
	})
})
