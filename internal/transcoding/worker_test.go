package transcoding

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stretchr/testify/mock"

	"github.com/datahearth/streamline/ent"
	entmovie "github.com/datahearth/streamline/ent/movie"
	"github.com/datahearth/streamline/ent/transcodejob"
	"github.com/datahearth/streamline/internal/db"
	dbmocks "github.com/datahearth/streamline/internal/db/mocks"
	"github.com/datahearth/streamline/internal/ffmpeg"
	"github.com/datahearth/streamline/internal/testutil/configtest"
	"github.com/datahearth/streamline/internal/transcoding/mocks"
)

// shortDurationProbe is the verification-failure fixture: the same shape as
// testdata/ffprobe_h264_sdr.json with a duration nowhere near the source's.
const shortDurationProbe = `{
	"streams": [
		{"codec_type": "video", "codec_name": "hevc", "width": 1920,
		 "height": 1080, "duration": "120.000000"},
		{"codec_type": "audio", "codec_name": "aac", "channels": 2}
	],
	"format": {"format_name": "matroska,webm", "duration": "120.000000"}
}`

var _ = Describe("Worker", Label("integration", "transcoding"), func() {
	var (
		ctx       context.Context
		client    *ent.Client
		store     *db.DB
		ms        *mocks.MockMediaServerRefresher
		worker    *Worker
		root      string
		movieRoot string
		bin       string
		tmdbSeq   uint32
	)

	// profiles: "hevc" (the default) transcodes anything above 12 Mbit/s;
	// "lenient" passes the SDR fixture's 14.5 Mbit/s untouched.
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
		root = GinkgoT().TempDir()
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

		encoded := filepath.Join(root, "encoded.bin")
		Expect(os.WriteFile(encoded, []byte("transcoded"), 0o644)).To(Succeed())
		GinkgoT().Setenv("FAKE_INPUT", encoded)

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

	// seedMovieFileExt writes a library file with the given extension on disk
	// and returns the MediaFile row pointing at it, owned by a movie on the
	// named quality profile.
	seedMovieFileExt := func(profile, ext string) *ent.MediaFile {
		GinkgoHelper()
		tmdbSeq++
		m, err := store.CreateMovie(ctx, db.CreateMovieParams{
			Title: "Dune", OriginalTitle: "Dune", Year: 2021, TmdbID: tmdbSeq,
			Status: entmovie.StatusAvailable, QualityProfile: profile,
		})
		Expect(err).NotTo(HaveOccurred())

		path := filepath.Join(
			movieRoot,
			m.Title+"."+string(rune('a'+tmdbSeq))+ext,
		)
		Expect(os.WriteFile(path, []byte("original-bytes"), 0o644)).To(Succeed())
		mf, err := store.CreateMediaFile(ctx, db.CreateMediaFileParams{
			Path: path, Size: int64(len("original-bytes")), MovieID: m.ID,
		})
		Expect(err).NotTo(HaveOccurred())
		return mf
	}

	seedMovieFile := func(profile string) *ent.MediaFile {
		GinkgoHelper()
		return seedMovieFileExt(profile, ".mkv")
	}

	queueJob := func(mf *ent.MediaFile) *ent.TranscodeJob {
		GinkgoHelper()
		job, err := store.CreateTranscodeJob(ctx, mf.ID)
		Expect(err).NotTo(HaveOccurred())
		return job
	}

	tempFiles := func() []string {
		GinkgoHelper()
		matches, err := filepath.Glob(filepath.Join(movieRoot, "*.streamline-tmp.*"))
		Expect(err).NotTo(HaveOccurred())
		return matches
	}

	reload := func(job *ent.TranscodeJob) *ent.TranscodeJob {
		GinkgoHelper()
		got, err := client.TranscodeJob.Get(ctx, job.ID)
		Expect(err).NotTo(HaveOccurred())
		return got
	}

	It("transcodes a non-compliant file and swaps it in", func() {
		mf := seedMovieFile("hevc")
		job := queueJob(mf)
		ms.EXPECT().
			RefreshAll(mock.Anything, "movie", movieRoot).
			Return(nil).
			Once()

		Expect(worker.tick(ctx)).To(BeTrue())

		Expect(os.ReadFile(mf.Path)).To(Equal([]byte("transcoded")))
		Expect(tempFiles()).To(BeEmpty())

		got, err := store.FindMediaFileByID(ctx, mf.ID)
		Expect(err).NotTo(HaveOccurred())
		Expect(got.Path).To(Equal(mf.Path))
		Expect(got.Size).To(Equal(int64(len("transcoded"))))
		Expect(got.SizeBefore).To(Equal(mf.Size))
		Expect(got.Format).To(Equal("mkv"))
		Expect(got.TranscodedAt).NotTo(BeNil())
		Expect(got.ProbedAt).To(BeNil())

		done := reload(job)
		Expect(done.Status).To(Equal(transcodejob.StatusSucceeded))
		Expect(done.SizeBefore).To(Equal(mf.Size))
		Expect(done.SizeAfter).To(Equal(int64(len("transcoded"))))
		Expect(done.FinishedAt).NotTo(BeNil())
	})

	It("completes a compliant file as a no-op, leaving it untouched", func() {
		mf := seedMovieFile("lenient")
		job := queueJob(mf)

		Expect(worker.tick(ctx)).To(BeTrue())

		Expect(os.ReadFile(mf.Path)).To(Equal([]byte("original-bytes")))
		Expect(tempFiles()).To(BeEmpty())

		got, err := store.FindMediaFileByID(ctx, mf.ID)
		Expect(err).NotTo(HaveOccurred())
		Expect(got.TranscodedAt).To(BeNil())

		done := reload(job)
		Expect(done.Status).To(Equal(transcodejob.StatusSucceeded))
		Expect(done.SizeBefore).To(Equal(mf.Size))
		Expect(done.SizeAfter).To(Equal(mf.Size))
	})

	It("leaves an HDR file alone under a profile it otherwise fails", func() {
		dv, err := filepath.Abs("testdata/ffprobe_hevc_dv.json")
		Expect(err).NotTo(HaveOccurred())
		GinkgoT().Setenv("FFPROBE_FIXTURE", dv)

		mf := seedMovieFile("hevc")
		job := queueJob(mf)

		Expect(worker.tick(ctx)).To(BeTrue())

		Expect(os.ReadFile(mf.Path)).To(Equal([]byte("original-bytes")))
		Expect(reload(job).Status).To(Equal(transcodejob.StatusSucceeded))
	})

	It(
		"remuxes into the policy's container, moving the file off its old extension",
		func() {
			// The DV fixture suspends the codec and bitrate rules, so the container
			// is the only rule left to fail — which is remux, and the one action
			// whose output lands on a path different from the source's.
			dv, err := filepath.Abs("testdata/ffprobe_hevc_dv.json")
			Expect(err).NotTo(HaveOccurred())
			GinkgoT().Setenv("FFPROBE_FIXTURE", dv)

			mf := seedMovieFileExt("hevc", ".avi")
			job := queueJob(mf)
			ms.EXPECT().
				RefreshAll(mock.Anything, "movie", movieRoot).
				Return(nil).
				Once()

			Expect(worker.tick(ctx)).To(BeTrue())

			finalPath := strings.TrimSuffix(mf.Path, ".avi") + ".mkv"
			Expect(os.ReadFile(finalPath)).To(Equal([]byte("transcoded")))
			Expect(mf.Path).NotTo(BeAnExistingFile())
			Expect(tempFiles()).To(BeEmpty())

			got, err := store.FindMediaFileByID(ctx, mf.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(got.Path).To(Equal(finalPath))
			Expect(got.Format).To(Equal("mkv"))
			Expect(got.SizeBefore).To(Equal(mf.Size))

			Expect(reload(job).Status).To(Equal(transcodejob.StatusSucceeded))
		},
	)

	It("requeues the job when ffmpeg fails, leaving the original in place", func() {
		GinkgoT().Setenv("FAKE_FFMPEG_FAIL", "1")
		mf := seedMovieFile("hevc")
		job := queueJob(mf)

		Expect(worker.tick(ctx)).To(BeTrue())

		Expect(os.ReadFile(mf.Path)).To(Equal([]byte("original-bytes")))
		Expect(tempFiles()).To(BeEmpty())

		got := reload(job)
		Expect(got.Status).To(Equal(transcodejob.StatusQueued))
		Expect(got.Attempts).To(Equal(uint8(1)))
		Expect(got.Error).To(ContainSubstring("broken pipe"))
		Expect(got.FinishedAt).To(BeNil())
	})

	It("fails the job terminally once max_failures attempts are spent", func() {
		GinkgoT().Setenv("FAKE_FFMPEG_FAIL", "1")
		mf := seedMovieFile("hevc")
		job := queueJob(mf)
		Expect(client.TranscodeJob.UpdateOneID(job.ID).
			SetAttempts(2).Exec(ctx)).To(Succeed())

		Expect(worker.tick(ctx)).To(BeTrue())

		got := reload(job)
		Expect(got.Status).To(Equal(transcodejob.StatusFailed))
		Expect(got.Attempts).To(Equal(uint8(3)))
		Expect(got.FinishedAt).NotTo(BeNil())
	})

	It("requeues the job when the output fails verification", func() {
		divergent := filepath.Join(root, "short.json")
		Expect(os.WriteFile(divergent, []byte(shortDurationProbe), 0o644)).
			To(Succeed())
		GinkgoT().Setenv("FFPROBE_FIXTURE_TMP", divergent)

		mf := seedMovieFile("hevc")
		job := queueJob(mf)

		Expect(worker.tick(ctx)).To(BeTrue())

		Expect(os.ReadFile(mf.Path)).To(Equal([]byte("original-bytes")))
		Expect(tempFiles()).To(BeEmpty())

		got := reload(job)
		Expect(got.Status).To(Equal(transcodejob.StatusQueued))
		Expect(got.Error).To(ContainSubstring("output verification"))
	})

	It("requeues running jobs and sweeps stray temp files on recovery", func() {
		mf := seedMovieFile("hevc")
		job := queueJob(mf)
		claimed, err := store.ClaimNextTranscodeJob(ctx)
		Expect(err).NotTo(HaveOccurred())
		Expect(claimed).NotTo(BeNil())

		stray := filepath.Join(movieRoot, "Leftover.streamline-tmp.mkv")
		Expect(os.WriteFile(stray, []byte("half"), 0o644)).To(Succeed())

		Expect(worker.recover(ctx)).To(Succeed())

		Expect(reload(job).Status).To(Equal(transcodejob.StatusQueued))
		Expect(stray).NotTo(BeAnExistingFile())
	})

	It("cancels a running job and cleans up after it", func() {
		GinkgoT().Setenv("FAKE_FFMPEG_SLEEP", "5")
		mf := seedMovieFile("hevc")
		job := queueJob(mf)

		finished := make(chan struct{})
		go func() {
			defer GinkgoRecover()
			defer close(finished)
			worker.tick(ctx)
		}()

		// The fake's first progress block is one second in at 2x, which is what
		// reaches Progress while the encode is still running.
		Eventually(func() float64 {
			snap, ok := worker.Progress(job.ID)
			if !ok {
				return 0
			}
			return snap.Speed
		}, 10*time.Second, 20*time.Millisecond).Should(Equal(2.0))

		Expect(worker.Cancel(ctx, job.ID)).To(Succeed())
		Eventually(finished, 10*time.Second).Should(BeClosed())

		Expect(reload(job).Status).To(Equal(transcodejob.StatusCanceled))
		Expect(tempFiles()).To(BeEmpty())
		Expect(os.ReadFile(mf.Path)).To(Equal([]byte("original-bytes")))

		_, ok := worker.Progress(job.ID)
		Expect(ok).To(BeFalse())
	})

	It("honours a cancel issued between the claim and the encode", func() {
		mf := seedMovieFile("hevc")
		job := queueJob(mf)

		// The row is running from the claim, so this is the window a Cancel
		// used to fall through: it marked the row canceled while the encode
		// went on to complete over it.
		c, err := worker.claim(ctx)
		Expect(err).NotTo(HaveOccurred())
		Expect(c).NotTo(BeNil())
		defer worker.release()

		Expect(worker.Cancel(ctx, job.ID)).To(Succeed())
		worker.runJob(ctx, c)

		Expect(reload(job).Status).To(Equal(transcodejob.StatusCanceled))
		Expect(os.ReadFile(mf.Path)).To(Equal([]byte("original-bytes")))
		Expect(tempFiles()).To(BeEmpty())
	})

	It("honours a cancel issued while the output is being verified", func() {
		marker := filepath.Join(root, "verifying")
		GinkgoT().Setenv("FFPROBE_MARKER_TMP", marker)
		GinkgoT().Setenv("FFPROBE_SLEEP_TMP", "2")

		mf := seedMovieFile("hevc")
		job := queueJob(mf)

		finished := make(chan struct{})
		go func() {
			defer GinkgoRecover()
			defer close(finished)
			worker.tick(ctx)
		}()

		// The marker is written by the verification probe, so the encode has
		// already finished and the swap has not happened yet.
		Eventually(marker, 10*time.Second, 20*time.Millisecond).
			Should(BeAnExistingFile())

		Expect(worker.Cancel(ctx, job.ID)).To(Succeed())
		Eventually(finished, 10*time.Second).Should(BeClosed())

		Expect(reload(job).Status).To(Equal(transcodejob.StatusCanceled))
		Expect(os.ReadFile(mf.Path)).To(Equal([]byte("original-bytes")))
		Expect(tempFiles()).To(BeEmpty())
	})

	It("fails terminally when the row cannot be updated after the swap", func() {
		// The store is mocked here because the failure under test is the store
		// itself refusing the write once the bytes on disk have already been
		// replaced: a retry would re-probe a source that no longer exists, so
		// the job must not go back to the queue.
		path := filepath.Join(movieRoot, "Arrival.mkv")
		Expect(os.WriteFile(path, []byte("original-bytes"), 0o644)).To(Succeed())

		mf := &ent.MediaFile{ID: 7, Path: path, Size: 14}
		mf.Edges.Movie = &ent.Movie{ID: 1, QualityProfile: "hevc"}
		job := &ent.TranscodeJob{ID: 3, Attempts: 1}
		job.Edges.MediaFile = mf

		mockStore := dbmocks.NewMockStore(GinkgoT())
		mockStore.EXPECT().
			ClaimNextTranscodeJob(mock.Anything).
			Return(job, nil).
			Once()
		mockStore.EXPECT().
			UpdateMediaFileAfterTranscode(
				mock.Anything, mf.ID, path, mock.Anything, mf.Size, "mkv",
			).
			Return(errors.New("database is locked")).
			Once()
		mockStore.EXPECT().
			FailTranscodeJob(mock.Anything, job.ID, mock.Anything, true).
			Return(nil).
			Once()

		w := NewWorker(
			Deps{DB: mockStore, Prober: ffmpeg.NewCLI(bin), MediaServer: ms},
		)
		Expect(w.tick(ctx)).To(BeTrue())

		// The swap did happen — that is exactly what makes the job terminal.
		Expect(os.ReadFile(path)).To(Equal([]byte("transcoded")))
		Expect(tempFiles()).To(BeEmpty())
	})

	It("runs no more jobs at once than max_concurrent allows", func() {
		GinkgoT().Setenv("FAKE_FFMPEG_SLEEP", "5")
		first := queueJob(seedMovieFile("hevc"))
		second := queueJob(seedMovieFile("hevc"))

		worker.fill(ctx)

		Eventually(worker.active, 10*time.Second, 20*time.Millisecond).
			Should(Equal(1))
		Consistently(worker.active, 300*time.Millisecond, 50*time.Millisecond).
			Should(Equal(1))
		Expect(reload(second).Status).To(Equal(transcodejob.StatusQueued))

		Expect(worker.Cancel(ctx, first.ID)).To(Succeed())
		Expect(worker.Cancel(ctx, second.ID)).To(Succeed())
		Eventually(worker.active, 10*time.Second, 20*time.Millisecond).
			Should(BeZero())
		worker.jobs.Wait()

		Expect(reload(first).Status).To(Equal(transcodejob.StatusCanceled))
		Expect(reload(second).Status).To(Equal(transcodejob.StatusCanceled))
	})
})
