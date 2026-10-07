package config_test

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/testutil/configtest"
)

var _ = Describe("Audiobook quality profile CRUD", Label("unit", "config"), func() {
	BeforeEach(func() { configtest.SetupFile() })

	entry := func(name string) config.AudiobookQualityProfileEntry {
		return config.AudiobookQualityProfileEntry{
			Name: name, Formats: []string{"m4b", "mp3"}, Cutoff: "m4b",
		}
	}

	It("adds a profile and rejects a duplicate name", func() {
		Expect(
			config.AddAudiobookQualityProfile(context.Background(), entry("main")),
		).To(Succeed())
		_, ok := config.ResolveAudiobookQualityProfile("main")
		Expect(ok).To(BeTrue())

		dup := entry("alt")
		dup.Name = "main"
		err := config.AddAudiobookQualityProfile(context.Background(), dup)
		Expect(err).To(MatchError(config.ErrAudiobookQualityProfileExists))
	})

	It("applies a patch", func() {
		Expect(
			config.AddAudiobookQualityProfile(context.Background(), entry("main")),
		).To(Succeed())
		cutoff := "mp3"
		Expect(config.UpdateAudiobookQualityProfile(
			context.Background(),
			"main",
			config.AudiobookQualityProfilePatch{Cutoff: &cutoff},
		)).To(Succeed())

		p, ok := config.ResolveAudiobookQualityProfile("main")
		Expect(ok).To(BeTrue())
		Expect(p.Cutoff).To(Equal("mp3"))
		Expect(p.Formats).To(Equal([]string{"m4b", "mp3"}))
	})

	It("reports an unknown profile on update", func() {
		err := config.UpdateAudiobookQualityProfile(
			context.Background(), "nope", config.AudiobookQualityProfilePatch{},
		)
		Expect(err).To(MatchError(config.ErrAudiobookQualityProfileNotFound))
	})

	It("deletes a profile once", func() {
		Expect(
			config.AddAudiobookQualityProfile(context.Background(), entry("main")),
		).To(Succeed())
		Expect(
			config.DeleteAudiobookQualityProfile(context.Background(), "main"),
		).To(Succeed())
		err := config.DeleteAudiobookQualityProfile(context.Background(), "main")
		Expect(err).To(MatchError(config.ErrAudiobookQualityProfileNotFound))
	})

	It("refuses to delete the default profile", func() {
		configtest.SetupFile(map[string]any{
			"audiobook_quality_profiles": []map[string]any{
				{"name": "main", "formats": []string{"m4b"}, "cutoff": "m4b"},
			},
			"audiobook_quality_default_profile": "main",
		})
		err := config.DeleteAudiobookQualityProfile(context.Background(), "main")
		Expect(err).To(MatchError(config.ErrAudiobookQualityProfileInUseAsDefault))
	})
})
