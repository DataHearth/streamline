package hygiene

import (
	"context"
	"os"
	"path/filepath"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stretchr/testify/mock"

	"github.com/datahearth/streamline/ent"
	entmovie "github.com/datahearth/streamline/ent/movie"
	"github.com/datahearth/streamline/internal/db"
	"github.com/datahearth/streamline/internal/library"
	"github.com/datahearth/streamline/internal/metadata"
	metamocks "github.com/datahearth/streamline/internal/metadata/mocks"
	"github.com/datahearth/streamline/internal/testutil/configtest"
	"github.com/datahearth/streamline/internal/testutil/dbtest"
)

var _ = Describe("hygiene end-to-end", Label("integration", "hygiene"), func() {
	var (
		ctx       context.Context
		tmpDir    string
		entClient *ent.Client
		store     db.Store
		meta      *metamocks.MockProvider
		imp       *library.ImportService
		svc       *Service
	)

	BeforeEach(func() {
		ctx = context.Background()
		tmpDir = GinkgoT().TempDir()
		cfg := configtest.Setup(map[string]any{
			"library": map[string]any{
				"movie_path":          tmpDir,
				"movie_naming":        "{title} ({year})/{title}.{ext}",
				"import_mode":         "copy",
				"import_max_attempts": 3,
				"drift_grace_ticks":   3,
				"default_quality": map[string]any{
					"preferred_resolution": "1080p",
					"min_resolution":       "720p",
					"no_match_cooldown":    "6h",
					"max_grab_failures":    3,
				},
			},
		})
		entClient = dbtest.SetupTestDB(ctx)
		DeferCleanup(entClient.Close)
		store = db.New(entClient)
		meta = metamocks.NewMockProvider(GinkgoT())
		imp = library.NewImportService(&cfg.Library)
		svc = New(
			store,
			meta,
			metamocks.NewMockTVProvider(GinkgoT()),
			imp,
			&cfg.Library,
		)
	})

	It("adopts an orphan into the library for a tracked movie", func() {
		srcDir := filepath.Join(tmpDir, "incoming")
		Expect(os.MkdirAll(srcDir, 0o755)).To(Succeed())
		orphan := filepath.Join(srcDir, "Inception.2010.1080p.BluRay.mkv")
		Expect(os.WriteFile(orphan, make([]byte, 60*1024*1024), 0o644)).To(Succeed())

		// Auto-import only adopts files for already-tracked movies; brand-new
		// matches are routed to the bulk-import wizard instead. Track the movie
		// (no media file yet) so the orphan scan adopts the file into it.
		_, err := store.CreateMovie(ctx, db.CreateMovieParams{
			Title:         "Inception",
			OriginalTitle: "Inception",
			Year:          2010,
			TmdbID:        27205,
			Status:        entmovie.StatusWanted,
		})
		Expect(err).NotTo(HaveOccurred())

		meta.EXPECT().SearchMovie(mock.Anything, "Inception", uint16(2010)).
			Return([]metadata.MovieResult{
				{TMDBID: 27205, Title: "Inception", Year: 2010},
			}, nil).Once()

		Expect(svc.RunOrphanScan(ctx)).To(Succeed())

		movie, err := store.FindMovieByTMDBID(ctx, 27205)
		Expect(err).NotTo(HaveOccurred())
		Expect(movie).NotTo(BeNil())
		Expect(string(movie.Status)).To(Equal("available"))
		Expect(filepath.Join(tmpDir, "Inception (2010)", "Inception.mkv")).
			To(BeAnExistingFile())
	})

	It(
		"appends to the existing review queue instead of a new scan each run",
		func() {
			// Orphans trickle in across runs (a migrated *arr library classifies
			// over several scans). Every run must fold new orphans into the one
			// open review queue for the directory — one import entry per directory,
			// not a fresh scan on every restart.
			meta.EXPECT().SearchMovie(mock.Anything, mock.Anything, mock.Anything).
				Return(nil, nil) // everything unmatched → queued for review

			a := filepath.Join(tmpDir, "First Unmatched File.mkv")
			Expect(os.WriteFile(a, make([]byte, 60*1024*1024), 0o644)).To(Succeed())
			Expect(svc.RunOrphanScan(ctx)).To(Succeed())

			// A second orphan shows up; the first is already pending review.
			b := filepath.Join(tmpDir, "Second Unmatched File.mkv")
			Expect(os.WriteFile(b, make([]byte, 60*1024*1024), 0o644)).To(Succeed())
			Expect(svc.RunOrphanScan(ctx)).To(Succeed())

			scans, total, err := store.ListImportScans(ctx, 0, 100)
			Expect(err).NotTo(HaveOccurred())
			Expect(total).To(Equal(uint32(1)), "one review entry per directory")

			_, fileCount, err := store.FilterImportScanFiles(
				ctx, db.FilterImportScanFilesParams{ScanID: scans[0].ID},
			)
			Expect(err).NotTo(HaveOccurred())
			Expect(fileCount).To(Equal(uint32(2)), "both orphans in the one scan")
		},
	)

	It("does not re-queue the hardlink source on a second scan", func() {
		// Force hardlink mode so the auto-import leaves the source file in place,
		// mirroring the deployed default.
		cfg := configtest.Setup(map[string]any{
			"library": map[string]any{
				"movie_path":          tmpDir,
				"movie_naming":        "{title} ({year})/{title}.{ext}",
				"import_mode":         "hardlink",
				"import_max_attempts": 3,
				"drift_grace_ticks":   3,
				"default_quality": map[string]any{
					"preferred_resolution": "1080p",
					"min_resolution":       "720p",
					"no_match_cooldown":    "6h",
					"max_grab_failures":    3,
				},
			},
		})
		imp = library.NewImportService(&cfg.Library)
		svc = New(
			store,
			meta,
			metamocks.NewMockTVProvider(GinkgoT()),
			imp,
			&cfg.Library,
		)

		srcDir := filepath.Join(tmpDir, "incoming")
		Expect(os.MkdirAll(srcDir, 0o755)).To(Succeed())
		orphan := filepath.Join(srcDir, "Inception.2010.1080p.BluRay.mkv")
		Expect(os.WriteFile(orphan, make([]byte, 60*1024*1024), 0o644)).To(Succeed())

		_, err := store.CreateMovie(ctx, db.CreateMovieParams{
			Title: "Inception", OriginalTitle: "Inception", Year: 2010,
			TmdbID: 27205, Status: entmovie.StatusWanted,
		})
		Expect(err).NotTo(HaveOccurred())

		meta.EXPECT().SearchMovie(mock.Anything, "Inception", uint16(2010)).
			Return([]metadata.MovieResult{
				{TMDBID: 27205, Title: "Inception", Year: 2010},
			}, nil)

		// First scan: auto-import (hardlink leaves the source in place).
		Expect(svc.RunOrphanScan(ctx)).To(Succeed())
		// Second scan = a restart.
		Expect(svc.RunOrphanScan(ctx)).To(Succeed())

		_, total, err := store.ListImportScans(ctx, 0, 100)
		Expect(err).NotTo(HaveOccurred())
		Expect(
			total,
		).To(BeZero(), "leftover hardlink source was re-queued for review")
	})

	It("reverts a Movie when its file disappears past the grace window", func() {
		path := filepath.Join(tmpDir, "movie.mkv")
		Expect(os.WriteFile(path, []byte("data"), 0o644)).To(Succeed())

		m, err := store.CreateMovie(ctx, db.CreateMovieParams{
			Title:         "Gone",
			OriginalTitle: "Gone",
			Year:          2024,
			TmdbID:        1234,
			Status:        entmovie.StatusAvailable,
		})
		Expect(err).NotTo(HaveOccurred())
		mf, err := store.CreateMediaFile(ctx, db.CreateMediaFileParams{
			MovieID: m.ID, Path: path, Size: 4,
		})
		Expect(err).NotTo(HaveOccurred())

		// Back-date last_seen_at via the ent client so the grace window expires immediately.
		Expect(entClient.MediaFile.UpdateOneID(mf.ID).
			SetLastSeenAt(time.Now().Add(-2 * time.Hour)).
			Exec(ctx)).To(Succeed())

		Expect(os.Remove(path)).To(Succeed())

		// grace_ticks=3 × 1ms interval = 3ms; one tick is enough at 2h staleness.
		Expect(svc.RunDriftCheck(ctx, time.Millisecond)).To(Succeed())

		refreshed, err := store.FindMovieByID(ctx, m.ID)
		Expect(err).NotTo(HaveOccurred())
		Expect(string(refreshed.Status)).To(Equal("wanted"))
	})
})

