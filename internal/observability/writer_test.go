package observability

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"

	"github.com/go-chi/chi/v5"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/testutil/configtest"
)

var _ = Describe("StderrSink", Label("unit", "observability"), func() {
	It("routes stderr-configured HTTP access logs to StderrSink", func() {
		var buf bytes.Buffer
		prev := StderrSink
		StderrSink = &buf
		DeferCleanup(func() { StderrSink = prev })

		l, err := NewHTTPLogger(config.HTTPLog{
			Enabled: true,
			Output:  "stderr",
			Format:  "json",
		})
		Expect(err).NotTo(HaveOccurred())

		r := chi.NewRouter()
		r.Use(l.Middleware(nil))
		r.Get("/ping", func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		})
		srv := httptest.NewServer(r)
		DeferCleanup(srv.Close)

		resp, err := http.Get(srv.URL + "/ping")
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.Body.Close()).To(Succeed())

		var entry map[string]any
		Expect(
			json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &entry),
		).To(Succeed())
		Expect(entry["path"]).To(Equal("/ping"))
	})

	It("resolves a relative file output under config data_dir", func() {
		cfg := configtest.Setup()
		logPath := filepath.Join(cfg.DataDir, "logs", "http.log")

		l, err := NewHTTPLogger(config.HTTPLog{
			Enabled: true,
			Output:  "logs/http.log",
			Format:  "json",
		})
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { Expect(l.Close()).To(Succeed()) })

		r := chi.NewRouter()
		r.Use(l.Middleware(nil))
		r.Get("/ping", func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		})
		srv := httptest.NewServer(r)
		DeferCleanup(srv.Close)

		resp, err := http.Get(srv.URL + "/ping")
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.Body.Close()).To(Succeed())

		data, err := os.ReadFile(logPath)
		Expect(err).NotTo(HaveOccurred())
		var entry map[string]any
		Expect(
			json.Unmarshal(bytes.TrimSpace(data), &entry),
		).To(Succeed())
		Expect(entry["path"]).To(Equal("/ping"))
	})
})
