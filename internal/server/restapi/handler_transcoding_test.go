package restapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stretchr/testify/mock"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/transcodejob"
	"github.com/datahearth/streamline/internal/db"
	"github.com/datahearth/streamline/internal/testutil/configtest"
)

// jsonKeyReq builds a JSON request authenticated with the given key — the
// harness's own jsonReq is admin-only, and the 403 specs need a member.
func jsonKeyReq(app *apiKeyApp, method, path, key, body string) *http.Request {
	GinkgoHelper()
	req := app.req(method, path, key, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	return req
}

// transcodingOn is the config overlay every spec that expects the endpoints to
// answer needs: the four transcoding routes are 409 while the feature is off.
func transcodingOn() map[string]any {
	return map[string]any{
		"transcoding": map[string]any{
			"enabled":        true,
			"max_concurrent": 1,
			"max_failures":   3,
		},
	}
}

func movieJob(id uint32, status transcodejob.Status) *ent.TranscodeJob {
	GinkgoHelper()
	mf := &ent.MediaFile{ID: 10, Path: "/movies/Heat (1995)/Heat.mkv", Size: 42}
	mf.Edges.Movie = &ent.Movie{ID: 7, Title: "Heat", Year: 1995}
	j := &ent.TranscodeJob{
		ID:         id,
		Status:     status,
		Attempts:   1,
		CreateTime: time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC),
	}
	j.Edges.MediaFile = mf
	return j
}

func episodeJob(id uint32) *ent.TranscodeJob {
	GinkgoHelper()
	show := &ent.TVShow{ID: 3, Title: "Kaamelott"}
	season := &ent.Season{ID: 4, Number: 2}
	season.Edges.TvShow = show
	ep := &ent.Episode{ID: 5, Number: 7}
	ep.Edges.Season = season
	mf := &ent.MediaFile{ID: 11, Path: "/tv/Kaamelott/S02E07.mkv"}
	mf.Edges.Episode = ep
	j := &ent.TranscodeJob{
		ID:         id,
		Status:     transcodejob.StatusQueued,
		CreateTime: time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC),
	}
	j.Edges.MediaFile = mf
	return j
}

