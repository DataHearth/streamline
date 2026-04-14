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

		host := "newhost"
		blank := ""
		Expect(
			config.UpdateIndexer(
				ctx,
				"torznab1",
				config.IndexerPatch{Host: &host, APIKey: &blank},
			),
		).
			To(Succeed())
		got, _ := config.FindIndexer("torznab1")
		Expect(got.Host).To(Equal("newhost"))
		Expect(got.APIKey).To(Equal("k")) // preserved

		Expect(config.DeleteIndexer(ctx, "torznab1")).To(Succeed())
		Expect(config.Get().Indexers).To(BeEmpty())
		Expect(config.DeleteIndexer(ctx, "torznab1")).
			To(MatchError(config.ErrIndexerNotFound))
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
})
