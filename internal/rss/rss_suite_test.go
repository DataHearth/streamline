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

// Seed the config singleton with the quality/library knobs every spec runs
// against. Specs that need different values call configtest.Setup again
// inside their own BeforeEach with overrides.
var _ = BeforeEach(func() {
	configtest.Setup(defaultRSSConfig())
})

const uhdProfile = "uhd"

// defaultRSSConfig returns the quality + library overlay the rss specs run
// against: a 720p-floor default profile plus a 2160p-floor "uhd" profile so
// per-item profile resolution is exercised against a real second profile.
// An enabled download client is part of the baseline — the missing-search
// passes skip themselves without one, so its absence would make every grab
// spec silently test nothing.
func defaultRSSConfig() map[string]any {
	return map[string]any{
		"library": map[string]any{
			"no_match_cooldown": "6h",
			"max_grab_failures": 3,
		},
		"download_clients": []map[string]any{
			{
				"name":        "qbit",
				"client_type": "qbittorrent",
				"host":        "127.0.0.1",
				"port":        8080,
				"auth_method": "password",
				"enabled":     true,
			},
		},
		"quality_default_profile": "default",
		"quality_profiles": []map[string]any{
			{
				"name":                 "default",
				"preferred_resolution": "1080p",
				"min_resolution":       "720p",
				"upgrade_allowed":      true,
			},
			{
				"name":                 uhdProfile,
				"preferred_resolution": "2160p",
				"min_resolution":       "2160p",
				"upgrade_allowed":      true,
			},
		},
	}
}

func newTestSearcher(
	client *ent.Client,
	indexers IndexerSearcher,
	downloads Downloader,
) *MissingSearcher {
	return NewMissingSearcher(db.New(client), indexers, downloads)
}
