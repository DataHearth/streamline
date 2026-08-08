package config

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/providers/confmap"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/v2"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/types"
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
  movie_rss_sync: 15m
  movie_metadata_refresh: 24h
  download_monitor: 30s
  movie_missing_search: 12h
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

// writeConfigFile assembles a YAML config from overrides on top of a temp
// data_dir — the same confmap-over-koanf shape configtest uses, which this
// package's own tests cannot import without an import cycle — writes it to a
// temp dir and returns its path.
func writeConfigFile(overrides ...map[string]any) string {
	GinkgoHelper()
	dir := GinkgoT().TempDir()
	return writeConfigFileIn(
		dir,
		append([]map[string]any{{"data_dir": dir}}, overrides...)...,
	)
}

// writeConfigFileIn writes a config file into dir built from overrides alone,
// so a spec can leave a key out of the file — data_dir included — and hand it
// to the environment instead.
func writeConfigFileIn(dir string, overrides ...map[string]any) string {
	GinkgoHelper()
	k := koanf.New(".")
	for _, o := range overrides {
		Expect(k.Load(confmap.Provider(o, "."), nil)).To(Succeed())
	}
	raw, err := k.Marshal(yaml.Parser())
	Expect(err).ToNot(HaveOccurred())
	path := filepath.Join(dir, "config.yaml")
	Expect(os.WriteFile(path, raw, 0o600)).To(Succeed())
	return path
}

// onDisk parses the config file at path, with no defaults and no environment
// layer, so specs can assert on exactly what was written.
func onDisk(path string) *koanf.Koanf {
	GinkgoHelper()
	k := koanf.New(".")
	Expect(k.Load(file.Provider(path), yaml.Parser())).To(Succeed())
	return k
}

