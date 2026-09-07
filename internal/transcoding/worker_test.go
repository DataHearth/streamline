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
	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/db"
	dbmocks "github.com/datahearth/streamline/internal/db/mocks"
	"github.com/datahearth/streamline/internal/ffmpeg"
	mockffmpeg "github.com/datahearth/streamline/internal/ffmpeg/mocks"
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

	// setup applies the standard worker config, overriding transcoding.verify
	// when verify is non-nil — the rejection specs only need to name what
	// they're testing, not repeat the whole map.
	setup := func(verify map[string]any) {
		transcoding := map[string]any{
			"enabled":        true,
			"max_concurrent": 1,
			"max_failures":   3,
		}
		if verify != nil {
			transcoding["verify"] = verify
		}
		configtest.Setup(map[string]any{
			"ffmpeg": map[string]any{"enabled": true, "path": bin},
			"library": map[string]any{
				"movie_path":  movieRoot,
				"series_path": filepath.Join(root, "series"),
			},
			"transcoding": transcoding,
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

		setup(nil)

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
		// The output's own probe, taken by verify, so nothing scores the row
		// off the release name until a backfill catches up with it.
		Expect(got.ProbedAt).NotTo(BeNil())
		Expect(got.VideoCodec).To(Equal("h264"))
		Expect(got.AudioTracks).To(Equal(uint8(2)))

		done := reload(job)
		Expect(done.Status).To(Equal(transcodejob.StatusSucceeded))
		Expect(done.SizeBefore).To(Equal(mf.Size))
		Expect(done.SizeAfter).To(Equal(int64(len("transcoded"))))
		Expect(done.FinishedAt).NotTo(BeNil())
	})

	// The rejection specs drive the outcome through what the fake encode
	// "produces": the BeforeEach's 10-byte output is 71 % of the 14-byte
	// source and passes the default band.
	setEncoded := func(content string) {
		GinkgoHelper()
		encoded := filepath.Join(root, "encoded.bin")
		Expect(os.WriteFile(encoded, []byte(content), 0o644)).To(Succeed())
	}

	It("rejects an encode larger than the source and leaves the file alone", func() {
		setEncoded("this output is bigger") // 21 bytes → 150 %
		mf := seedMovieFile("hevc")
		job := queueJob(mf)

		Expect(worker.tick(ctx)).To(BeTrue())

		Expect(os.ReadFile(mf.Path)).To(Equal([]byte("original-bytes")))
		Expect(tempFiles()).To(BeEmpty())
		got, err := store.FindMediaFileByID(ctx, mf.ID)
		Expect(err).NotTo(HaveOccurred())
		Expect(got.TranscodedAt).To(BeNil())

		done := reload(job)
		Expect(done.Status).To(Equal(transcodejob.StatusRejected))
		Expect(
			done.Error,
		).To(Equal("output rejected: output is 150% of the source (max 100%)"))
		Expect(done.SizeBefore).To(Equal(mf.Size))
		Expect(done.SizeAfter).To(Equal(int64(21)))
		Expect(done.FinishedAt).NotTo(BeNil())
	})

	It("rejects an encode that fails the decode health check", func() {
		setup(map[string]any{"health_check": true})
		GinkgoT().Setenv("FAKE_HEALTH_FAIL", "1")
		mf := seedMovieFile("hevc")
		job := queueJob(mf)

		Expect(worker.tick(ctx)).To(BeTrue())

		Expect(os.ReadFile(mf.Path)).To(Equal([]byte("original-bytes")))
		done := reload(job)
		Expect(done.Status).To(Equal(transcodejob.StatusRejected))
		Expect(
			done.Error,
		).To(HavePrefix("output rejected: decode check failed: Error while decoding"))
	})

	It(
		"cancels a health check in progress instead of recording its verdict",
		func() {
			setup(map[string]any{"health_check": true})
			marker := filepath.Join(root, "health-checking")
			GinkgoT().Setenv("FAKE_HEALTH_MARKER", marker)
			GinkgoT().Setenv("FAKE_HEALTH_SLEEP", "5")

			mf := seedMovieFile("hevc")
			job := queueJob(mf)

			finished := make(chan struct{})
			go func() {
				defer GinkgoRecover()
				defer close(finished)
				worker.tick(ctx)
			}()

			// The marker is touched right as the decode check starts, so the
			// cancel below lands mid-check rather than before or after it.
			Eventually(marker, 10*time.Second, 20*time.Millisecond).
				Should(BeAnExistingFile())

			Expect(worker.Cancel(ctx, job.ID)).To(Succeed())
			Eventually(finished, 10*time.Second).Should(BeClosed())

			Expect(reload(job).Status).To(Equal(transcodejob.StatusCanceled))
			Expect(os.ReadFile(mf.Path)).To(Equal([]byte("original-bytes")))
			Expect(tempFiles()).To(BeEmpty())
		},
	)

	It(
		"cancels a VMAF window in progress instead of failing the attempt",
		func() {
			setup(map[string]any{"min_vmaf": 90})
			marker := filepath.Join(root, "vmaf-scoring")
			GinkgoT().Setenv("FAKE_VMAF_MARKER", marker)
			GinkgoT().Setenv("FAKE_VMAF_SLEEP", "5")

			mf := seedMovieFile("hevc")
			job := queueJob(mf)

			finished := make(chan struct{})
			go func() {
				defer GinkgoRecover()
				defer close(finished)
				worker.tick(ctx)
			}()

			// The marker is touched right as the first window starts, so the
			// cancel below lands mid-window rather than before or after it.
			Eventually(marker, 10*time.Second, 20*time.Millisecond).
				Should(BeAnExistingFile())

			Expect(worker.Cancel(ctx, job.ID)).To(Succeed())
			Eventually(finished, 10*time.Second).Should(BeClosed())

			Expect(reload(job).Status).To(Equal(transcodejob.StatusCanceled))
			Expect(os.ReadFile(mf.Path)).To(Equal([]byte("original-bytes")))
			Expect(tempFiles()).To(BeEmpty())
		},
	)

	It(
		"keeps a health-check exec failure retryable, not a rejection",
		func() {
			setup(map[string]any{"health_check": true})

			info := &ffmpeg.Info{
				DurationSec: 5400, Width: 1920, Height: 1080, AudioTracks: 2,
			}
			outPath := filepath.Join(root, "unreachable.mkv")
			Expect(os.WriteFile(outPath, []byte("output"), 0o644)).To(Succeed())

			// FFmpegPath is normally resolved at boot from ffmpeg.path (or
			// $PATH); pointing it at a binary that isn't there is what makes
			// the health check fail to launch rather than exit non-zero.
			prober := mockffmpeg.NewMockProber(GinkgoT())
			prober.EXPECT().Probe(mock.Anything, outPath).Return(info, nil).Once()
			prober.EXPECT().FFmpegPath().Return("/nonexistent/ffmpeg").Once()

			w := NewWorker(Deps{Prober: prober})
			_, err := w.verify(
				ctx, ctx, outPath, "/irrelevant/src.mkv", info,
				int64(len("output")), ActionRemux,
				config.TranscodePolicy{To: config.TranscodeTo{Container: "mkv"}},
			)

			Expect(err).To(HaveOccurred())
			_, isRejection := errors.AsType[*rejection](err)
			Expect(isRejection).To(BeFalse())
			Expect(err.Error()).To(ContainSubstring("output verification"))
		},
	)

	It("rejects an encode scoring under min_vmaf", func() {
		setup(map[string]any{"min_vmaf": 90})
		GinkgoT().Setenv("FAKE_VMAF", "82.5")
		mf := seedMovieFile("hevc")
		job := queueJob(mf)

		Expect(worker.tick(ctx)).To(BeTrue())

		Expect(os.ReadFile(mf.Path)).To(Equal([]byte("original-bytes")))
		done := reload(job)
		Expect(done.Status).To(Equal(transcodejob.StatusRejected))
		Expect(done.Error).To(Equal("output rejected: VMAF 82.5 below 90"))
	})

	It("swaps anyway when ffmpeg carries no libvmaf, logging once", func() {
		setup(map[string]any{"min_vmaf": 90})
		GinkgoT().Setenv("FAKE_VMAF_MISSING", "1")
		mf := seedMovieFile("hevc")
		job := queueJob(mf)
		ms.EXPECT().RefreshAll(mock.Anything, "movie", movieRoot).Return(nil).Once()

		Expect(worker.tick(ctx)).To(BeTrue())

		Expect(os.ReadFile(mf.Path)).To(Equal([]byte("transcoded")))
		Expect(reload(job).Status).To(Equal(transcodejob.StatusSucceeded))
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

	It(
		"rejects the job when the output's duration diverges from the source",
		func() {
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
			Expect(got.Status).To(Equal(transcodejob.StatusRejected))
			Expect(got.Error).To(ContainSubstring("duration"))
		},
	)

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

	It("drops the encode when the row moved out from under it", func() {
		marker := filepath.Join(root, "verifying-moved")
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

		// Parked on the verification probe: the encode is finished and the
		// swap has not happened, which is the window a replace import or a
		// rename lands in.
		Eventually(marker, 10*time.Second, 20*time.Millisecond).
			Should(BeAnExistingFile())
		moved := filepath.Join(movieRoot, "Renamed.mkv")
		Expect(store.UpdateMediaFilePath(ctx, mf.ID, moved)).To(Succeed())

		Eventually(finished, 10*time.Second).Should(BeClosed())

		Expect(os.ReadFile(mf.Path)).To(Equal([]byte("original-bytes")))
		Expect(moved).NotTo(BeAnExistingFile())
		Expect(tempFiles()).To(BeEmpty())
		Expect(reload(job).Status).To(Equal(transcodejob.StatusCanceled))
	})

	It(
		"cancels rather than records a rejection when the file changed under it",
		func() {
			marker := filepath.Join(root, "verifying-resized")
			GinkgoT().Setenv("FFPROBE_MARKER_TMP", marker)
			GinkgoT().Setenv("FFPROBE_SLEEP_TMP", "2")
			setEncoded(
				"this output is bigger",
			) // 21 bytes → rejected as 150% of the 14-byte source

			mf := seedMovieFile("hevc")
			job := queueJob(mf)

			finished := make(chan struct{})
			go func() {
				defer GinkgoRecover()
				defer close(finished)
				worker.tick(ctx)
			}()

			// Parked on the verification probe: the encode already ran against
			// the original bytes and would be rejected as too large against
			// them, which is exactly the window a replace import lands in.
			Eventually(marker, 10*time.Second, 20*time.Millisecond).
				Should(BeAnExistingFile())
			resized := []byte("a replace import wrote this")
			Expect(os.WriteFile(mf.Path, resized, 0o644)).To(Succeed())
			Expect(client.MediaFile.UpdateOneID(mf.ID).
				SetSize(int64(len(resized))).
				Exec(ctx)).To(Succeed())

			Eventually(finished, 10*time.Second).Should(BeClosed())

			Expect(os.ReadFile(mf.Path)).To(Equal(resized))
			Expect(tempFiles()).To(BeEmpty())
			Expect(reload(job).Status).To(Equal(transcodejob.StatusCanceled))
		},
	)

	It("leaves the row running when shutdown lands during the source probe", func() {
		marker := filepath.Join(root, "probing")
		GinkgoT().Setenv("FFPROBE_MARKER", marker)
		GinkgoT().Setenv("FFPROBE_SLEEP", "2")

		mf := seedMovieFile("hevc")
		job := queueJob(mf)

		runCtx, stop := context.WithCancel(ctx)
		defer stop()
		finished := make(chan struct{})
		go func() {
			defer GinkgoRecover()
			defer close(finished)
			worker.tick(runCtx)
		}()

		Eventually(marker, 10*time.Second, 20*time.Millisecond).
			Should(BeAnExistingFile())
		stop()
		Eventually(finished, 10*time.Second).Should(BeClosed())

		// canceled is terminal — recording one here would retire the file.
		got := reload(job)
		Expect(got.Status).To(Equal(transcodejob.StatusRunning))
		Expect(os.ReadFile(mf.Path)).To(Equal([]byte("original-bytes")))
	})

	It("refuses a container change onto a path another file already holds", func() {
		dv, err := filepath.Abs("testdata/ffprobe_hevc_dv.json")
		Expect(err).NotTo(HaveOccurred())
		GinkgoT().Setenv("FFPROBE_FIXTURE", dv)

		mf := seedMovieFileExt("hevc", ".avi")
		job := queueJob(mf)
		sibling := strings.TrimSuffix(mf.Path, ".avi") + ".mkv"
		Expect(os.WriteFile(sibling, []byte("sibling"), 0o644)).To(Succeed())

		Expect(worker.tick(ctx)).To(BeTrue())

		Expect(os.ReadFile(sibling)).To(Equal([]byte("sibling")))
		Expect(os.ReadFile(mf.Path)).To(Equal([]byte("original-bytes")))
		Expect(tempFiles()).To(BeEmpty())

		got := reload(job)
		Expect(got.Status).To(Equal(transcodejob.StatusQueued))
		Expect(got.Error).To(ContainSubstring("already exists"))
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
			FindMediaFileByID(mock.Anything, mf.ID).
			Return(mf, nil).
			Once()
		mockStore.EXPECT().
			UpdateMediaFileAfterTranscode(
				mock.Anything, mf.ID, path, mock.Anything, mf.Size, "mkv",
				mock.Anything,
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
