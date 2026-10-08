package library

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/internal/config"
)

var _ = Describe("ParseMusicRelease", Label("unit", "library"), func() {
	DescribeTable(
		"format detection",
		func(name, wantFormat string) {
			Expect(ParseMusicRelease(name).Format).To(Equal(wantFormat))
		},
		Entry(
			"flac 24bit",
			"Nirvana - Nevermind (1991) [FLAC 24bit-96kHz]",
			"flac-24",
		),
		Entry("plain flac", "Nirvana - Nevermind 1991 FLAC", "flac"),
		Entry("cbr 320", "Nirvana - Nevermind [MP3 320]", "mp3-320"),
		Entry("v0", "Nirvana - Nevermind (V0)", "mp3-v0"),
		Entry("256", "Nirvana.Nevermind.1991.MP3.256", "mp3-256"),
		Entry("bracketed 192", "Nirvana - Nevermind [192]", "mp3-192"),
		Entry("bare 320 in a title is not a bitrate", "Group 320 - Album", "other"),
		Entry("unknown", "Nirvana - Nevermind", "other"),
	)

	It("extracts year and flags discographies", func() {
		p := ParseMusicRelease("Nirvana - Discography (1989-2002) FLAC")
		Expect(p.Discography).To(BeTrue())
		p = ParseMusicRelease("Nirvana - Nevermind (1991) FLAC")
		Expect(p.Year).To(Equal(uint16(1991)))
		Expect(p.Discography).To(BeFalse())
	})
})

var _ = Describe("ScoreMusicRelease", Label("unit", "library"), func() {
	It(
		"ranks by profile format order and rejects formats outside the profile",
		func() {
			profile := config.MusicQualityProfileEntry{
				Name:    "hifi",
				Formats: []string{"flac-24", "flac", "mp3-320"},
				Cutoff:  "flac",
			}
			flac := ScoreMusicRelease(ParseMusicRelease("X - Y FLAC"), profile)
			cbr := ScoreMusicRelease(ParseMusicRelease("X - Y MP3 320"), profile)
			v0 := ScoreMusicRelease(ParseMusicRelease("X - Y V0"), profile)
			Expect(flac).To(BeNumerically(">", cbr))
			Expect(v0).To(Equal(-1))
		},
	)
})