var _ = Describe(
	"Handler: Transcoding",
	Label("unit", "server", "transcoding"),
	func() {
		var app *apiKeyApp

		BeforeEach(func() {
			configtest.SetupFile(transcodingOn())
			app = newAPIKeyApp()
		})

		Describe("ListTranscodeQueue", func() {
			It("maps a movie job's title, path and ids", func() {
				app.store.EXPECT().
					ListTranscodeJobs(mock.Anything, 200).
					Return([]*ent.TranscodeJob{
						movieJob(1, transcodejob.StatusSucceeded),
					}, nil).
					Once()

				resp := app.do(app.req(
					http.MethodGet, "/api/v1/transcoding/queue", "", nil,
				))
				defer resp.Body.Close()
				Expect(resp.StatusCode).To(Equal(http.StatusOK))

				var jobs []TranscodeJob
				Expect(json.NewDecoder(resp.Body).Decode(&jobs)).To(Succeed())
				Expect(jobs).To(HaveLen(1))
				Expect(jobs[0].Id).To(Equal(uint32(1)))
				Expect(jobs[0].MediaTitle).To(Equal("Heat (1995)"))
				Expect(jobs[0].FilePath).
					To(Equal("/movies/Heat (1995)/Heat.mkv"))
				Expect(jobs[0].MovieId).To(HaveValue(Equal(uint32(7))))
				Expect(jobs[0].SeriesId).To(BeNil())
				Expect(jobs[0].EpisodeId).To(BeNil())
				Expect(jobs[0].Attempts).To(Equal(uint8(1)))
			})

			It("drops the parentheses for a movie with no year", func() {
				j := movieJob(1, transcodejob.StatusQueued)
				j.Edges.MediaFile.Edges.Movie.Year = 0
				app.store.EXPECT().
					ListTranscodeJobs(mock.Anything, 200).
					Return([]*ent.TranscodeJob{j}, nil).
					Once()

				resp := app.do(app.req(
					http.MethodGet, "/api/v1/transcoding/queue", "", nil,
				))
				defer resp.Body.Close()
				Expect(resp.StatusCode).To(Equal(http.StatusOK))

				var jobs []TranscodeJob
				Expect(json.NewDecoder(resp.Body).Decode(&jobs)).To(Succeed())
				Expect(jobs[0].MediaTitle).To(Equal("Heat"))
			})

			It("maps an episode job through season → show", func() {
				app.store.EXPECT().
					ListTranscodeJobs(mock.Anything, 200).
					Return([]*ent.TranscodeJob{episodeJob(2)}, nil).
					Once()

				resp := app.do(app.req(
					http.MethodGet, "/api/v1/transcoding/queue", "", nil,
				))
				defer resp.Body.Close()
				Expect(resp.StatusCode).To(Equal(http.StatusOK))

				var jobs []TranscodeJob
				Expect(json.NewDecoder(resp.Body).Decode(&jobs)).To(Succeed())
				Expect(jobs).To(HaveLen(1))
				Expect(jobs[0].MediaTitle).To(Equal("Kaamelott S02E07"))
				Expect(jobs[0].SeriesId).To(HaveValue(Equal(uint32(3))))
				Expect(jobs[0].EpisodeId).To(HaveValue(Equal(uint32(5))))
				Expect(jobs[0].MovieId).To(BeNil())
			})

			It(
				"omits the live fields for a running job nothing is encoding",
				func() {
					app.store.EXPECT().
						ListTranscodeJobs(mock.Anything, 200).
						Return([]*ent.TranscodeJob{
							movieJob(3, transcodejob.StatusRunning),
						}, nil).
						Once()

					resp := app.do(app.req(
						http.MethodGet, "/api/v1/transcoding/queue", "", nil,
					))
					defer resp.Body.Close()
					Expect(resp.StatusCode).To(Equal(http.StatusOK))

					var jobs []TranscodeJob
					Expect(json.NewDecoder(resp.Body).Decode(&jobs)).
						To(Succeed())
					Expect(jobs[0].Status).
						To(Equal(TranscodeJobStatus("running")))
					Expect(jobs[0].Percent).To(BeNil())
					Expect(jobs[0].EtaSeconds).To(BeNil())
					Expect(jobs[0].Speed).To(BeNil())
				},
			)

			It("answers 409 while transcoding is disabled", func() {
				configtest.SetupFile()

				resp := app.do(app.req(
					http.MethodGet, "/api/v1/transcoding/queue", "", nil,
				))
				defer resp.Body.Close()
				Expect(resp.StatusCode).To(Equal(http.StatusConflict))
			})

			It("refuses a non-admin", func() {
				app.addMember("member@test.com")

				resp := app.do(app.req(
					http.MethodGet,
					"/api/v1/transcoding/queue",
					app.memberKey,
					nil,
				))
				defer resp.Body.Close()
				Expect(resp.StatusCode).To(Equal(http.StatusForbidden))
			})
		})

		Describe("CancelTranscodeJob", func() {
			It("cancels an open job", func() {
				app.store.EXPECT().
					MarkTranscodeJobCanceled(mock.Anything, uint32(4)).
					Return(nil).
					Once()

				resp := app.do(app.req(
					http.MethodPost,
					"/api/v1/transcoding/jobs/4/cancel",
					"",
					nil,
				))
				defer resp.Body.Close()
				Expect(resp.StatusCode).To(Equal(http.StatusNoContent))
			})

			// An unknown id reaches the same arm: the store's conditional
			// update reports zero rows for a missing job and a finished one
			// alike, so there is no 404 to assert.
			It("answers 409 for a finished job", func() {
				app.store.EXPECT().
					MarkTranscodeJobCanceled(mock.Anything, uint32(4)).
					Return(db.ErrTranscodeJobNotCancelable).
					Once()

				resp := app.do(app.req(
					http.MethodPost,
					"/api/v1/transcoding/jobs/4/cancel",
					"",
					nil,
				))
				defer resp.Body.Close()
				Expect(resp.StatusCode).To(Equal(http.StatusConflict))
			})

			It("answers 409 while transcoding is disabled", func() {
				configtest.SetupFile()

				resp := app.do(app.req(
					http.MethodPost,
					"/api/v1/transcoding/jobs/4/cancel",
					"",
					nil,
				))
				defer resp.Body.Close()
				Expect(resp.StatusCode).To(Equal(http.StatusConflict))
			})

			It("refuses a non-admin", func() {
				app.addMember("member@test.com")

				resp := app.do(app.req(
					http.MethodPost,
					"/api/v1/transcoding/jobs/4/cancel",
					app.memberKey,
					nil,
				))
				defer resp.Body.Close()
				Expect(resp.StatusCode).To(Equal(http.StatusForbidden))
			})
		})

		Describe("RetryTranscodeJob", func() {
			It("re-queues a failed job", func() {
				app.store.EXPECT().
					RetryTranscodeJob(mock.Anything, uint32(9)).
					Return(nil).
					Once()

				resp := app.do(app.req(
					http.MethodPost,
					"/api/v1/transcoding/jobs/9/retry",
					"",
					nil,
				))
				defer resp.Body.Close()
				Expect(resp.StatusCode).To(Equal(http.StatusNoContent))
			})

			// Same conflation as cancel: an unknown id is this arm, not a 404.
			It("answers 409 for a job that never failed", func() {
				app.store.EXPECT().
					RetryTranscodeJob(mock.Anything, uint32(9)).
					Return(db.ErrTranscodeJobNotRetryable).
					Once()

				resp := app.do(app.req(
					http.MethodPost,
					"/api/v1/transcoding/jobs/9/retry",
					"",
					nil,
				))
				defer resp.Body.Close()
				Expect(resp.StatusCode).To(Equal(http.StatusConflict))
			})

			It("answers 409 while transcoding is disabled", func() {
				configtest.SetupFile()

				resp := app.do(app.req(
					http.MethodPost,
					"/api/v1/transcoding/jobs/9/retry",
					"",
					nil,
				))
				defer resp.Body.Close()
				Expect(resp.StatusCode).To(Equal(http.StatusConflict))
			})

			It("refuses a non-admin", func() {
				app.addMember("member@test.com")

				resp := app.do(app.req(
					http.MethodPost,
					"/api/v1/transcoding/jobs/9/retry",
					app.memberKey,
					nil,
				))
				defer resp.Body.Close()
				Expect(resp.StatusCode).To(Equal(http.StatusForbidden))
			})
		})

		Describe("StartTranscodeScan", func() {
			// Scan is the one endpoint that also asks whether the worker
			// could claim what it queues, which reads the resolved binary.
			BeforeEach(func() {
				app.prober.EXPECT().
					FFmpegPath().
					Return("/usr/bin/ffmpeg").
					Maybe()
			})

			It("dispatches a scan, then refuses a second one", func() {
				// The scan runs detached, so the first call only returns once
				// the goroutine is inside ListMediaFilesWithTranscodeOwners.
				// Blocking there is what keeps it in flight long enough for
				// the second call to see ErrScanRunning.
				entered := make(chan struct{})
				release := make(chan struct{})
				DeferCleanup(func() { close(release) })
				app.store.EXPECT().
					ListMediaFilesWithTranscodeOwners(mock.Anything).
					RunAndReturn(func(context.Context) ([]*ent.MediaFile, error) {
						close(entered)
						<-release
						return nil, nil
					}).
					Once()

				first := app.do(app.req(
					http.MethodPost, "/api/v1/transcoding/scan", "", nil,
				))
				defer first.Body.Close()
				Expect(first.StatusCode).To(Equal(http.StatusAccepted))
				Eventually(entered).Should(BeClosed())

				second := app.do(app.req(
					http.MethodPost, "/api/v1/transcoding/scan", "", nil,
				))
				defer second.Body.Close()
				Expect(second.StatusCode).To(Equal(http.StatusConflict))
			})

			It("answers 409 while transcoding is disabled", func() {
				configtest.SetupFile()

				resp := app.do(app.req(
					http.MethodPost, "/api/v1/transcoding/scan", "", nil,
				))
				defer resp.Body.Close()
				Expect(resp.StatusCode).To(Equal(http.StatusConflict))
			})

			// Queueing rows a worker that cannot run will never claim leaves
			// them stuck with nothing saying why, so scan refuses instead.
			It("answers 409 when ffmpeg is disabled", func() {
				configtest.SetupFile(
					transcodingOn(),
					map[string]any{"ffmpeg": map[string]any{"enabled": false}},
				)

				resp := app.do(app.req(
					http.MethodPost, "/api/v1/transcoding/scan", "", nil,
				))
				defer resp.Body.Close()
				Expect(resp.StatusCode).To(Equal(http.StatusConflict))

				var body Error
				Expect(json.NewDecoder(resp.Body).Decode(&body)).To(Succeed())
				Expect(body.Message).To(Equal(
					"transcoding worker cannot run: ffmpeg disabled or not found",
				))
			})

			It("answers 409 when the ffmpeg binary is not found", func() {
				app.prober.EXPECT().FFmpegPath().Unset()
				app.prober.EXPECT().FFmpegPath().Return("").Once()

				resp := app.do(app.req(
					http.MethodPost, "/api/v1/transcoding/scan", "", nil,
				))
				defer resp.Body.Close()
				Expect(resp.StatusCode).To(Equal(http.StatusConflict))

				// Asserted so this cannot pass through the feature-off arm,
				// which the config here deliberately leaves satisfied.
				var body Error
				Expect(json.NewDecoder(resp.Body).Decode(&body)).To(Succeed())
				Expect(body.Message).To(Equal(
					"transcoding worker cannot run: ffmpeg disabled or not found",
				))
			})

			It("refuses a non-admin", func() {
				app.addMember("member@test.com")

				resp := app.do(app.req(
					http.MethodPost,
					"/api/v1/transcoding/scan",
					app.memberKey,
					nil,
				))
				defer resp.Body.Close()
				Expect(resp.StatusCode).To(Equal(http.StatusForbidden))
			})
		})

		Describe("GetConfigTranscoding", func() {
			It("returns the configured section", func() {
				resp := app.do(app.req(
					http.MethodGet, "/api/v1/config/transcoding", "", nil,
				))
				defer resp.Body.Close()
				Expect(resp.StatusCode).To(Equal(http.StatusOK))

				var view TranscodingConfigView
				Expect(json.NewDecoder(resp.Body).Decode(&view)).To(Succeed())
				Expect(view.Enabled).To(BeTrue())
				Expect(view.MaxConcurrent).To(Equal(1))
				Expect(view.MaxFailures).To(Equal(3))
			})

			It("refuses a non-admin", func() {
				app.addMember("member@test.com")

				resp := app.do(app.req(
					http.MethodGet,
					"/api/v1/config/transcoding",
					app.memberKey,
					nil,
				))
				defer resp.Body.Close()
				Expect(resp.StatusCode).To(Equal(http.StatusForbidden))
			})
		})

		Describe("UpdateConfigTranscoding", func() {
			It("applies a patch and echoes the new state", func() {
				resp := app.do(jsonKeyReq(app,
					http.MethodPatch,
					"/api/v1/config/transcoding",
					"",
					`{"max_concurrent": 4, "max_failures": 2}`,
				))
				defer resp.Body.Close()
				Expect(resp.StatusCode).To(Equal(http.StatusOK))

				var view TranscodingConfigView
				Expect(json.NewDecoder(resp.Body).Decode(&view)).To(Succeed())
				Expect(view.MaxConcurrent).To(Equal(4))
				Expect(view.MaxFailures).To(Equal(2))
				Expect(view.Enabled).To(BeTrue())
			})

			It("refuses max_concurrent that narrows to a valid uint8", func() {
				// uint8(300) is 44, which the config's own max=8 would accept.
				resp := app.do(jsonKeyReq(app,
					http.MethodPatch,
					"/api/v1/config/transcoding",
					"",
					`{"max_concurrent": 300}`,
				))
				defer resp.Body.Close()
				Expect(resp.StatusCode).
					To(Equal(http.StatusUnprocessableEntity))
			})

			It("refuses an out-of-range max_failures", func() {
				resp := app.do(jsonKeyReq(app,
					http.MethodPatch,
					"/api/v1/config/transcoding",
					"",
					`{"max_failures": 11}`,
				))
				defer resp.Body.Close()
				Expect(resp.StatusCode).
					To(Equal(http.StatusUnprocessableEntity))
			})

			It("refuses a non-admin", func() {
				app.addMember("member@test.com")

				resp := app.do(jsonKeyReq(app,
					http.MethodPatch,
					"/api/v1/config/transcoding",
					app.memberKey,
					`{"enabled": false}`,
				))
				defer resp.Body.Close()
				Expect(resp.StatusCode).To(Equal(http.StatusForbidden))
			})
		})

		Describe("transcode policy on a quality profile", func() {
			It("round-trips a transcode block through create", func() {
				body := `{"name": "hevc", "preferred_resolution": "1080p",` +
					` "min_resolution": "720p", "transcode": {` +
					`"if": {"video_codecs": ["hevc"], "containers": ["mkv"],` +
					` "max_video_bitrate": "8M"},` +
					` "to": {"container": "mkv", "video_codec": "hevc",` +
					` "crf": 22, "preset": "slow", "audio_codec": "aac",` +
					` "audio_passthrough": ["truehd"]}}}`

				resp := app.do(jsonKeyReq(app,
					http.MethodPost, "/api/v1/quality-profiles", "", body,
				))
				defer resp.Body.Close()
				Expect(resp.StatusCode).To(Equal(http.StatusCreated))

				var qp QualityProfile
				Expect(json.NewDecoder(resp.Body).Decode(&qp)).To(Succeed())
				Expect(qp.Transcode).NotTo(BeNil())
				Expect(string(qp.Transcode.To.Container)).To(Equal("mkv"))
				Expect(qp.Transcode.To.Crf).To(HaveValue(Equal(uint8(22))))
				Expect(qp.Transcode.If).NotTo(BeNil())
				Expect(*qp.Transcode.If.MaxVideoBitrate).To(Equal("8M"))
			})

			It("leaves a stored policy alone when update omits it", func() {
				create := `{"name": "hevc", "preferred_resolution": "1080p",` +
					` "min_resolution": "720p", "transcode": {"to": {` +
					`"container": "mkv", "video_codec": "hevc",` +
					` "preset": "slow", "audio_codec": "aac"}}}`
				created := app.do(jsonKeyReq(app,
					http.MethodPost, "/api/v1/quality-profiles", "", create,
				))
				defer created.Body.Close()
				Expect(created.StatusCode).To(Equal(http.StatusCreated))

				update := `{"name": "hevc", "preferred_resolution": "2160p"}`
				resp := app.do(jsonKeyReq(app,
					http.MethodPut,
					"/api/v1/quality-profiles/hevc",
					"",
					update,
				))
				defer resp.Body.Close()
				Expect(resp.StatusCode).To(Equal(http.StatusOK))

				var qp QualityProfile
				Expect(json.NewDecoder(resp.Body).Decode(&qp)).To(Succeed())
				Expect(string(qp.PreferredResolution)).To(Equal("2160p"))
				Expect(qp.Transcode).NotTo(BeNil())
				Expect(string(qp.Transcode.To.VideoCodec)).To(Equal("hevc"))
			})
		})

		Describe("transcode fields on a media file", func() {
			It("emits transcoded_at and size_before once set", func() {
				at := time.Date(2026, 9, 5, 8, 0, 0, 0, time.UTC)
				out := mediaFileToAPI(&ent.MediaFile{
					ID:           1,
					Path:         "/movies/x.mkv",
					Size:         100,
					TranscodedAt: &at,
					SizeBefore:   400,
				})
				Expect(out.TranscodedAt).To(HaveValue(Equal(at)))
				Expect(out.SizeBefore).To(HaveValue(Equal(int64(400))))
			})

			It("omits both for a file the worker never touched", func() {
				out := mediaFileToAPI(&ent.MediaFile{
					ID: 1, Path: "/movies/x.mkv", Size: 100,
				})
				Expect(out.TranscodedAt).To(BeNil())
				Expect(out.SizeBefore).To(BeNil())
			})

			It("carries them onto an episode", func() {
				at := time.Date(2026, 9, 5, 8, 0, 0, 0, time.UTC)
				ep := &ent.Episode{ID: 1, Number: 2}
				ep.Edges.MediaFiles = []*ent.MediaFile{{
					ID:           1,
					Path:         "/tv/x.mkv",
					Size:         100,
					TranscodedAt: &at,
					SizeBefore:   400,
				}}
				out := episodeToAPI(ep, time.Now(), "")
				Expect(out.TranscodedAt).To(HaveValue(Equal(at)))
				Expect(out.SizeBefore).To(HaveValue(Equal(int64(400))))
			})
		})

		Describe("ffmpeg version", func() {
			It("reports the prober's version on /config/ffmpeg", func() {
				app.prober.EXPECT().Available().Return(true).Once()
				app.prober.EXPECT().
					ResolvedPath().
					Return("/usr/bin/ffprobe").
					Once()
				app.prober.EXPECT().
					Version(mock.Anything).
					Return("7.1", nil).
					Once()

				resp := app.do(app.req(
					http.MethodGet, "/api/v1/config/ffmpeg", "", nil,
				))
				defer resp.Body.Close()
				Expect(resp.StatusCode).To(Equal(http.StatusOK))

				var view FFmpegConfigView
				Expect(json.NewDecoder(resp.Body).Decode(&view)).To(Succeed())
				Expect(view.Version).To(HaveValue(Equal("7.1")))
			})

			It("omits the version when ffmpeg cannot answer", func() {
				app.prober.EXPECT().Available().Return(false).Once()
				app.prober.EXPECT().
					Version(mock.Anything).
					Return("", fmt.Errorf("ffmpeg not found")).
					Once()

				resp := app.do(app.req(
					http.MethodGet, "/api/v1/config/ffmpeg", "", nil,
				))
				defer resp.Body.Close()
				Expect(resp.StatusCode).To(Equal(http.StatusOK))

				var view FFmpegConfigView
				Expect(json.NewDecoder(resp.Body).Decode(&view)).To(Succeed())
				Expect(view.Version).To(BeNil())
			})
		})
	},
)
