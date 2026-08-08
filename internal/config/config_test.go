package config

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// leafKoanfPaths walks a config struct and returns the dotted koanf path of
// every leaf field (basic types and slices). Nested structs are recursed;
// fields without a koanf tag (e.g. the unexported pinned map) are skipped.
//
// A slice is a leaf even when its elements are structs, and deliberately: a
// list's default is the empty list, and its elements have no koanf path until
// an operator writes one, so there is no auth.oidc.allow_admin for defaults()
// to seed. The consequence is that the "seeds a default for every config field"
// spec below asserts nothing about any per-element key — auth.oidc[],
// indexers[], download_clients[], quality_profiles[] and
// media_server.servers[] are covered instead by "declares every field of every
// config list element" in schema_test.go, which walks the same structs against
// api/config.schema.json.
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

		Context("with pre-media-split schedules keys", func() {
			// Each renamed key drove one job; missing_search/metadata_refresh/
			// orphan_scan drove two, so the old value has to reach both halves.
			renamed := func(dataDir string) string {
				return "data_dir: " + dataDir + `
schedules:
  rss_sync: 5m
  missing_search: 3h
  metadata_refresh: 48h
  orphan_scan: 9h
`
			}

			It("carries an old interval onto every replacement key", func() {
				Expect(LoadReader(
					strings.NewReader(renamed(GinkgoT().TempDir())),
				)).To(Succeed())

				s := Get().Schedule
				Expect(s.MovieRSSSync).To(Equal("5m"))
				Expect(s.MovieMissingSearch).To(Equal("3h"))
				Expect(s.TVMissingSearch).To(Equal("3h"))
				Expect(s.MovieMetadataRefresh).To(Equal("48h"))
				Expect(s.TVMetadataRefresh).To(Equal("48h"))
				Expect(s.MovieOrphanScan).To(Equal("9h"))
				Expect(s.TVOrphanScan).To(Equal("9h"))
			})

			It("warns, naming the old key and its replacements", func() {
				var logs bytes.Buffer
				GinkgoWriter.TeeTo(&logs)
				DeferCleanup(GinkgoWriter.ClearTeeWriters)

				Expect(LoadReader(
					strings.NewReader(renamed(GinkgoT().TempDir())),
				)).To(Succeed())

				Expect(logs.String()).To(ContainSubstring("schedules.rss_sync"))
				Expect(
					logs.String(),
				).To(ContainSubstring("schedules.movie_rss_sync"))
			})

			It("lets an explicitly set replacement win over the old key", func() {
				raw := "data_dir: " + GinkgoT().TempDir() + `
schedules:
  missing_search: 3h
  tv_missing_search: 30m
`
				Expect(LoadReader(strings.NewReader(raw))).To(Succeed())

				s := Get().Schedule
				Expect(s.TVMissingSearch).To(Equal("30m"))
				Expect(s.MovieMissingSearch).To(Equal("3h"))
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

		It("defaults auth.trusted_role away from admin", func() {
			// trusted-network mode hands this role out on an IP match with no
			// credentials at all, so the default must not be admin.
			cfg, err := Load("")
			Expect(err).ToNot(HaveOccurred())
			Expect(cfg.Auth.TrustedRole).To(Equal("member"))
		})

		// The two OIDC trust axes both have to default closed, and separately.
		Describe("auth.oidc trust axes", func() {
			loadProvider := func(extra string) *Config {
				GinkgoHelper()
				dir := GinkgoT().TempDir()
				dataDir := filepath.Join(dir, "data")
				Expect(os.MkdirAll(dataDir, 0o755)).To(Succeed())
				cfgFile := filepath.Join(dir, "config.yaml")
				Expect(os.WriteFile(cfgFile, []byte("data_dir: "+dataDir+`
auth:
  oidc:
    - name: kc
      issuer: https://kc.example.com
      client_id: streamline
      client_secret: secret
`+extra), 0o600)).To(Succeed())
				cfg, err := Load(cfgFile)
				Expect(err).ToNot(HaveOccurred())
				Expect(cfg.Auth.OIDC).To(HaveLen(1))
				return cfg
			}

			It("defaults a provider to no adoption and no admin", func() {
				cfg := loadProvider("")
				Expect(cfg.Auth.OIDC[0].EmailLinking).
					To(Equal(OIDCEmailLinkingDisabled))
				Expect(cfg.Auth.OIDC[0].AllowAdmin).To(BeFalse())
			})

			It("keeps allow_admin independent of the adoption tier", func() {
				cfg := loadProvider("      allow_admin: true\n")
				Expect(cfg.Auth.OIDC[0].EmailLinking).
					To(Equal(OIDCEmailLinkingDisabled))
				Expect(cfg.Auth.OIDC[0].AllowAdmin).To(BeTrue())
			})

			It("leaves allow_admin off for the loosest adoption tier", func() {
				cfg := loadProvider("      email_linking: all\n")
				Expect(cfg.Auth.OIDC[0].EmailLinking).
					To(Equal(OIDCEmailLinkingAll))
				Expect(cfg.Auth.OIDC[0].AllowAdmin).To(BeFalse())
			})
		})

		// Two entries sharing a name split the settings that authenticate a
		// token from the settings that bound what it may become: findOIDCProvider
		// reads the first match, oidcManager.Init keyed the map so the last write
		// won. A token from the second entry's issuer was then capped by the
		// first entry's allow_admin.
		Describe("duplicate names in name-keyed lists", func() {
			loadWith := func(body string) error {
				GinkgoHelper()
				dir := GinkgoT().TempDir()
				dataDir := filepath.Join(dir, "data")
				Expect(os.MkdirAll(dataDir, 0o755)).To(Succeed())
				cfgFile := filepath.Join(dir, "config.yaml")
				Expect(os.WriteFile(
					cfgFile,
					[]byte("data_dir: "+dataDir+"\n"+body),
					0o600,
				)).To(Succeed())
				_, err := Load(cfgFile)
				return err
			}

			It("rejects two auth.oidc providers sharing a name", func() {
				err := loadWith(`auth:
  oidc:
    - name: kc
      issuer: https://strong.example.com
      client_id: streamline
      client_secret: secret
      allow_admin: true
    - name: kc
      issuer: https://weak.example.com
      client_id: streamline
      client_secret: secret
`)
				Expect(err).To(MatchError(ContainSubstring("kc")))
				Expect(err).To(MatchError(ContainSubstring("auth.oidc")))
			})

			It("accepts two providers with distinct names", func() {
				Expect(loadWith(`auth:
  oidc:
    - name: kc
      issuer: https://strong.example.com
      client_id: streamline
      client_secret: secret
    - name: authentik
      issuer: https://weak.example.com
      client_id: streamline
      client_secret: secret
`)).To(Succeed())
			})

			// The other four lists carry a `unique=Name` tag. Asserting on the
			// tag name keeps these from passing on some unrelated validation
			// error in the fixture instead of on the duplicate.
			It("rejects two indexers sharing a name", func() {
				Expect(loadWith(`indexers:
  - name: nzb
    host: a.example.com
    port: 443
    api_key: a
    protocol: torznab
  - name: nzb
    host: b.example.com
    port: 443
    api_key: b
    protocol: torznab
`)).To(MatchError(ContainSubstring("'unique' tag")))
			})

			It("rejects two download clients sharing a name", func() {
				Expect(loadWith(`download_clients:
  - name: qb
    client_type: qbittorrent
    host: a.example.com
    port: 8080
    auth_method: password
  - name: qb
    client_type: qbittorrent
    host: b.example.com
    port: 8080
    auth_method: password
`)).To(MatchError(ContainSubstring("'unique' tag")))
			})

			It("rejects two quality profiles sharing a name", func() {
				Expect(loadWith(`quality_default_profile: default
quality_profiles:
  - name: default
    preferred_resolution: 1080p
    min_resolution: 1080p
  - name: default
    preferred_resolution: 720p
    min_resolution: 720p
`)).To(MatchError(ContainSubstring("'unique' tag")))
			})

			It("rejects two media servers sharing a name", func() {
				Expect(loadWith(`media_server:
  servers:
    - name: plex
      server_type: plex
      host: a.example.com
      api_key: a
    - name: plex
      server_type: plex
      host: b.example.com
      api_key: b
`)).To(MatchError(ContainSubstring("'unique' tag")))
			})
		})

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

		Context("server.trusted_proxies", func() {
			loadProxies := func(entry string) error {
				GinkgoHelper()
				dir := GinkgoT().TempDir()
				cfgFile := filepath.Join(dir, "config.yaml")
				dataDir := filepath.Join(dir, "data")
				Expect(os.MkdirAll(dataDir, 0o755)).To(Succeed())
				yaml := "data_dir: " + dataDir +
					"\nserver:\n  trusted_proxies:\n    - \"" + entry + "\"\n"
				Expect(os.WriteFile(cfgFile, []byte(yaml), 0o644)).To(Succeed())
				_, err := Load(cfgFile)
				return err
			}

			It("defaults to trusting nothing", func() {
				cfg, err := Load("")
				Expect(err).ToNot(HaveOccurred())
				Expect(cfg.Server.TrustedProxies).To(BeEmpty())
			})

			It("accepts a plain CIDR", func() {
				Expect(loadProxies("10.1.0.0/16")).To(Succeed())
			})

			It("rejects a non-CIDR entry", func() {
				Expect(loadProxies("10.1.0.5")).To(HaveOccurred())
			})

			It("rejects an IPv4-mapped range that would never match", func() {
				Expect(loadProxies("::ffff:10.1.0.0/112")).To(HaveOccurred())
			})
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

// Nothing written to the config file can make an environment-supplied setting
// outlive its variable, so Load says out loud what the environment owns while
// it still holds it — and shouts on the boot where a setting that decides where
// data goes fell back to the built-in default, which is the boot where an
// install that lost STREAMLINE_DATA_DIR comes up on an empty database.
var _ = Describe("Load provenance signals", Label("unit", "config"), func() {
	var logs bytes.Buffer

	BeforeEach(func() {
		ResetForTest()
		DeferCleanup(ResetForTest)
		logs.Reset()
		GinkgoWriter.TeeTo(&logs)
		DeferCleanup(GinkgoWriter.ClearTeeWriters)
	})

	// data_dir resolves to ./data relative to this package's directory.
	expectDefaultDataDir := func() {
		GinkgoHelper()
		DeferCleanup(func() {
			Expect(os.RemoveAll("./data")).To(Succeed())
		})
	}

	It(
		"warns when data_dir comes from neither the file nor the environment",
		func() {
			expectDefaultDataDir()

			Expect(LoadReader(strings.NewReader("auth:\n  mode: disabled\n"))).
				To(Succeed())

			Expect(logs.String()).To(ContainSubstring("data_dir=./data"))
			Expect(logs.String()).To(ContainSubstring("database.path"))
		},
	)

	It("stays quiet about data_dir when the environment supplies it", func() {
		GinkgoT().Setenv("STREAMLINE_DATA_DIR", GinkgoT().TempDir())

		Expect(LoadReader(strings.NewReader("auth:\n  mode: disabled\n"))).
			To(Succeed())

		Expect(logs.String()).ToNot(ContainSubstring("data_dir="))
		Expect(logs.String()).ToNot(ContainSubstring("database.path"))
	})

	It("stays quiet about data_dir when the file supplies it", func() {
		raw := "data_dir: " + GinkgoT().TempDir() + "\n"

		Expect(LoadReader(strings.NewReader(raw))).To(Succeed())

		Expect(logs.String()).ToNot(ContainSubstring("data_dir="))
		Expect(logs.String()).ToNot(ContainSubstring("database.path"))
	})

	// An env-only setting that is simply gone at the next boot leaves no trace
	// of ever having been set, so the warning has to come from the one thing
	// that boot can still see: the value it is actually running on being the
	// built-in default.
	It(
		"warns for every watched setting that fell back to its default, not just data_dir",
		func() {
			raw := "data_dir: " + GinkgoT().TempDir() + "\n"

			Expect(LoadReader(strings.NewReader(raw))).To(Succeed())

			Expect(logs.String()).
				To(ContainSubstring("library.movie_path=/media/movies"))
			Expect(logs.String()).
				To(ContainSubstring("library.download_path=/downloads"))
			Expect(logs.String()).To(ContainSubstring("auth.session_ttl=168h"))
		},
	)

	It("stays quiet about a watched setting the file supplies", func() {
		raw := "data_dir: " + GinkgoT().TempDir() +
			"\nlibrary:\n  movie_path: /mnt/bigdisk/movies\n"

		Expect(LoadReader(strings.NewReader(raw))).To(Succeed())

		Expect(logs.String()).ToNot(ContainSubstring("library.movie_path="))
	})

	// The file carrying its own value is the case warnDefaultedKeys can never
	// catch: with the variable gone that boot looks exactly like an install
	// that was always configured that way, so the only boot that can say
	// anything is this one.
	It("warns when the environment shadows a value the file also sets", func() {
		dir := GinkgoT().TempDir()
		fileDir := filepath.Join(dir, "filedir")
		envDir := filepath.Join(dir, "envdir")
		Expect(os.MkdirAll(fileDir, 0o755)).To(Succeed())
		Expect(os.MkdirAll(envDir, 0o755)).To(Succeed())
		GinkgoT().Setenv("STREAMLINE_DATA_DIR", envDir)

		Expect(LoadReader(strings.NewReader("data_dir: " + fileDir + "\n"))).
			To(Succeed())
		Expect(Get().DataDir).To(Equal(envDir))

		Expect(logs.String()).To(ContainSubstring("config.file.shadowed"))
		Expect(logs.String()).To(ContainSubstring("data_dir=" + fileDir))
	})

	It("stays quiet when the environment and the file agree", func() {
		dataDir := GinkgoT().TempDir()
		GinkgoT().Setenv("STREAMLINE_DATA_DIR", dataDir)

		Expect(LoadReader(strings.NewReader("data_dir: " + dataDir + "\n"))).
			To(Succeed())

		Expect(logs.String()).ToNot(ContainSubstring("config.file.shadowed"))
	})

	It("names the keys the environment supplied, never their values", func() {
		GinkgoT().Setenv("STREAMLINE_METADATA__TMDB_API_KEY", "env-only-tmdb-key")
		raw := "data_dir: " + GinkgoT().TempDir() + "\n"

		Expect(LoadReader(strings.NewReader(raw))).To(Succeed())

		Expect(logs.String()).To(ContainSubstring("metadata.tmdb_api_key"))
		Expect(logs.String()).ToNot(ContainSubstring("env-only-tmdb-key"))
	})

	It("leaves out STREAMLINE_ variables that name no config key", func() {
		GinkgoT().Setenv("STREAMLINE_PUBLIC_URL", "https://stream.example.com")
		GinkgoT().Setenv("STREAMLINE_E2E_CONTAINERS", "1")
		GinkgoT().Setenv("STREAMLINE_METADATA__TMDB__BASE_URL", "http://127.0.0.1:9")
		raw := "data_dir: " + GinkgoT().TempDir() + "\n"

		Expect(LoadReader(strings.NewReader(raw))).To(Succeed())

		Expect(logs.String()).ToNot(ContainSubstring("public_url"))
		Expect(logs.String()).ToNot(ContainSubstring("e2e_containers"))
		Expect(logs.String()).ToNot(ContainSubstring("metadata.tmdb.base_url"))
	})

	// The keys a deprecated alias expands to are appended while ranging over a
	// map, so the list came out in a different order on every run — and named
	// the deprecated key itself, which is no config key at all.
	It("names the environment's keys in a stable order", func() {
		GinkgoT().Setenv("STREAMLINE_SCHEDULES__MISSING_SEARCH", "3h")
		GinkgoT().Setenv("STREAMLINE_SCHEDULES__ORPHAN_SCAN", "9h")
		raw := "data_dir: " + GinkgoT().TempDir() + "\n"

		Expect(LoadReader(strings.NewReader(raw))).To(Succeed())

		Expect(logs.String()).To(ContainSubstring(
			"schedules.movie_missing_search, schedules.movie_orphan_scan, " +
				"schedules.tv_missing_search, schedules.tv_orphan_scan",
		))
	})

	// The environment is always a string and the file holds a list as []any, so
	// rendering both and comparing the text reported a key as shadowed by the
	// value it already had — on every boot, for a setting nothing was overriding.
	It("stays quiet when a list the file sets matches the environment", func() {
		GinkgoT().
			Setenv("STREAMLINE_LIBRARY__ALLOWED_DOWNLOAD_ROOTS", "/downloads")
		raw := "data_dir: " + GinkgoT().TempDir() +
			"\nlibrary:\n  allowed_download_roots:\n    - /downloads\n"

		Expect(LoadReader(strings.NewReader(raw))).To(Succeed())
		Expect(Get().Library.AllowedDownloadRoots).To(ConsistOf("/downloads"))

		Expect(logs.String()).ToNot(ContainSubstring("config.file.shadowed"))
	})

	It("still warns when a list the file sets differs from the environment", func() {
		GinkgoT().
			Setenv("STREAMLINE_LIBRARY__ALLOWED_DOWNLOAD_ROOTS", "/mnt/other")
		raw := "data_dir: " + GinkgoT().TempDir() +
			"\nlibrary:\n  allowed_download_roots:\n    - /downloads\n"

		Expect(LoadReader(strings.NewReader(raw))).To(Succeed())

		Expect(logs.String()).To(ContainSubstring("config.file.shadowed"))
		Expect(logs.String()).
			To(ContainSubstring("library.allowed_download_roots=[/downloads]"))
	})

	// A file that only loads because a variable fills a gap starts every boot
	// one dropped variable away from not starting at all. The write-back is not
	// what put it in that state and does not report it to whoever is watching
	// the logs at boot, so Load says it too.
	It("warns when the file does not load without the environment", func() {
		GinkgoT().Setenv("STREAMLINE_QUALITY_DEFAULT_PROFILE", "uhd-remux")
		raw := "data_dir: " + GinkgoT().TempDir() + `
quality_profiles:
  - name: uhd-remux
    preferred_resolution: 2160p
    min_resolution: 2160p
    upgrade_allowed: true
`

		Expect(LoadReader(strings.NewReader(raw))).To(Succeed())

		Expect(logs.String()).
			To(ContainSubstring("does not load without the environment"))
		Expect(logs.String()).To(ContainSubstring("quality_default_profile"))
	})

	It("stays quiet when the file loads on its own", func() {
		GinkgoT().Setenv("STREAMLINE_METADATA__TMDB_API_KEY", "env-only-tmdb-key")
		raw := "data_dir: " + GinkgoT().TempDir() + "\n"

		Expect(LoadReader(strings.NewReader(raw))).To(Succeed())

		Expect(logs.String()).
			ToNot(ContainSubstring("does not load without the environment"))
	})

	// The boot that loses STREAMLINE_DATA_DIR is the one that comes up on an
	// empty database, and it is also the one that cannot name where the old one
	// went. What it can do is look at the data_dirs its own config names.
	It("warns when a database sits outside the configured data_dir", func() {
		dir := GinkgoT().TempDir()
		fileDir := filepath.Join(dir, "filedir")
		envDir := filepath.Join(dir, "envdir")
		Expect(os.MkdirAll(fileDir, 0o755)).To(Succeed())
		Expect(os.MkdirAll(envDir, 0o755)).To(Succeed())
		Expect(os.WriteFile(
			filepath.Join(fileDir, "streamline.db"), []byte("db"), 0o600,
		)).To(Succeed())
		GinkgoT().Setenv("STREAMLINE_DATA_DIR", envDir)

		Expect(LoadReader(strings.NewReader("data_dir: " + fileDir + "\n"))).
			To(Succeed())

		Expect(logs.String()).To(ContainSubstring("database.elsewhere"))
		Expect(logs.String()).
			To(ContainSubstring(filepath.Join(fileDir, "streamline.db")))
	})

	It("stays quiet when the configured data_dir holds the database", func() {
		dir := GinkgoT().TempDir()
		Expect(os.WriteFile(
			filepath.Join(dir, "streamline.db"), []byte("db"), 0o600,
		)).To(Succeed())

		Expect(LoadReader(strings.NewReader("data_dir: " + dir + "\n"))).
			To(Succeed())

		Expect(logs.String()).ToNot(ContainSubstring("database.elsewhere"))
	})
})
