package config_test

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/testutil/configtest"
)

var _ = Describe("Music quality profile CRUD", Label("unit", "config"), func() {
	BeforeEach(func() { configtest.SetupFile() })

	entry := func(name string) config.MusicQualityProfileEntry {
		return config.MusicQualityProfileEntry{
			Name: name, Formats: []string{"flac", "mp3-320"}, Cutoff: "flac",
		}
	}

	It("adds a profile and rejects a duplicate name", func() {
		Expect(
			config.AddMusicQualityProfile(context.Background(), entry("lossless")),
		).To(Succeed())
		_, ok := config.ResolveMusicQualityProfile("lossless")
		Expect(ok).To(BeTrue())

		dup := entry("lossy")
		dup.Name = "lossless"
		err := config.AddMusicQualityProfile(context.Background(), dup)
		Expect(err).To(MatchError(config.ErrMusicQualityProfileExists))
	})

	It("applies a patch", func() {
		Expect(
			config.AddMusicQualityProfile(context.Background(), entry("lossless")),
		).To(Succeed())
		cutoff := "mp3-320"
		Expect(config.UpdateMusicQualityProfile(
			context.Background(),
			"lossless",
			config.MusicQualityProfilePatch{Cutoff: &cutoff},
		)).To(Succeed())

		p, ok := config.ResolveMusicQualityProfile("lossless")
		Expect(ok).To(BeTrue())
		Expect(p.Cutoff).To(Equal("mp3-320"))
		Expect(p.Formats).To(Equal([]string{"flac", "mp3-320"}))
	})

	It("reports an unknown profile on update", func() {
		err := config.UpdateMusicQualityProfile(
			context.Background(), "nope", config.MusicQualityProfilePatch{},
		)
		Expect(err).To(MatchError(config.ErrMusicQualityProfileNotFound))
	})

	It("deletes a profile once", func() {
		Expect(
			config.AddMusicQualityProfile(context.Background(), entry("lossless")),
		).To(Succeed())
		Expect(
			config.DeleteMusicQualityProfile(context.Background(), "lossless"),
		).To(Succeed())
		err := config.DeleteMusicQualityProfile(context.Background(), "lossless")
		Expect(err).To(MatchError(config.ErrMusicQualityProfileNotFound))
	})

	It("refuses to delete the default profile", func() {
		configtest.SetupFile(map[string]any{
			"music_quality_profiles": []map[string]any{
				{"name": "lossless", "formats": []string{"flac"}, "cutoff": "flac"},
			},
			"music_quality_default_profile": "lossless",
		})
		err := config.DeleteMusicQualityProfile(context.Background(), "lossless")
		Expect(err).To(MatchError(config.ErrMusicQualityProfileInUseAsDefault))
	})
})
