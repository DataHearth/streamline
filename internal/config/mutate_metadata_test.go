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

	It("stores a MusicBrainz mirror and its interval", func() {
		configtest.SetupFile()
		url, interval := "http://musicbrainz:5000", "0s"
		out, err := config.UpdateMetadata(ctx, config.MetadataPatch{
			MusicBrainzURL: &url, MusicBrainzInterval: &interval,
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(out.MusicBrainzURL).To(Equal("http://musicbrainz:5000"))
		Expect(out.MusicBrainzIntervalDuration()).To(BeZero())
	})

	DescribeTable("refuses a MusicBrainz value it could not use",
		func(patch config.MetadataPatch, want string) {
			configtest.SetupFile()
			_, err := config.UpdateMetadata(ctx, patch)
			Expect(err).To(MatchError(ContainSubstring(want)))
			Expect(config.Get().Metadata.MusicBrainzURL).
				To(Equal("https://musicbrainz.org"))
			Expect(config.Get().Metadata.MusicBrainzInterval).To(Equal("1s"))
		},
		Entry("an interval that is not a duration",
			config.MetadataPatch{MusicBrainzInterval: new("1 second")},
			"metadata.musicbrainz_interval"),
		Entry("a negative interval",
			config.MetadataPatch{MusicBrainzInterval: new("-1s")},
			"must not be negative"),
		Entry("a URL that is not http(s)",
			config.MetadataPatch{MusicBrainzURL: new("musicbrainz:5000")},
			"MusicBrainzURL"),
	)
})