var _ = Describe(
	"Update and the environment layer",
	Label("unit", "config"),
	func() {
		const envTMDBKey = "STREAMLINE_METADATA__TMDB_API_KEY"

		BeforeEach(func() {
			ResetForTest()
			DeferCleanup(ResetForTest)
		})

		It("keeps a secret injected only through the environment off disk", func() {
			GinkgoT().Setenv(envTMDBKey, "env-only-tmdb-key")
			path := writeConfigFile()
			_, err := Load(path)
			Expect(err).ToNot(HaveOccurred())
			Expect(Get().Metadata.TMDBAPIKey).To(Equal("env-only-tmdb-key"))

			Expect(Update(context.Background(), func(c *Config) error {
				c.MediaServer.PlexClientID = "plex-id"
				return nil
			})).To(Succeed())

			raw, err := os.ReadFile(path)
			Expect(err).ToNot(HaveOccurred())
			Expect(string(raw)).ToNot(ContainSubstring("env-only-tmdb-key"))
			Expect(onDisk(path).String("metadata.tmdb_api_key")).To(BeEmpty())

			// The mutation that triggered the write-back still landed, and the
			// running process keeps the environment's value.
			Expect(onDisk(path).String("media_server.plex_client_id")).
				To(Equal("plex-id"))
			Expect(Get().Metadata.TMDBAPIKey).To(Equal("env-only-tmdb-key"))
		})

		It("leaves a secret the file itself carries untouched", func() {
			path := writeConfigFile(map[string]any{
				"metadata": map[string]any{"tmdb_api_key": "file-tmdb-key"},
			})
			_, err := Load(path)
			Expect(err).ToNot(HaveOccurred())

			Expect(Update(context.Background(), func(c *Config) error {
				c.MediaServer.PlexClientID = "plex-id"
				return nil
			})).To(Succeed())

			Expect(onDisk(path).String("metadata.tmdb_api_key")).
				To(Equal("file-tmdb-key"))
		})

		It(
			"restores the file's own value for a key the environment shadows",
			func() {
				GinkgoT().Setenv(envTMDBKey, "env-tmdb-key")
				path := writeConfigFile(map[string]any{
					"metadata": map[string]any{"tmdb_api_key": "file-tmdb-key"},
				})
				_, err := Load(path)
				Expect(err).ToNot(HaveOccurred())
				Expect(Get().Metadata.TMDBAPIKey).To(Equal("env-tmdb-key"))

				Expect(Update(context.Background(), func(c *Config) error {
					c.MediaServer.PlexClientID = "plex-id"
					return nil
				})).To(Succeed())

				raw, err := os.ReadFile(path)
				Expect(err).ToNot(HaveOccurred())
				Expect(string(raw)).ToNot(ContainSubstring("env-tmdb-key"))
				Expect(onDisk(path).String("metadata.tmdb_api_key")).
					To(Equal("file-tmdb-key"))
			},
		)

		It("still persists a generated session secret", func() {
			path := writeConfigFile()
			_, err := Load(path)
			Expect(err).ToNot(HaveOccurred())
			Expect(Get().Auth.SessionSecret).To(BeEmpty())

			Expect(Update(context.Background(), func(c *Config) error {
				c.Auth.SessionSecret = "generated-session-secret"
				return nil
			})).To(Succeed())
			Expect(onDisk(path).String("auth.session_secret")).
				To(Equal("generated-session-secret"))

			// Simulate a restart — the secret has to come back, or every session
			// dies on every boot.
			ResetForTest()
			reloaded, err := Load(path)
			Expect(err).ToNot(HaveOccurred())
			Expect(reloaded.Auth.SessionSecret).To(Equal("generated-session-secret"))
		})

		It("keeps an environment-supplied session secret off disk", func() {
			GinkgoT().
				Setenv("STREAMLINE_AUTH__SESSION_SECRET", "env-session-secret")
			path := writeConfigFile()
			_, err := Load(path)
			Expect(err).ToNot(HaveOccurred())
			Expect(Get().Auth.SessionSecret).To(Equal("env-session-secret"))

			Expect(Update(context.Background(), func(c *Config) error {
				c.MediaServer.PlexClientID = "plex-id"
				return nil
			})).To(Succeed())

			raw, err := os.ReadFile(path)
			Expect(err).ToNot(HaveOccurred())
			Expect(string(raw)).ToNot(ContainSubstring("env-session-secret"))
			Expect(onDisk(path).Exists("auth.session_secret")).To(BeFalse())
			Expect(Get().Auth.SessionSecret).To(Equal("env-session-secret"))
		})

		It("strips again on a second Update", func() {
			GinkgoT().Setenv(envTMDBKey, "env-only-tmdb-key")
			path := writeConfigFile()
			_, err := Load(path)
			Expect(err).ToNot(HaveOccurred())

			Expect(Update(context.Background(), func(c *Config) error {
				c.MediaServer.PlexClientID = "plex-id"
				return nil
			})).To(Succeed())
			// The first Update left the environment's value in the singleton,
			// which is what the second one clones and writes back.
			Expect(Update(context.Background(), func(c *Config) error {
				c.Auth.RegistrationMode = "invite"
				return nil
			})).To(Succeed())

			raw, err := os.ReadFile(path)
			Expect(err).ToNot(HaveOccurred())
			Expect(string(raw)).ToNot(ContainSubstring("env-only-tmdb-key"))

			d := onDisk(path)
			Expect(d.Exists("metadata.tmdb_api_key")).To(BeFalse())
			Expect(d.String("media_server.plex_client_id")).To(Equal("plex-id"))
			Expect(d.String("auth.registration_mode")).To(Equal("invite"))
		})

		It(
			"leaves out a non-secret the file never carried instead of writing the default over it",
			func() {
				// data_dir is the sharp one: writing ./data here would send the
				// next boot without STREAMLINE_DATA_DIR to a brand-new empty
				// SQLite database while the file looked authoritative.
				dir := GinkgoT().TempDir()
				envDataDir := filepath.Join(dir, "srv")
				Expect(os.MkdirAll(envDataDir, 0o755)).To(Succeed())
				GinkgoT().Setenv("STREAMLINE_DATA_DIR", envDataDir)
				path := writeConfigFileIn(dir, map[string]any{
					"log": map[string]any{"app": map[string]any{"level": "debug"}},
				})

				_, err := Load(path)
				Expect(err).ToNot(HaveOccurred())
				Expect(Get().DataDir).To(Equal(envDataDir))

				Expect(Update(context.Background(), func(c *Config) error {
					c.MediaServer.PlexClientID = "plex-id"
					return nil
				})).To(Succeed())

				d := onDisk(path)
				Expect(d.Exists("data_dir")).To(BeFalse())
				Expect(d.String("log.app.level")).To(Equal("debug"))
				Expect(d.String("media_server.plex_client_id")).To(Equal("plex-id"))
			},
		)

		It("does not persist environment-supplied trust lists", func() {
			// These revert in the safe direction — nothing trusted — and must
			// keep doing so: a persisted proxy or network range would go on
			// granting the trusted-role identity after the variable was pulled.
			GinkgoT().Setenv("STREAMLINE_AUTH__TRUSTED_NETWORKS", "10.9.0.0/16")
			GinkgoT().
				Setenv("STREAMLINE_SERVER__TRUSTED_PROXIES", "192.168.9.1/32")
			path := writeConfigFile()
			_, err := Load(path)
			Expect(err).ToNot(HaveOccurred())
			Expect(Get().Auth.TrustedNetworks).To(ConsistOf("10.9.0.0/16"))
			Expect(Get().Server.TrustedProxies).To(ConsistOf("192.168.9.1/32"))

			Expect(Update(context.Background(), func(c *Config) error {
				c.MediaServer.PlexClientID = "plex-id"
				return nil
			})).To(Succeed())

			raw, err := os.ReadFile(path)
			Expect(err).ToNot(HaveOccurred())
			Expect(string(raw)).ToNot(ContainSubstring("10.9.0.0/16"))
			Expect(string(raw)).ToNot(ContainSubstring("192.168.9.1/32"))
			Expect(onDisk(path).Exists("auth.trusted_networks")).To(BeFalse())
			Expect(onDisk(path).Exists("server.trusted_proxies")).To(BeFalse())
		})

		It(
			"treats a value the deprecated schedules alias moved as environment data",
			func() {
				GinkgoT().Setenv("STREAMLINE_SCHEDULES__RSS_SYNC", "1m")
				path := writeConfigFile()
				_, err := Load(path)
				Expect(err).ToNot(HaveOccurred())
				Expect(Get().Schedule.MovieRSSSync).To(Equal("1m"))

				Expect(Update(context.Background(), func(c *Config) error {
					c.MediaServer.PlexClientID = "plex-id"
					return nil
				})).To(Succeed())

				d := onDisk(path)
				Expect(d.Exists("schedules.movie_rss_sync")).To(BeFalse())
				// Only the aliased key is withheld; the rest of the block is
				// written from the struct as usual.
				Expect(d.String("schedules.download_monitor")).To(Equal("30s"))
			},
		)

		It(
			"keeps the file's own deprecated schedules value when the environment shadows it",
			func() {
				GinkgoT().Setenv("STREAMLINE_SCHEDULES__RSS_SYNC", "1m")
				path := writeConfigFile(map[string]any{
					"schedules": map[string]any{"rss_sync": "5m"},
				})
				_, err := Load(path)
				Expect(err).ToNot(HaveOccurred())
				Expect(Get().Schedule.MovieRSSSync).To(Equal("1m"))

				Expect(Update(context.Background(), func(c *Config) error {
					c.MediaServer.PlexClientID = "plex-id"
					return nil
				})).To(Succeed())

				Expect(onDisk(path).String("schedules.movie_rss_sync")).
					To(Equal("5m"))
			},
		)

		// Withholding a key the update did not touch is the design. Withholding
		// one it did means the caller's write went nowhere, and a caller told
		// "saved" acts on it: the rotation hands out a token signed with a
		// secret the next restart will not have, the schedules handler answers
		// 200 for a cadence that reverts.
		It(
			"refuses a rotation of a session secret the environment supplies",
			func() {
				GinkgoT().Setenv(
					"STREAMLINE_AUTH__SESSION_SECRET",
					"ENV-OLD-COMPROMISED-SECRET",
				)
				path := writeConfigFile()
				_, err := Load(path)
				Expect(err).ToNot(HaveOccurred())

				err = Update(context.Background(), func(c *Config) error {
					c.Auth.SessionSecret = "ROTATED-SECRET-999"
					return nil
				})
				Expect(err).To(MatchError(ErrEnvOwned))
				Expect(err.Error()).To(ContainSubstring("auth.session_secret"))

				// The compromised value is still the one that survives a
				// restart, and the caller was told so instead of being handed a
				// token signed with a secret that only exists in memory.
				Expect(onDisk(path).Exists("auth.session_secret")).To(BeFalse())
				Expect(Get().Auth.SessionSecret).
					To(Equal("ENV-OLD-COMPROMISED-SECRET"))
			},
		)

		It("refuses a write to a non-secret the environment supplies", func() {
			GinkgoT().Setenv("STREAMLINE_SCHEDULES__MOVIE_RSS_SYNC", "5m")
			path := writeConfigFile()
			_, err := Load(path)
			Expect(err).ToNot(HaveOccurred())

			err = Update(context.Background(), func(c *Config) error {
				c.Schedule.MovieRSSSync = "45m"
				return nil
			})
			Expect(err).To(MatchError(ErrEnvOwned))
			Expect(err.Error()).To(ContainSubstring("schedules.movie_rss_sync"))
			Expect(onDisk(path).Exists("schedules.movie_rss_sync")).To(BeFalse())
			Expect(Get().Schedule.MovieRSSSync).To(Equal("5m"))
		})

		It("refuses the whole update, including the keys it could keep", func() {
			GinkgoT().Setenv("STREAMLINE_SCHEDULES__MOVIE_RSS_SYNC", "5m")
			path := writeConfigFile()
			_, err := Load(path)
			Expect(err).ToNot(HaveOccurred())

			err = Update(context.Background(), func(c *Config) error {
				c.MediaServer.PlexClientID = "plex-id"
				c.Schedule.MovieRSSSync = "45m"
				return nil
			})
			Expect(err).To(MatchError(ErrEnvOwned))
			Expect(onDisk(path).Exists("media_server.plex_client_id")).
				To(BeFalse())
			Expect(Get().MediaServer.PlexClientID).To(BeEmpty())
		})

		It(
			"drops a write of the value the environment already supplies, and says which key",
			func() {
				var logs bytes.Buffer
				GinkgoWriter.TeeTo(&logs)
				DeferCleanup(GinkgoWriter.ClearTeeWriters)
				GinkgoT().Setenv(envTMDBKey, "env-only-tmdb-key")
				path := writeConfigFile()
				_, err := Load(path)
				Expect(err).ToNot(HaveOccurred())

				// The setting ends up on the value the caller asked for either
				// way, so there is nothing to refuse.
				Expect(Update(context.Background(), func(c *Config) error {
					c.Metadata.TMDBAPIKey = "env-only-tmdb-key"
					c.MediaServer.PlexClientID = "plex-id"
					return nil
				})).To(Succeed())
				Expect(onDisk(path).String("media_server.plex_client_id")).
					To(Equal("plex-id"))

				// What the caller does not get is the file recording it — an
				// operator pinning that value in the UI sees a success and no
				// change. The warning naming the key is the only place it shows.
				Expect(onDisk(path).Exists("metadata.tmdb_api_key")).
					To(BeFalse())
				Expect(logs.String()).
					To(ContainSubstring("were left out of the config file"))
				Expect(logs.String()).
					To(ContainSubstring("metadata.tmdb_api_key"))
			},
		)

		// Stripping is per key; loading is not. A file can lose a key it never
		// owned and be left holding a combination that Load rejects outright,
		// and the write-back is the last moment anything can tell — by the time
		// Load disagrees these bytes have already replaced the good ones.
		It("refuses to write a file this update is what breaks", func() {
			dir := GinkgoT().TempDir()
			secret := filepath.Join(dir, "session.secret")
			Expect(os.WriteFile(secret, []byte("s3cr3t"), 0o600)).To(Succeed())
			path := writeConfigFileIn(dir, map[string]any{
				"data_dir": dir,
				"auth":     map[string]any{"session_secret_file": secret},
			})
			_, err := Load(path)
			Expect(err).ToNot(HaveOccurred())

			err = Update(context.Background(), func(c *Config) error {
				c.Auth.SessionSecretFile = filepath.Join(dir, "gone_secret")
				return nil
			})
			Expect(err).To(MatchError(ErrWriteBackUnloadable))
			Expect(err.Error()).To(ContainSubstring("gone_secret"))

			// The file the operator wrote is still the file on disk, and the
			// running process still resolves the secret it was loaded with.
			Expect(onDisk(path).String("auth.session_secret_file")).
				To(Equal(secret))
			Expect(Get().Auth.SessionSecretFile).To(Equal(secret))
		})

		// store's callers all hand it a layer, but nothing made them: the one
		// that did not left a nil envOverlay for the write-back to walk into,
		// reachable the moment a path was captured too.
		It("writes back for a config stored without an environment layer", func() {
			path := writeConfigFile()
			cfg, err := Load(path)
			Expect(err).ToNot(HaveOccurred())
			store(cfg, path, nil, nil)

			Expect(Update(context.Background(), func(c *Config) error {
				c.MediaServer.PlexClientID = "plex-id"
				return nil
			})).To(Succeed())
			Expect(onDisk(path).String("media_server.plex_client_id")).
				To(Equal("plex-id"))
		})

		// koanf turns indexers.0.api_key into a map keyed "0" that replaces the
		// whole list, so the entry loses the fields it needs and Load stops at
		// validation. That failure is the only thing keeping these secrets off
		// disk: write-back addresses a list as one leaf value, so it cannot
		// recognise — and would not strip — a key reaching inside one. Pin it,
		// or a koanf that learns to merge list elements starts leaking quietly.
		DescribeTable(
			"refuses a load that addresses a list element through the environment",
			func(envVar string, entries map[string]any) {
				GinkgoT().Setenv(envVar, "env-list-secret")
				path := writeConfigFile(entries)

				_, err := Load(path)
				Expect(err).To(HaveOccurred())
				Expect(Get()).To(BeNil())
			},
			Entry("indexers[].api_key", "STREAMLINE_INDEXERS__0__API_KEY",
				map[string]any{"indexers": []map[string]any{{
					"name":     "idx",
					"host":     "idx.example.com",
					"port":     9117,
					"protocol": "torznab",
					"api_key":  "file-indexer-key",
				}}}),
			Entry(
				"download_clients[].password",
				"STREAMLINE_DOWNLOAD_CLIENTS__0__PASSWORD",
				map[string]any{"download_clients": []map[string]any{{
					"name":        "qb",
					"client_type": "qbittorrent",
					"host":        "qb.example.com",
					"port":        8080,
					"auth_method": "password",
					"password":    "file-client-password",
				}}},
			),
			Entry("auth.oidc[].client_secret",
				"STREAMLINE_AUTH__OIDC__0__CLIENT_SECRET",
				map[string]any{"auth": map[string]any{
					"oidc": []map[string]any{{
						"name":          "idp",
						"issuer":        "https://idp.example.com",
						"client_id":     "cid",
						"client_secret": "file-oidc-secret",
					}},
				}}),
		)
	},
)

