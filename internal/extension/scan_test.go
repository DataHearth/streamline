package extension

import (
	"context"
	"errors"
	"net/http"
	"path/filepath"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/db"
	"github.com/datahearth/streamline/internal/testutil/configtest"
)

type fakeScanner struct {
	status   ScanStatus
	started  int
	startErr error
}

func (f *fakeScanner) ScanStatus(context.Context) (ScanStatus, error) {
	return f.status, nil
}

func (f *fakeScanner) StartScan(context.Context) error {
	f.started++
	return f.startErr
}

var _ = Describe("library scan service", Label("integration"), func() {
	var (
		ctx     = context.Background()
		cfg     *config.Config
		scanner *fakeScanner
		mgr     *Manager
	)

	start := func(wasm string, services ...string) {
		GinkgoHelper()
		src, err := filepath.Abs(filepath.Join("testdata/echo", wasm))
		Expect(err).NotTo(HaveOccurred())
		cfg.Extensions = []config.ExtensionEntry{{
			Name: "echo", Source: src, Enabled: true,
			Grants: config.ExtensionGrants{
				Mounts:   []string{"/echo"},
				Views:    []string{"books"},
				Services: services,
				Folders:  folderGrantsFor(wasm),
			},
		}}
		Expect(mgr.Reconcile(ctx)).To(Succeed())
		for _, s := range mgr.Statuses() {
			Expect(s.State).To(Equal(StateRunning), s.Reason)
		}
	}

	call := func(method, path string) Response {
		GinkgoHelper()
		resp, ok, err := mgr.Call(ctx, "echo", Request{Method: method, Path: path})
		Expect(err).NotTo(HaveOccurred())
		Expect(ok).To(BeTrue())
		return resp
	}

	BeforeEach(func() {
		cfg = configtest.Setup()
		scanner = &fakeScanner{}
		corePath := filepath.Join(cfg.DataDir, "core.db")
		client, err := db.Open(ctx, corePath)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { client.Close() })
		mgr = NewManager(cfg.DataDir, corePath, nil, scanner)
		DeferCleanup(func() { Expect(mgr.Close(ctx)).To(Succeed()) })
	})

	Context("with the grant", func() {
		BeforeEach(func() {
			start("echo-full.wasm", "serve_file", "ffmpeg", "library_scan")
		})

		It("reports the scan status to the guest", func() {
			last := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
			scanner.status = ScanStatus{Scanning: true, Count: 42, LastScan: &last}

			resp := call(http.MethodGet, "/scan/status")

			Expect(resp.Status).To(Equal(200), string(resp.Body))
			Expect(resp.Body).To(MatchJSON(
				`{"scanning":true,"count":42,"last_scan":"2026-11-01T12:00:00Z"}`,
			))
		})

		It("starts a scan for the guest", func() {
			Expect(call(http.MethodPost, "/scan/start").Status).To(Equal(204))
			Expect(scanner.started).To(Equal(1))
		})

		It("hands a failed start back to the guest", func() {
			scanner.startErr = errors.New("scheduler is not running")

			resp := call(http.MethodPost, "/scan/start")

			Expect(resp.Status).To(Equal(502))
			Expect(
				string(resp.Body),
			).To(ContainSubstring("scheduler is not running"))
		})
	})

	Context("without the grant", func() {
		BeforeEach(func() { start("echo.wasm", "serve_file") })

		It("refuses scan_status", func() {
			resp := call(http.MethodGet, "/scan/status")
			Expect(resp.Status).To(Equal(502))
			Expect(string(resp.Body)).To(ContainSubstring("not granted"))
		})

		It("refuses scan_start and starts nothing", func() {
			resp := call(http.MethodPost, "/scan/start")
			Expect(resp.Status).To(Equal(502))
			Expect(scanner.started).To(BeZero())
		})
	})
})

func folderGrantsFor(wasm string) []string {
	if wasm == "echo-full.wasm" {
		return []string{"music", "artwork", "cache"}
	}
	return nil
}
