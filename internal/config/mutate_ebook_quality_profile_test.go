package config_test

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/testutil/configtest"
)

var _ = Describe("Ebook quality profile CRUD", Label("unit", "config"), func() {
	BeforeEach(func() { configtest.SetupFile() })

	entry := func(name string) config.EbookQualityProfileEntry {
		return config.EbookQualityProfileEntry{
			Name: name, Formats: []string{"epub", "azw3"}, Cutoff: "epub",
		}
	}

	It("adds a profile and rejects a duplicate name", func() {
		Expect(
			config.AddEbookQualityProfile(context.Background(), entry("main")),
		).To(Succeed())
		_, ok := config.ResolveEbookQualityProfile("main")
		Expect(ok).To(BeTrue())

		dup := entry("alt")
		dup.Name = "main"
		err := config.AddEbookQualityProfile(context.Background(), dup)
		Expect(err).To(MatchError(config.ErrEbookQualityProfileExists))
	})

	It("applies a patch", func() {
		Expect(
			config.AddEbookQualityProfile(context.Background(), entry("main")),
		).To(Succeed())
		cutoff := "azw3"
		Expect(config.UpdateEbookQualityProfile(
			context.Background(),
			"main",
			config.EbookQualityProfilePatch{Cutoff: &cutoff},
		)).To(Succeed())

		p, ok := config.ResolveEbookQualityProfile("main")
		Expect(ok).To(BeTrue())
		Expect(p.Cutoff).To(Equal("azw3"))
		Expect(p.Formats).To(Equal([]string{"epub", "azw3"}))
	})

	It("reports an unknown profile on update", func() {
		err := config.UpdateEbookQualityProfile(
			context.Background(), "nope", config.EbookQualityProfilePatch{},
		)
		Expect(err).To(MatchError(config.ErrEbookQualityProfileNotFound))
	})

	It("deletes a profile once", func() {
		Expect(
			config.AddEbookQualityProfile(context.Background(), entry("main")),
		).To(Succeed())
		Expect(
			config.DeleteEbookQualityProfile(context.Background(), "main"),
		).To(Succeed())
		err := config.DeleteEbookQualityProfile(context.Background(), "main")
		Expect(err).To(MatchError(config.ErrEbookQualityProfileNotFound))
	})

	It("refuses to delete the default profile", func() {
		configtest.SetupFile(map[string]any{
			"ebook_quality_profiles": []map[string]any{
				{"name": "main", "formats": []string{"epub"}, "cutoff": "epub"},
			},
			"ebook_quality_default_profile": "main",
		})
		err := config.DeleteEbookQualityProfile(context.Background(), "main")
		Expect(err).To(MatchError(config.ErrEbookQualityProfileInUseAsDefault))
	})
})
