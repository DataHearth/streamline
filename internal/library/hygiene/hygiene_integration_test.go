package hygiene

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stretchr/testify/mock"

	"github.com/datahearth/streamline/ent"
	entepisode "github.com/datahearth/streamline/ent/episode"
	entmediaevent "github.com/datahearth/streamline/ent/mediaevent"
	entmovie "github.com/datahearth/streamline/ent/movie"
	"github.com/datahearth/streamline/internal/db"
	"github.com/datahearth/streamline/internal/events"
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
			},
		})
		entClient = dbtest.SetupTestDB(ctx)
		DeferCleanup(entClient.Close)
		// The drift specs below assert the MediaEvent rows a sweep writes, and
		// events.Record needs the package default client the server wires up.
		events.Register(entClient)
		store = db.New(entClient)
		meta = metamocks.NewMockProvider(GinkgoT())
		imp = library.NewImportService()
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
			},
		})
		imp = library.NewImportService()
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

		for _, t := range []entmediaevent.Type{
			entmediaevent.TypeDriftDetected,
			entmediaevent.TypeDriftConfirmed,
		} {
			evs, err := entClient.MediaEvent.Query().
				Where(entmediaevent.TypeEQ(t)).
				WithMovie().
				All(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(evs).To(HaveLen(1), string(t))
			Expect(evs[0].Edges.Movie).NotTo(BeNil(), "movie drift stays per movie")
			Expect(evs[0].Edges.Movie.ID).To(Equal(m.ID))
			Expect(evs[0].Payload["path"]).To(Equal(path))
		}
	})

	It("reverts an Episode when its file disappears past the grace window", func() {
		path := filepath.Join(tmpDir, "episode.mkv")
		Expect(os.WriteFile(path, []byte("data"), 0o644)).To(Succeed())

		air := time.Now()
		show, err := store.CreateTVShow(ctx, db.CreateTVShowParams{
			Title: "Gone Show", Year: 2024, TvdbID: 4321,
			Seasons: []db.SeasonSeed{
				{
					Number: 1,
					Episodes: []db.EpisodeSeed{
						{Number: 1, Title: "Pilot", AirDate: &air},
					},
				},
			},
		})
		Expect(err).NotTo(HaveOccurred())
		ep := show.Edges.Seasons[0].Edges.Episodes[0]
		Expect(store.SetEpisodeStatus(ctx, ep.ID, entepisode.StatusAvailable)).
			To(Succeed())

		mf, err := store.CreateMediaFile(ctx, db.CreateMediaFileParams{
			EpisodeID: ep.ID, Path: path, Size: 4,
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(entClient.MediaFile.UpdateOneID(mf.ID).
			SetLastSeenAt(time.Now().Add(-2 * time.Hour)).
			Exec(ctx)).To(Succeed())

		Expect(os.Remove(path)).To(Succeed())

		Expect(svc.RunDriftCheck(ctx, time.Millisecond)).To(Succeed())

		_, err = store.FindMediaFileByID(ctx, mf.ID)
		Expect(ent.IsNotFound(err)).To(BeTrue())
		refreshed, err := entClient.Episode.Get(ctx, ep.ID)
		Expect(err).NotTo(HaveOccurred())
		Expect(string(refreshed.Status)).To(Equal("wanted"))
	})

	It("folds a whole show's missing files into one event per type", func() {
		air := time.Now()
		show, err := store.CreateTVShow(ctx, db.CreateTVShowParams{
			Title: "Vanished", Year: 2024, TvdbID: 5555,
			Seasons: []db.SeasonSeed{
				{
					Number: 1,
					Episodes: []db.EpisodeSeed{
						{Number: 1, Title: "One", AirDate: &air},
						{Number: 2, Title: "Two", AirDate: &air},
					},
				},
				{
					Number: 2,
					Episodes: []db.EpisodeSeed{
						{Number: 1, Title: "Three", AirDate: &air},
					},
				},
			},
		})
		Expect(err).NotTo(HaveOccurred())

		// Staggered so the aggregate's missing_since is provably the earliest
		// of the three and not just whichever row happened to be walked first.
		oldest := time.Now().Add(-5 * time.Hour).UTC().Truncate(time.Second)
		offset := 0
		for _, season := range show.Edges.Seasons {
			for _, ep := range season.Edges.Episodes {
				Expect(store.SetEpisodeStatus(
					ctx, ep.ID, entepisode.StatusAvailable,
				)).To(Succeed())
				path := filepath.Join(tmpDir, fmt.Sprintf(
					"Vanished S%02dE%02d.mkv", season.Number, ep.Number,
				))
				Expect(os.WriteFile(path, []byte("data"), 0o644)).To(Succeed())
				mf, err := store.CreateMediaFile(ctx, db.CreateMediaFileParams{
					EpisodeID: ep.ID, Path: path, Size: 4,
				})
				Expect(err).NotTo(HaveOccurred())
				Expect(entClient.MediaFile.UpdateOneID(mf.ID).
					SetLastSeenAt(oldest.Add(time.Duration(offset) * time.Hour)).
					Exec(ctx)).To(Succeed())
				Expect(os.Remove(path)).To(Succeed())
				offset++
			}
		}

		Expect(svc.RunDriftCheck(ctx, time.Millisecond)).To(Succeed())

		for _, t := range []entmediaevent.Type{
			entmediaevent.TypeDriftDetected,
			entmediaevent.TypeDriftConfirmed,
		} {
			evs, err := entClient.MediaEvent.Query().
				Where(entmediaevent.TypeEQ(t)).
				WithTvShow().
				WithEpisode().
				All(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(evs).To(HaveLen(1), string(t))
			Expect(evs[0].Edges.Episode).To(BeNil(), "aggregated, not per episode")
			Expect(evs[0].Edges.TvShow).NotTo(BeNil())
			Expect(evs[0].Edges.TvShow.ID).To(Equal(show.ID))
			Expect(evs[0].Payload["seasons"]).To(HaveExactElements(
				BeEquivalentTo(1), BeEquivalentTo(2),
			))
			Expect(evs[0].Payload["episodes"]).To(BeEquivalentTo(3))
			Expect(evs[0].Payload).NotTo(HaveKey("path"))
		}

		confirmed, err := entClient.MediaEvent.Query().
			Where(entmediaevent.TypeEQ(entmediaevent.TypeDriftConfirmed)).
			Only(ctx)
		Expect(err).NotTo(HaveOccurred())
		since, err := time.Parse(
			time.RFC3339Nano, confirmed.Payload["missing_since"].(string),
		)
		Expect(err).NotTo(HaveOccurred())
		Expect(since.UTC()).To(BeTemporally("==", oldest))
	})

	It("deletes a media_file row whose owner is gone", func() {
		path := filepath.Join(tmpDir, "orphan.mkv")
		Expect(os.WriteFile(path, []byte("data"), 0o644)).To(Succeed())

		mf, err := entClient.MediaFile.Create().
			SetPath(path).
			SetSize(4).
			SetLastSeenAt(time.Now().Add(-2 * time.Hour)).
			Save(ctx)
		Expect(err).NotTo(HaveOccurred())
		Expect(os.Remove(path)).To(Succeed())

		Expect(svc.RunDriftCheck(ctx, time.Millisecond)).To(Succeed())

		_, err = store.FindMediaFileByID(ctx, mf.ID)
		Expect(ent.IsNotFound(err)).To(BeTrue())
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
				library.NewImportService(), &cfg.Library)
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
