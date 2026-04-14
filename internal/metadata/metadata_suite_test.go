package metadata

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/internal/testutil"
	"github.com/datahearth/streamline/internal/testutil/configtest"
)

func TestMetadata(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Metadata Suite")
}

var _ = BeforeSuite(func() {
	DeferCleanup(testutil.InstallSlog())
})

// Seed the metadata.tmdb_api_key into the config singleton for every spec;
// the TMDB client pulls its key from config.Get() at construction.
var _ = BeforeEach(func() {
	configtest.Setup(map[string]any{
		"metadata": map[string]any{
			"tmdb_api_key": "test-key",
			"language":     "en",
		},
	})
})