// A config file that only loads because a variable fills a gap was already in
// that state before anything wrote it back, so Update reports it and saves.
// Refusing instead would leave an install that boots and runs fine unable to
// add an indexer, register a Plex client ID or rotate a secret — for a
// condition the save neither caused nor could fix.
var _ = Describe(
	"Update and a file the environment props up",
	Label("unit", "config"),
	func() {
		var logs bytes.Buffer

		BeforeEach(func() {
			ResetForTest()
			DeferCleanup(ResetForTest)
			logs.Reset()
			GinkgoWriter.TeeTo(&logs)
			DeferCleanup(GinkgoWriter.ClearTeeWriters)
		})

		// StructExcept drops every rule on the field, so the check meant to skip
		// data_dir's `dir` tag skipped its `required` tag too and saw nothing.
		It("reports a data_dir only the environment fills in", func() {
			dir := GinkgoT().TempDir()
			envDataDir := filepath.Join(dir, "srv")
			Expect(os.MkdirAll(envDataDir, 0o755)).To(Succeed())
			GinkgoT().Setenv("STREAMLINE_DATA_DIR", envDataDir)
			path := writeConfigFileIn(dir, map[string]any{"data_dir": ""})
			_, err := Load(path)
			Expect(err).ToNot(HaveOccurred())
			Expect(Get().DataDir).To(Equal(envDataDir))

			Expect(Update(context.Background(), func(c *Config) error {
				c.MediaServer.PlexClientID = "plex-id"
				return nil
			})).To(Succeed())

			Expect(logs.String()).
				To(ContainSubstring("does not load without the environment"))
			Expect(logs.String()).To(ContainSubstring("DataDir"))

			// The save landed, and the state the warning names is real: the
			// file is the operator's own, and the next boot without the
			// variable stops on it.
			Expect(onDisk(path).String("media_server.plex_client_id")).
				To(Equal("plex-id"))
			Expect(os.Unsetenv("STREAMLINE_DATA_DIR")).To(Succeed())
			ResetForTest()
			_, err = Load(path)
			Expect(err).To(MatchError(ContainSubstring("'Config.DataDir'")))
		})

		// Load runs Validate and loadSecretFiles; the gate ran only the first
		// half, so a *_file path the environment replaces went back to disk with
		// nothing said about the next boot failing to read it.
		It("reports a secret file only the environment replaces", func() {
			dir := GinkgoT().TempDir()
			live := filepath.Join(dir, "session.secret")
			Expect(os.WriteFile(live, []byte("s3cr3t"), 0o600)).To(Succeed())
			GinkgoT().Setenv("STREAMLINE_AUTH__SESSION_SECRET_FILE", live)
			path := writeConfigFileIn(dir, map[string]any{
				"data_dir": dir,
				"auth": map[string]any{
					"session_secret_file": filepath.Join(dir, "gone_secret"),
				},
			})
			_, err := Load(path)
			Expect(err).ToNot(HaveOccurred())
			Expect(Get().Auth.SessionSecretFile).To(Equal(live))

			Expect(Update(context.Background(), func(c *Config) error {
				c.MediaServer.PlexClientID = "plex-id"
				return nil
			})).To(Succeed())

			Expect(logs.String()).
				To(ContainSubstring("does not load without the environment"))
			Expect(logs.String()).To(ContainSubstring("gone_secret"))
			Expect(onDisk(path).String("auth.session_secret_file")).
				To(HaveSuffix("gone_secret"))
		})

		// Every settings save goes through here. Refusing them all over a file
		// the operator can still boot is a bricked settings page, not a
		// protection — round 2 saved happily.
		It("keeps saving while the environment props the file up", func() {
			GinkgoT().Setenv("STREAMLINE_QUALITY_DEFAULT_PROFILE", "uhd-remux")
			path := writeConfigFile(map[string]any{
				"quality_profiles": []map[string]any{{
					"name":                 "uhd-remux",
					"preferred_resolution": "2160p",
					"min_resolution":       "2160p",
					"upgrade_allowed":      true,
				}},
			})
			_, err := Load(path)
			Expect(err).ToNot(HaveOccurred())

			Expect(Update(context.Background(), func(c *Config) error {
				c.MediaServer.PlexClientID = "plex-id"
				return nil
			})).To(Succeed())
			Expect(Update(context.Background(), func(c *Config) error {
				c.Auth.RegistrationMode = "invite"
				return nil
			})).To(Succeed())

			Expect(logs.String()).
				To(ContainSubstring("does not load without the environment"))
			d := onDisk(path)
			Expect(d.String("media_server.plex_client_id")).To(Equal("plex-id"))
			Expect(d.String("auth.registration_mode")).To(Equal("invite"))
		})

		// Validation used to create data_dir, so a refused update left the
		// directory tree of a change that never happened.
		It("creates nothing on disk when it refuses the update", func() {
			GinkgoT().Setenv("STREAMLINE_SCHEDULES__MOVIE_RSS_SYNC", "5m")
			dir := GinkgoT().TempDir()
			path := writeConfigFileIn(dir, map[string]any{"data_dir": dir})
			_, err := Load(path)
			Expect(err).ToNot(HaveOccurred())

			sideEffect := filepath.Join(dir, "sideeffect", "deep", "path")
			err = Update(context.Background(), func(c *Config) error {
				c.DataDir = sideEffect
				c.Schedule.MovieRSSSync = "45m"
				return nil
			})
			Expect(err).To(MatchError(ErrEnvOwned))
			Expect(sideEffect).ToNot(BeADirectory())
			Expect(filepath.Join(dir, "sideeffect")).ToNot(BeADirectory())
		})
	},
)

