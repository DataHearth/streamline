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

	DescribeTable(
		"collection detection",
		func(name string, want bool) {
			Expect(ParseBookRelease(name).Collection).To(Equal(want))
		},
		Entry("complete series", "Sword Art Online Complete Series EPUB", true),
		Entry("integrale", "Blake et Mortimer Intégrale CBZ", true),
		Entry("integrale without accent", "Blake et Mortimer Integrale CBZ", true),
		Entry("a range of tomes", "One Piece Tomes 1-9 CBZ", true),
		Entry("a range of volumes", "Berserk Vol 1-12 CBZ", true),
		Entry("a single tome", "One Piece Tome 12 CBZ", false),
	)

	DescribeTable(
		"volume detection",
		func(name string, want float64, prefix string) {
			p := ParseBookRelease(name)
			Expect(p.Volume).NotTo(BeNil())
			Expect(*p.Volume).To(Equal(want))
			Expect(p.VolumePrefix).To(Equal(prefix))
		},
		Entry("tome", "One Piece Tome 12 FRENCH CBZ", 12.0, "One Piece"),
		Entry("t and digits", "One Piece T01 CBZ", 1.0, "One Piece"),
		Entry("vol dot", "Berserk Vol. 03 CBZ", 3.0, "Berserk"),
		Entry("volume", "Berserk Volume 7 EPUB", 7.0, "Berserk"),
		Entry("hash", "Saga #5 EPUB", 5.0, "Saga"),
		Entry("v and digits", "Vinland Saga v02 CBZ", 2.0, "Vinland Saga"),
		Entry("fractional", "Berserk Vol 1.5 CBZ", 1.5, "Berserk"),
		Entry("dotted separators", "One.Piece.Tome.12.CBZ", 12.0, "One Piece"),
	)

	It("leaves the volume empty when the name carries none", func() {
		p := ParseBookRelease("Brandon Sanderson - Elantris (2005) EPUB")
		Expect(p.Volume).To(BeNil())
		Expect(p.VolumePrefix).To(BeEmpty())
	})

	DescribeTable(
		"language words",
		func(name, want string) {
			Expect(ParseBookRelease(name).Language).To(Equal(want))
		},
		Entry("french", "One Piece T01 FRENCH CBZ", "fr"),
		Entry("vf", "One Piece T01 VF CBZ", "fr"),
		Entry("english", "Saga ENGLISH EPUB", "en"),
		Entry("german", "Saga GERMAN EPUB", "de"),
		Entry("dotted", "Saga.Japanese.CBZ", "ja"),
		Entry("none", "Saga EPUB", ""),
		Entry("code in brackets", "Saga [FR] EPUB", "fr"),
		Entry("title word It", "Stephen King - It (2017) EPUB", ""),
		Entry("title word De", "Robert De Niro - Biography EPUB", ""),
		Entry("title word en", "Rock en Seine (2019) EPUB", ""),
	)

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
