package library

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/quality"
)

var _ = Describe("ParseMusicRelease", Label("unit", "library"), func() {
	DescribeTable(
		"source label and tier",
		func(name, wantSource string, wantTier quality.AudioTier) {
			p := ParseMusicRelease(name)
			Expect(p.TierKnown).To(BeTrue(), name)
			Expect(p.Source).To(Equal(wantSource))
			Expect(p.Tier).To(Equal(wantTier))
		},
		Entry("flac 24/96 by kHz",
			"Nirvana - Nevermind (1991) [FLAC 24bit-96kHz]",
			"FLAC 24/96", quality.TierHiRes),
		Entry("flac 24/192 by slash",
			"Artist - Album FLAC 24/192", "FLAC 24/192", quality.TierHiRes),
		Entry("flac 24/44.1",
			"Artist - Album FLAC 24/44.1", "FLAC 24/44.1", quality.TierHiRes),
		Entry("flac 24-bit with no rate",
			"Artist - Album [FLAC 24bit]", "FLAC 24-bit", quality.TierHiRes),
		Entry(
			"hi-res word",
			"Artist - Album Hi-Res FLAC",
			"FLAC 24-bit",
			quality.TierHiRes,
		),
		Entry(
			"plain flac",
			"Nirvana - Nevermind 1991 FLAC",
			"FLAC",
			quality.TierLossless,
		),
		Entry("lossless word reads as flac",
			"Nirvana - Nevermind Lossless", "FLAC", quality.TierLossless),
		Entry("alac", "Artist - Album [ALAC]", "ALAC", quality.TierLossless),
		Entry("wav", "Artist - Album WAV", "WAV", quality.TierLossless),
		Entry(
			"mp3 320",
			"Nirvana - Nevermind [MP3 320]",
			"MP3 320",
			quality.TierHigh,
		),
		Entry("v0", "Nirvana - Nevermind (V0)", "MP3 V0", quality.TierHigh),
		Entry("v2", "Artist - Album MP3 V2", "MP3 V2", quality.TierStandard),
		Entry(
			"mp3 256",
			"Nirvana.Nevermind.1991.MP3.256",
			"MP3 256",
			quality.TierStandard,
		),
		Entry(
			"bracketed 192",
			"Nirvana - Nevermind [192]",
			"MP3 192",
			quality.TierStandard,
		),
		Entry("mp3 128", "Artist - Album MP3 128", "MP3 128", quality.TierLow),
		Entry("aac 256", "Artist - Album AAC 256kbps", "AAC 256", quality.TierHigh),
		Entry(
			"m4a reads as aac",
			"Artist - Album M4A 256",
			"AAC 256",
			quality.TierHigh,
		),
		Entry("aac 128", "Artist - Album AAC 128", "AAC 128", quality.TierLow),
		Entry("ogg 320", "Artist - Album OGG 320", "OGG 320", quality.TierHigh),
		Entry(
			"kbps with no codec",
			"Artist - Album 320kbps",
			"MP3 320",
			quality.TierHigh,
		),
	)

	DescribeTable("a name that states no quality",
		func(name string) {
			p := ParseMusicRelease(name)
			Expect(p.TierKnown).To(BeFalse())
			Expect(p.Source).To(BeEmpty())
		},
		Entry("bare 320 in a title is not a bit rate", "Group 320 - Album"),
		Entry("nothing at all", "Nirvana - Nevermind"),
		Entry("a year is not a bit rate", "Artist - Album 2019 MP3"),
	)

	It("extracts year and flags discographies", func() {
		p := ParseMusicRelease("Nirvana - Discography (1989-2002) FLAC")
		Expect(p.Discography).To(BeTrue())
		p = ParseMusicRelease("Nirvana - Nevermind (1991) FLAC")
		Expect(p.Year).To(Equal(uint16(1991)))
		Expect(p.Discography).To(BeFalse())
		Expect(
			ParseMusicRelease("Artist - Discographie complète MP3 320").Discography,
		).
			To(BeTrue())
		Expect(ParseMusicRelease("Artist - Anthology FLAC").Discography).To(BeTrue())
	})
})

var _ = Describe("ScoreMusicRelease", Label("unit", "library"), func() {
	profile := config.MusicQualityProfileEntry{
		Name:      "hifi",
		Tiers:     []string{"hires", "lossless", "high"},
		Preferred: "lossless",
	}
	score := func(name string, scope MusicScope) (int, string) {
		return JudgeMusicRelease(ParseMusicRelease(name), profile, scope)
	}

	It("ranks by tier and rejects a tier outside the profile", func() {
		hires, _ := score("X - Y FLAC 24/96", MusicScopeAlbum)
		flac, _ := score("X - Y FLAC", MusicScopeAlbum)
		cbr, _ := score("X - Y MP3 320", MusicScopeAlbum)
		v0, _ := score("X - Y V0", MusicScopeAlbum)
		Expect(hires).To(BeNumerically(">", flac))
		Expect(flac).To(BeNumerically(">", cbr))
		Expect(cbr).To(BeNumerically(">", v0))
		low, reason := score("X - Y MP3 128", MusicScopeAlbum)
		Expect(low).To(Equal(-1))
		Expect(reason).To(Equal("tier not in the profile"))
	})

	It("keeps a release with no stated quality in the list, rejected", func() {
		s, reason := score("X - Y", MusicScopeAlbum)
		Expect(s).To(Equal(-1))
		Expect(reason).To(Equal("quality is not stated in the release name"))
	})

	It("rejects a discography for an album and accepts it for an artist", func() {
		s, reason := score("X - Discography (1990-2000) FLAC", MusicScopeAlbum)
		Expect(s).To(Equal(-1))
		Expect(reason).To(ContainSubstring("discography"))
		s, _ = score("X - Discography (1990-2000) FLAC", MusicScopeArtist)
		Expect(s).To(BeNumerically(">", 0))
	})

	It("scores through ScoreMusicRelease", func() {
		Expect(ScoreMusicRelease(
			ParseMusicRelease("X - Y FLAC"), profile, MusicScopeAlbum,
		)).To(BeNumerically(">", 0))
	})
})
