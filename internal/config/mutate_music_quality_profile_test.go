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
			Name:      name,
			Tiers:     []string{"lossless", "hires"},
			Preferred: "lossless",
		}
	}

	It(
		"adds a profile, stores tiers best first and rejects a duplicate name",
		func() {
			Expect(
				config.AddMusicQualityProfile(
					context.Background(),
					entry("lossless"),
				),
			).To(Succeed())
			p, ok := config.ResolveMusicQualityProfile("lossless")
			Expect(ok).To(BeTrue())
			Expect(p.Tiers).To(Equal([]string{"hires", "lossless"}))

			err := config.AddMusicQualityProfile(
				context.Background(),
				entry("lossless"),
			)
			Expect(err).To(MatchError(config.ErrMusicQualityProfileExists))
		},
	)

	It("makes the first profile the default and leaves a later one alone", func() {
		Expect(
			config.AddMusicQualityProfile(context.Background(), entry("a")),
		).To(Succeed())
		Expect(
			config.AddMusicQualityProfile(context.Background(), entry("b")),
		).To(Succeed())
		Expect(config.Get().MusicQualityDefaultProfile).To(Equal("a"))
	})

	It("rejects a preferred tier outside the ticked ones", func() {
		e := entry("odd")
		e.Preferred = "low"
		Expect(
			config.AddMusicQualityProfile(context.Background(), e),
		).NotTo(Succeed())
		_, ok := config.LookupMusicQualityProfile("odd")
		Expect(ok).To(BeFalse())
	})

	It("rejects a patch that leaves the preferred tier unticked", func() {
		Expect(
			config.AddMusicQualityProfile(context.Background(), entry("lossless")),
		).To(Succeed())
		tiers := []string{"high"}
		Expect(config.UpdateMusicQualityProfile(
			context.Background(),
			"lossless",
			config.MusicQualityProfilePatch{Tiers: &tiers},
		)).NotTo(Succeed())
	})

	It("applies a patch", func() {
		Expect(
			config.AddMusicQualityProfile(context.Background(), entry("lossless")),
		).To(Succeed())
		preferred := "hires"
		up := true
		Expect(config.UpdateMusicQualityProfile(
			context.Background(),
			"lossless",
			config.MusicQualityProfilePatch{
				Preferred:      &preferred,
				UpgradeAllowed: &up,
			},
		)).To(Succeed())

		p, ok := config.ResolveMusicQualityProfile("lossless")
		Expect(ok).To(BeTrue())
		Expect(p.Preferred).To(Equal("hires"))
		Expect(p.UpgradeAllowed).To(BeTrue())
	})

	It("reports an unknown profile on update", func() {
		err := config.UpdateMusicQualityProfile(
			context.Background(), "nope", config.MusicQualityProfilePatch{},
		)
		Expect(err).To(MatchError(config.ErrMusicQualityProfileNotFound))
	})

	It("moves the default and then deletes the old one once", func() {
		Expect(
			config.AddMusicQualityProfile(context.Background(), entry("a")),
		).To(Succeed())
		Expect(
			config.AddMusicQualityProfile(context.Background(), entry("b")),
		).To(Succeed())
		Expect(
			config.DeleteMusicQualityProfile(context.Background(), "a"),
		).To(MatchError(config.ErrMusicQualityProfileInUseAsDefault))
		Expect(
			config.SetDefaultMusicQualityProfile(context.Background(), "b"),
		).To(Succeed())
		Expect(
			config.DeleteMusicQualityProfile(context.Background(), "a"),
		).To(Succeed())
		Expect(
			config.DeleteMusicQualityProfile(context.Background(), "a"),
		).To(MatchError(config.ErrMusicQualityProfileNotFound))
	})

	It("refuses to default an unknown profile", func() {
		Expect(
			config.SetDefaultMusicQualityProfile(context.Background(), "ghost"),
		).To(MatchError(config.ErrMusicQualityProfileNotFound))
	})
})