// The gate's whole job is telling the breakage an update introduces from the
// breakage it inherited, so both halves of that question are pinned here: one
// mutation, run against a file that loads on its own and against a file whose
// only flaw is somewhere else entirely. Comparing the two answers as booleans
// — loadable against unloadable — made the second install take the write the
// first one refused.
var _ = Describe(
	"Update and the file the next boot would load",
	Label("unit", "config"),
	func() {
		BeforeEach(func() {
			ResetForTest()
			DeferCleanup(ResetForTest)
		})

		DescribeTable(
			"refuses the secret file this update is what points at nothing",
			func(prepare func() map[string]any, fileAlone types.GomegaMatcher) {
				dir := GinkgoT().TempDir()
				secret := filepath.Join(dir, "session.secret")
				Expect(os.WriteFile(secret, []byte("s3cr3t"), 0o600)).
					To(Succeed())
				entries := prepare()
				entries["data_dir"] = dir
				entries["auth"] = map[string]any{"session_secret_file": secret}
				path := writeConfigFileIn(dir, entries)
				_, err := Load(path)
				Expect(err).ToNot(HaveOccurred())
				// The control: what this file is worth without the environment,
				// before the update proposes anything.
				Expect(checkLoadable(onDisk(path))).To(fileAlone)

				missing := filepath.Join(dir, "brand-new-missing.secret")
				err = Update(context.Background(), func(c *Config) error {
					c.Auth.SessionSecretFile = missing
					return nil
				})
				Expect(err).To(MatchError(ErrWriteBackUnloadable))
				Expect(err.Error()).
					To(ContainSubstring("brand-new-missing.secret"))
				// Named for what this update did, not for what it inherited:
				// the pre-existing reason is nothing the caller can act on.
				Expect(err.Error()).
					ToNot(ContainSubstring("quality_default_profile"))

				Expect(onDisk(path).String("auth.session_secret_file")).
					To(Equal(secret))
				Expect(Get().Auth.SessionSecretFile).To(Equal(secret))
			},
			Entry(
				"on a file that loads on its own",
				func() map[string]any { return map[string]any{} },
				Succeed(),
			),
			// The environment supplies quality_default_profile, so the file's
			// own profile list answers to no default name once the write-back
			// strips it. That is this install's entire pre-existing flaw, and
			// it has nothing to do with the key the update breaks — nor can the
			// environment mask that key, which the file owns outright.
			Entry(
				"on a file the environment already props up",
				func() map[string]any {
					GinkgoT().Setenv(
						"STREAMLINE_QUALITY_DEFAULT_PROFILE", "uhd-remux",
					)
					return map[string]any{
						"quality_profiles": []map[string]any{{
							"name":                 "uhd-remux",
							"preferred_resolution": "2160p",
							"min_resolution":       "2160p",
							"upgrade_allowed":      true,
						}},
					}
				},
				MatchError(ContainSubstring("quality_default_profile")),
			),
		)

		// Same masking, one phase in: the secret-file read used to stop at the
		// first unreadable path, so an install already missing one had its
		// answer fixed no matter what else the update broke.
		It("refuses a second secret file while the first is already gone", func() {
			dir := GinkgoT().TempDir()
			live := filepath.Join(dir, "session.secret")
			Expect(os.WriteFile(live, []byte("s3cr3t"), 0o600)).To(Succeed())
			GinkgoT().Setenv("STREAMLINE_AUTH__SESSION_SECRET_FILE", live)
			path := writeConfigFileIn(dir, map[string]any{
				"data_dir": dir,
				"auth": map[string]any{
					"session_secret_file": filepath.Join(dir, "gone_secret"),
				},
			})
			_, err := Load(path)
			Expect(err).ToNot(HaveOccurred())

			missing := filepath.Join(dir, "brand-new-missing.secret")
			err = Update(context.Background(), func(c *Config) error {
				c.Metadata.TMDBAPIKeyFile = missing
				return nil
			})
			Expect(err).To(MatchError(ErrWriteBackUnloadable))
			Expect(err.Error()).To(ContainSubstring("brand-new-missing.secret"))
			Expect(err.Error()).ToNot(ContainSubstring("gone_secret"))
			Expect(onDisk(path).Exists("metadata.tmdb_api_key_file")).
				To(BeFalse())
		})

		// One mutation, three installs: one healthy, two carrying an unrelated
		// flaw the environment corrects. Reasons were collected across the
		// phases but not inside one, so whichever flaw a phase reported first
		// stood for that phase on both sides of the comparison — the delete
		// came out with nothing caused, and the file lost the profile the
		// default still names.
		DescribeTable(
			"refuses the delete that leaves the default profile naming nothing",
			func(prepare func(dir string) map[string]any) {
				GinkgoT().Setenv("STREAMLINE_QUALITY_DEFAULT_PROFILE", "uhd")
				dir := GinkgoT().TempDir()
				entries := prepare(dir)
				entries["data_dir"] = dir
				entries["quality_profiles"] = []map[string]any{
					{
						"name":                 "default",
						"preferred_resolution": "1080p",
						"min_resolution":       "1080p",
						"upgrade_allowed":      true,
					},
					{
						"name":                 "uhd",
						"preferred_resolution": "2160p",
						"min_resolution":       "2160p",
						"upgrade_allowed":      true,
					},
				}
				path := writeConfigFileIn(dir, entries)
				_, err := Load(path)
				Expect(err).ToNot(HaveOccurred())

				// The file never names a default, so the write-back's own
				// quality_default_profile is the built-in "default" — the
				// profile this call deletes.
				err = DeleteQualityProfile(context.Background(), "default")
				Expect(err).To(MatchError(ErrWriteBackUnloadable))
				Expect(err.Error()).To(ContainSubstring(
					`quality_default_profile "default" names no profile`,
				))

				Expect(onDisk(path).Get("quality_profiles")).To(HaveLen(2))
				Expect(Get().QualityProfiles).To(HaveLen(2))
			},
			Entry(
				"on a file that loads on its own",
				func(string) map[string]any { return map[string]any{} },
			),
			// An invariant ahead of the one the delete breaks: both are
			// checkInvariants', which used to answer with its first.
			Entry(
				"on a file whose proxy list only the environment corrects",
				func(string) map[string]any {
					GinkgoT().Setenv(
						"STREAMLINE_SERVER__TRUSTED_PROXIES", "10.0.0.0/8",
					)
					return map[string]any{"server": map[string]any{
						"trusted_proxies": []string{"::ffff:10.0.0.0/104"},
					}}
				},
			),
			// A struct tag ahead of it: Validate used to return on the tags, so
			// no invariant ran at all on either side of the comparison.
			Entry(
				"on a file whose secret path only the environment corrects",
				func(dir string) map[string]any {
					live := filepath.Join(dir, "tmdb.key")
					Expect(os.WriteFile(live, []byte("k3y"), 0o600)).
						To(Succeed())
					GinkgoT().
						Setenv("STREAMLINE_METADATA__TMDB_API_KEY_FILE", live)
					// A directory is a path the `filepath` tag rejects.
					return map[string]any{"metadata": map[string]any{
						"tmdb_api_key_file": dir,
					}}
				},
			),
			// A value that will not decode, which is a phase ahead of both:
			// the config was thrown away whole, so nothing else was asked.
			Entry(
				"on a file whose port only the environment corrects",
				func(string) map[string]any {
					GinkgoT().Setenv("STREAMLINE_SERVER__PORT", "8080")
					return map[string]any{
						"server": map[string]any{"port": "not-a-port"},
					}
				},
			),
		)

		// The same masking, asked of the gate directly: a caller comparing
		// these lists can only see a reason that is in one of them.
		DescribeTable(
			"reports every reason a boot would refuse, not the first",
			func(keys map[string]any, ids ...types.GomegaMatcher) {
				k := koanf.New(".")
				Expect(k.Load(confmap.Provider(keys, "."), nil)).To(Succeed())

				issues := loadIssues(k)
				got := make([]string, 0, len(issues))
				for _, issue := range issues {
					got = append(got, issue.id)
				}
				Expect(got).To(ConsistOf(ids))
			},
			Entry(
				"an invariant behind another invariant",
				map[string]any{
					"server": map[string]any{
						"trusted_proxies": []string{"::ffff:10.0.0.0/104"},
					},
					"quality_profiles": []map[string]any{{
						"name":                 "uhd",
						"preferred_resolution": "2160p",
						"min_resolution":       "2160p",
						"upgrade_allowed":      true,
					}},
				},
				ContainSubstring("server.trusted_proxies"),
				ContainSubstring("quality_default_profile"),
			),
			Entry(
				"an invariant behind two struct tags",
				map[string]any{
					"log": map[string]any{
						"app": map[string]any{
							"level":  "shout",
							"format": "morse",
						},
					},
					"quality_profiles": []map[string]any{{
						"name":                 "uhd",
						"preferred_resolution": "2160p",
						"min_resolution":       "2160p",
						"upgrade_allowed":      true,
					}},
				},
				Equal("Config.Log.App.Level oneof"),
				Equal("Config.Log.App.Format oneof"),
				ContainSubstring("quality_default_profile"),
			),
			// The key that would not decode is left at its zero value, so the
			// tags report it a second time. Both readings are the same on
			// either side of the comparison, which is what matters.
			Entry(
				"an invariant behind a value that will not decode",
				map[string]any{
					"server": map[string]any{"port": "not-a-port"},
					"quality_profiles": []map[string]any{{
						"name":                 "uhd",
						"preferred_resolution": "2160p",
						"min_resolution":       "2160p",
						"upgrade_allowed":      true,
					}},
				},
				ContainSubstring("'server.port' cannot parse value as 'uint16'"),
				Equal("Config.Server.Port required"),
				ContainSubstring("quality_default_profile"),
			),
		)

		// Creating data_dir is a boot step, and the gate is not a boot: it
		// creates nothing, and the restart that will use the directory may well
		// be able to make it where this process cannot. So a data_dir the next
		// boot cannot create is written like any other value and lands at that
		// boot — on a healthy install, with nothing propping anything up.
		It("saves a data_dir the next boot cannot create", func() {
			dir := GinkgoT().TempDir()
			blocker := filepath.Join(dir, "blocker")
			Expect(os.WriteFile(blocker, []byte("a file"), 0o600)).To(Succeed())
			path := writeConfigFileIn(dir, map[string]any{"data_dir": dir})
			_, err := Load(path)
			Expect(err).ToNot(HaveOccurred())

			unmakeable := filepath.Join(blocker, "data")
			Expect(Update(context.Background(), func(c *Config) error {
				c.DataDir = unmakeable
				return nil
			})).To(Succeed())
			Expect(onDisk(path).String("data_dir")).To(Equal(unmakeable))
			Expect(unmakeable).ToNot(BeADirectory())

			ResetForTest()
			_, err = Load(path)
			Expect(err).To(MatchError(ContainSubstring("data_dir")))
		})
	},
)

