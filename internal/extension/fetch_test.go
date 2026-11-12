package extension

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/db"
	"github.com/datahearth/streamline/internal/testutil/configtest"
)

var _ = Describe("http service", Label("integration"), func() {
	var (
		ctx    = context.Background()
		cfg    *config.Config
		mgr    *Manager
		remote *httptest.Server
		hits   int
	)

	start := func(hosts []string, services ...string) {
		GinkgoHelper()
		src, err := filepath.Abs("testdata/echo/echo.wasm")
		Expect(err).NotTo(HaveOccurred())
		cfg.Extensions = []config.ExtensionEntry{{
			Name: "echo", Source: src, Enabled: true, Hosts: hosts,
			Grants: config.ExtensionGrants{
				Mounts:   []string{"/echo"},
				Views:    []string{"books"},
				Services: services,
			},
		}}
		Expect(mgr.Reconcile(ctx)).To(Succeed())
		for _, s := range mgr.Statuses() {
			Expect(s.State).To(Equal(StateRunning), s.Reason)
		}
	}

	fetch := func(target string) Response {
		GinkgoHelper()
		resp, ok, err := mgr.Call(ctx, "echo", Request{
			Method: http.MethodPost, Path: "/fetch",
			Query: "url=" + url.QueryEscape(target), Body: []byte("ping"),
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(ok).To(BeTrue())
		return resp
	}

	host := func() string { return strings.TrimPrefix(remote.URL, "http://") }

	BeforeEach(func() {
		cfg = configtest.Setup()
		corePath := filepath.Join(cfg.DataDir, "core.db")
		client, err := db.Open(ctx, corePath)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { client.Close() })
		mgr = NewManager(cfg.DataDir, corePath, nil, nil)
		DeferCleanup(func() { Expect(mgr.Close(ctx)).To(Succeed()) })

		hits = 0
		remote = httptest.NewServer(http.HandlerFunc(
			func(w http.ResponseWriter, r *http.Request) {
				hits++
				if r.URL.Path == "/away" {
					http.Redirect(w, r, "http://example.invalid/", http.StatusFound)
					return
				}
				body, _ := io.ReadAll(r.Body)
				w.WriteHeader(http.StatusCreated)
				_, _ = w.Write([]byte(r.Header.Get("X-Echo") + string(body)))
			},
		))
		DeferCleanup(remote.Close)
	})

	It("calls a listed host for the guest", func() {
		start([]string{host()}, "serve_file", "http")

		resp := fetch(remote.URL + "/v1/sessions")

		Expect(resp.Status).To(Equal(http.StatusCreated), string(resp.Body))
		Expect(string(resp.Body)).To(Equal("1ping"))
	})

	It("hands a redirect back instead of following it", func() {
		start([]string{host()}, "serve_file", "http")

		resp := fetch(remote.URL + "/away")

		Expect(resp.Status).To(Equal(http.StatusFound))
		Expect(hits).To(Equal(1))
	})

	It(
		"refuses a host that is not listed, another port of a listed one included",
		func() {
			start([]string{"127.0.0.1:1"}, "serve_file", "http")

			resp := fetch(remote.URL)

			Expect(resp.Status).To(Equal(502))
			Expect(string(resp.Body)).To(ContainSubstring("hosts list"))
			Expect(hits).To(BeZero())
		},
	)

	It("refuses every call without the grant", func() {
		start([]string{host()}, "serve_file")

		resp := fetch(remote.URL)

		Expect(resp.Status).To(Equal(502))
		Expect(string(resp.Body)).To(ContainSubstring("not granted"))
		Expect(hits).To(BeZero())
	})
})
