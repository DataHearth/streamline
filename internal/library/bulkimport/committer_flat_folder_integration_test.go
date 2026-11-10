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
	"github.com/datahearth/streamline/internal/db"
	"github.com/datahearth/streamline/internal/events"
	"github.com/datahearth/streamline/internal/library"
	"github.com/datahearth/streamline/internal/media/movie"
	"github.com/datahearth/streamline/internal/metadata"
	metamocks "github.com/datahearth/streamline/internal/metadata/mocks"
	"github.com/datahearth/streamline/internal/testutil/configtest"
	"github.com/datahearth/streamline/internal/testutil/dbtest"
)

var _ = Describe(
	"Movie commit (rename from a flat folder)",
	Label("integration", "bulkimport"),
	func() {
		const (
			fightClubTMDB = uint32(550)
			heatTMDB      = uint32(949)
			fightClubSize = 60 * 1024 * 1024
			heatSize      = 61 * 1024 * 1024
		)
		var (
			ctx       context.Context
			libDir    string
			fightClub string
			heat      string
			client    *ent.Client
			store     db.Store
			meta      *metamocks.MockProvider
			svc       *Service
			scan      *ent.ImportScan
		)

		BeforeEach(func() {
			ctx = context.Background()
			root := GinkgoT().TempDir()
			// One folder, one file per movie, no folder per movie.
			srcDir := filepath.Join(root, "downloads")
			libDir = filepath.Join(root, "movies")
			Expect(os.MkdirAll(srcDir, 0o755)).To(Succeed())
			fightClub = filepath.Join(srcDir, "Fight.Club.1999.1080p.mkv")
			heat = filepath.Join(srcDir, "Heat.1995.1080p.mkv")
			Expect(os.WriteFile(fightClub, make([]byte, fightClubSize), 0o644)).
				To(Succeed())
			Expect(os.WriteFile(heat, make([]byte, heatSize), 0o644)).To(Succeed())
			configtest.Setup(map[string]any{
				"library": map[string]any{
					"movie_path":   libDir,
					"movie_naming": "{title} ({year})/{title}.{ext}",
				},
				// No region, no digital-release lookup on add.
				"metadata": map[string]any{"tmdb_region": ""},
			})
			client = dbtest.SetupTestDB(ctx)
			events.Register(client)
			DeferCleanup(client.Close)
			store = db.New(client)
			meta = metamocks.NewMockProvider(GinkgoT())
			svc = NewService(
				store, meta, nil, library.NewImportService(),
				movie.NewService(store, meta, nil, nil, nil), nil, nil,
				libDir, libDir,
				nil, nil, nil, nil,
			)
			scan = &ent.ImportScan{
				ID:         1,
				Mode:       entimportscan.ModeRename,
				ImportMode: entimportscan.ImportModeHardlink,
			}
		})

		// expectImported asserts the movie holds one row, at the path the
		// template renders, and that the file there is the scanned one.
		expectImported := func(movieID uint32, rel string, size int64) {
			GinkgoHelper()
			row := client.MediaFile.Query().
				Where(entmediafile.HasMovieWith(entmovie.ID(movieID))).
				OnlyX(ctx)
			Expect(row.Path).To(Equal(filepath.Join(libDir, rel)))
			Expect(row.Size).To(Equal(size))
			info, err := os.Stat(row.Path)
			Expect(err).NotTo(HaveOccurred())
			Expect(info.Size()).To(Equal(size))
		}

		It("attaches a tracked movie's file lying beside another movie's", func() {
			tracked, err := store.CreateMovie(ctx, db.CreateMovieParams{
				Title: "Fight Club", OriginalTitle: "Fight Club", Year: 1999,
				TmdbID: fightClubTMDB, Status: entmovie.StatusWanted,
			})
			Expect(err).NotTo(HaveOccurred())

			outcome, msg, movieID := svc.commitAttach(ctx, scan, &ent.ImportScanFile{
				ID: 7, ExistingMovieID: tracked.ID, SourcePath: fightClub,
			})

			Expect(msg).To(BeEmpty())
			Expect(outcome).To(Equal(entimportscanfile.OutcomeAttached))
			Expect(movieID).To(Equal(tracked.ID))
			expectImported(
				tracked.ID, "Fight Club (1999)/Fight Club.mkv", fightClubSize,
			)
		})

		It("imports every new movie of the folder", func() {
			meta.EXPECT().GetMovie(mock.Anything, fightClubTMDB).
				Return(&metadata.MovieDetails{
					TMDBID:        fightClubTMDB,
					Title:         "Fight Club",
					OriginalTitle: "Fight Club",
					Year:          1999,
				}, nil).Once()
			meta.EXPECT().GetMovie(mock.Anything, heatTMDB).
				Return(&metadata.MovieDetails{
					TMDBID:        heatTMDB,
					Title:         "Heat",
					OriginalTitle: "Heat",
					Year:          1995,
				}, nil).Once()

			outcome, msg, fightClubID := svc.commitRename(
				ctx, scan, &ent.ImportScanFile{ID: 7, SourcePath: fightClub},
				fightClubTMDB,
			)
			Expect(msg).To(BeEmpty())
			Expect(outcome).To(Equal(entimportscanfile.OutcomeCreated))
			outcome, msg, heatID := svc.commitRename(
				ctx, scan, &ent.ImportScanFile{ID: 8, SourcePath: heat}, heatTMDB,
			)
			Expect(msg).To(BeEmpty())
			Expect(outcome).To(Equal(entimportscanfile.OutcomeCreated))

			expectImported(
				fightClubID, "Fight Club (1999)/Fight Club.mkv", fightClubSize,
			)
			expectImported(heatID, "Heat (1995)/Heat.mkv", heatSize)
		})
	},
)