var _ = Describe("HiddenString", Label("unit", "config"), func() {
	BeforeEach(func() {
		ResetForTest()
		DeferCleanup(ResetForTest)
	})

	It("returns a raw key that is not part of the Config struct", func() {
		dataDir := GinkgoT().TempDir()
		base := minimalYAML(dataDir)
		raw := base + "metadata:\n  tmdb:\n    base_url: http://127.0.0.1:9\n"
		Expect(LoadReader(strings.NewReader(raw))).To(Succeed())
		Expect(HiddenString("metadata.tmdb.base_url")).
			To(Equal("http://127.0.0.1:9"))
	})

	It("returns empty for an unset key", func() {
		dataDir := GinkgoT().TempDir()
		Expect(LoadReader(strings.NewReader(minimalYAML(dataDir)))).To(Succeed())
		Expect(HiddenString("metadata.tmdb.base_url")).To(BeEmpty())
	})

	It("returns empty before any config is loaded", func() {
		Expect(HiddenString("metadata.tmdb.base_url")).To(BeEmpty())
	})

	It("returns a hidden key loaded from a file via Load(path)", func() {
		dir := GinkgoT().TempDir()
		cfgPath := filepath.Join(dir, "cfg.yaml")
		dataDir := filepath.Join(dir, "data")
		Expect(os.MkdirAll(dataDir, 0o755)).To(Succeed())
		base := minimalYAML(dataDir)
		raw := base + "metadata:\n  tmdb:\n    base_url: http://127.0.0.1:9\n"
		Expect(os.WriteFile(cfgPath, []byte(raw), 0o600)).To(Succeed())

		_, err := Load(cfgPath)
		Expect(err).ToNot(HaveOccurred())
		Expect(HiddenString("metadata.tmdb.base_url")).
			To(Equal("http://127.0.0.1:9"))
	})
})
