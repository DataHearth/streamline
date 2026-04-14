package config_test

import (
	"context"

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

		host := "newhost"
		Expect(
			config.UpdateDownloadClient(
				ctx,
				"qbit",
				config.DownloadClientPatch{Host: &host},
			),
		).
			To(Succeed())
		got, _ := config.FindDownloadClient("qbit")
		Expect(got.Host).To(Equal("newhost"))
		Expect(got.Password).To(Equal("secret")) // preserved

		Expect(config.DeleteDownloadClient(ctx, "qbit")).To(Succeed())
		Expect(config.Get().DownloadClients).To(BeEmpty())
		Expect(config.DeleteDownloadClient(ctx, "qbit")).
			To(MatchError(config.ErrDownloadClientNotFound))
	})
})
