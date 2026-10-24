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

var _ = Describe("Download client CRUD", Label("unit", "config"), func() {
	BeforeEach(func() { configtest.SetupFile() })

	entry := func(name string) config.DownloadClientEntry {
		return config.DownloadClientEntry{
			Name: name, ClientType: "qbittorrent", Host: "h", Port: 8080,
			AuthMethod: "password", Password: "secret", Enabled: true,
		}
	}

	It("adds, updates (preserving blank secret), and deletes", func() {
		ctx := context.Background()
		Expect(config.AddDownloadClient(ctx, entry("qbit"))).To(Succeed())
		Expect(config.Get().DownloadClients).To(HaveLen(1))

		Expect(config.AddDownloadClient(ctx, entry("qbit"))).
			To(MatchError(config.ErrDownloadClientExists))

		user, blank := "admin", ""
		Expect(
			config.UpdateDownloadClient(
				ctx,
				"qbit",
				config.DownloadClientPatch{Username: &user, Password: &blank},
			),
		).
			To(Succeed())
		got, _ := config.FindDownloadClient("qbit")
		Expect(got.Username).To(Equal("admin"))
		Expect(got.Password).To(Equal("secret")) // preserved

		Expect(config.DeleteDownloadClient(ctx, "qbit")).To(Succeed())
		Expect(config.Get().DownloadClients).To(BeEmpty())
		Expect(config.DeleteDownloadClient(ctx, "qbit")).
			To(MatchError(config.ErrDownloadClientNotFound))
	})

	It("patches builtin knobs", func() {
		ctx := context.Background()
		Expect(config.AddDownloadClient(ctx, config.DownloadClientEntry{
			Name: "embedded", ClientType: "builtin", DownloadDir: "/downloads",
		})).To(Succeed())
		dir := "/data/torrents"
		ratio := 2.0
		seedTime := "72h"
		iface := "wg0"
		Expect(
			config.UpdateDownloadClient(ctx, "embedded", config.DownloadClientPatch{
				DownloadDir: &dir, SeedRatio: &ratio, SeedTime: &seedTime,
				BindInterface: &iface,
			}),
		).To(Succeed())
		e, ok := config.FindDownloadClient("embedded")
		Expect(ok).To(BeTrue())
		Expect(e.DownloadDir).To(Equal("/data/torrents"))
		Expect(e.SeedRatio).To(Equal(2.0))
		Expect(e.SeedTime).To(Equal("72h"))
		Expect(e.BindInterface).To(Equal("wg0"))
	})

	Describe("moving to another destination", func() {
		ctx := context.Background()

		It("refuses a new host, port or TLS without the password", func() {
			Expect(config.AddDownloadClient(ctx, entry("qbit"))).To(Succeed())
			host, port, ssl := "elsewhere", uint16(9091), true
			for _, p := range []config.DownloadClientPatch{
				{Host: &host}, {Port: &port}, {UseSSL: &ssl},
			} {
				Expect(config.UpdateDownloadClient(ctx, "qbit", p)).
					To(MatchError(config.ErrSecretRequired))
			}
			got, _ := config.FindDownloadClient("qbit")
			Expect(got.Host).To(Equal("h"))
			Expect(got.Password).To(Equal("secret"))
		})

		It("moves with the password entered again", func() {
			Expect(config.AddDownloadClient(ctx, entry("qbit"))).To(Succeed())
			host, pw := "elsewhere", "secret2"
			Expect(config.UpdateDownloadClient(
				ctx,
				"qbit",
				config.DownloadClientPatch{
					Host:     &host,
					Password: &pw,
				},
			)).To(Succeed())
			got, _ := config.FindDownloadClient("qbit")
			Expect(got.Host).To(Equal("elsewhere"))
			Expect(got.Password).To(Equal("secret2"))
		})

		It(
			"asks for the key under api_key auth and drops the unused password",
			func() {
				e := entry("qbit")
				e.AuthMethod = "api_key"
				e.APIKey = "key"
				Expect(config.AddDownloadClient(ctx, e)).To(Succeed())

				host := "elsewhere"
				Expect(config.UpdateDownloadClient(ctx, "qbit",
					config.DownloadClientPatch{Host: &host})).
					To(MatchError(config.ErrSecretRequired))

				key := "key2"
				Expect(config.UpdateDownloadClient(
					ctx,
					"qbit",
					config.DownloadClientPatch{
						Host:   &host,
						APIKey: &key,
					},
				)).To(Succeed())
				got, _ := config.FindDownloadClient("qbit")
				Expect(got.APIKey).To(Equal("key2"))
				Expect(got.Password).To(BeEmpty())
			},
		)

		It("moves a client that stores no secret", func() {
			e := entry("tr")
			e.ClientType = "transmission"
			e.Password = ""
			Expect(config.AddDownloadClient(ctx, e)).To(Succeed())
			host := "elsewhere"
			Expect(config.UpdateDownloadClient(ctx, "tr",
				config.DownloadClientPatch{Host: &host})).To(Succeed())
		})

		It("refuses any move of a file-backed password", func() {
			pwPath := filepath.Join(GinkgoT().TempDir(), "qbit.pw")
			Expect(os.WriteFile(pwPath, []byte("pw\n"), 0o600)).To(Succeed())
			configtest.SetupFile(map[string]any{
				"download_clients": []map[string]any{
					{
						"name": "fb", "client_type": "qbittorrent", "host": "h",
						"port": 8080, "auth_method": "password",
						"password_file": pwPath,
					},
				},
			})

			host := "elsewhere"
			Expect(config.UpdateDownloadClient(ctx, "fb",
				config.DownloadClientPatch{Host: &host})).
				To(MatchError(config.ErrSecretFileDestination))
		})
	})
})
