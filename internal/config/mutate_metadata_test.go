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

var _ = Describe("UpdateMetadata", Label("unit", "config"), func() {
	ctx := context.Background()

	It("stores a Hardcover key and preserves it on a blank patch", func() {
		configtest.SetupFile()
		key := "hc-key"
		out, err := config.UpdateMetadata(
			ctx,
			config.MetadataPatch{HardcoverAPIKey: &key},
		)
		Expect(err).NotTo(HaveOccurred())
		Expect(out.HardcoverAPIKey).To(Equal("hc-key"))

		blank := " "
		out, err = config.UpdateMetadata(
			ctx,
			config.MetadataPatch{HardcoverAPIKey: &blank},
		)
		Expect(err).NotTo(HaveOccurred())
		Expect(out.HardcoverAPIKey).To(Equal("hc-key"))
	})

	It("refuses an inline Hardcover key while a key file owns it", func() {
		keyPath := filepath.Join(GinkgoT().TempDir(), "hc.key")
		Expect(os.WriteFile(keyPath, []byte("file-key\n"), 0o600)).To(Succeed())
		configtest.SetupFile(map[string]any{
			"metadata": map[string]any{"hardcover_api_key_file": keyPath},
		})

		key := "inline"
		_, err := config.UpdateMetadata(
			ctx,
			config.MetadataPatch{HardcoverAPIKey: &key},
		)
		Expect(err).To(MatchError(config.ErrSecretFileManaged))
	})
})
