package library

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/internal/library/audiotags"
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

var _ = Describe("TrackMatcher", Label("unit", "library"), func() {
	album := []*ent.Track{
		{ID: 1, Disc: 1, Position: 1, Title: "Regret"},
		{ID: 2, Disc: 1, Position: 2, Title: "House of Eternal Hunt"},
		{ID: 3, Disc: 1, Position: 3, Title: "The Eagle Has Landed"},
		{ID: 4, Disc: 1, Position: 4, Title: "New Land"},
		{ID: 5, Disc: 2, Position: 1, Title: "Intro"},
		{ID: 6, Disc: 2, Position: 2, Title: "Hail the Apocalypse"},
	}

	It("trusts numbers that name the album's titles", func() {
		m := NewTrackMatcherByName(album, []string{
			"CD1/01 Regret.flac", "CD1/02 House Of Eternal Hunt.flac",
			"CD1/03 The Eagle Has Landed (Remastered).flac", "CD1/04 Track 4.flac",
		})
		Expect(m.ByNumber()).To(BeTrue())
		Expect(m.MatchByName("CD1/04 Track 4.flac").ID).To(Equal(uint32(4)))
	})

	It("matches by title when the numbers name other titles", func() {
		// Feathers & Flesh (In His Own Words): an interlude before each song.
		files := []string{
			"CD1/01 An Ancient Old Tale.flac", "CD1/02 Regret.flac",
			"CD1/03 Each Feather A Blade.flac", "CD1/04 House Of Eternal Hunt.flac",
			"CD2/01 Wickedly They Play.flac", "CD2/02 New Land.flac",
		}
		m := NewTrackMatcherByName(album, files)
		Expect(m.ByNumber()).To(BeFalse())
		Expect(m.MatchByName("CD1/01 An Ancient Old Tale.flac")).To(BeNil())
		Expect(m.MatchByName("CD1/02 Regret.flac").ID).To(Equal(uint32(1)))
		Expect(
			m.MatchByName("CD1/04 House Of Eternal Hunt.flac").ID,
		).To(Equal(uint32(2)))
		Expect(m.MatchByName("CD2/02 New Land.flac").ID).To(Equal(uint32(4)))
		Expect(m.MatchByName("CD2/01 Wickedly They Play.flac")).To(BeNil())
	})

	It("prefers the file's disc when a title appears twice", func() {
		tracks := []*ent.Track{
			{ID: 1, Disc: 1, Position: 3, Title: "Hail the Apocalypse"},
			{ID: 2, Disc: 2, Position: 2, Title: "Hail the Apocalypse"},
			{ID: 3, Disc: 1, Position: 1, Title: "Regret"},
		}
		m := NewTrackMatcher(tracks, []audiotags.Info{
			{Disc: 1, Track: 1, Title: "Hail the Apocalypse"},
			{Disc: 1, Track: 3, Title: "Regret"},
		})
		Expect(m.ByNumber()).To(BeFalse())
		Expect(m.Match(audiotags.Info{Disc: 2, Title: "Hail the Apocalypse"}).ID).
			To(Equal(uint32(2)))
		Expect(m.Match(audiotags.Info{Title: "Hail the Apocalypse"}).ID).
			To(Equal(uint32(1)))
	})

	It("trusts numbers whose titles name no track of the album", func() {
		m := NewTrackMatcher(album, []audiotags.Info{
			{Disc: 1, Track: 1, Title: "Regret (version française)"},
			{Disc: 1, Track: 2, Title: "Maison de la chasse éternelle"},
			{Disc: 1, Track: 3, Title: "L'aigle a atterri"},
		})
		Expect(m.ByNumber()).To(BeTrue())
	})

	It("does not count a title in another script as a disagreement", func() {
		m := NewTrackMatcher(album, []audiotags.Info{
			{Disc: 1, Track: 1, Title: "後悔"},
			{Disc: 1, Track: 2, Title: "永遠の狩りの家"},
		})
		Expect(m.ByNumber()).To(BeTrue())
	})
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

var _ = Describe("CueImage", Label("unit", "library"), func() {
	DescribeTable("one audio file beside a CUE sheet",
		func(paths []string, want bool) {
			Expect(CueImage(paths)).To(Equal(want))
		},
		Entry(
			"flac image",
			[]string{
				"Album/Album.flac",
				"Album/Album.cue",
				"Album/Album.log",
			},
			true,
		),
		Entry("ape image, a format the library never imports",
			[]string{"Album/CD.ape", "Album/CD.cue"}, true),
		Entry(
			"one image per disc folder",
			[]string{
				"A/CD1/a.flac",
				"A/CD1/a.cue",
				"A/CD2/b.flac",
				"A/CD2/b.cue",
			},
			true,
		),
		Entry("tracks with a sheet",
			[]string{"A/01.flac", "A/02.flac", "A/A.cue"}, false),
		Entry("one file, no sheet", []string{"A/A.flac"}, false),
		Entry("sheet in another folder",
			[]string{"A/A.flac", "A/Scans/A.cue"}, false),
		Entry("OS separators", []string{`A\A.wv`, `A\A.CUE`}, true),
	)
})
