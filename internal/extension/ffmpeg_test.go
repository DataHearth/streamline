package extension

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gexec"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/db"
	"github.com/datahearth/streamline/internal/testutil/configtest"
)

// ffmpegSpans is installed at package init: otel binds a tracer to its
// delegate on the first SetTracerProvider call and never re-binds.
var ffmpegSpans = func() *tracetest.SpanRecorder {
	rec := tracetest.NewSpanRecorder()
	otel.SetTracerProvider(
		sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec)),
	)
	return rec
}()

type fakeTools struct{ ffmpeg, ffprobe string }

func (f *fakeTools) FFmpegPath() string   { return f.ffmpeg }
func (f *fakeTools) ResolvedPath() string { return f.ffprobe }

var _ = Describe("ffmpeg service", Ordered, Label("integration"), func() {
	var (
		ctx     = context.Background()
		binary  string
		cfg     *config.Config
		tools   *fakeTools
		mgr     *Manager
		h       http.Handler
		music   string
		cacheAt string
	)

	BeforeAll(func() {
		built, err := gexec.Build(
			"github.com/datahearth/streamline/internal/extension/testdata/fakeffmpeg",
		)
		Expect(err).NotTo(HaveOccurred())
		binary = built
		DeferCleanup(gexec.CleanupBuildArtifacts)
	})

	start := func(wasm string, services ...string) {
		GinkgoHelper()
		src, err := filepath.Abs(filepath.Join("testdata/echo", wasm))
		Expect(err).NotTo(HaveOccurred())
		cfg.Extensions = []config.ExtensionEntry{{
			Name: "echo", Source: src, Enabled: true,
			Grants: config.ExtensionGrants{
				Mounts:   []string{"/echo"},
				Views:    []string{"books"},
				Services: append([]string{"serve_file"}, services...),
				Folders:  []string{"music", "artwork", "cache"},
			},
		}}
		Expect(mgr.Reconcile(ctx)).To(Succeed())
		for _, s := range mgr.Statuses() {
			Expect(s.State).To(Equal(StateRunning), s.Reason)
		}
	}

	running := func() *Running {
		GinkgoHelper()
		run, ok := mgr.AcquireNamed("echo")
		Expect(ok).To(BeTrue())
		DeferCleanup(run.Release)
		return run
	}

	run := func(args ...string) FFmpegResult {
		GinkgoHelper()
		body, err := json.Marshal(FFmpegCall{Tool: "ffmpeg", Args: args})
		Expect(err).NotTo(HaveOccurred())
		resp, ok, err := mgr.Call(ctx, "echo", Request{
			Method: http.MethodPost, Path: "/ffmpeg/run", Body: body,
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(ok).To(BeTrue())
		Expect(resp.Status).To(Equal(200), string(resp.Body))
		var res FFmpegResult
		Expect(json.Unmarshal(resp.Body, &res)).To(Succeed())
		return res
	}

	streamRequest := func(
		reqCtx context.Context, fallback string, args ...string,
	) *http.Request {
		q := url.Values{"arg": args, "type": {"audio/mpeg"}}
		if fallback != "" {
			q.Set("fallback", fallback)
		}
		return httptest.NewRequest(
			http.MethodGet, "/echo/ffmpeg/stream?"+q.Encode(), nil,
		).WithContext(reqCtx)
	}

	stream := func(fallback string, args ...string) *httptest.ResponseRecorder {
		GinkgoHelper()
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, streamRequest(ctx, fallback, args...))
		return rr
	}

	// hold starts a stream that stays open until the returned cancel runs.
	hold := func() (context.CancelFunc, <-chan struct{}) {
		reqCtx, cancel := context.WithCancel(ctx)
		done := make(chan struct{})
		rr := httptest.NewRecorder()
		go func() {
			defer GinkgoRecover()
			defer close(done)
			h.ServeHTTP(rr, streamRequest(reqCtx, "", "sleep"))
		}()
		return cancel, done
	}

	BeforeEach(func() {
		root := GinkgoT().TempDir()
		music = filepath.Join(root, "music")
		Expect(os.MkdirAll(music, 0o750)).To(Succeed())
		Expect(os.WriteFile(
			filepath.Join(music, "a.txt"), []byte("fallback bytes"), 0o600,
		)).To(Succeed())

		bin := GinkgoT().TempDir()
		for _, name := range []string{"ffmpeg", "ffprobe"} {
			data, err := os.ReadFile(binary)
			Expect(err).NotTo(HaveOccurred())
			Expect(os.WriteFile(filepath.Join(bin, name), data, 0o700)).To(Succeed())
		}
		tools = &fakeTools{
			ffmpeg:  filepath.Join(bin, "ffmpeg"),
			ffprobe: filepath.Join(bin, "ffprobe"),
		}

		cfg = configtest.Setup(map[string]any{
			"library": map[string]any{"music_path": music},
		})
		cfg.FFmpeg.Enabled = true
		corePath := filepath.Join(cfg.DataDir, "core.db")
		client, err := db.Open(ctx, corePath)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { client.Close() })

		mgr = NewManager(cfg.DataDir, corePath, tools, nil)
		DeferCleanup(func() { Expect(mgr.Close(ctx)).To(Succeed()) })
		cacheAt = filepath.Join(cfg.DataDir, "extensions", "echo", "cache")

		h = mgr.Middleware(fakeFiles{client: client}, &fakePosters{})(
			http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusTeapot)
			}),
		)
	})

	Context("run to completion", func() {
		BeforeEach(func() { start("echo-full.wasm", "ffmpeg", "library_scan") })

		It("returns stdout, stderr and the exit code of a run", func() {
			res := run("echo", "hello")
			Expect(res.Error).To(BeEmpty())
			Expect(res.ExitCode).To(Equal(0))
			Expect(string(res.Stdout)).To(Equal("hello"))

			Expect(run("exit", "3").ExitCode).To(Equal(3))
		})

		It("passes arguments as given, with no shell", func() {
			res := run("echo", "$(id); a b")
			Expect(string(res.Stdout)).To(Equal("$(id); a b"))
		})

		It("starts in the cache folder without STREAMLINE variables", func() {
			GinkgoT().Setenv("STREAMLINE_FOO", "1")

			Expect(string(run("pwd").Stdout)).To(Equal(cacheAt))
			Expect(string(run("env").Stdout)).NotTo(ContainSubstring("STREAMLINE_"))
		})

		It("keeps the tail of a flood of stderr and does not deadlock", func() {
			res := run("flood")
			Expect(string(res.Stdout)).To(Equal("done"))
			Expect(res.Stderr).To(HaveLen(MaxFFmpegStderr))
		})

		It("kills a run at the request deadline", func() {
			r := running()
			dl, cancel := context.WithTimeout(ctx, 200*time.Millisecond)
			defer cancel()

			res := r.runFFmpeg(
				dl, FFmpegCall{Tool: "ffmpeg", Args: []string{"sleep"}},
			)
			Expect(res.Error).NotTo(BeEmpty())
			Expect(r.ffmpegSlots).To(BeEmpty())
		})

		It("refuses a tool that is neither ffmpeg nor ffprobe", func() {
			res := running().runFFmpeg(
				ctx, FFmpegCall{Tool: "sh", Args: []string{"-c", "id"}},
			)
			Expect(res.Error).NotTo(BeEmpty())
			Expect(res.Stdout).To(BeEmpty())
		})

		It("refuses while ffmpeg is disabled in the config", func() {
			cfg.FFmpeg.Enabled = false
			Expect(run("echo", "x").Error).NotTo(BeEmpty())
		})
	})

	It("labels a guest-chosen tool name with a fixed metric value", func() {
		Expect(metricTool("ffmpeg")).To(Equal("ffmpeg"))
		Expect(metricTool("ffprobe")).To(Equal("ffprobe"))
		Expect(metricTool("tool-12345")).To(Equal("unknown"))
	})

	It("marks the span of a refused or failed run as an error", func() {
		start("echo-full.wasm", "ffmpeg", "library_scan")
		before := len(ffmpegSpans.Ended())

		running().runFFmpeg(ctx, FFmpegCall{Tool: "sh"})
		run("exit", "3")
		run("echo", "ok")

		spans := ffmpegSpans.Ended()[before:]
		Expect(spans).To(HaveLen(3))
		Expect(spans[0].Status().Code).To(Equal(codes.Error))
		Expect(spans[1].Status().Code).To(Equal(codes.Error))
		Expect(spans[2].Status().Code).NotTo(Equal(codes.Error))
	})

	It("refuses a run without the ffmpeg grant", func() {
		start("echo.wasm")

		res := run("echo", "x")
		Expect(res.Error).NotTo(BeEmpty())
		Expect(res.Stdout).To(BeEmpty())
	})

	Context("streaming", func() {
		BeforeEach(func() { start("echo-full.wasm", "ffmpeg", "library_scan") })

		fallbackPath := func() string { return filepath.Join(music, "a.txt") }

		It("streams output to the client", func() {
			rr := stream("", "stream", "5")
			Expect(rr.Code).To(Equal(http.StatusOK))
			Expect(rr.Body.Len()).To(Equal(5 << 10))
			Expect(rr.Header().Get("Content-Type")).To(Equal("audio/mpeg"))
		})

		It("kills the process when the client disconnects", func() {
			r := running()
			cancel, done := hold()
			Eventually(r.ffmpegSlots).Should(HaveLen(1))

			cancel()
			Eventually(done, time.Second).Should(BeClosed())
			Eventually(r.ffmpegSlots, time.Second).Should(BeEmpty())
		})

		It("kills the process when the extension is disabled", func() {
			r := running()
			_, done := hold()
			Eventually(r.ffmpegSlots).Should(HaveLen(1))

			cfg.Extensions[0].Enabled = false
			Expect(mgr.Reconcile(ctx)).To(Succeed())
			Eventually(done, 2*time.Second).Should(BeClosed())
			Eventually(r.ffmpegSlots, time.Second).Should(BeEmpty())
		})

		It("serves the fallback when ffmpeg is unavailable", func() {
			tools.ffmpeg, tools.ffprobe = "", ""

			rr := stream(fallbackPath(), "stream", "5")
			Expect(rr.Code).To(Equal(http.StatusOK))
			Expect(rr.Body.String()).To(Equal("fallback bytes"))
		})

		Context("with every slot busy", func() {
			BeforeEach(func() {
				r := running()
				for range MaxFFmpegProcs {
					cancel, done := hold()
					DeferCleanup(func() {
						cancel()
						Eventually(done, 2*time.Second).Should(BeClosed())
					})
				}
				Eventually(r.ffmpegSlots).Should(HaveLen(MaxFFmpegProcs))
			})

			It("serves the fallback", func() {
				rr := stream(fallbackPath(), "stream", "5")
				Expect(rr.Code).To(Equal(http.StatusOK))
				Expect(rr.Body.String()).To(Equal("fallback bytes"))
			})

			It("answers 503 with Retry-After when there is no fallback", func() {
				rr := stream("", "stream", "5")
				Expect(rr.Code).To(Equal(http.StatusServiceUnavailable))
				Expect(rr.Header().Get("Retry-After")).To(Equal("5"))
			})
		})

		It("answers 502 when ffmpeg exits non-zero before any output", func() {
			rr := stream("", "exit", "3")
			Expect(rr.Code).To(Equal(http.StatusBadGateway))
		})

		It("serves the fallback when ffmpeg fails before any output", func() {
			rr := stream(fallbackPath(), "exit", "3")
			Expect(rr.Code).To(Equal(http.StatusOK))
			Expect(strings.TrimSpace(rr.Body.String())).To(Equal("fallback bytes"))
		})
	})

	It("keeps the extension running when ffmpeg is missing", func() {
		tools.ffmpeg, tools.ffprobe = "", ""
		start("echo-full.wasm", "ffmpeg", "library_scan")

		Expect(mgr.Statuses()[0].State).To(Equal(StateRunning))
	})
})
