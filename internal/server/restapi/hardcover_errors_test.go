package restapi

import (
	"errors"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/media/book"
	"github.com/datahearth/streamline/internal/metadata"
)

var _ = Describe(
	"errHardcoverUnavailable",
	Label("unit", "server", "restapi"),
	func() {
		DescribeTable(
			"maps the cause to a code",
			func(err error, want string) {
				Expect(providerUnavailable(err)).To(BeTrue())
				Expect(errHardcoverUnavailable(err).Code).To(HaveValue(Equal(want)))
			},
			Entry(
				"service without a provider",
				book.ErrNotConfigured,
				"hardcover_not_configured",
			),
			Entry(
				"client without a key",
				metadata.ErrHardcoverKeyMissing,
				"hardcover_not_configured",
			),
			Entry(
				"wrapped missing key",
				fmt.Errorf("add: %w", metadata.ErrHardcoverKeyMissing),
				"hardcover_not_configured",
			),
			Entry(
				"key refused",
				metadata.ErrHardcoverUnauthorized,
				"hardcover_key_rejected",
			),
			Entry(
				"wrapped refused key",
				fmt.Errorf("approve: %w", metadata.ErrHardcoverUnauthorized),
				"hardcover_key_rejected",
			),
		)

		It("does not take a rate limit for either", func() {
			err := &metadata.RateLimitedError{}
			Expect(providerUnavailable(err)).To(BeFalse())
			Expect(rateLimited(err)).To(BeTrue())
		})

		It("does not take an unrelated failure for either", func() {
			Expect(providerUnavailable(errors.New("boom"))).To(BeFalse())
		})
	},
)

var _ = Describe("metadataConfigView", Label("unit", "server", "restapi"), func() {
	It("reports the Hardcover key without echoing it", func() {
		v := metadataConfigView(config.MetadataConfig{HardcoverAPIKey: "secret"})
		Expect(v.HardcoverApiKeySet).To(BeTrue())
		Expect(*v.HardcoverApiKeyFileManaged).To(BeFalse())
	})

	It("flags a file-managed Hardcover key", func() {
		v := metadataConfigView(
			config.MetadataConfig{HardcoverAPIKeyFile: "/run/secrets/hc"},
		)
		Expect(*v.HardcoverApiKeyFileManaged).To(BeTrue())
	})
})
