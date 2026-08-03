package api

import (
	"os"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/e2e/apptest"
	"github.com/datahearth/streamline/e2e/fakes"
	"github.com/datahearth/streamline/internal/server"
	"github.com/datahearth/streamline/internal/testutil"
)

// scratch roots every suite-lifetime directory the container specs need.
const scratch = "/tmp/streamline"

var (
	baseURL string

	// app is the wired application behind baseURL. Specs that need what
	// cmd/main.go starts around the router — the scheduler loop — reach it
	// through here.
	app *server.App

	// downloadDir is bind-mounted into the qBittorrent container under its own
	// host path, so it is minted under /tmp/streamline rather than with
	// GinkgoT().TempDir(): /tmp stays mountable on Docker Desktop/colima-style
	// setups where only a short list of host roots is shared, and the path
	// must outlive any single spec.
	downloadDir string

	// moviesDir is the movie library root. It shares a filesystem with
	// downloadDir so the importer's default hardlink mode can place a
	// completed download without falling back to a copy.
	moviesDir string
)

func TestAPI(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "E2E API Suite", Label("api"))
}

var _ = BeforeSuite(func() {
	DeferCleanup(testutil.InstallSlog())

	Expect(os.MkdirAll(scratch, 0o755)).To(Succeed())
	downloadDir = scratchDir("e2e-dl-")
	moviesDir = scratchDir("e2e-movies-")

	tmdb := fakes.NewTMDB()
	app, baseURL = apptest.Start(map[string]any{
		"metadata": map[string]any{
			"tmdb": map[string]any{"base_url": tmdb.URL},
		},
		"library": map[string]any{
			"download_path": downloadDir,
			"movie_path":    moviesDir,
		},
	})
	bootstrapIdentities()
})

// scratchDir mints a suite-lifetime directory under scratch and schedules its
// removal.
func scratchDir(prefix string) string {
	GinkgoHelper()
	dir, err := os.MkdirTemp(scratch, prefix)
	Expect(err).NotTo(HaveOccurred())
	DeferCleanup(os.RemoveAll, dir)
	return dir
}
