package config

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func minimalYAML(dataDir string) string {
	return `
data_dir: ` + dataDir + `
auth:
  mode: disabled
  trusted_role: admin
library:
  movie_path: /x
  movie_naming: m
  import_mode: hardlink
  default_quality:
    preferred_resolution: 1080p
    min_resolution: 720p
    no_match_cooldown: 6h
    max_grab_failures: 3
schedules:
  rss_sync: 15m
  metadata_refresh: 24h
  download_monitor: 30s
  missing_search: 12h
  cleanup: 24h
log:
  level: info
  format: text
`
}

var _ = Describe("Singleton", Label("unit", "config"), func() {
	BeforeEach(func() {
		ResetForTest()
	})

	It("returns nil before Load", func() {
		Expect(Get()).To(BeNil())
	})

	It("returns loaded config after LoadReader", func() {
		dataDir := GinkgoT().TempDir()

		err := LoadReader(strings.NewReader(minimalYAML(dataDir)))
		Expect(err).ToNot(HaveOccurred())
		Expect(Get()).ToNot(BeNil())
		Expect(Get().Auth.Mode).To(Equal("disabled"))
	})

	It("returns loaded config after Load(path)", func() {
		dir := GinkgoT().TempDir()
		cfgPath := filepath.Join(dir, "cfg.yaml")
		dataDir := filepath.Join(dir, "data")
		Expect(os.MkdirAll(dataDir, 0o755)).To(Succeed())
		Expect(
			os.WriteFile(cfgPath, []byte(minimalYAML(dataDir)), 0o600),
		).To(Succeed())

		_, err := Load(cfgPath)
		Expect(err).ToNot(HaveOccurred())
		Expect(Get()).ToNot(BeNil())
		Expect(Get().Auth.Mode).To(Equal("disabled"))
	})

	It("Update mutates, validates, and swaps the singleton", func() {
		dir := GinkgoT().TempDir()
		cfgPath := filepath.Join(dir, "cfg.yaml")
		dataDir := filepath.Join(dir, "data")
		Expect(os.MkdirAll(dataDir, 0o755)).To(Succeed())
		Expect(
			os.WriteFile(cfgPath, []byte(minimalYAML(dataDir)), 0o600),
		).To(Succeed())
		_, err := Load(cfgPath)
		Expect(err).ToNot(HaveOccurred())

		Expect(Update(context.Background(), func(c *Config) error {
			c.Auth.Mode = "trusted-network"
			return nil
		})).To(Succeed())
		Expect(Get().Auth.Mode).To(Equal("trusted-network"))
	})

	It("Update rejects invalid mutations and leaves singleton unchanged", func() {
		dir := GinkgoT().TempDir()
		cfgPath := filepath.Join(dir, "cfg.yaml")
		dataDir := filepath.Join(dir, "data")
		Expect(os.MkdirAll(dataDir, 0o755)).To(Succeed())
		Expect(
			os.WriteFile(cfgPath, []byte(minimalYAML(dataDir)), 0o600),
		).To(Succeed())
		_, err := Load(cfgPath)
		Expect(err).ToNot(HaveOccurred())

		before := Get().Auth.Mode
		err = Update(context.Background(), func(c *Config) error {
			c.Auth.Mode = "not-a-mode"
			return nil
		})
		Expect(err).To(HaveOccurred())
		Expect(Get().Auth.Mode).To(Equal(before))
	})

	It("Update without a backing path returns ErrNoPath", func() {
		dataDir := GinkgoT().TempDir()
		err := LoadReader(strings.NewReader(minimalYAML(dataDir)))
		Expect(err).ToNot(HaveOccurred())

		err = Update(context.Background(), func(c *Config) error {
			c.Auth.Mode = "disabled"
			return nil
		})
		Expect(err).To(MatchError(ErrNoPath))
	})

	It("Update returns ErrReadOnly when config is read_only", func() {
		dataDir := GinkgoT().TempDir()
		err := LoadReader(
			strings.NewReader("read_only: true\n" + minimalYAML(dataDir)),
		)
		Expect(err).ToNot(HaveOccurred())
		Expect(Get().ReadOnly).To(BeTrue())

		err = Update(context.Background(), func(c *Config) error {
			c.Auth.Mode = "trusted-network"
			return nil
		})
		Expect(err).To(MatchError(ErrReadOnly))
	})

	It("Update returns fn error and leaves singleton unchanged", func() {
		dir := GinkgoT().TempDir()
		cfgPath := filepath.Join(dir, "cfg.yaml")
		dataDir := filepath.Join(dir, "data")
		Expect(os.MkdirAll(dataDir, 0o755)).To(Succeed())
		Expect(
			os.WriteFile(cfgPath, []byte(minimalYAML(dataDir)), 0o600),
		).To(Succeed())
		_, err := Load(cfgPath)
		Expect(err).ToNot(HaveOccurred())

		sentinel := errors.New("boom")
		err = Update(context.Background(), func(c *Config) error { return sentinel })
		Expect(err).To(MatchError(sentinel))
	})

	It("Update overwrites in place when the backing dir is read-only", func() {
		// Mirrors a Docker single-file bind mount: the config file is writable
		// but its parent dir is not, so the tmp-file + rename path can't land.
		// Update must fall back to overwriting the file in place.
		dir := GinkgoT().TempDir()
		cfgPath := filepath.Join(dir, "cfg.yaml")
		dataDir := filepath.Join(dir, "data")
		Expect(os.MkdirAll(dataDir, 0o755)).To(Succeed())
		Expect(
			os.WriteFile(cfgPath, []byte(minimalYAML(dataDir)), 0o600),
		).To(Succeed())
		_, err := Load(cfgPath)
		Expect(err).ToNot(HaveOccurred())

		Expect(os.Chmod(dir, 0o500)).To(Succeed())
		DeferCleanup(func() { _ = os.Chmod(dir, 0o700) })

		Expect(Update(context.Background(), func(c *Config) error {
			c.Auth.Mode = "trusted-network"
			return nil
		})).To(Succeed())

		// No stray tmp file leaked into the read-only config dir.
		entries, err := os.ReadDir(dir)
		Expect(err).ToNot(HaveOccurred())
		for _, e := range entries {
			Expect(e.Name()).ToNot(HaveSuffix(".tmp"))
		}

		// Survives a simulated restart — the change reached disk in place.
		ResetForTest()
		reloaded, err := Load(cfgPath)
		Expect(err).ToNot(HaveOccurred())
		Expect(reloaded.Auth.Mode).To(Equal("trusted-network"))
	})

	It("Update with file-backed config persists across re-Load", func() {
		dir := GinkgoT().TempDir()
		cfgPath := filepath.Join(dir, "cfg.yaml")
		dataDir := filepath.Join(dir, "data")
		Expect(os.MkdirAll(dataDir, 0o755)).To(Succeed())
		Expect(
			os.WriteFile(cfgPath, []byte(minimalYAML(dataDir)), 0o600),
		).To(Succeed())

		_, err := Load(cfgPath)
		Expect(err).ToNot(HaveOccurred())

		Expect(Update(context.Background(), func(c *Config) error {
			c.Auth.Mode = "trusted-network"
			return nil
		})).To(Succeed())

		// Simulate a process restart — clear singleton, re-Load from disk.
		ResetForTest()
		reloaded, err := Load(cfgPath)
		Expect(err).ToNot(HaveOccurred())
		Expect(reloaded.Auth.Mode).To(Equal("trusted-network"))
	})
})
