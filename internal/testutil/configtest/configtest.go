// Package configtest provides config-singleton setup helpers for tests. Kept
// separate from internal/testutil to avoid an import cycle: testutil is
// imported by config's own internal test suite, but this helper depends on
// internal/config.
package configtest

import (
	"bytes"
	"os"
	"path/filepath"

	"github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/providers/confmap"
	"github.com/knadh/koanf/v2"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/internal/config"
)

// Setup seeds the process-wide config singleton with defaults plus any
// caller-supplied overrides, registers a DeferCleanup to reset the singleton,
// and returns the resulting *config.Config.
//
// The data_dir key is automatically pointed at a Ginkgo-managed temp
// directory because config validation requires it to exist on disk.
// Overrides merge on top of defaults + the tmp data_dir — they use the same
// nested map shape as the YAML config file.
//
// Intended for use in BeforeEach or BeforeSuite:
//
//	BeforeEach(func() { configtest.Setup(map[string]any{
//	    "auth": map[string]any{
//	        "session_secret": "test-secret",
//	        "session_ttl":    "1h",
//	    },
//	}) })
func Setup(overrides ...map[string]any) *config.Config {
	GinkgoHelper()
	raw := marshalOverrides(GinkgoT().TempDir(), overrides)
	Expect(config.LoadReader(bytes.NewReader(raw))).To(Succeed())
	DeferCleanup(config.ResetForTest)
	return config.Get()
}

// SetupFile mirrors Setup but writes a YAML file to a temp dir and calls
// config.Load(path), so config.Update has a backing file to write back to.
// Use only when the test exercises mutation paths (Update / AddOIDCProvider);
// prefer Setup otherwise.
func SetupFile(overrides ...map[string]any) *config.Config {
	GinkgoHelper()
	dir := GinkgoT().TempDir()
	raw := marshalOverrides(dir, overrides)
	path := filepath.Join(dir, "config.yaml")
	Expect(os.WriteFile(path, raw, 0o644)).To(Succeed())
	_, err := config.Load(path)
	Expect(err).NotTo(HaveOccurred())
	DeferCleanup(config.ResetForTest)
	return config.Get()
}

func marshalOverrides(tmpDir string, overrides []map[string]any) []byte {
	GinkgoHelper()
	downloadDir := filepath.Join(tmpDir, "downloads")
	Expect(os.MkdirAll(downloadDir, 0o755)).To(Succeed())
	base := map[string]any{
		"data_dir": tmpDir,
		"library": map[string]any{
			"download_path": downloadDir,
		},
	}
	k := koanf.New(".")
	Expect(k.Load(confmap.Provider(base, "."), nil)).To(Succeed())
	for _, o := range overrides {
		Expect(k.Load(confmap.Provider(o, "."), nil)).To(Succeed())
	}
	raw, err := k.Marshal(yaml.Parser())
	Expect(err).NotTo(HaveOccurred())
	return raw
}
