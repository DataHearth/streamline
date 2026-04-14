package config

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// leafKoanfPaths walks a config struct and returns the dotted koanf path of
// every leaf field (basic types and slices). Nested structs are recursed;
// fields without a koanf tag (e.g. the unexported pinned map) are skipped.
func leafKoanfPaths(t reflect.Type, prefix string) []string {
	var paths []string
	for _, f := range reflect.VisibleFields(t) {
		tag := f.Tag.Get("koanf")
		if tag == "" {
			continue
		}
		path := tag
		if prefix != "" {
			path = prefix + "." + tag
		}
		if f.Type.Kind() == reflect.Struct {
			paths = append(paths, leafKoanfPaths(f.Type, path)...)
			continue
		}
		paths = append(paths, path)
	}
	return paths
}

var _ = Describe("Config", Label("unit", "config"), func() {
	BeforeEach(func() {
		ResetForTest()
	})

	Describe("Load", func() {
		BeforeEach(func() {
			Expect(os.MkdirAll("./data", 0o755)).To(Succeed())
			DeferCleanup(func() {
				Expect(os.RemoveAll("./data")).To(Succeed())
			})
		})

		Context("with no file or env vars", func() {
			It("should return defaults", func() {
				cfg, err := Load("")
				Expect(err).NotTo(HaveOccurred())

				Expect(cfg.Server.Host).To(Equal("0.0.0.0"))
				Expect(cfg.Server.Port).To(Equal(uint16(8080)))
				Expect(cfg.DataDir).To(Equal("./data"))
				Expect(cfg.DatabasePath()).To(Equal("data/streamline.db"))
				Expect(cfg.Auth.Mode).To(Equal("full"))
				Expect(cfg.Log.App.Level).To(Equal("info"))
				Expect(cfg.Log.App.Format).To(Equal("text"))
				Expect(cfg.Log.App.Enabled).To(BeTrue())
				Expect(cfg.Log.App.Output).To(Equal("stderr"))
				Expect(cfg.Log.HTTP.Enabled).To(BeTrue())
				Expect(cfg.Log.HTTP.Format).To(Equal("json"))
				Expect(cfg.Log.HTTP.Output).To(Equal("stderr"))
				Expect(cfg.Library.ImportMode).To(Equal("hardlink"))
			})
		})

		Context("with a config file", func() {
			It("should override defaults with file values", func() {
				dir := GinkgoT().TempDir()
				cfgFile := filepath.Join(dir, "config.yaml")
				err := os.WriteFile(
					cfgFile,
					[]byte(
						"server:\n  port: 9090\nlog:\n  app:\n    level: debug\n",
					),
					0o644,
				)
				Expect(err).NotTo(HaveOccurred())

				cfg, err := Load(cfgFile)
				Expect(err).NotTo(HaveOccurred())

				Expect(cfg.Server.Port).To(Equal(uint16(9090)))
				Expect(cfg.Log.App.Level).To(Equal("debug"))
				Expect(cfg.Server.Host).To(Equal("0.0.0.0"))
			})
		})

		Context("quality defaults", func() {
			It("seeds global quality knobs and the default profile", func() {
				cfg, err := Load("")
				Expect(err).NotTo(HaveOccurred())
				Expect(cfg.Library.NoMatchCooldown).To(Equal("6h"))
				Expect(cfg.Library.MaxGrabFailures).To(Equal(uint8(3)))
				Expect(cfg.QualityDefaultProfile).To(Equal("default"))
				Expect(cfg.QualityProfiles).To(HaveLen(1))
				p := cfg.QualityProfiles[0]
				Expect(p.Name).To(Equal("default"))
				Expect(p.PreferredResolution).To(Equal("1080p"))
				Expect(p.MinResolution).To(Equal("1080p"))
				Expect(p.UpgradeAllowed).To(BeTrue())
			})
		})

		Context("metadata.language default", func() {
			It("defaults metadata.language to \"en\"", func() {
				cfg, err := Load("")
				Expect(err).NotTo(HaveOccurred())
				Expect(cfg.Metadata.Language).To(Equal("en"))
			})

			It("accepts a valid BCP-47 override", func() {
				dir := GinkgoT().TempDir()
				cfgFile := filepath.Join(dir, "config.yaml")
				dataDir := filepath.Join(dir, "data")
				Expect(os.MkdirAll(dataDir, 0o755)).To(Succeed())
				yaml := "data_dir: " + dataDir + "\nmetadata:\n  language: fr\n"
				Expect(os.WriteFile(cfgFile, []byte(yaml), 0o644)).To(Succeed())

				cfg, err := Load(cfgFile)
				Expect(err).NotTo(HaveOccurred())
				Expect(cfg.Metadata.Language).To(Equal("fr"))
			})

			It("rejects a non-BCP-47 value", func() {
				dir := GinkgoT().TempDir()
				cfgFile := filepath.Join(dir, "config.yaml")
				dataDir := filepath.Join(dir, "data")
				Expect(os.MkdirAll(dataDir, 0o755)).To(Succeed())
				yaml := "data_dir: " + dataDir + "\nmetadata:\n  language: not-a-tag!\n"
				Expect(os.WriteFile(cfgFile, []byte(yaml), 0o644)).To(Succeed())

				_, err := Load(cfgFile)
				Expect(err).To(HaveOccurred())
			})
		})

		Context("import pipeline defaults", func() {
			It("seeds library + schedule defaults for the importer", func() {
				cfg, err := Load("")
				Expect(err).NotTo(HaveOccurred())
				Expect(cfg.Library.KeepTorrentSeeding).To(BeTrue())
				Expect(cfg.Library.ImportMaxAttempts).To(Equal(uint8(3)))
				Expect(cfg.Library.AllowedDownloadRoots).To(BeEmpty())
				Expect(cfg.Library.MovieNaming).
					To(Equal("{title} ({year}) {tmdb-{tmdb_id}}/{title} ({year}) [{quality}].{ext}"))
				Expect(cfg.Schedule.ImportScan).To(Equal("60s"))
			})
		})

		Context("with environment variables", func() {
			BeforeEach(func() {
				os.Setenv("STREAMLINE_SERVER__PORT", "7070")
				os.Setenv("STREAMLINE_AUTH__SESSION_SECRET", "env-secret")
				DeferCleanup(func() {
					os.Unsetenv("STREAMLINE_SERVER__PORT")
					os.Unsetenv("STREAMLINE_AUTH__SESSION_SECRET")
				})
			})

			It("maps __ to the path delimiter, leaving single _ literal", func() {
				cfg, err := Load("")
				Expect(err).NotTo(HaveOccurred())
				// server.port reached via __; session_secret's own _ stays literal.
				Expect(cfg.Server.Port).To(Equal(uint16(7070)))
				Expect(cfg.Auth.SessionSecret).To(Equal("env-secret"))
			})
		})

		Context("with *_file secrets", func() {
			// writeCfg writes a config.yaml + data dir and returns the path.
			writeCfg := func(body string) string {
				GinkgoHelper()
				dir := GinkgoT().TempDir()
				dataDir := filepath.Join(dir, "data")
				Expect(os.MkdirAll(dataDir, 0o755)).To(Succeed())
				cfgFile := filepath.Join(dir, "config.yaml")
				Expect(os.WriteFile(cfgFile,
					[]byte("data_dir: "+dataDir+"\n"+body), 0o600)).To(Succeed())
				return cfgFile
			}

			It(
				"resolves scalar + list *_file refs without mutating the struct",
				func() {
					dir := GinkgoT().TempDir()
					tmdbPath := filepath.Join(dir, "tmdb")
					idxPath := filepath.Join(dir, "idx")
					Expect(
						os.WriteFile(tmdbPath, []byte("  file-tmdb\n"), 0o600),
					).To(Succeed())
					Expect(
						os.WriteFile(idxPath, []byte("file-idx\n"), 0o600),
					).To(Succeed())

					cfgFile := writeCfg(
						"metadata:\n  tmdb_api_key_file: " + tmdbPath + "\n" +
							"indexers:\n  - name: prowlarr\n    host: prowlarr.local\n" +
							"    port: 9696\n    protocol: torznab\n    api_key_file: " + idxPath + "\n",
					)
					cfg, err := Load(cfgFile)
					Expect(err).NotTo(HaveOccurred())

					// Struct keeps the operator's paths; inline stays empty.
					Expect(cfg.Metadata.TMDBAPIKey).To(BeEmpty())
					Expect(cfg.Metadata.TMDBAPIKeyFile).To(Equal(tmdbPath))
					Expect(cfg.Indexers[0].APIKey).To(BeEmpty())

					// SecretValue resolves + trims, for both scalar and list secrets.
					Expect(
						SecretValue(
							cfg.Metadata.TMDBAPIKey,
							cfg.Metadata.TMDBAPIKeyFile,
						),
					).
						To(Equal("file-tmdb"))
					Expect(
						SecretValue(
							cfg.Indexers[0].APIKey,
							cfg.Indexers[0].APIKeyFile,
						),
					).
						To(Equal("file-idx"))
				},
			)

			It("rejects setting both inline and file for one secret", func() {
				cfgFile := writeCfg(
					"metadata:\n  tmdb_api_key: inline\n  tmdb_api_key_file: /x\n")
				_, err := Load(cfgFile)
				Expect(err).To(HaveOccurred())
			})

			It("fails when a referenced secret file is unreadable", func() {
				cfgFile := writeCfg(
					"metadata:\n  tmdb_api_key_file: /no/such/streamline-secret\n")
				_, err := Load(cfgFile)
				Expect(err).To(HaveOccurred())
			})
		})

		It(
			"defaults auth.registration_mode=disabled, session_ttl=168h, oidc_default_role=member",
			func() {
				cfg, err := Load("")
				Expect(err).ToNot(HaveOccurred())
				Expect(cfg.Auth.RegistrationMode).To(Equal("disabled"))
				Expect(cfg.Auth.SessionTTL).To(Equal("168h"))
				Expect(cfg.Auth.OIDCDefaultRole).To(Equal("member"))
			},
		)

		It("rejects invalid registration_mode", func() {
			dir := GinkgoT().TempDir()
			cfgFile := filepath.Join(dir, "config.yaml")
			dataDir := filepath.Join(dir, "data")
			Expect(os.MkdirAll(dataDir, 0o755)).To(Succeed())
			yaml := "data_dir: " + dataDir + "\nauth:\n  registration_mode: nonsense\n"
			Expect(os.WriteFile(cfgFile, []byte(yaml), 0o644)).To(Succeed())
			_, err := Load(cfgFile)
			Expect(err).To(HaveOccurred())
		})
	})

	Describe("DumpDefaults", func() {
		It("writes valid YAML with expected top-level keys", func() {
			var buf bytes.Buffer
			Expect(DumpDefaults(&buf)).To(Succeed())

			Expect(buf.Len()).To(BeNumerically(">", 0))

			output := buf.String()
			Expect(output).To(ContainSubstring("server"))
			Expect(output).To(ContainSubstring("data_dir"))
			Expect(output).To(ContainSubstring("auth"))
			Expect(output).To(ContainSubstring("library"))
		})

		It("seeds a default for every config field", func() {
			d := defaults()
			for _, path := range leafKoanfPaths(reflect.TypeFor[Config](), "") {
				_, ok := d[path]
				Expect(ok).To(BeTrue(), "defaults() missing key %q", path)
			}
		})

		It("round-trips through Load and validates", func() {
			Expect(os.MkdirAll("./data", 0o755)).To(Succeed())
			DeferCleanup(func() {
				Expect(os.RemoveAll("./data")).To(Succeed())
			})

			cfgFile := filepath.Join(GinkgoT().TempDir(), "config.yaml")
			var buf bytes.Buffer
			Expect(DumpDefaults(&buf)).To(Succeed())
			Expect(os.WriteFile(cfgFile, buf.Bytes(), 0o644)).To(Succeed())

			_, err := Load(cfgFile)
			Expect(err).NotTo(HaveOccurred())
		})
	})
})