var _ = Describe(
	"Service.RunSeriesOrphanScan (integration)",
	Label("integration", "hygiene"),
	func() {
		var (
			ctx       context.Context
			tmpDir    string
			entClient *ent.Client
			store     db.Store
			tvmeta    *metamocks.MockTVProvider
			svc       *Service
		)

		BeforeEach(func() {
			ctx = context.Background()
			tmpDir = GinkgoT().TempDir()
			cfg := configtest.Setup(map[string]any{
				"library": map[string]any{
					"series_path": tmpDir,
					"import_mode": "copy",
				},
			})
			entClient = dbtest.SetupTestDB(ctx)
			DeferCleanup(entClient.Close)
			store = db.New(entClient)
			tvmeta = metamocks.NewMockTVProvider(GinkgoT())
			svc = New(store, metamocks.NewMockProvider(GinkgoT()), tvmeta,
				library.NewImportService(&cfg.Library), &cfg.Library)
		})

		placeShow := func(show, file string) {
			dir := filepath.Join(tmpDir, show)
			Expect(os.MkdirAll(dir, 0o755)).To(Succeed())
			Expect(
				os.WriteFile(
					filepath.Join(dir, file),
					make([]byte, 60*1024*1024),
					0o644,
				),
			).To(Succeed())
		}

		It("folds shows found across runs into one series scan", func() {
			tvmeta.EXPECT().SearchSeries(mock.Anything, "Breaking Bad").
				Return([]metadata.TVResult{{TVDBID: 81189, Title: "Breaking Bad", Year: 2008}}, nil).
				Once()
			tvmeta.EXPECT().SearchSeries(mock.Anything, "The Wire").
				Return([]metadata.TVResult{{TVDBID: 79126, Title: "The Wire", Year: 2002}}, nil).
				Once()

			placeShow("Breaking Bad", "Breaking Bad S01E01.mkv")
			Expect(svc.RunSeriesOrphanScan(ctx)).To(Succeed())

			// Second run with a new folder must fold into the same open review scan.
			placeShow("The Wire", "The Wire S01E01.mkv")
			Expect(svc.RunSeriesOrphanScan(ctx)).To(Succeed())

			scans, total, err := store.ListImportScans(ctx, 0, 50)
			Expect(err).NotTo(HaveOccurred())
			Expect(total).To(Equal(uint32(1)))

			shows, showTotal, err := store.ListImportScanShows(
				ctx, db.ListImportScanShowsParams{ScanID: scans[0].ID})
			Expect(err).NotTo(HaveOccurred())
			Expect(showTotal).To(Equal(uint32(2)))
			Expect([]string{shows[0].FolderPath, shows[1].FolderPath}).To(ConsistOf(
				filepath.Join(tmpDir, "Breaking Bad"),
				filepath.Join(tmpDir, "The Wire"),
			))
		})
	},
)
