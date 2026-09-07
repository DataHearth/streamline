package transcoding

import (
	"context"
	"os"
	"path/filepath"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/mediafile"
	entmovie "github.com/datahearth/streamline/ent/movie"
	"github.com/datahearth/streamline/ent/transcodejob"
	"github.com/datahearth/streamline/internal/db"
	"github.com/datahearth/streamline/internal/ffmpeg"
	"github.com/datahearth/streamline/internal/testutil/configtest"
	"github.com/datahearth/streamline/internal/transcoding/mocks"
)

var _ = Describe("Worker.Scan", Label("integration", "transcoding"), func() {
	var (
		ctx       context.Context
		client    *ent.Client
		store     *db.DB
		ms        *mocks.MockMediaServerRefresher
		worker    *Worker
		movieRoot string
		bin       string
		tmdbSeq   uint32
	)

	transcodePolicy := func(maxBitrate string) map[string]any {
		return map[string]any{
			"if": map[string]any{
				"max_video_bitrate": maxBitrate,
				"containers":        []string{"mkv"},
			},
			"to": map[string]any{
				"container":   "mkv",
				"video_codec": "hevc",
				"crf":         22,
				"preset":      "medium",
				"audio_codec": "aac",
			},
		}
	}

	BeforeEach(func() {
		ctx = context.Background()
		root := GinkgoT().TempDir()
		movieRoot = filepath.Join(root, "movies")
		seriesRoot := filepath.Join(root, "series")
		Expect(os.MkdirAll(movieRoot, 0o755)).To(Succeed())
		Expect(os.MkdirAll(seriesRoot, 0o755)).To(Succeed())

		var err error
		bin, err = filepath.Abs("testdata/bin")
		Expect(err).NotTo(HaveOccurred())
		sdr, err := filepath.Abs("testdata/ffprobe_h264_sdr.json")
		Expect(err).NotTo(HaveOccurred())
		GinkgoT().Setenv("FFPROBE_FIXTURE", sdr)

		configtest.Setup(map[string]any{
			"ffmpeg": map[string]any{"enabled": true, "path": bin},
			"library": map[string]any{
				"movie_path":  movieRoot,
				"series_path": seriesRoot,
			},
			"transcoding": map[string]any{
				"enabled":        true,
				"max_concurrent": 1,
				"max_failures":   3,
			},
			"quality_profiles": []map[string]any{
				{
					"name":                 "hevc",
					"preferred_resolution": "1080p",
					"min_resolution":       "1080p",
					"transcode":            transcodePolicy("12M"),
				},
				{
					"name":                 "lenient",
					"preferred_resolution": "1080p",
					"min_resolution":       "1080p",
					"transcode":            transcodePolicy("20M"),
				},
			},
			"quality_default_profile": "hevc",
		})

		client, err = db.Open(ctx, ":memory:")
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { Expect(client.Close()).To(Succeed()) })
		store = db.New(client)
		tmdbSeq = 0

		ms = mocks.NewMockMediaServerRefresher(GinkgoT())
		worker = NewWorker(Deps{
			DB:          store,
			Prober:      ffmpeg.NewCLI(bin),
			MediaServer: ms,
		})
	})

	seedMovieFile := func(profile string) *ent.MediaFile {
		GinkgoHelper()
		tmdbSeq++
		m, err := store.CreateMovie(ctx, db.CreateMovieParams{
			Title: "Dune", OriginalTitle: "Dune", Year: 2021, TmdbID: tmdbSeq,
			Status: entmovie.StatusAvailable, QualityProfile: profile,
		})
		Expect(err).NotTo(HaveOccurred())

		path := filepath.Join(
			movieRoot,
			m.Title+"."+string(rune('a'+tmdbSeq))+".mkv",
		)
		Expect(os.WriteFile(path, []byte("original-bytes"), 0o644)).To(Succeed())
		mf, err := store.CreateMediaFile(ctx, db.CreateMediaFileParams{
			Path: path, Size: int64(len("original-bytes")), MovieID: m.ID,
		})
		Expect(err).NotTo(HaveOccurred())
		return mf
	}

	jobCount := func() int {
		GinkgoHelper()
		n, err := client.TranscodeJob.Query().Count(ctx)
		Expect(err).NotTo(HaveOccurred())
		return n
	}

	hasQueuedJobFor := func(mf *ent.MediaFile) bool {
		GinkgoHelper()
		ok, err := client.TranscodeJob.Query().
			Where(transcodejob.HasMediaFileWith(mediafile.IDEQ(mf.ID))).
			Exist(ctx)
		Expect(err).NotTo(HaveOccurred())
		return ok
	}

	// awaitScanDone blocks until the scan goroutine has cleared its running
	// flag, so a spec never ends — and tears down its client/env — while that
	// goroutine is still in flight.
	awaitScanDone := func() {
		GinkgoHelper()
		Eventually(
			func() bool { return !worker.scanning.Load() },
			10*time.Second,
			20*time.Millisecond,
		).
			Should(BeTrue())
	}

	It(
		"queues non-compliant files, skips compliant ones and dedupes an already-queued file",
		func() {
			nonCompliant := seedMovieFile("hevc")
			compliant := seedMovieFile("lenient")
			alreadyQueued := seedMovieFile("hevc")
			existing, err := store.CreateTranscodeJob(ctx, alreadyQueued.ID)
			Expect(err).NotTo(HaveOccurred())

			Expect(worker.Scan(ctx)).To(Succeed())
			awaitScanDone()

			Expect(jobCount()).To(Equal(2))
			Expect(hasQueuedJobFor(nonCompliant)).To(BeTrue())
			Expect(hasQueuedJobFor(compliant)).To(BeFalse())

			stillOpen, err := client.TranscodeJob.Get(ctx, existing.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(stillOpen.Status).To(Equal(transcodejob.StatusQueued))
		},
	)

	It("returns ErrScanRunning when a scan is already in flight", func() {
		marker := filepath.Join(GinkgoT().TempDir(), "scanning")
		GinkgoT().Setenv("FFPROBE_MARKER", marker)
		GinkgoT().Setenv("FFPROBE_SLEEP", "2")

		seedMovieFile("hevc")

		Expect(worker.Scan(ctx)).To(Succeed())

		Eventually(marker, 10*time.Second, 20*time.Millisecond).
			Should(BeAnExistingFile())

		Expect(worker.Scan(ctx)).To(MatchError(ErrScanRunning))

		awaitScanDone()
		Expect(jobCount()).To(Equal(1))
	})
})
