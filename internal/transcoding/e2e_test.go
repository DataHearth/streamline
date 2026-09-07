package transcoding

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stretchr/testify/mock"

	"github.com/datahearth/streamline/ent"
	entmovie "github.com/datahearth/streamline/ent/movie"
	"github.com/datahearth/streamline/ent/transcodejob"
	"github.com/datahearth/streamline/internal/db"
	"github.com/datahearth/streamline/internal/ffmpeg"
	"github.com/datahearth/streamline/internal/testutil/configtest"
	"github.com/datahearth/streamline/internal/transcoding/mocks"
)

var _ = Describe(
	"Worker against the real ffmpeg",
	Label("e2e", "transcoding"),
	func() {
		var (
			ctx       context.Context
			client    *ent.Client
			store     *db.DB
			ms        *mocks.MockMediaServerRefresher
			worker    *Worker
			movieRoot string
			srcPath   string
		)

		BeforeEach(func() {
			// The suite ships fake binaries for the unit/integration specs; this one
			// is the only place the real encoder is exercised, so it is skipped
			// rather than failed where the devshell's ffmpeg-headless is absent.
			for _, bin := range []string{"ffmpeg", "ffprobe"} {
				if _, err := exec.LookPath(bin); err != nil {
					Skip("real " + bin + " is not on PATH")
				}
			}

			ctx = context.Background()
			root := GinkgoT().TempDir()
			movieRoot = filepath.Join(root, "movies")
			seriesRoot := filepath.Join(root, "series")
			Expect(os.MkdirAll(seriesRoot, 0o755)).To(Succeed())
			Expect(os.MkdirAll(filepath.Join(movieRoot, "Movie (2020)"), 0o755)).
				To(Succeed())

			srcPath = filepath.Join(movieRoot, "Movie (2020)", "movie.mp4")
			// Two seconds of colour bars and a tone: long enough for the duration
			// check to be meaningful, short enough that libx265 at ultrafast keeps
			// the whole spec inside the e2e suite's budget.
			//nolint:gosec // the binary is a literal and srcPath is this spec's own TempDir
			gen := exec.CommandContext(ctx, "ffmpeg",
				"-hide_banner", "-loglevel", "error", "-y",
				"-f", "lavfi", "-i", "testsrc=duration=2:size=320x240:rate=24",
				"-f", "lavfi", "-i", "sine=frequency=440:duration=2",
				"-c:v", "libx264", "-preset", "ultrafast",
				"-c:a", "aac", "-shortest", srcPath,
			)
			out, err := gen.CombinedOutput()
			Expect(err).NotTo(HaveOccurred(), string(out))

			configtest.Setup(map[string]any{
				// Empty path: both binaries resolve off $PATH, which is what the
				// devshell (and a container image shipping static ffmpeg) provides.
				"ffmpeg": map[string]any{"enabled": true, "path": ""},
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
						"min_resolution":       "720p",
						"transcode": map[string]any{
							// Both rules fail the h264/mp4 source. The codec one is
							// what makes this a re-encode: a container rule on its
							// own evaluates to ActionRemux — `-c copy`, which still
							// lands on .mkv but leaves the stream h264.
							"if": map[string]any{
								"video_codecs": []string{"hevc"},
								"containers":   []string{"mkv"},
							},
							"to": map[string]any{
								"container":   "mkv",
								"video_codec": "hevc",
								"crf":         30,
								"preset":      "ultrafast",
								"audio_codec": "aac",
							},
						},
					},
				},
				"quality_default_profile": "hevc",
			})

			var openErr error
			client, openErr = db.Open(ctx, ":memory:")
			Expect(openErr).NotTo(HaveOccurred())
			DeferCleanup(func() { Expect(client.Close()).To(Succeed()) })
			store = db.New(client)

			ms = mocks.NewMockMediaServerRefresher(GinkgoT())
			worker = NewWorker(Deps{
				DB:          store,
				Prober:      ffmpeg.NewCLI(""),
				MediaServer: ms,
			})
		})

		It("re-encodes a real file into the policy's container and codec", func() {
			st, err := os.Stat(srcPath)
			Expect(err).NotTo(HaveOccurred())

			m, err := store.CreateMovie(ctx, db.CreateMovieParams{
				Title: "Movie", OriginalTitle: "Movie", Year: 2020, TmdbID: 1,
				Status: entmovie.StatusAvailable, QualityProfile: "hevc",
			})
			Expect(err).NotTo(HaveOccurred())
			mf, err := store.CreateMediaFile(ctx, db.CreateMediaFileParams{
				Path: srcPath, Size: st.Size(), MovieID: m.ID,
			})
			Expect(err).NotTo(HaveOccurred())

			job, err := store.CreateTranscodeJob(ctx, mf.ID)
			Expect(err).NotTo(HaveOccurred())

			ms.EXPECT().
				RefreshAll(mock.Anything, "movie", movieRoot).
				Return(nil).
				Once()

			Expect(worker.tick(ctx)).To(BeTrue())

			done, err := client.TranscodeJob.Get(ctx, job.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(done.Status).To(Equal(transcodejob.StatusSucceeded), done.Error)

			outPath := strings.TrimSuffix(srcPath, ".mp4") + ".mkv"
			Expect(outPath).To(BeAnExistingFile())
			Expect(srcPath).NotTo(BeAnExistingFile())
			Expect(filepath.Glob(
				filepath.Join(movieRoot, "*", "*"+tempMarker+"*"),
			)).To(BeEmpty())

			info, err := ffmpeg.NewCLI("").Probe(ctx, outPath)
			Expect(err).NotTo(HaveOccurred())
			Expect(info.VideoCodec).To(Equal("hevc"))

			got, err := store.FindMediaFileByID(ctx, mf.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(got.Path).To(Equal(outPath))
			Expect(got.Format).To(Equal("mkv"))
			Expect(got.SizeBefore).To(Equal(st.Size()))
			Expect(got.Size).NotTo(BeZero())
			Expect(got.Size).NotTo(Equal(st.Size()))
			Expect(got.TranscodedAt).NotTo(BeNil())
			// Restamped from the encode's own probe, so the row describes the
			// hevc file rather than the h264 release name it came in under.
			Expect(got.ProbedAt).NotTo(BeNil())
			Expect(got.VideoCodec).To(Equal("hevc"))
		})
	},
)
