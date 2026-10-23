package observability

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"

	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/testutil/configtest"
	"github.com/datahearth/streamline/internal/utils/httputil"
	"github.com/go-chi/chi/v5"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("HTTPLogger.Middleware", Label("unit", "observability"), func() {
	defaultRotate := config.LogRotate{
		MaxSizeMB:  100,
		MaxBackups: 5,
		MaxAgeDays: 30,
		Compress:   true,
	}

	newLoggerWithBuf := func(format string) (*HTTPLogger, *bytes.Buffer) {
		GinkgoHelper()
		buf := &bytes.Buffer{}
		prev := StderrSink
		StderrSink = buf
		DeferCleanup(func() { StderrSink = prev })

		l, err := NewHTTPLogger(
			config.HTTPLog{
				Enabled: true,
				Output:  "stderr",
				Format:  format,
				Rotate:  defaultRotate,
			},
		)
		Expect(err).NotTo(HaveOccurred())
		return l, buf
	}

	It("emits one JSON line per non-skipped request", func() {
		l, buf := newLoggerWithBuf("json")

		r := chi.NewRouter()
		r.Use(l.Middleware(nil))
		r.Get("/api/v1/movies", func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
			if _, err := w.Write([]byte("ok")); err != nil {
				Fail(err.Error())
			}
		})

		srv := httptest.NewServer(r)
		DeferCleanup(srv.Close)

		resp, err := http.Get(srv.URL + "/api/v1/movies")
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.Body.Close()).To(Succeed())

		var entry map[string]any
		Expect(json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &entry)).To(Succeed())
		Expect(entry["method"]).To(Equal("GET"))
		Expect(entry["path"]).To(Equal("/api/v1/movies"))
		Expect(entry["status"]).To(Equal(float64(200)))
		Expect(entry["bytes"]).To(BeNumerically(">", 0))
		Expect(entry["duration_ms"]).To(BeNumerically(">=", 0))
		Expect(entry).To(HaveKey("user_agent"))
		Expect(entry).To(HaveKey("referer"))
	})

	It("emits nothing when skip returns true", func() {
		l, buf := newLoggerWithBuf("json")

		r := chi.NewRouter()
		r.Use(
			l.Middleware(
				func(req *http.Request) bool { return req.URL.Path == "/health" },
			),
		)
		r.Get(
			"/health",
			func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) },
		)

		srv := httptest.NewServer(r)
		DeferCleanup(srv.Close)

		resp, err := http.Get(srv.URL + "/health")
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.Body.Close()).To(Succeed())

		Expect(buf.Len()).To(Equal(0))
	})

	It("returns a passthrough when HTTPLogger is nil", func() {
		var nilLogger *HTTPLogger
		mw := nilLogger.Middleware(nil)
		Expect(mw).NotTo(BeNil())

		called := false
		next := http.HandlerFunc(
			func(http.ResponseWriter, *http.Request) { called = true },
		)
		req := httptest.NewRequest("GET", "/", nil)
		mw(next).ServeHTTP(httptest.NewRecorder(), req)
		Expect(called).To(BeTrue())
	})

	It("emits combined-format when configured", func() {
		l, buf := newLoggerWithBuf("combined")

		r := chi.NewRouter()
		r.Use(l.Middleware(nil))
		r.Get(
			"/x",
			func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) },
		)

		srv := httptest.NewServer(r)
		DeferCleanup(srv.Close)

		resp, err := http.Get(srv.URL + "/x")
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.Body.Close()).To(Succeed())

		Expect(buf.String()).To(MatchRegexp(`"GET /x HTTP/1.1" 200 `))
	})

	logRemoteIP := func(remoteAddr, xff string) string {
		GinkgoHelper()
		l, buf := newLoggerWithBuf("json")

		r := chi.NewRouter()
		r.Use(httputil.ClientIPResolver())
		r.Use(l.Middleware(nil))
		r.Get(
			"/x",
			func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) },
		)

		req := httptest.NewRequest("GET", "/x", nil)
		req.RemoteAddr = remoteAddr
		req.Header.Set("X-Forwarded-For", xff)
		r.ServeHTTP(httptest.NewRecorder(), req)

		var entry map[string]any
		Expect(json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &entry)).To(Succeed())
		return entry["remote_ip"].(string)
	}

	It("populates remote_ip from X-Forwarded-For behind a trusted proxy", func() {
		configtest.Setup(map[string]any{
			"server": map[string]any{
				"trusted_proxies": []string{"10.1.0.0/16"},
			},
		})
		Expect(logRemoteIP("10.1.0.5:9000", "9.10.11.12")).
			To(Equal("9.10.11.12"))
	})

	It("logs the peer address when the header is not trusted", func() {
		configtest.Setup()
		Expect(logRemoteIP("9.10.11.12:1234", "5.6.7.8")).
			To(Equal("9.10.11.12"))
	})
})
