package importer

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stretchr/testify/mock"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/downloadrecord"
	"github.com/datahearth/streamline/ent/schema"
	"github.com/datahearth/streamline/ent/tvshow"
	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/db"
	mockdb "github.com/datahearth/streamline/internal/db/mocks"
	"github.com/datahearth/streamline/internal/ffmpeg"
	mockffmpeg "github.com/datahearth/streamline/internal/ffmpeg/mocks"
	mockimp "github.com/datahearth/streamline/internal/importer/mocks"
	"github.com/datahearth/streamline/internal/library"
	"github.com/datahearth/streamline/internal/testutil/configtest"
)

func seedMediaFile(dir, name string) {
	GinkgoHelper()
	p := filepath.Join(dir, name)
	f, err := os.Create(p)
	Expect(err).NotTo(HaveOccurred())
	DeferCleanup(f.Close)
	Expect(f.Truncate(60 << 20)).To(Succeed())
}

func fixtureRecord(
	id, movieID uint32,
	savePath string,
	attempts uint8,
) *ent.DownloadRecord {
	m := &ent.Movie{ID: movieID, Title: "Flick", Year: 2024, TmdbID: 999}
	r := &ent.DownloadRecord{
		ID:                 id,
		TorrentHash:        "hash",
		SavePath:           savePath,
		ImportAttempts:     attempts,
		Status:             downloadrecord.StatusImporting,
		DownloadClientName: "qbit",
	}
	r.Edges.Movie = m
	return r
}

