package config_test

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/testutil/configtest"
)

var _ = Describe("Quality profile CRUD", Label("unit", "config"), func() {
	BeforeEach(func() { configtest.SetupFile() })

	entry := func(name string) config.QualityProfileEntry {
		return config.QualityProfileEntry{
			Name: name, PreferredResolution: "2160p", MinResolution: "1080p",
		}
	}

	It("adds, updates, and deletes a non-default profile", func() {
		ctx := context.Background()
		Expect(config.AddQualityProfile(ctx, entry("uhd"))).To(Succeed())
		_, ok := config.ResolveQualityProfile("uhd")
		Expect(ok).To(BeTrue())

		Expect(config.AddQualityProfile(ctx, entry("uhd"))).
			To(MatchError(config.ErrQualityProfileExists))

		pref := "1080p"
		Expect(
			config.UpdateQualityProfile(
				ctx,
				"uhd",
				config.QualityProfilePatch{PreferredResolution: &pref},
			),
		).
			To(Succeed())
		p, _ := config.ResolveQualityProfile("uhd")
		Expect(p.PreferredResolution).To(Equal("1080p"))

		Expect(config.DeleteQualityProfile(ctx, "uhd")).To(Succeed())
		Expect(config.DeleteQualityProfile(ctx, "uhd")).
			To(MatchError(config.ErrQualityProfileNotFound))
	})

	It("blocks deleting the default profile", func() {
		Expect(config.DeleteQualityProfile(context.Background(), "default")).
			To(MatchError(config.ErrQualityProfileInUseAsDefault))
	})
})
