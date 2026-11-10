package bulkimport

import (
	"context"
	"os"
	"path/filepath"
	"runtime"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/ent"
	entimportscan "github.com/datahearth/streamline/ent/importscan"
	entimportscanfile "github.com/datahearth/streamline/ent/importscanfile"
	entmediafile "github.com/datahearth/streamline/ent/mediafile"
	entmovie "github.com/datahearth/streamline/ent/movie"
	"github.com/datahearth/streamline/internal/db"
	"github.com/datahearth/streamline/internal/events"
	"github.com/datahearth/streamline/internal/library"
	"github.com/datahearth/streamline/internal/testutil/configtest"
	"github.com/datahearth/streamline/internal/testutil/dbtest"
)

var _ = Describe(
	"Movie commit (attach replacing a tracked file)",
	Label("integration", "bulkimport"),
	func() {
		var (
			ctx    context.Context
			srcDir string
			libDir string
			client *ent.Client
			store  db.Store
			svc    *Service
			movie  *ent.Movie
			scan   *ent.ImportScan
		)

		BeforeEach(func() {
			ctx = context.Background()
			root := GinkgoT().TempDir()
			srcDir = filepath.Join(root, "downloads", "Fight Club (1999)")
			libDir = filepath.Join(root, "movies")
			Expect(os.MkdirAll(srcDir, 0o755)).To(Succeed())
			configtest.Setup(map[string]any{
				"library": map[string]any{
					"movie_path":   libDir,
					"movie_naming": "{title} ({year})/{title}.{ext}",
				},
			})
			client = dbtest.SetupTestDB(ctx)
			events.Register(client)
			DeferCleanup(client.Close)
			store = db.New(client)
			svc = NewService(
				store, nil, nil, library.NewImportService(), nil, nil, nil,
				libDir, libDir,
				nil, nil, nil, nil,
			)
			var err error
			movie, err = store.CreateMovie(ctx, db.CreateMovieParams{
				Title:         "Fight Club",
				OriginalTitle: "Fight Club",
				Year:          1999,
				TmdbID:        550,
				Status:        entmovie.StatusAvailable,
			})
			Expect(err).NotTo(HaveOccurred())
			scan = &ent.ImportScan{
				ID:         1,
				Mode:       entimportscan.ModeRename,
				ImportMode: entimportscan.ImportModeCopy,
			}
		})

		// track writes a file in the library and records it as the movie's.
		track := func(rel string) *ent.MediaFile {
			path := filepath.Join(libDir, rel)
			Expect(os.MkdirAll(filepath.Dir(path), 0o755)).To(Succeed())
			Expect(os.WriteFile(path, make([]byte, 60*1024*1024), 0o644)).
				To(Succeed())
			mf, err := store.CreateMediaFile(ctx, db.CreateMediaFileParams{
				MovieID: movie.ID, Path: path, Size: 60 * 1024 * 1024,
				Source: entmediafile.SourceWizard,
			})
			Expect(err).NotTo(HaveOccurred())
			return mf
		}
		placeSource := func(size int) string {
			src := filepath.Join(srcDir, "Fight.Club.1999.REPACK.1080p.mkv")
			Expect(os.WriteFile(src, make([]byte, size), 0o644)).To(Succeed())
			return src
		}
		attach := func(src string) (entimportscanfile.Outcome, string) {
			outcome, msg, _ := svc.commitAttach(ctx, scan, &ent.ImportScanFile{
				ID: 7, ExistingMovieID: movie.ID, SourcePath: src,
			})
			return outcome, msg
		}

		It(
			"keeps the movie's tracked file and row when its replacement fails to import",
			func() {
				tracked := track("Fight Club (1999)/Fight Club 720p.mkv")
				// An untracked file holds the replacement's rename destination.
				Expect(os.WriteFile(
					filepath.Join(libDir, "Fight Club (1999)", "Fight Club.mkv"),
					[]byte("someone else's"), 0o644,
				)).To(Succeed())

				outcome, msg := attach(placeSource(60 * 1024 * 1024))

				Expect(outcome).To(Equal(entimportscanfile.OutcomeFailed))
				Expect(msg).To(ContainSubstring(library.ErrDestExists.Error()))
				Expect(tracked.Path).To(BeAnExistingFile())
				rows := client.MediaFile.Query().AllX(ctx)
				Expect(rows).To(HaveLen(1))
				Expect(rows[0].ID).To(Equal(tracked.ID))
				Expect(rows[0].Path).To(Equal(tracked.Path))
				Expect(client.Movie.GetX(ctx, movie.ID).Status).
					To(Equal(entmovie.StatusAvailable))
			},
		)

		It(
			"replaces a tracked file whose replacement renders to the same path",
			func() {
				tracked := track("Fight Club (1999)/Fight Club.mkv")
				const repackSize = 61 * 1024 * 1024

				outcome, msg := attach(placeSource(repackSize))

				Expect(msg).To(BeEmpty())
				Expect(outcome).To(Equal(entimportscanfile.OutcomeAttached))
				row := client.MediaFile.Query().OnlyX(ctx)
				Expect(row.ID).NotTo(Equal(tracked.ID))
				Expect(row.Path).To(Equal(tracked.Path))
				Expect(row.Size).To(BeNumerically("==", repackSize))
				info, err := os.Stat(tracked.Path)
				Expect(err).NotTo(HaveOccurred())
				Expect(info.Size()).To(BeNumerically("==", repackSize))
				// Nothing set aside is left behind.
				entries, err := os.ReadDir(filepath.Dir(tracked.Path))
				Expect(err).NotTo(HaveOccurred())
				Expect(entries).To(HaveLen(1))
			},
		)

		It("removes the old file once a replacement at another path lands", func() {
			tracked := track("Fight Club (1999)/Fight Club 720p.mkv")

			outcome, msg := attach(placeSource(60 * 1024 * 1024))

			Expect(msg).To(BeEmpty())
			Expect(outcome).To(Equal(entimportscanfile.OutcomeAttached))
			row := client.MediaFile.Query().OnlyX(ctx)
			Expect(row.Path).To(Equal(
				filepath.Join(libDir, "Fight Club (1999)", "Fight Club.mkv"),
			))
			Expect(row.Path).To(BeAnExistingFile())
			Expect(tracked.Path).NotTo(BeAnExistingFile())
		})

		Context("on an in-place scan", func() {
			BeforeEach(func() {
				scan = &ent.ImportScan{ID: 1, Mode: entimportscan.ModeInPlace}
			})

			// expectUntouched asserts the movie still holds exactly the row
			// and the file it had before the commit.
			expectUntouched := func(tracked *ent.MediaFile) {
				GinkgoHelper()
				Expect(tracked.Path).To(BeAnExistingFile())
				row := client.MediaFile.Query().OnlyX(ctx)
				Expect(row.ID).To(Equal(tracked.ID))
				Expect(row.Path).To(Equal(tracked.Path))
			}

			It(
				"keeps a tracked file scanned again under its symlink-resolved path",
				func() {
					// The library root is a symlink: the row keeps the path
					// the file was imported under, the scanner resolves it.
					realLib := filepath.Join(filepath.Dir(libDir), "real-movies")
					Expect(os.MkdirAll(realLib, 0o755)).To(Succeed())
					symlinkOrSkip(realLib, libDir)
					tracked := track("Fight Club (1999)/Fight Club.mkv")
					resolved, err := filepath.EvalSymlinks(tracked.Path)
					Expect(err).NotTo(HaveOccurred())
					Expect(resolved).NotTo(Equal(tracked.Path))

					outcome, msg := attach(resolved)

					Expect(msg).To(BeEmpty())
					Expect(outcome).To(Equal(entimportscanfile.OutcomeAttached))
					expectUntouched(tracked)
				},
			)
		})
	},
)

// symlinkOrSkip creates link pointing at target, skipping the spec where the
// platform refuses: Windows needs a privilege or developer mode for it.
func symlinkOrSkip(target, link string) {
	GinkgoHelper()
	err := os.Symlink(target, link)
	if err != nil && runtime.GOOS == "windows" {
		Skip("symlinks are unavailable here: " + err.Error())
	}
	Expect(err).NotTo(HaveOccurred())
}
