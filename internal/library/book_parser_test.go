package library

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/internal/config"
)

var _ = Describe("ParseBookRelease", Label("unit", "library"), func() {
	DescribeTable("format detection",
		func(name, wantFormat string) {
			Expect(ParseBookRelease(name).Format).To(Equal(wantFormat))
		},
		Entry("epub", "Brandon Sanderson - Elantris (2005) EPUB", "epub"),
		Entry("azw3", "Elantris by Brandon Sanderson [AZW3]", "azw3"),
		Entry("azw", "Elantris.AZW", "azw3"),
		Entry("mobi", "Elantris.2005.MOBI", "mobi"),
		Entry("pdf", "Elantris (PDF)", "pdf"),
		Entry("retail flag ignored for format", "Elantris EPUB Retail", "epub"),
		Entry("m4b", "Elantris (Unabridged) M4B 64kbps", "m4b"),
		Entry("mp3 audiobook", "Elantris Audiobook MP3", "mp3"),
		Entry("bare mp3 is not a book", "Some Artist - Some Album MP3", "other"),
		Entry("unknown", "Brandon Sanderson - Elantris", "other"),
	)

	It("extracts year and flags collections", func() {
		p := ParseBookRelease(
			"Brandon Sanderson - Complete Cosmere Collection (2005-2023) EPUB",
		)
		Expect(p.Collection).To(BeTrue())
		p = ParseBookRelease("Brandon Sanderson - Elantris (2005) EPUB")
		Expect(p.Year).To(Equal(uint16(2005)))
		Expect(p.Collection).To(BeFalse())
		Expect(ParseBookRelease("Cosmere Anthology EPUB").Collection).To(BeTrue())
	})

	It("classifies the slot kind from the format", func() {
		Expect(ParseBookRelease("X EPUB").Kind).To(Equal("ebook"))
		Expect(ParseBookRelease("X M4B").Kind).To(Equal("audiobook"))
		Expect(ParseBookRelease("X Audiobook MP3").Kind).To(Equal("audiobook"))
		Expect(ParseBookRelease("X").Kind).To(Equal(""))
		Expect(ParseBookRelease("Some Album MP3").Kind).To(Equal(""))
	})
})

var _ = Describe("ScoreBookRelease", Label("unit", "library"), func() {
	ebookProfile := config.EbookQualityProfileEntry{
		Name: "std", Formats: []string{"epub", "azw3"}, Cutoff: "epub",
	}
	audiobookProfile := config.AudiobookQualityProfileEntry{
		Name: "m4b", Formats: []string{"m4b", "mp3"}, Cutoff: "m4b",
	}

	It("ranks by ebook profile format order and rejects formats outside it", func() {
		epub := ScoreEbookRelease(ParseBookRelease("X EPUB"), ebookProfile)
		azw3 := ScoreEbookRelease(ParseBookRelease("X AZW3"), ebookProfile)
		pdf := ScoreEbookRelease(ParseBookRelease("X PDF"), ebookProfile)
		Expect(epub).To(BeNumerically(">", azw3))
		Expect(pdf).To(Equal(-1))
	})

	It("scores audiobook releases against the audiobook family", func() {
		Expect(ScoreAudiobookRelease(ParseBookRelease("X M4B"), audiobookProfile)).
			To(BeNumerically(">", ScoreAudiobookRelease(ParseBookRelease("X Audiobook MP3"), audiobookProfile)))
	})

	It("rejects a release of the other family", func() {
		Expect(
			ScoreEbookRelease(ParseBookRelease("X M4B"), ebookProfile),
		).To(Equal(-1))
		Expect(
			ScoreAudiobookRelease(ParseBookRelease("X EPUB"), audiobookProfile),
		).To(Equal(-1))
	})

	It("rejects an undetectable release even when other is in the profile", func() {
		profile := config.EbookQualityProfileEntry{
			Name:    "any",
			Formats: []string{"epub", "other"},
		}
		Expect(ScoreEbookRelease(ParseBookRelease("X"), profile)).To(Equal(-1))
	})
})
