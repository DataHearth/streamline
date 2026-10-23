package config_test

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/testutil/configtest"
)

var _ = Describe("Book quality profile CRUD", Label("unit", "config"), func() {
	BeforeEach(func() { configtest.SetupFile() })

	entry := func(name string) config.BookQualityProfileEntry {
		return config.BookQualityProfileEntry{
			Name: name,
			Ebook: config.EbookSlot{
				Formats: []string{"AZW3", "EPUB"}, Preferred: "EPUB",
			},
			Audiobook: config.AudiobookSlot{
				Formats: []string{"MP3", "M4B"}, Preferred: "M4B", MinBitrate: 64,
			},
		}
	}

	It("stores formats best first and rejects a duplicate name", func() {
		Expect(
			config.AddBookQualityProfile(context.Background(), entry("retail")),
		).To(Succeed())
		p, ok := config.LookupBookQualityProfile("retail")
		Expect(ok).To(BeTrue())
		Expect(p.Ebook.Formats).To(Equal([]string{"EPUB", "AZW3"}))
		Expect(p.Audiobook.Formats).To(Equal([]string{"M4B", "MP3"}))

		Expect(
			config.AddBookQualityProfile(context.Background(), entry("retail")),
		).To(MatchError(config.ErrBookQualityProfileExists))
	})

	It(
		"keeps the seeded default for every kind when another profile is added",
		func() {
			Expect(
				config.AddBookQualityProfile(context.Background(), entry("retail")),
			).To(Succeed())
			for _, k := range config.BookKinds {
				p, ok := config.ResolveBookQualityProfile("", k)
				Expect(ok).To(BeTrue())
				Expect(p.Name).To(Equal("default"), k)
			}
		},
	)

	It("rejects a preferred format outside the ticked ones", func() {
		e := entry("odd")
		e.Ebook.Preferred = "PDF"
		Expect(
			config.AddBookQualityProfile(context.Background(), e),
		).NotTo(Succeed())

		e = entry("odd")
		e.Audiobook.Preferred = "FLAC"
		Expect(
			config.AddBookQualityProfile(context.Background(), e),
		).NotTo(Succeed())
	})

	It("rejects a min_bitrate above 1024", func() {
		e := entry("loud")
		e.Audiobook.MinBitrate = 1025
		Expect(
			config.AddBookQualityProfile(context.Background(), e),
		).NotTo(Succeed())
	})

	It("replaces a slot on patch", func() {
		Expect(
			config.AddBookQualityProfile(context.Background(), entry("retail")),
		).To(Succeed())
		slot := config.AudiobookSlot{
			Formats: []string{"FLAC"}, Preferred: "FLAC", MinBitrate: 0,
		}
		Expect(config.UpdateBookQualityProfile(
			context.Background(), "retail",
			config.BookQualityProfilePatch{Audiobook: &slot},
		)).To(Succeed())
		p, _ := config.LookupBookQualityProfile("retail")
		Expect(p.Audiobook.Formats).To(Equal([]string{"FLAC"}))
		Expect(p.Ebook.Preferred).To(Equal("EPUB"))
	})

	It(
		"sets one kind's default and refuses to delete a profile that holds any",
		func() {
			Expect(
				config.AddBookQualityProfile(context.Background(), entry("comics")),
			).To(Succeed())
			Expect(
				config.SetDefaultBookQualityProfile(
					context.Background(),
					"comics",
					"bd",
				),
			).To(Succeed())
			Expect(
				config.Get().BookQualityDefaultProfiles.For("bd"),
			).To(Equal("comics"))
			Expect(config.Get().BookQualityDefaultProfiles.For("novel")).
				To(Equal("default"))
			Expect(
				config.DeleteBookQualityProfile(context.Background(), "comics"),
			).To(MatchError(config.ErrBookQualityProfileInUseAsDefault))

			Expect(
				config.SetDefaultBookQualityProfile(
					context.Background(),
					"default",
					"bd",
				),
			).To(Succeed())
			Expect(
				config.DeleteBookQualityProfile(context.Background(), "comics"),
			).To(Succeed())
		},
	)

	It("rejects an unknown kind and an unknown profile", func() {
		Expect(
			config.SetDefaultBookQualityProfile(
				context.Background(),
				"default",
				"pulp",
			),
		).To(MatchError(config.ErrBookKindUnknown))
		Expect(
			config.SetDefaultBookQualityProfile(context.Background(), "ghost", "bd"),
		).To(MatchError(config.ErrBookQualityProfileNotFound))
	})
})