var _ = Describe("Worker", Label("unit", "importer"), func() {
	var (
		storeMk *mockdb.MockStore
		msMk    *mockimp.MockMediaServerDispatcher
		libSvc  *library.ImportService
		w       *Worker
		tmp     string
		libDir  string
	)

	BeforeEach(func() {
		tmp = GinkgoT().TempDir()
		libDir = filepath.Join(tmp, "library")
		Expect(os.MkdirAll(libDir, 0o755)).To(Succeed())

		configtest.Setup(map[string]any{
			"library": map[string]any{
				"movie_path":           libDir,
				"import_mode":          "copy",
				"import_max_attempts":  3,
				"keep_torrent_seeding": true,
				"movie_naming":         "{title} ({year})/{title}.{ext}",
				"series_path":          libDir,
				"series_naming":        "{title}/{title} S{season}E{episode}.{ext}",
			},
		})

		storeMk = mockdb.NewMockStore(GinkgoT())
		msMk = mockimp.NewMockMediaServerDispatcher(GinkgoT())
		libSvc = library.NewImportService(&config.Get().Library)
		w = NewWorker(Deps{DB: storeMk, Library: libSvc, MediaServer: msMk})
	})

	It("happy path: success writes success + refreshes media server", func() {
		src := filepath.Join(tmp, "dl")
		Expect(os.MkdirAll(src, 0o755)).To(Succeed())
		seedMediaFile(src, "Flick.2024.1080p.mkv")
		rec := fixtureRecord(1, 10, src, 0)

		storeMk.EXPECT().FindImportingDownloadRecordByID(mock.Anything, uint32(1)).
			Return(rec, nil).Once()
		storeMk.EXPECT().ListMediaFilesByMovieID(mock.Anything, uint32(10)).
			Return(nil, nil).Once()
		storeMk.EXPECT().
			RecordImportSuccess(mock.Anything, mock.MatchedBy(func(p db.RecordImportSuccessParams) bool {
				return p.RecordID == 1 && p.MovieID == 10
			})).
			Return(nil).
			Once()
		storeMk.EXPECT().
			MarkRequestsAvailable(mock.Anything, mock.Anything, mock.Anything).
			Return(nil).Once()
		msMk.EXPECT().RefreshAll(mock.Anything, libDir).Return(nil).Once()

		Expect(w.runImport(context.Background(), 1)).To(Succeed())
	})

	It("attaches probe info to the movie media file row", func() {
		src := filepath.Join(tmp, "dl")
		Expect(os.MkdirAll(src, 0o755)).To(Succeed())
		seedMediaFile(src, "Flick.2024.1080p.mkv")
		rec := fixtureRecord(1, 10, src, 0)

		prober := mockffmpeg.NewMockProber(GinkgoT())
		prober.EXPECT().Available().Return(true).Once()
		prober.EXPECT().Probe(mock.Anything, mock.Anything).
			Return(&ffmpeg.Info{
				VideoCodec:  "h264",
				Width:       1920,
				Height:      1080,
				DurationSec: 5400,
				Container:   "matroska",
			}, nil).Once()
		wp := NewWorker(Deps{
			DB: storeMk, Library: libSvc, MediaServer: msMk, Prober: prober,
		})

		storeMk.EXPECT().FindImportingDownloadRecordByID(mock.Anything, uint32(1)).
			Return(rec, nil).Once()
		storeMk.EXPECT().ListMediaFilesByMovieID(mock.Anything, uint32(10)).
			Return(nil, nil).Once()
		storeMk.EXPECT().
			RecordImportSuccess(mock.Anything, mock.MatchedBy(func(p db.RecordImportSuccessParams) bool {
				return p.RecordID == 1 && p.MovieID == 10 &&
					p.File.Probe != nil && p.File.Probe.VideoCodec == "h264"
			})).
			Return(nil).
			Once()
		storeMk.EXPECT().
			MarkRequestsAvailable(mock.Anything, mock.Anything, mock.Anything).
			Return(nil).Once()
		msMk.EXPECT().RefreshAll(mock.Anything, libDir).Return(nil).Once()

		Expect(wp.runImport(context.Background(), 1)).To(Succeed())
	})

	It("imports with no probe row when probing fails and is bypassed", func() {
		src := filepath.Join(tmp, "dl")
		Expect(os.MkdirAll(src, 0o755)).To(Succeed())
		seedMediaFile(src, "Flick.2024.1080p.mkv")
		rec := fixtureRecord(1, 10, src, 0)
		rec.VerificationBypassed = true

		prober := mockffmpeg.NewMockProber(GinkgoT())
		prober.EXPECT().Available().Return(true).Once()
		prober.EXPECT().Probe(mock.Anything, mock.Anything).
			Return(nil, ffmpeg.ErrUnreadable).Once()
		wp := NewWorker(Deps{
			DB: storeMk, Library: libSvc, MediaServer: msMk, Prober: prober,
		})

		storeMk.EXPECT().FindImportingDownloadRecordByID(mock.Anything, uint32(1)).
			Return(rec, nil).Once()
		storeMk.EXPECT().ListMediaFilesByMovieID(mock.Anything, uint32(10)).
			Return(nil, nil).Once()
		storeMk.EXPECT().
			RecordImportSuccess(mock.Anything, mock.MatchedBy(func(p db.RecordImportSuccessParams) bool {
				return p.RecordID == 1 && p.MovieID == 10 && p.File.Probe == nil
			})).
			Return(nil).
			Once()
		storeMk.EXPECT().
			MarkRequestsAvailable(mock.Anything, mock.Anything, mock.Anything).
			Return(nil).Once()
		msMk.EXPECT().RefreshAll(mock.Anything, libDir).Return(nil).Once()

		Expect(wp.runImport(context.Background(), 1)).To(Succeed())
	})

	It("skips probing when ffmpeg is disabled", func() {
		configtest.Setup(map[string]any{
			"library": map[string]any{
				"movie_path":           libDir,
				"import_mode":          "copy",
				"import_max_attempts":  3,
				"keep_torrent_seeding": true,
				"movie_naming":         "{title} ({year})/{title}.{ext}",
				"series_path":          libDir,
				"series_naming":        "{title}/{title} S{season}E{episode}.{ext}",
			},
			"ffmpeg": map[string]any{
				"enabled": false,
			},
		})
		src := filepath.Join(tmp, "dl")
		Expect(os.MkdirAll(src, 0o755)).To(Succeed())
		seedMediaFile(src, "Flick.2024.1080p.mkv")
		rec := fixtureRecord(1, 10, src, 0)

		// No expectations set: mockery fails the spec if Probe or Available
		// is called while probing is disabled.
		prober := mockffmpeg.NewMockProber(GinkgoT())
		wp := NewWorker(Deps{
			DB: storeMk, Library: libSvc, MediaServer: msMk, Prober: prober,
		})

		storeMk.EXPECT().FindImportingDownloadRecordByID(mock.Anything, uint32(1)).
			Return(rec, nil).Once()
		storeMk.EXPECT().ListMediaFilesByMovieID(mock.Anything, uint32(10)).
			Return(nil, nil).Once()
		storeMk.EXPECT().
			RecordImportSuccess(mock.Anything, mock.MatchedBy(func(p db.RecordImportSuccessParams) bool {
				return p.RecordID == 1 && p.MovieID == 10 && p.File.Probe == nil
			})).
			Return(nil).
			Once()
		storeMk.EXPECT().
			MarkRequestsAvailable(mock.Anything, mock.Anything, mock.Anything).
			Return(nil).Once()
		msMk.EXPECT().RefreshAll(mock.Anything, libDir).Return(nil).Once()

		Expect(wp.runImport(context.Background(), 1)).To(Succeed())
	})

	It("existing file + replace flag: old file replaced, import succeeds", func() {
		src := filepath.Join(tmp, "dl")
		Expect(os.MkdirAll(src, 0o755)).To(Succeed())
		seedMediaFile(src, "Flick.2024.1080p.mkv")
		old := filepath.Join(libDir, "old.mkv")
		Expect(os.WriteFile(old, []byte("old"), 0o644)).To(Succeed())
		rec := fixtureRecord(1, 10, src, 0)
		rec.ReplaceExisting = true

		storeMk.EXPECT().FindImportingDownloadRecordByID(mock.Anything, uint32(1)).
			Return(rec, nil).Once()
		storeMk.EXPECT().ListMediaFilesByMovieID(mock.Anything, uint32(10)).
			Return([]*ent.MediaFile{{ID: 5, Path: old}}, nil).Once()
		storeMk.EXPECT().
			DeleteMediaFileAndRevertMovie(mock.Anything, uint32(5), uint32(10)).
			Return(nil).Once()
		storeMk.EXPECT().
			RecordImportSuccess(mock.Anything, mock.Anything).
			Return(nil).Once()
		storeMk.EXPECT().
			MarkRequestsAvailable(mock.Anything, mock.Anything, mock.Anything).
			Return(nil).Once()
		msMk.EXPECT().RefreshAll(mock.Anything, libDir).Return(nil).Once()

		Expect(w.runImport(context.Background(), 1)).To(Succeed())
		Expect(old).NotTo(BeAnExistingFile())
	})

	It("existing file without replace flag: terminal ErrMovieHasFile", func() {
		src := filepath.Join(tmp, "dl")
		Expect(os.MkdirAll(src, 0o755)).To(Succeed())
		seedMediaFile(src, "Flick.2024.1080p.mkv")
		rec := fixtureRecord(1, 10, src, 0)

		storeMk.EXPECT().FindImportingDownloadRecordByID(mock.Anything, uint32(1)).
			Return(rec, nil).Twice()
		storeMk.EXPECT().ListMediaFilesByMovieID(mock.Anything, uint32(10)).
			Return([]*ent.MediaFile{{ID: 5, Path: "/lib/old.mkv"}}, nil).Once()
		storeMk.EXPECT().
			RecordImportFailure(mock.Anything, mock.MatchedBy(func(p db.RecordImportFailureParams) bool {
				return p.Terminal && p.Attempts == 1
			})).
			Return(nil).
			Once()

		err := w.runImport(context.Background(), 1)
		Expect(err).To(MatchError(ErrMovieHasFile))
		w.handleOutcome(context.Background(), 1, err)
	})

	It("two records for one movie: only one of them transfers", func() {
		srcA := filepath.Join(tmp, "dlA")
		srcB := filepath.Join(tmp, "dlB")
		for _, d := range []string{srcA, srcB} {
			Expect(os.MkdirAll(d, 0o755)).To(Succeed())
			seedMediaFile(d, "Flick.2024.1080p.mkv")
		}

		var mu sync.Mutex
		var files []*ent.MediaFile

		storeMk.EXPECT().FindImportingDownloadRecordByID(mock.Anything, uint32(1)).
			Return(fixtureRecord(1, 10, srcA, 0), nil).Once()
		storeMk.EXPECT().FindImportingDownloadRecordByID(mock.Anything, uint32(2)).
			Return(fixtureRecord(2, 10, srcB, 0), nil).Once()
		storeMk.EXPECT().ListMediaFilesByMovieID(mock.Anything, uint32(10)).
			RunAndReturn(func(context.Context, uint32) ([]*ent.MediaFile, error) {
				mu.Lock()
				defer mu.Unlock()
				return files, nil
			}).Twice()
		storeMk.EXPECT().RecordImportSuccess(mock.Anything, mock.Anything).
			RunAndReturn(func(_ context.Context, p db.RecordImportSuccessParams) error {
				mu.Lock()
				defer mu.Unlock()
				files = append(files, &ent.MediaFile{ID: 5, Path: p.File.Path})
				return nil
			})
		storeMk.EXPECT().
			MarkRequestsAvailable(mock.Anything, mock.Anything, mock.Anything).
			Return(nil)
		msMk.EXPECT().RefreshAll(mock.Anything, libDir).Return(nil)

		errs := make(chan error, 2)
		var wg sync.WaitGroup
		for _, id := range []uint32{1, 2} {
			wg.Go(func() { errs <- w.runImport(context.Background(), id) })
		}
		wg.Wait()
		close(errs)

		failed := 0
		for err := range errs {
			if err != nil {
				Expect(err).To(MatchError(ErrMovieHasFile))
				failed++
			}
		}
		Expect(failed).To(Equal(1))
		Expect(files).To(HaveLen(1))
	})

	It("retryable error increments attempts, does not flip movie to failed", func() {
		rec := fixtureRecord(1, 10, filepath.Join(tmp, "nope"), 0)

		storeMk.EXPECT().FindImportingDownloadRecordByID(mock.Anything, uint32(1)).
			Return(rec, nil).Twice()
		storeMk.EXPECT().ListMediaFilesByMovieID(mock.Anything, uint32(10)).
			Return(nil, nil).Once()
		storeMk.EXPECT().
			RecordImportFailure(mock.Anything, mock.MatchedBy(func(p db.RecordImportFailureParams) bool {
				return p.RecordID == 1 && !p.Terminal && p.Attempts == 1
			})).
			Return(nil).
			Once()

		err := w.runImport(context.Background(), 1)
		Expect(err).To(HaveOccurred())
		w.handleOutcome(context.Background(), 1, err)
	})

	It("terminal error (ErrMultipleMedia) flips to failed on attempt 1", func() {
		src := filepath.Join(tmp, "dl")
		Expect(os.MkdirAll(src, 0o755)).To(Succeed())
		seedMediaFile(src, "a.mkv")
		seedMediaFile(src, "b.mkv")
		rec := fixtureRecord(1, 10, src, 0)

		storeMk.EXPECT().FindImportingDownloadRecordByID(mock.Anything, uint32(1)).
			Return(rec, nil).Twice()
		storeMk.EXPECT().ListMediaFilesByMovieID(mock.Anything, uint32(10)).
			Return(nil, nil).Once()
		storeMk.EXPECT().
			RecordImportFailure(mock.Anything, mock.MatchedBy(func(p db.RecordImportFailureParams) bool {
				return p.Terminal && p.Attempts == 1
			})).
			Return(nil).
			Once()

		err := w.runImport(context.Background(), 1)
		Expect(err).To(MatchError(library.ErrMultipleMedia))
		w.handleOutcome(context.Background(), 1, err)
	})

	It("retry exhaustion: attempts at MaxAttempts-1 + 1 flips to failed", func() {
		rec := fixtureRecord(1, 10, filepath.Join(tmp, "nope"), 2)

		storeMk.EXPECT().FindImportingDownloadRecordByID(mock.Anything, uint32(1)).
			Return(rec, nil).Twice()
		storeMk.EXPECT().ListMediaFilesByMovieID(mock.Anything, uint32(10)).
			Return(nil, nil).Once()
		storeMk.EXPECT().
			RecordImportFailure(mock.Anything, mock.MatchedBy(func(p db.RecordImportFailureParams) bool {
				return p.Terminal && p.Attempts == 3
			})).
			Return(nil).
			Once()

		err := w.runImport(context.Background(), 1)
		Expect(err).To(HaveOccurred())
		w.handleOutcome(context.Background(), 1, err)
	})

	It("ctx cancel mid-run leaves state untouched", func() {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		w.handleOutcome(ctx, 1, context.Canceled)
	})

	It("media server refresh failure does not fail the import", func() {
		src := filepath.Join(tmp, "dl2")
		Expect(os.MkdirAll(src, 0o755)).To(Succeed())
		seedMediaFile(src, "Flick.2024.mkv")
		rec := fixtureRecord(2, 11, src, 0)

		storeMk.EXPECT().FindImportingDownloadRecordByID(mock.Anything, uint32(2)).
			Return(rec, nil).Once()
		storeMk.EXPECT().ListMediaFilesByMovieID(mock.Anything, uint32(11)).
			Return(nil, nil).Once()
		storeMk.EXPECT().
			RecordImportSuccess(mock.Anything, mock.Anything).
			Return(nil).
			Once()
		storeMk.EXPECT().
			MarkRequestsAvailable(mock.Anything, mock.Anything, mock.Anything).
			Return(nil).Once()
		msMk.EXPECT().
			RefreshAll(mock.Anything, libDir).
			Return(errors.New("boom")).
			Once()

		Expect(w.runImport(context.Background(), 2)).To(Succeed())
	})

	It("AllowedDownloadRoots non-empty + save_path outside = terminal", func() {
		config.Get().Library.AllowedDownloadRoots = []string{"/safe"}

		rec := fixtureRecord(3, 12, "/unsafe/path", 0)
		storeMk.EXPECT().FindImportingDownloadRecordByID(mock.Anything, uint32(3)).
			Return(rec, nil).Twice()
		storeMk.EXPECT().
			RecordImportFailure(mock.Anything, mock.MatchedBy(func(p db.RecordImportFailureParams) bool {
				return p.Terminal
			})).
			Return(nil).
			Once()

		err := w.runImport(context.Background(), 3)
		Expect(err).To(MatchError(ErrPathNotAllowed))
		w.handleOutcome(context.Background(), 3, err)
	})

	It("AllowedDownloadRoots: a sibling sharing the prefix is refused", func() {
		config.Get().Library.AllowedDownloadRoots = []string{"/safe"}

		rec := fixtureRecord(4, 12, "/safe-evil/path", 0)
		storeMk.EXPECT().FindImportingDownloadRecordByID(mock.Anything, uint32(4)).
			Return(rec, nil).Once()

		Expect(w.runImport(context.Background(), 4)).
			To(MatchError(ErrPathNotAllowed))
	})

	It("Enqueue dedupe: in-flight IDs are dropped", func() {
		w.mu.Lock()
		w.inFlight[7] = struct{}{}
		w.mu.Unlock()
		w.Enqueue(7)
		Expect(w.ch).To(BeEmpty())
	})

	It("Start returns once ctx is canceled", func() {
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		go func() { w.Start(ctx); close(done) }()

		cancel()
		Eventually(done).WithTimeout(time.Second).Should(BeClosed())
	})

	It("Enqueue after shutdown is a no-op, not a panic", func() {
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		go func() { w.Start(ctx); close(done) }()

		cancel()
		Eventually(done).WithTimeout(time.Second).Should(BeClosed())

		Expect(func() { w.Enqueue(99) }).NotTo(Panic())
		Expect(w.ch).To(BeEmpty())
	})

	It("Scan picks up importing rows and calls Enqueue for each", func() {
		storeMk.EXPECT().ListImportingDownloadRecords(mock.Anything).
			Return([]*ent.DownloadRecord{{ID: 42}, {ID: 43}}, nil).Once()
		Expect(w.Scan(context.Background())).To(Succeed())

		Eventually(func() int { return len(w.ch) }).
			WithTimeout(100 * time.Millisecond).
			Should(Equal(2))
	})

	It(
		"single-episode record imports the file + marks the episode available",
		func() {
			season, eps := buildShow()
			src := filepath.Join(tmp, "ep")
			Expect(os.MkdirAll(src, 0o755)).To(Succeed())
			seedMediaFile(src, "Show.S01E01.1080p.mkv")
			rec := episodeRecord(1, src, season, eps[0])

			storeMk.EXPECT().
				FindImportingDownloadRecordByID(mock.Anything, uint32(1)).
				Return(rec, nil).
				Once()
			storeMk.EXPECT().
				FindMediaFileByEpisodeID(mock.Anything, eps[0].ID).
				Return(nil, &ent.NotFoundError{}).Once()
			storeMk.EXPECT().
				RecordEpisodeImportSuccess(mock.Anything, mock.MatchedBy(func(p db.RecordEpisodeImportSuccessParams) bool {
					return p.RecordID == 1 && p.EpisodeID == eps[0].ID
				})).
				Return(nil).Once()
			storeMk.EXPECT().
				MarkRequestsAvailable(mock.Anything, mock.Anything, mock.Anything).
				Return(nil).Once()
			msMk.EXPECT().RefreshAll(mock.Anything, libDir).Return(nil).Once()

			Expect(w.runImport(context.Background(), 1)).To(Succeed())
		},
	)

	It("existing episode file + replace flag: replaced then imported", func() {
		season, eps := buildShow()
		src := filepath.Join(tmp, "ep-r")
		Expect(os.MkdirAll(src, 0o755)).To(Succeed())
		seedMediaFile(src, "Show.S01E01.1080p.mkv")
		old := filepath.Join(libDir, "old-ep.mkv")
		Expect(os.WriteFile(old, []byte("old"), 0o644)).To(Succeed())
		rec := episodeRecord(
			1,
			filepath.Join(src, "Show.S01E01.1080p.mkv"),
			season,
			eps[0],
		)
		rec.ReplaceExisting = true

		storeMk.EXPECT().
			FindImportingDownloadRecordByID(mock.Anything, uint32(1)).
			Return(rec, nil).Once()
		storeMk.EXPECT().
			FindMediaFileByEpisodeID(mock.Anything, eps[0].ID).
			Return(&ent.MediaFile{ID: 8, Path: old}, nil).Once()
		storeMk.EXPECT().
			DeleteMediaFileAndRevertEpisode(mock.Anything, uint32(8), eps[0].ID).
			Return(nil).Once()
		storeMk.EXPECT().
			RecordEpisodeImportSuccess(mock.Anything, mock.Anything).
			Return(nil).Once()
		storeMk.EXPECT().
			MarkRequestsAvailable(mock.Anything, mock.Anything, mock.Anything).
			Return(nil).Once()
		msMk.EXPECT().RefreshAll(mock.Anything, libDir).Return(nil).Once()

		Expect(w.runImport(context.Background(), 1)).To(Succeed())
		Expect(old).NotTo(BeAnExistingFile())
	})

	It("existing episode file without replace: terminal ErrEpisodeHasFile", func() {
		season, eps := buildShow()
		src := filepath.Join(tmp, "ep-n")
		Expect(os.MkdirAll(src, 0o755)).To(Succeed())
		seedMediaFile(src, "Show.S01E01.1080p.mkv")
		rec := episodeRecord(
			1,
			filepath.Join(src, "Show.S01E01.1080p.mkv"),
			season,
			eps[0],
		)

		storeMk.EXPECT().
			FindImportingDownloadRecordByID(mock.Anything, uint32(1)).
			Return(rec, nil).Twice()
		storeMk.EXPECT().
			FindMediaFileByEpisodeID(mock.Anything, eps[0].ID).
			Return(&ent.MediaFile{ID: 8, Path: "/lib/old-ep.mkv"}, nil).Once()
		storeMk.EXPECT().
			RecordImportFailure(mock.Anything, mock.MatchedBy(func(p db.RecordImportFailureParams) bool {
				return p.Terminal && p.Attempts == 1
			})).
			Return(nil).Once()

		err := w.runImport(context.Background(), 1)
		Expect(err).To(MatchError(ErrEpisodeHasFile))
		w.handleOutcome(context.Background(), 1, err)
	})

	It("season pack matches each file to its episode + records both", func() {
		season, eps := buildShow()
		src := filepath.Join(tmp, "pack")
		Expect(os.MkdirAll(src, 0o755)).To(Succeed())
		seedMediaFile(src, "Show.S01E01.1080p.mkv")
		seedMediaFile(src, "Show.S01E02.1080p.mkv")
		rec := episodeRecord(2, src, season, eps[0])

		storeMk.EXPECT().FindImportingDownloadRecordByID(mock.Anything, uint32(2)).
			Return(rec, nil).Once()
		storeMk.EXPECT().FindMediaFileByEpisodeID(mock.Anything, mock.Anything).
			Return(nil, &ent.NotFoundError{}).Twice()
		recorded := map[uint32]bool{}
		storeMk.EXPECT().
			RecordEpisodeImportSuccess(mock.Anything, mock.MatchedBy(func(p db.RecordEpisodeImportSuccessParams) bool {
				recorded[p.EpisodeID] = true
				return p.RecordID == 2
			})).
			Return(nil).Twice()
		storeMk.EXPECT().
			MarkRequestsAvailable(mock.Anything, mock.Anything, mock.Anything).
			Return(nil).Once()
		msMk.EXPECT().RefreshAll(mock.Anything, libDir).Return(nil).Once()

		Expect(w.runImport(context.Background(), 2)).To(Succeed())
		Expect(recorded).To(HaveKey(eps[0].ID))
		Expect(recorded).To(HaveKey(eps[1].ID))
	})

	It("season pack skips a filed episode when replace is not requested", func() {
		season, eps := buildShow()
		src := filepath.Join(tmp, "pack-skip")
		Expect(os.MkdirAll(src, 0o755)).To(Succeed())
		seedMediaFile(src, "Show.S01E01.1080p.mkv")
		seedMediaFile(src, "Show.S01E02.1080p.mkv")
		rec := episodeRecord(2, src, season, eps[0])

		storeMk.EXPECT().FindImportingDownloadRecordByID(mock.Anything, uint32(2)).
			Return(rec, nil).Once()
		storeMk.EXPECT().FindMediaFileByEpisodeID(mock.Anything, eps[0].ID).
			Return(&ent.MediaFile{ID: 8, Path: "/lib/kept.mkv"}, nil).Once()
		storeMk.EXPECT().FindMediaFileByEpisodeID(mock.Anything, eps[1].ID).
			Return(nil, &ent.NotFoundError{}).Once()
		storeMk.EXPECT().
			RecordEpisodeImportSuccess(mock.Anything, mock.MatchedBy(func(p db.RecordEpisodeImportSuccessParams) bool {
				return p.EpisodeID == eps[1].ID
			})).
			Return(nil).Once()
		storeMk.EXPECT().
			MarkRequestsAvailable(mock.Anything, mock.Anything, mock.Anything).
			Return(nil).Once()
		msMk.EXPECT().RefreshAll(mock.Anything, libDir).Return(nil).Once()

		Expect(w.runImport(context.Background(), 2)).To(Succeed())
	})

	Describe("import verification", func() {
		// probing returns info for every file the worker hands it; a Times(n)
		// per file would just restate len(files).
		proberReturning := func(info *ffmpeg.Info, err error) *Worker {
			GinkgoHelper()
			prober := mockffmpeg.NewMockProber(GinkgoT())
			prober.EXPECT().Available().Return(true)
			prober.EXPECT().Probe(mock.Anything, mock.Anything).Return(info, err)
			return NewWorker(Deps{
				DB: storeMk, Library: libSvc, MediaServer: msMk, Prober: prober,
			})
		}
		probed := func(width uint16, codec string) *ffmpeg.Info {
			return &ffmpeg.Info{
				VideoCodec: codec, Width: width, Height: 800,
				DurationSec: 5400, Container: "matroska",
			}
		}

		It("holds a movie whose file is below the claimed resolution", func() {
			src := filepath.Join(tmp, "dl")
			Expect(os.MkdirAll(src, 0o755)).To(Succeed())
			seedMediaFile(src, "Flick.2024.1080p.mkv")
			rec := fixtureRecord(1, 10, src, 0)
			wp := proberReturning(probed(1280, "h264"), nil)

			storeMk.EXPECT().
				FindImportingDownloadRecordByID(mock.Anything, uint32(1)).
				Return(rec, nil).Once()
			storeMk.EXPECT().ListMediaFilesByMovieID(mock.Anything, uint32(10)).
				Return(nil, nil).Once()
			storeMk.EXPECT().
				HoldDownloadRecord(mock.Anything, uint32(1), mock.MatchedBy(
					func(rs []schema.HoldReason) bool {
						return len(rs) == 1 && rs[0].Check == "resolution"
					})).
				Return(nil).Once()

			Expect(wp.runImport(context.Background(), 1)).To(Succeed())
		})

		It("falls back to the release title for the resolution claim", func() {
			src := filepath.Join(tmp, "dl-generic")
			Expect(os.MkdirAll(src, 0o755)).To(Succeed())
			seedMediaFile(src, "movie.mkv")
			rec := fixtureRecord(1, 10, src, 0)
			rec.Title = "Flick.2024.2160p.WEB-DL.x265-GRP"
			wp := proberReturning(probed(1280, "h264"), nil)

			storeMk.EXPECT().
				FindImportingDownloadRecordByID(mock.Anything, uint32(1)).
				Return(rec, nil).Once()
			storeMk.EXPECT().ListMediaFilesByMovieID(mock.Anything, uint32(10)).
				Return(nil, nil).Once()
			storeMk.EXPECT().
				HoldDownloadRecord(mock.Anything, uint32(1), mock.MatchedBy(
					func(rs []schema.HoldReason) bool {
						return len(rs) == 1 && rs[0].Check == "resolution" &&
							rs[0].Expected == "2160p" && rs[0].Actual == "720p"
					})).
				Return(nil).Once()

			Expect(wp.runImport(context.Background(), 1)).To(Succeed())
		})

		It("holds a movie whose file will not probe", func() {
			src := filepath.Join(tmp, "dl-corrupt")
			Expect(os.MkdirAll(src, 0o755)).To(Succeed())
			seedMediaFile(src, "Flick.2024.1080p.mkv")
			rec := fixtureRecord(1, 10, src, 0)
			wp := proberReturning(nil, ffmpeg.ErrUnreadable)

			storeMk.EXPECT().
				FindImportingDownloadRecordByID(mock.Anything, uint32(1)).
				Return(rec, nil).Once()
			storeMk.EXPECT().ListMediaFilesByMovieID(mock.Anything, uint32(10)).
				Return(nil, nil).Once()
			storeMk.EXPECT().
				HoldDownloadRecord(mock.Anything, uint32(1), mock.MatchedBy(
					func(rs []schema.HoldReason) bool {
						return len(rs) == 1 && rs[0].Check == "corrupt"
					})).
				Return(nil).Once()

			Expect(wp.runImport(context.Background(), 1)).To(Succeed())
		})

		It("keeps the existing movie file when the replacement is held", func() {
			src := filepath.Join(tmp, "dl-replace-held")
			Expect(os.MkdirAll(src, 0o755)).To(Succeed())
			seedMediaFile(src, "Flick.2024.1080p.mkv")
			old := filepath.Join(libDir, "old.mkv")
			Expect(os.WriteFile(old, []byte("old"), 0o644)).To(Succeed())
			rec := fixtureRecord(1, 10, src, 0)
			rec.ReplaceExisting = true
			wp := proberReturning(probed(720, "h264"), nil)

			storeMk.EXPECT().
				FindImportingDownloadRecordByID(mock.Anything, uint32(1)).
				Return(rec, nil).Once()
			storeMk.EXPECT().ListMediaFilesByMovieID(mock.Anything, uint32(10)).
				Return([]*ent.MediaFile{{ID: 5, Path: old}}, nil).Once()
			storeMk.EXPECT().
				HoldDownloadRecord(mock.Anything, uint32(1), mock.Anything).
				Return(nil).Once()

			Expect(wp.runImport(context.Background(), 1)).To(Succeed())
			Expect(old).To(BeAnExistingFile())
		})

		It("keeps the existing episode file when the replacement is held", func() {
			season, eps := buildShow()
			src := filepath.Join(tmp, "ep-replace-held")
			Expect(os.MkdirAll(src, 0o755)).To(Succeed())
			seedMediaFile(src, "Show.S01E01.1080p.mkv")
			old := filepath.Join(libDir, "old-ep.mkv")
			Expect(os.WriteFile(old, []byte("old"), 0o644)).To(Succeed())
			rec := episodeRecord(
				1,
				filepath.Join(src, "Show.S01E01.1080p.mkv"),
				season,
				eps[0],
			)
			rec.ReplaceExisting = true
			wp := proberReturning(probed(1280, "hevc"), nil)

			storeMk.EXPECT().
				FindImportingDownloadRecordByID(mock.Anything, uint32(1)).
				Return(rec, nil).Once()
			storeMk.EXPECT().
				FindMediaFileByEpisodeID(mock.Anything, eps[0].ID).
				Return(&ent.MediaFile{ID: 8, Path: old}, nil).Once()
			storeMk.EXPECT().
				HoldDownloadRecord(mock.Anything, uint32(1), mock.Anything).
				Return(nil).Once()

			Expect(wp.runImport(context.Background(), 1)).To(Succeed())
			Expect(old).To(BeAnExistingFile())
		})

		It("imports a rejected file once verification is bypassed", func() {
			src := filepath.Join(tmp, "dl-bypass")
			Expect(os.MkdirAll(src, 0o755)).To(Succeed())
			seedMediaFile(src, "Flick.2024.1080p.mkv")
			rec := fixtureRecord(1, 10, src, 0)
			rec.VerificationBypassed = true
			wp := proberReturning(probed(1280, "h264"), nil)

			storeMk.EXPECT().
				FindImportingDownloadRecordByID(mock.Anything, uint32(1)).
				Return(rec, nil).Once()
			storeMk.EXPECT().ListMediaFilesByMovieID(mock.Anything, uint32(10)).
				Return(nil, nil).Once()
			storeMk.EXPECT().
				RecordImportSuccess(mock.Anything, mock.Anything).
				Return(nil).Once()
			storeMk.EXPECT().
				MarkRequestsAvailable(mock.Anything, mock.Anything, mock.Anything).
				Return(nil).Once()
			msMk.EXPECT().RefreshAll(mock.Anything, libDir).Return(nil).Once()

			Expect(wp.runImport(context.Background(), 1)).To(Succeed())
		})

		It("holds a clean movie when always_ask is on, without a prober", func() {
			configtest.Setup(map[string]any{
				"library": map[string]any{
					"movie_path":   libDir,
					"import_mode":  "copy",
					"movie_naming": "{title} ({year})/{title}.{ext}",
					"probe":        map[string]any{"always_ask": true},
				},
			})
			src := filepath.Join(tmp, "dl-ask")
			Expect(os.MkdirAll(src, 0o755)).To(Succeed())
			seedMediaFile(src, "Flick.2024.1080p.mkv")
			rec := fixtureRecord(1, 10, src, 0)

			storeMk.EXPECT().
				FindImportingDownloadRecordByID(mock.Anything, uint32(1)).
				Return(rec, nil).Once()
			storeMk.EXPECT().ListMediaFilesByMovieID(mock.Anything, uint32(10)).
				Return(nil, nil).Once()
			storeMk.EXPECT().
				HoldDownloadRecord(mock.Anything, uint32(1), mock.MatchedBy(
					func(rs []schema.HoldReason) bool {
						return len(rs) == 1 && rs[0].Check == "always_ask"
					})).
				Return(nil).Once()

			Expect(w.runImport(context.Background(), 1)).To(Succeed())
		})

		It("fails, never holds, when the source file is missing", func() {
			configtest.Setup(map[string]any{
				"library": map[string]any{
					"movie_path":   libDir,
					"import_mode":  "copy",
					"movie_naming": "{title} ({year})/{title}.{ext}",
					"probe":        map[string]any{"always_ask": true},
				},
			})
			src := filepath.Join(tmp, "dl-empty")
			Expect(os.MkdirAll(src, 0o755)).To(Succeed())
			rec := fixtureRecord(1, 10, src, 0)

			storeMk.EXPECT().
				FindImportingDownloadRecordByID(mock.Anything, uint32(1)).
				Return(rec, nil).Once()
			storeMk.EXPECT().ListMediaFilesByMovieID(mock.Anything, uint32(10)).
				Return(nil, nil).Once()

			Expect(w.runImport(context.Background(), 1)).
				To(MatchError(library.ErrNoMedia))
		})

		It("holds a whole season pack when one file fails", func() {
			season, eps := buildShow()
			src := filepath.Join(tmp, "pack-held")
			Expect(os.MkdirAll(src, 0o755)).To(Succeed())
			seedMediaFile(src, "Show.S01E01.1080p.mkv")
			seedMediaFile(src, "Show.S01E02.1080p.mkv")
			rec := episodeRecord(2, src, season, eps[0])
			wp := proberReturning(probed(1280, "h264"), nil)

			storeMk.EXPECT().
				FindImportingDownloadRecordByID(mock.Anything, uint32(2)).
				Return(rec, nil).Once()
			storeMk.EXPECT().
				HoldDownloadRecord(mock.Anything, uint32(2), mock.MatchedBy(
					func(rs []schema.HoldReason) bool {
						return len(rs) == 2 && rs[0].Check == "resolution"
					})).
				Return(nil).Once()

			Expect(wp.runImport(context.Background(), 2)).To(Succeed())
		})
	})
})

// buildShow wires a one-season show with two episodes, with the season<->show
// and season->episodes edges populated for matcher + importer tests.
func buildShow() (*ent.Season, []*ent.Episode) {
	ep1 := &ent.Episode{ID: 101, Number: 1}
	ep2 := &ent.Episode{ID: 102, Number: 2}
	season := &ent.Season{ID: 11, Number: 1}
	season.Edges.Episodes = []*ent.Episode{ep1, ep2}
	show := &ent.TVShow{ID: 1, Title: "Show", Year: 2024, Type: tvshow.TypeStandard}
	show.Edges.Seasons = []*ent.Season{season}
	season.Edges.TvShow = show
	ep1.Edges.Season = season
	ep2.Edges.Season = season
	return season, []*ent.Episode{ep1, ep2}
}

func episodeRecord(
	id uint32,
	savePath string,
	season *ent.Season,
	ep *ent.Episode,
) *ent.DownloadRecord {
	r := &ent.DownloadRecord{
		ID:                 id,
		TorrentHash:        "hash",
		SavePath:           savePath,
		Status:             downloadrecord.StatusImporting,
		DownloadClientName: "qbit",
	}
	r.Edges.Episode = ep
	_ = season
	return r
}
