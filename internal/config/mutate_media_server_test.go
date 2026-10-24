package config_test

import (
	"context"
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/testutil/configtest"
)

var _ = Describe("Media server CRUD", Label("unit", "config"), func() {
	BeforeEach(func() { configtest.SetupFile() })

	entry := func(name string) config.MediaServerEntry {
		return config.MediaServerEntry{
			Name: name, ServerType: "plex", Host: "http://plex:32400",
			APIKey: "tok", Enabled: true,
		}
	}

	It(
		"adds, updates (preserving blank api_key, setting library section), and deletes",
		func() {
			ctx := context.Background()
			Expect(config.AddMediaServer(ctx, entry("home-plex"))).To(Succeed())
			Expect(config.Get().MediaServer.Servers).To(HaveLen(1))

			Expect(config.AddMediaServer(ctx, entry("home-plex"))).
				To(MatchError(config.ErrMediaServerExists))

			host := "http://Plex:32400/"
			blank := ""
			section := "Movies"
			Expect(
				config.UpdateMediaServer(ctx, "home-plex", config.MediaServerPatch{
					Host: &host, APIKey: &blank, LibrarySection: &section,
				}),
			).To(Succeed())
			got, _ := config.FindMediaServer("home-plex")
			Expect(got.Host).To(Equal("http://Plex:32400/"))
			Expect(got.APIKey).To(Equal("tok")) // preserved
			Expect(got.LibrarySection).NotTo(BeNil())
			Expect(*got.LibrarySection).To(Equal("Movies"))

			Expect(config.DeleteMediaServer(ctx, "home-plex")).To(Succeed())
			Expect(config.Get().MediaServer.Servers).To(BeEmpty())
			Expect(config.DeleteMediaServer(ctx, "home-plex")).
				To(MatchError(config.ErrMediaServerNotFound))
		},
	)

	Describe("library_section is Plex-only", func() {
		ctx := context.Background()
		section := "Movies"

		jellyfin := func() config.MediaServerEntry {
			return config.MediaServerEntry{
				Name: "jf", ServerType: "jellyfin", Host: "http://jf",
				APIKey: "tok", Enabled: true,
			}
		}

		It("rejects the field on a non-Plex add", func() {
			e := jellyfin()
			e.LibrarySection = &section
			Expect(config.AddMediaServer(ctx, e)).
				To(MatchError(config.ErrLibrarySectionNotPlex))
			Expect(config.Get().MediaServer.Servers).To(BeEmpty())
		})

		It("rejects a patch setting the field on a non-Plex server", func() {
			Expect(config.AddMediaServer(ctx, jellyfin())).To(Succeed())

			Expect(config.UpdateMediaServer(ctx, "jf", config.MediaServerPatch{
				LibrarySection: &section,
			})).To(MatchError(config.ErrLibrarySectionNotPlex))

			got, _ := config.FindMediaServer("jf")
			Expect(got.LibrarySection).To(BeNil())
		})

		It("rejects a type switch that would strand the field", func() {
			Expect(config.AddMediaServer(ctx, entry("home-plex"))).To(Succeed())
			Expect(
				config.UpdateMediaServer(ctx, "home-plex", config.MediaServerPatch{
					LibrarySection: &section,
				}),
			).To(Succeed())

			jf := "jellyfin"
			Expect(
				config.UpdateMediaServer(ctx, "home-plex", config.MediaServerPatch{
					ServerType: &jf,
				}),
			).To(MatchError(config.ErrLibrarySectionNotPlex))

			got, _ := config.FindMediaServer("home-plex")
			Expect(got.ServerType).To(Equal("plex"))
		})

		It("allows the type switch when the patch clears the field", func() {
			Expect(config.AddMediaServer(ctx, entry("home-plex"))).To(Succeed())
			Expect(
				config.UpdateMediaServer(ctx, "home-plex", config.MediaServerPatch{
					LibrarySection: &section,
				}),
			).To(Succeed())

			jf := "jellyfin"
			blank := ""
			Expect(
				config.UpdateMediaServer(ctx, "home-plex", config.MediaServerPatch{
					ServerType: &jf, LibrarySection: &blank,
				}),
			).To(Succeed())

			got, _ := config.FindMediaServer("home-plex")
			Expect(got.ServerType).To(Equal("jellyfin"))
			Expect(got.LibrarySection).To(BeNil())
		})
	})

	Describe("moving to another destination", func() {
		ctx := context.Background()

		It("refuses a new scheme, host or port without the token", func() {
			Expect(config.AddMediaServer(ctx, entry("plex"))).To(Succeed())
			blank := ""
			for _, host := range []string{
				"https://plex:32400", "http://plex2:32400", "http://plex:32401",
				"http://plex",
			} {
				Expect(config.UpdateMediaServer(ctx, "plex", config.MediaServerPatch{
					Host: &host, APIKey: &blank,
				})).To(MatchError(config.ErrSecretRequired), host)
			}
			got, _ := config.FindMediaServer("plex")
			Expect(got.Host).To(Equal("http://plex:32400"))
			Expect(got.APIKey).To(Equal("tok"))
		})

		It("does not count a default port or a path as a move", func() {
			e := entry("jf")
			e.Host = "https://jf.example"
			Expect(config.AddMediaServer(ctx, e)).To(Succeed())
			host := "https://jf.example:443/jellyfin"
			Expect(config.UpdateMediaServer(ctx, "jf",
				config.MediaServerPatch{Host: &host})).To(Succeed())
			got, _ := config.FindMediaServer("jf")
			Expect(got.APIKey).To(Equal("tok"))
		})

		It("moves with the token entered again", func() {
			Expect(config.AddMediaServer(ctx, entry("plex"))).To(Succeed())
			host, tok := "http://plex2:32400", "tok2"
			Expect(config.UpdateMediaServer(ctx, "plex",
				config.MediaServerPatch{Host: &host, APIKey: &tok})).To(Succeed())
			got, _ := config.FindMediaServer("plex")
			Expect(got.Host).To(Equal("http://plex2:32400"))
			Expect(got.APIKey).To(Equal("tok2"))
		})

		It("refuses any move of a file-backed token", func() {
			keyPath := filepath.Join(GinkgoT().TempDir(), "plex.key")
			Expect(os.WriteFile(keyPath, []byte("tok\n"), 0o600)).To(Succeed())
			configtest.SetupFile(map[string]any{
				"media_server": map[string]any{
					"servers": []map[string]any{
						{
							"name": "fb", "server_type": "plex",
							"host": "http://plex:32400", "api_key_file": keyPath,
							"enabled": true,
						},
					},
				},
			})

			host := "http://plex2:32400"
			Expect(config.UpdateMediaServer(ctx, "fb",
				config.MediaServerPatch{Host: &host})).
				To(MatchError(config.ErrSecretFileDestination))
		})
	})
})
