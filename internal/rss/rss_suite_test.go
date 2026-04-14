package rss

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/internal/db"
	"github.com/datahearth/streamline/internal/testutil"
	"github.com/datahearth/streamline/internal/testutil/configtest"
)

func TestRSS(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "RSS Suite")
}

var _ = BeforeSuite(func() {
	DeferCleanup(testutil.InstallSlog())
})

// Seed the config singleton with default quality settings for every spec.
// Specs that need a different MinResolution / cooldown / max_grab_failures
// call configtest.Setup again inside their own BeforeEach with overrides.
var _ = BeforeEach(func() {
	configtest.Setup(defaultRSSConfig())
})

// defaultRSSConfig returns the minimum library.default_quality overlay the
// rss.New constructor needs. Helpers merge their own values on top.
func defaultRSSConfig() map[string]any {
	return map[string]any{
		"library": map[string]any{
			"default_quality": map[string]any{
				"preferred_resolution": "1080p",
				"min_resolution":       "720p",
				"upgrade_allowed":      true,
				"no_match_cooldown":    "6h",
				"max_grab_failures":    3,
			},
		},
	}
}

// newTestSearcher builds a MissingSearcher under test, failing the spec on
// construction errors. Used in place of direct rss.NewMissingSearcher calls
// so tests don't need to handle construction errors inline.
func newTestSearcher(
	client *ent.Client,
	indexers IndexerSearcher,
	downloads Downloader,
) *MissingSearcher {
	GinkgoHelper()
	s, err := NewMissingSearcher(db.New(client), indexers, downloads)
	Expect(err).NotTo(HaveOccurred())
	return s
}
