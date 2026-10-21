package library

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/ent"
)

var _ = Describe("TrackInfoFromName", Label("unit", "library"), func() {
	DescribeTable("disc, track and title",
		func(p string, disc uint8, track uint16, title string) {
			info := TrackInfoFromName(p)
			Expect(info.Disc).To(Equal(disc), "disc")
			Expect(info.Track).To(Equal(track), "track")
			Expect(info.Title).To(Equal(title), "title")
		},
		Entry("number then title",
			"Avatar - Feathers & Flesh (2016)/03 - Black Waters.flac",
			uint8(0), uint16(3), "Black Waters"),
		Entry("dotted number",
			"Album/07. House of Eternal Hunt.flac",
			uint8(0), uint16(7), "House of Eternal Hunt"),
		Entry("number, artist, title",
			"Album/03 - Avatar - Black Waters.mp3",
			uint8(0), uint16(3), "Black Waters"),
		Entry("artist, album, number, title",
			"Album/Avatar - Feathers & Flesh - 11 - Night Never Ends.flac",
			uint8(0), uint16(11), "Night Never Ends"),
		Entry("disc folder",
			"Album/CD2/04 - Title.flac", uint8(2), uint16(4), "Title"),
		Entry("disc-track prefix",
			"Album/2-05 Title.flac", uint8(2), uint16(5), "Title"),
		Entry("three digits are disc and track",
			"Album/105 - Title.flac", uint8(1), uint16(5), "Title"),
		Entry("windows separators",
			`Album\CD1\01 - Title.flac`, uint8(1), uint16(1), "Title"),
		Entry("no number",
			"Album/Title.flac", uint8(0), uint16(0), "Title"),
		Entry("a year is not a track number",
			"Album/1984.flac", uint8(0), uint16(0), "1984"),
	)
})

var _ = Describe("MatchTrackByName", Label("unit", "library"), func() {
	tracks := []*ent.Track{
		{ID: 1, Disc: 1, Position: 1, Title: "Open Water"},
		{ID: 2, Disc: 1, Position: 2, Title: "Pray the Sun Away"},
		{ID: 3, Disc: 2, Position: 1, Title: "Fiddler's Day: Part One"},
	}

	It("matches on disc and number", func() {
		Expect(MatchTrackByName(tracks, "A/02 - whatever.flac").ID).
			To(Equal(uint32(2)))
		Expect(MatchTrackByName(tracks, "A/CD2/01 - whatever.flac").ID).
			To(Equal(uint32(3)))
	})

	It("falls back to a loose title when there is no number", func() {
		Expect(MatchTrackByName(tracks, "A/Fiddlers Day Part One.flac").ID).
			To(Equal(uint32(3)))
	})

	It("matches nothing for an unknown name", func() {
		Expect(MatchTrackByName(tracks, "A/09 - Bonus.flac")).To(BeNil())
	})
})
