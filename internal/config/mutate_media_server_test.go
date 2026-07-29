package config_test

import (
	"context"

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

			host := "http://plex2:32400"
			blank := ""
			section := "Movies"
			Expect(
				config.UpdateMediaServer(ctx, "home-plex", config.MediaServerPatch{
					Host: &host, APIKey: &blank, LibrarySection: &section,
				}),
			).To(Succeed())
			got, _ := config.FindMediaServer("home-plex")
			Expect(got.Host).To(Equal("http://plex2:32400"))
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
})
