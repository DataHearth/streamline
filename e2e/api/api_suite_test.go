package api

import (
	"os"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/e2e/apptest"
	"github.com/datahearth/streamline/e2e/fakes"
	"github.com/datahearth/streamline/internal/testutil"
)

var (
	baseURL string

	// downloadDir is bind-mounted into the qBittorrent container under its own
	// host path, so it is minted under /tmp/streamline rather than with
	// GinkgoT().TempDir(): /tmp stays mountable on Docker Desktop/colima-style
	// setups where only a short list of host roots is shared, and the path
	// must outlive any single spec.
	downloadDir string
)

func TestAPI(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "E2E API Suite", Label("api"))
}

var _ = BeforeSuite(func() {
	DeferCleanup(testutil.InstallSlog())

	const scratch = "/tmp/streamline"
	Expect(os.MkdirAll(scratch, 0o755)).To(Succeed())
	var err error
	downloadDir, err = os.MkdirTemp(scratch, "e2e-dl-")
	Expect(err).NotTo(HaveOccurred())
	DeferCleanup(os.RemoveAll, downloadDir)

	tmdb := fakes.NewTMDB()
	baseURL = apptest.Start(map[string]any{
		"metadata": map[string]any{
			"tmdb": map[string]any{"base_url": tmdb.URL},
		},
	})
	bootstrapIdentities()
})
