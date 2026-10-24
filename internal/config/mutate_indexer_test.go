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

var _ = Describe("Indexer CRUD", Label("unit", "config"), func() {
	BeforeEach(func() { configtest.SetupFile() })

	entry := func(name string) config.IndexerEntry {
		return config.IndexerEntry{
			Name: name, Host: "h", Port: 9117, APIKey: "k",
			Protocol: "torznab", Enabled: true,
		}
	}

	It("adds, updates (preserving blank api_key), and deletes", func() {
		ctx := context.Background()
		Expect(config.AddIndexer(ctx, entry("torznab1"))).To(Succeed())
		Expect(config.Get().Indexers).To(HaveLen(1))

		Expect(config.AddIndexer(ctx, entry("torznab1"))).
			To(MatchError(config.ErrIndexerExists))

		path := "/api/v2"
		blank := ""
		Expect(
			config.UpdateIndexer(
				ctx,
				"torznab1",
				config.IndexerPatch{Path: &path, APIKey: &blank},
			),
		).
			To(Succeed())
		got, _ := config.FindIndexer("torznab1")
		Expect(got.Path).To(Equal("/api/v2"))
		Expect(got.APIKey).To(Equal("k")) // preserved

		Expect(config.DeleteIndexer(ctx, "torznab1")).To(Succeed())
		Expect(config.Get().Indexers).To(BeEmpty())
		Expect(config.DeleteIndexer(ctx, "torznab1")).
			To(MatchError(config.ErrIndexerNotFound))
	})

	It("updates private and leaves it alone when the patch omits it", func() {
		ctx := context.Background()
		Expect(config.AddIndexer(ctx, entry("tracker"))).To(Succeed())
		got, _ := config.FindIndexer("tracker")
		Expect(got.Private).To(BeFalse())

		private := true
		Expect(config.UpdateIndexer(ctx, "tracker",
			config.IndexerPatch{Private: &private})).To(Succeed())
		got, _ = config.FindIndexer("tracker")
		Expect(got.Private).To(BeTrue())

		priority := uint8(5)
		Expect(config.UpdateIndexer(ctx, "tracker",
			config.IndexerPatch{Priority: &priority})).To(Succeed())
		got, _ = config.FindIndexer("tracker")
		Expect(got.Private).To(BeTrue())
	})

	It("loads private from the config file", func() {
		configtest.Setup(map[string]any{
			"indexers": []map[string]any{
				{
					"name": "tracker", "host": "h", "port": 9117,
					"protocol": "torznab", "api_key": "k", "private": true,
				},
			},
		})
		got, ok := config.FindIndexer("tracker")
		Expect(ok).To(BeTrue())
		Expect(got.Private).To(BeTrue())
	})

	It("rejects setting api_key inline on a file-backed indexer", func() {
		ctx := context.Background()
		keyPath := filepath.Join(GinkgoT().TempDir(), "idx.key")
		Expect(os.WriteFile(keyPath, []byte("file-key\n"), 0o600)).To(Succeed())
		configtest.SetupFile(map[string]any{
			"indexers": []map[string]any{
				{
					"name": "fb", "host": "h", "port": 9117,
					"protocol": "torznab", "api_key_file": keyPath,
				},
			},
		})

		key := "newkey"
		Expect(config.UpdateIndexer(ctx, "fb", config.IndexerPatch{APIKey: &key})).
			To(MatchError(config.ErrSecretFileManaged))
	})

	Describe("moving to another destination", func() {
		ctx := context.Background()

		It("refuses a new host, port or TLS without the key", func() {
			Expect(config.AddIndexer(ctx, entry("idx"))).To(Succeed())
			host, port, ssl, blank := "elsewhere", uint16(9696), true, ""
			for _, p := range []config.IndexerPatch{
				{Host: &host, APIKey: &blank},
				{Port: &port},
				{UseSSL: &ssl},
			} {
				Expect(config.UpdateIndexer(ctx, "idx", p)).
					To(MatchError(config.ErrSecretRequired))
			}
			got, _ := config.FindIndexer("idx")
			Expect(got.Host).To(Equal("h"))
			Expect(got.Port).To(Equal(uint16(9117)))
			Expect(got.UseSSL).To(BeFalse())
			Expect(got.APIKey).To(Equal("k"))
		})

		It("moves with the key entered again", func() {
			Expect(config.AddIndexer(ctx, entry("idx"))).To(Succeed())
			host, key := "elsewhere", "k2"
			Expect(config.UpdateIndexer(ctx, "idx",
				config.IndexerPatch{Host: &host, APIKey: &key})).To(Succeed())
			got, _ := config.FindIndexer("idx")
			Expect(got.Host).To(Equal("elsewhere"))
			Expect(got.APIKey).To(Equal("k2"))
		})

		It("does not count a letter case change as a move", func() {
			Expect(config.AddIndexer(ctx, entry("idx"))).To(Succeed())
			host := "H"
			Expect(config.UpdateIndexer(ctx, "idx",
				config.IndexerPatch{Host: &host})).To(Succeed())
			got, _ := config.FindIndexer("idx")
			Expect(got.APIKey).To(Equal("k"))
		})

		It("refuses any move of a file-backed key", func() {
			keyPath := filepath.Join(GinkgoT().TempDir(), "idx.key")
			Expect(os.WriteFile(keyPath, []byte("file-key\n"), 0o600)).To(Succeed())
			configtest.SetupFile(map[string]any{
				"indexers": []map[string]any{
					{
						"name": "fb", "host": "h", "port": 9117,
						"protocol": "torznab", "api_key_file": keyPath,
					},
				},
			})

			host := "elsewhere"
			Expect(
				config.UpdateIndexer(ctx, "fb", config.IndexerPatch{Host: &host}),
			).
				To(MatchError(config.ErrSecretFileDestination))
			got, _ := config.FindIndexer("fb")
			Expect(got.Host).To(Equal("h"))
		})
	})
})
