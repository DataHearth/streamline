package library

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/internal/config"
)

var _ = Describe("ParseBookRelease", Label("unit", "library"), func() {
	DescribeTable(
		"format detection",
		func(name, wantFormat, wantKind string) {
			p := ParseBookRelease(name)
			Expect(p.Format).To(Equal(wantFormat))
			Expect(p.Kind).To(Equal(wantKind))
		},
		Entry("epub", "Brandon Sanderson - Elantris (2005) EPUB", "EPUB", "ebook"),
		Entry("azw3", "Elantris by Brandon Sanderson [AZW3]", "AZW3", "ebook"),
		Entry("azw", "Elantris.AZW", "AZW3", "ebook"),
		Entry("mobi", "Elantris.2005.MOBI", "MOBI", "ebook"),
		Entry("pdf", "Elantris (PDF)", "PDF", "ebook"),
		Entry("cbz", "Saga T01 CBZ", "CBZ", "ebook"),
		Entry("cbr as a comic container", "Saga T01 CBR", "CBR", "ebook"),
		Entry(
			"retail flag ignored for format",
			"Elantris EPUB Retail",
			"EPUB",
			"ebook",
		),
		Entry("m4b", "Elantris (Unabridged) M4B 64kbps", "M4B", "audiobook"),
		Entry("mp3 audiobook", "Elantris Audiobook MP3", "MP3", "audiobook"),
		Entry("m4a audiobook", "Elantris Unabridged M4A", "M4A", "audiobook"),
		Entry("flac audiobook", "Elantris narrated FLAC", "FLAC", "audiobook"),
		Entry("bare mp3 is not a book", "Some Artist - Some Album MP3", "", ""),
		Entry("bare m4a is not a book", "Some Artist - Some Album M4A", "", ""),
		Entry("bare flac is not a book", "Some Artist - Some Album FLAC", "", ""),
		Entry(
			"constant bit rate is not a comic",
			"Foo Audiobook MP3 CBR 64k",
			"MP3",
			"audiobook",
		),
		Entry("cbr beside an audio token is a bit rate", "Foo MP3 CBR", "", ""),
		Entry("cbr rate with no audio token is a bit rate", "Foo CBR 128", "", ""),
		Entry("unknown", "Brandon Sanderson - Elantris", "", ""),
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

	It("reads a stated audiobook bit rate and no other", func() {
		Expect(
			ParseBookRelease("X Unabridged M4B 64kbps").BitrateKbps,
		).To(Equal(uint32(64)))
		Expect(
			ParseBookRelease("X Audiobook MP3 128k").BitrateKbps,
		).To(Equal(uint32(128)))
		Expect(ParseBookRelease("X M4B").BitrateKbps).To(BeZero())
		Expect(ParseBookRelease("X EPUB 300k words").BitrateKbps).To(BeZero())
	})
})

var _ = Describe("ScoreBookRelease", Label("unit", "library"), func() {
	profile := config.BookQualityProfileEntry{
		Name:           "std",
		UpgradeAllowed: true,
		Ebook: config.EbookSlot{
			Formats: []string{"EPUB", "AZW3", "CBZ"}, Preferred: "EPUB",
		},
		Audiobook: config.AudiobookSlot{
			Formats: []string{"M4B", "MP3"}, Preferred: "M4B", MinBitrate: 64,
		},
	}

	It("ranks by the ebook ladder and rejects formats outside the profile", func() {
		epub := ScoreEbookRelease(ParseBookRelease("X EPUB"), profile)
		azw3 := ScoreEbookRelease(ParseBookRelease("X AZW3"), profile)
		cbz := ScoreEbookRelease(ParseBookRelease("X CBZ"), profile)
		Expect(epub).To(BeNumerically(">", azw3))
		Expect(azw3).To(BeNumerically(">", cbz))
		score, reason := JudgeEbookRelease(ParseBookRelease("X PDF"), profile)
		Expect(score).To(Equal(-1))
		Expect(reason).To(Equal("PDF is not in the profile"))
	})

	It("scores audiobook releases against the audiobook slot", func() {
		Expect(ScoreAudiobookRelease(ParseBookRelease("X M4B"), profile)).
			To(BeNumerically(">", ScoreAudiobookRelease(ParseBookRelease("X Audiobook MP3"), profile)))
	})

	It(
		"rejects a stated rate under the floor with the numbers and accepts an unstated one",
		func() {
			score, reason := JudgeAudiobookRelease(
				ParseBookRelease("X Unabridged M4B 32kbps"),
				profile,
			)
			Expect(score).To(Equal(-1))
			Expect(reason).To(Equal("32 kbps is below the profile's minimum of 64"))
			Expect(
				ScoreAudiobookRelease(ParseBookRelease("X M4B"), profile),
			).To(BeNumerically(">", 0))
		},
	)

	It("rejects a release of the other family", func() {
		Expect(ScoreEbookRelease(ParseBookRelease("X M4B"), profile)).To(Equal(-1))
		Expect(
			ScoreAudiobookRelease(ParseBookRelease("X EPUB"), profile),
		).To(Equal(-1))
	})

	It("rejects an undetectable release", func() {
		Expect(ScoreEbookRelease(ParseBookRelease("X"), profile)).To(Equal(-1))
	})
})
