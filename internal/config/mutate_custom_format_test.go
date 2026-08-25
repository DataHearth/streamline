package config_test

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/testutil/configtest"
)

var _ = Describe("Custom format CRUD", Label("unit", "config"), func() {
	BeforeEach(func() { configtest.SetupFile() })

	entry := func(name string) config.CustomFormatEntry {
		return config.CustomFormatEntry{
			Name: name,
			Conditions: []config.CustomFormatConditionEntry{
				{Type: "release_title", Pattern: "(?i)french", Required: true},
			},
		}
	}

	It("adds, updates, and deletes a custom format", func() {
		ctx := context.Background()
		Expect(config.AddCustomFormat(ctx, entry("french-vf"))).To(Succeed())
		got, ok := config.FindCustomFormat("french-vf")
		Expect(ok).To(BeTrue())
		Expect(got.Conditions).To(HaveLen(1))

		Expect(config.AddCustomFormat(ctx, entry("french-vf"))).
			To(MatchError(config.ErrCustomFormatExists))

		updated := entry("french-vf")
		updated.Conditions[0].Pattern = "(?i)vff"
		Expect(config.UpdateCustomFormat(ctx, "french-vf", updated)).To(Succeed())
		got, _ = config.FindCustomFormat("french-vf")
		Expect(got.Conditions[0].Pattern).To(Equal("(?i)vff"))

		Expect(config.DeleteCustomFormat(ctx, "french-vf")).To(Succeed())
		_, ok = config.FindCustomFormat("french-vf")
		Expect(ok).To(BeFalse())
	})

	It("rejects adding a format named like a built-in", func() {
		Expect(config.AddCustomFormat(context.Background(), entry("x265"))).
			To(MatchError(config.ErrCustomFormatBuiltin))
	})

	It("rejects adding a format whose conditions don't compile", func() {
		bad := entry("bad-regex")
		bad.Conditions[0].Pattern = "("
		Expect(config.AddCustomFormat(context.Background(), bad)).To(HaveOccurred())
	})

	It("rejects updating an unknown format", func() {
		Expect(
			config.UpdateCustomFormat(context.Background(), "ghost", entry("ghost")),
		).
			To(MatchError(config.ErrCustomFormatNotFound))
	})

	It("rejects updating a built-in name", func() {
		Expect(
			config.UpdateCustomFormat(context.Background(), "x265", entry("x265")),
		).
			To(MatchError(config.ErrCustomFormatBuiltin))
	})

	It("rejects an update whose entry name doesn't match the target name", func() {
		ctx := context.Background()
		Expect(config.AddCustomFormat(ctx, entry("french-vf"))).To(Succeed())
		Expect(config.UpdateCustomFormat(ctx, "french-vf", entry("renamed"))).
			To(HaveOccurred())
	})

	It("rejects updating with conditions that don't compile", func() {
		ctx := context.Background()
		Expect(config.AddCustomFormat(ctx, entry("french-vf"))).To(Succeed())
		bad := entry("french-vf")
		bad.Conditions[0].Pattern = "("
		Expect(config.UpdateCustomFormat(ctx, "french-vf", bad)).To(HaveOccurred())
	})

	It("rejects deleting an unknown format", func() {
		Expect(config.DeleteCustomFormat(context.Background(), "ghost")).
			To(MatchError(config.ErrCustomFormatNotFound))
	})

	It("rejects deleting a built-in name", func() {
		Expect(config.DeleteCustomFormat(context.Background(), "x265")).
			To(MatchError(config.ErrCustomFormatBuiltin))
	})

	It("refuses to delete a format still scored by a profile", func() {
		ctx := context.Background()
		Expect(config.AddCustomFormat(ctx, entry("french-vf"))).To(Succeed())
		Expect(config.AddQualityProfile(ctx, config.QualityProfileEntry{
			Name:                "scored",
			PreferredResolution: "2160p",
			MinResolution:       "1080p",
			Formats: []config.QualityProfileFormatScore{
				{Name: "french-vf", Score: 50},
			},
		})).To(Succeed())

		Expect(config.DeleteCustomFormat(ctx, "french-vf")).
			To(MatchError(config.ErrCustomFormatInUse))

		got, _ := config.FindCustomFormat("french-vf")
		Expect(got.Name).To(Equal("french-vf"))
	})
})
