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
		_, ok := config.ResolveQualityProfile(config.MediaMovie, "uhd")
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
		p, _ := config.ResolveQualityProfile(config.MediaMovie, "uhd")
		Expect(p.PreferredResolution).To(Equal("1080p"))

		Expect(config.DeleteQualityProfile(ctx, "uhd")).To(Succeed())
		Expect(config.DeleteQualityProfile(ctx, "uhd")).
			To(MatchError(config.ErrQualityProfileNotFound))
	})

	It("blocks deleting the default profile", func() {
		Expect(config.DeleteQualityProfile(context.Background(), "default")).
			To(MatchError(config.ErrQualityProfileInUseAsDefault))
	})

	Describe("per-media defaults", func() {
		var ctx context.Context

		BeforeEach(func() {
			ctx = context.Background()
			Expect(config.AddQualityProfile(ctx, entry("uhd"))).To(Succeed())
			Expect(config.AddQualityProfile(ctx, entry("anime"))).To(Succeed())
		})

		It("sets only the movie default for media=movie", func() {
			Expect(config.SetDefaultQualityProfile(ctx, "uhd", config.MediaMovie)).
				To(Succeed())
			c := config.Get()
			Expect(c.MovieQualityDefaultProfile).To(Equal("uhd"))
			Expect(c.SeriesQualityDefaultProfile).To(Equal("default"))
		})

		It("sets only the series default for media=series", func() {
			Expect(config.SetDefaultQualityProfile(ctx, "uhd", config.MediaSeries)).
				To(Succeed())
			c := config.Get()
			Expect(c.MovieQualityDefaultProfile).To(Equal("default"))
			Expect(c.SeriesQualityDefaultProfile).To(Equal("uhd"))
		})

		It("sets both when given both media", func() {
			Expect(config.SetDefaultQualityProfile(
				ctx, "uhd", config.MediaMovie, config.MediaSeries,
			)).To(Succeed())
			c := config.Get()
			Expect(c.MovieQualityDefaultProfile).To(Equal("uhd"))
			Expect(c.SeriesQualityDefaultProfile).To(Equal("uhd"))
		})

		It("refuses an unknown profile", func() {
			Expect(config.SetDefaultQualityProfile(ctx, "nope", config.MediaMovie)).
				To(MatchError(config.ErrQualityProfileNotFound))
		})

		It("refuses deleting a profile that is only the series default", func() {
			Expect(
				config.SetDefaultQualityProfile(ctx, "anime", config.MediaSeries),
			).
				To(Succeed())
			Expect(config.DeleteQualityProfile(ctx, "anime")).
				To(MatchError(config.ErrQualityProfileInUseAsDefault))
		})

		It("refuses deleting a profile that is only the movie default", func() {
			Expect(config.SetDefaultQualityProfile(ctx, "uhd", config.MediaMovie)).
				To(Succeed())
			Expect(config.DeleteQualityProfile(ctx, "uhd")).
				To(MatchError(config.ErrQualityProfileInUseAsDefault))
		})

		It("frees the old default once both media have moved off it", func() {
			Expect(config.SetDefaultQualityProfile(ctx, "uhd", config.MediaMovie)).
				To(Succeed())
			Expect(config.DeleteQualityProfile(ctx, "default")).
				To(MatchError(config.ErrQualityProfileInUseAsDefault))
			Expect(config.SetDefaultQualityProfile(ctx, "uhd", config.MediaSeries)).
				To(Succeed())
			Expect(config.DeleteQualityProfile(ctx, "default")).To(Succeed())
		})

		It("resolves an unknown name to the default of the media asked for", func() {
			Expect(config.SetDefaultQualityProfile(ctx, "uhd", config.MediaMovie)).
				To(Succeed())
			Expect(
				config.SetDefaultQualityProfile(ctx, "anime", config.MediaSeries),
			).
				To(Succeed())

			m, _ := config.ResolveQualityProfile(config.MediaMovie, "")
			s, _ := config.ResolveQualityProfile(config.MediaSeries, "")
			Expect(m.Name).To(Equal("uhd"))
			Expect(s.Name).To(Equal("anime"))

			Expect(config.TranscodeEligible(config.MediaMovie, "nope")).To(BeFalse())
		})
	})

	Describe("allowed_codecs", func() {
		It("defaults to empty, meaning any codec", func() {
			ctx := context.Background()
			e := entry("codecs")
			Expect(config.AddQualityProfile(ctx, e)).To(Succeed())
			got, _ := config.ResolveQualityProfile(config.MediaMovie, "codecs")
			Expect(got.AllowedCodecs).To(BeEmpty())
		})

		It("round-trips a non-empty list through update", func() {
			ctx := context.Background()
			Expect(config.AddQualityProfile(ctx, entry("codecs"))).To(Succeed())

			codecs := []string{"hevc", "av1"}
			Expect(config.UpdateQualityProfile(ctx, "codecs",
				config.QualityProfilePatch{AllowedCodecs: &codecs})).To(Succeed())

			got, _ := config.ResolveQualityProfile(config.MediaMovie, "codecs")
			Expect(got.AllowedCodecs).To(Equal([]string{"hevc", "av1"}))
		})

		It("clears the list back to empty when patched with []", func() {
			ctx := context.Background()
			e := entry("codecs")
			e.AllowedCodecs = []string{"hevc"}
			Expect(config.AddQualityProfile(ctx, e)).To(Succeed())

			empty := []string{}
			Expect(config.UpdateQualityProfile(ctx, "codecs",
				config.QualityProfilePatch{AllowedCodecs: &empty})).To(Succeed())

			got, _ := config.ResolveQualityProfile(config.MediaMovie, "codecs")
			Expect(got.AllowedCodecs).To(BeEmpty())
		})

		It("leaves allowed_codecs untouched when the patch omits it", func() {
			ctx := context.Background()
			e := entry("codecs")
			e.AllowedCodecs = []string{"hevc"}
			Expect(config.AddQualityProfile(ctx, e)).To(Succeed())

			pref := "1080p"
			Expect(config.UpdateQualityProfile(
				ctx,
				"codecs",
				config.QualityProfilePatch{
					PreferredResolution: &pref,
				},
			)).To(Succeed())

			got, _ := config.ResolveQualityProfile(config.MediaMovie, "codecs")
			Expect(got.AllowedCodecs).To(Equal([]string{"hevc"}))
		})
	})

	Describe("formats, min_score, upgrade_until_score", func() {
		It("round-trips through update", func() {
			ctx := context.Background()
			Expect(config.AddQualityProfile(ctx, entry("scored"))).To(Succeed())

			formats := []config.QualityProfileFormatScore{
				{Name: "x265", Score: 100},
			}
			minScore := 50
			upgradeUntil := 200
			Expect(config.UpdateQualityProfile(ctx, "scored",
				config.QualityProfilePatch{
					Formats:           &formats,
					MinScore:          &minScore,
					UpgradeUntilScore: &upgradeUntil,
				})).To(Succeed())

			got, _ := config.ResolveQualityProfile(config.MediaMovie, "scored")
			Expect(got.Formats).To(Equal(formats))
			Expect(got.MinScore).To(Equal(50))
			Expect(got.UpgradeUntilScore).To(Equal(200))
		})

		It(
			"leaves formats, min_score, upgrade_until_score untouched when the patch omits them",
			func() {
				ctx := context.Background()
				e := entry("scored")
				e.Formats = []config.QualityProfileFormatScore{
					{Name: "hdr", Score: 10},
				}
				e.MinScore = 5
				e.UpgradeUntilScore = 20
				Expect(config.AddQualityProfile(ctx, e)).To(Succeed())

				pref := "1080p"
				Expect(config.UpdateQualityProfile(
					ctx,
					"scored",
					config.QualityProfilePatch{
						PreferredResolution: &pref,
					},
				)).To(Succeed())

				got, _ := config.ResolveQualityProfile(config.MediaMovie, "scored")
				Expect(got.Formats).To(Equal(e.Formats))
				Expect(got.MinScore).To(Equal(5))
				Expect(got.UpgradeUntilScore).To(Equal(20))
			},
		)

		It(
			"rejects a patched format naming neither a built-in nor a user format",
			func() {
				ctx := context.Background()
				Expect(config.AddQualityProfile(ctx, entry("scored"))).To(Succeed())

				formats := []config.QualityProfileFormatScore{
					{Name: "ghost", Score: 1},
				}
				Expect(config.UpdateQualityProfile(
					ctx,
					"scored",
					config.QualityProfilePatch{
						Formats: &formats,
					},
				)).To(HaveOccurred())
			},
		)
	})
})
