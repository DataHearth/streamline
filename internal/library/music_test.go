package library

import (
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/ent"
	mockffmpeg "github.com/datahearth/streamline/internal/ffmpeg/mocks"
	"github.com/datahearth/streamline/internal/library/audiotags"
	"github.com/datahearth/streamline/internal/testutil/configtest"
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

var _ = Describe("ReadTrackInfo", Label("unit", "library"), func() {
	copyFixture := func(fixture, name string) string {
		GinkgoHelper()
		data, err := os.ReadFile(filepath.Join("audiotags", "testdata", fixture))
		Expect(err).NotTo(HaveOccurred())
		p := filepath.Join(GinkgoT().TempDir(), "CD2", name)
		Expect(os.MkdirAll(filepath.Dir(p), 0o755)).To(Succeed())
		Expect(os.WriteFile(p, data, 0o600)).To(Succeed())
		return p
	}

	It("reads an untagged file's disc, number and title off its path", func() {
		info, err := ReadTrackInfo(
			copyFixture("untagged.mp3", "7 - Gotta Get Away.mp3"),
		)
		Expect(err).NotTo(HaveOccurred())
		Expect(info.Disc).To(Equal(uint8(2)))
		Expect(info.Track).To(Equal(uint16(7)))
		Expect(info.Title).To(Equal("Gotta Get Away"))
	})

	It("fills only the fields the tags lack", func() {
		p := copyFixture("untagged.mp3", "7 - Name Title.mp3")
		Expect(audiotags.Write(p, audiotags.Tags{
			audiotags.KeyTitle: {"Tag Title"},
		}, audiotags.Policy{})).To(Succeed())
		info, err := ReadTrackInfo(p)
		Expect(err).NotTo(HaveOccurred())
		Expect(info.Title).To(Equal("Tag Title"))
		Expect(info.Track).To(Equal(uint16(7)))
	})
})

var _ = Describe("MatchTrack", Label("unit", "library"), func() {
	tracks := []*ent.Track{
		{ID: 1, Disc: 1, Position: 1, Title: "Eternal Rains Will Come"},
		{ID: 2, Disc: 1, Position: 2, Title: "Goblin"},
		{ID: 3, Disc: 1, Position: 3, Title: "葬送"},
	}

	It("matches a title read off a file name past its punctuation", func() {
		// "C1 - Goblin..flac": no tags, no track number, a trailing period.
		Expect(MatchTrack(tracks, audiotags.Info{Title: "Goblin."}).ID).
			To(Equal(uint32(2)))
		Expect(
			MatchTrack(tracks, audiotags.Info{Title: "eternal rains will come."}).ID,
		).
			To(Equal(uint32(1)))
	})

	It("still matches a title no normalization can read, when it is exact", func() {
		Expect(
			MatchTrack(tracks, audiotags.Info{Title: "葬送"}).ID,
		).To(Equal(uint32(3)))
	})

	It("matches nothing for another title", func() {
		Expect(MatchTrack(tracks, audiotags.Info{Title: "Goblins"})).To(BeNil())
	})
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

	It("suggests the track a file names over the one its number reaches", func() {
		m := NewTrackMatcher(album, nil)
		Expect(m.Suggest(audiotags.Info{Disc: 1, Track: 3, Title: "New Land"}).ID).
			To(Equal(uint32(4)))
		Expect(m.Suggest(audiotags.Info{Disc: 1, Track: 3, Title: "Bonus Song"}).ID).
			To(Equal(uint32(3)))
		Expect(
			m.Suggest(
				audiotags.Info{
					Disc:  1,
					Track: 3,
					Title: "The Eagle Has Landed (Remastered)",
				},
			).ID,
		).To(Equal(uint32(3)))
		Expect(m.Suggest(audiotags.Info{Title: "Bonus Song"})).To(BeNil())
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

var _ = Describe("TrackMatcher near titles", Label("unit", "library"), func() {
	// Sidilarsen's "Que la lumière soit": the release spells track 7 "Un
	// Élan De Coeur" where MusicBrainz has "Un élan du cœur".
	album := []*ent.Track{
		{ID: 1, Disc: 1, Position: 1, Title: "Deuxième vie"},
		{ID: 2, Disc: 1, Position: 2, Title: "Acide Occident"},
		{ID: 7, Disc: 1, Position: 7, Title: "Un élan du cœur"},
		{ID: 12, Disc: 1, Position: 12, Title: "Appel à résistance"},
	}
	files := []string{
		"Sidilarsen - Deuxième Vie.flac",
		"Sidilarsen - Acide Occident.flac",
		"Sidilarsen - Un Élan De Coeur.flac",
		"Sidilarsen - Appel À Résistance.flac",
	}

	It("pairs a misspelt title with the one track no file names", func() {
		m := NewTrackMatcherByName(album, files)
		Expect(m.MatchByName("Sidilarsen - Un Élan De Coeur.flac").ID).
			To(Equal(uint32(7)))
		Expect(m.Match(audiotags.Info{Title: "Un Élan De Coeur"}).ID).
			To(Equal(uint32(7)))
		Expect(m.MatchByName("Sidilarsen - Acide Occident.flac").ID).
			To(Equal(uint32(2)))
	})

	It("leaves a track a file names outright to that file", func() {
		m := NewTrackMatcherByName(album, append(
			files[:2:2],
			"Sidilarsen - Un élan du cœur.flac",
			"Sidilarsen - Un Élan De Coeur.flac",
		))
		Expect(m.MatchByName("Sidilarsen - Un élan du cœur.flac").ID).
			To(Equal(uint32(7)))
		Expect(m.MatchByName("Sidilarsen - Un Élan De Coeur.flac")).To(BeNil())
	})

	It("pairs nothing when two files are nearest to one track", func() {
		m := NewTrackMatcherByName(album, append(
			files[:2:2],
			"Sidilarsen - Un Élan De Coeur.flac",
			"Sidilarsen - Un Élan Du Coeurs.flac",
		))
		Expect(m.MatchByName("Sidilarsen - Un Élan De Coeur.flac")).To(BeNil())
		Expect(m.MatchByName("Sidilarsen - Un Élan Du Coeurs.flac")).To(BeNil())
	})

	It("pairs a reordered title and a store-labelled one", func() {
		// Biotop's "Remix Heretik System" and Dancefloor Bastards' title
		// track, as a store and another release name them.
		tracks := []*ent.Track{
			{ID: 1, Disc: 1, Position: 1, Title: "Cardiotonic"},
			{ID: 2, Disc: 1, Position: 2, Title: "Remix Heretik System"},
			{ID: 3, Disc: 1, Position: 3, Title: "Dancefloor Bastards"},
		}
		m := NewTrackMatcher(tracks, []audiotags.Info{
			{Title: "Cardiotronic"},
			{Title: "Heretik System Remix"},
			{Title: "Dancefloor Bastards [Explicit]"},
		})
		Expect(m.Match(audiotags.Info{Title: "Cardiotronic"}).ID).
			To(Equal(uint32(1)))
		Expect(m.Match(audiotags.Info{Title: "Heretik System Remix"}).ID).
			To(Equal(uint32(2)))
		Expect(
			m.Match(audiotags.Info{Title: "Dancefloor Bastards [Explicit]"}).ID,
		).To(Equal(uint32(3)))
	})

	It("sets aside the label every file of a live album carries", func() {
		// In Bikini Dura Sidi: MusicBrainz titles the live tracks bare, and
		// spells "Des milliards" as "Des Millards".
		titles := []string{
			"Retourner la France", "Back to Basics", "Spread It",
			"A qui je nuis me pardonne", "Guerre à vendre", "Wall of Shame",
			"I feel fine", "Dancefloor Bastards", "Frapper la terre", "Fluidité",
			"Où il veut", "Comme on vibre", "La morale de la fable", "Des Millards",
		}
		tracks := make([]*ent.Track, 0, len(titles))
		for i, title := range titles {
			tracks = append(tracks, &ent.Track{
				ID: uint32(i + 1), Disc: 1, Position: uint16(i + 1), Title: title,
			})
		}
		files := make([]string, 0, 13)
		for _, title := range []string{
			"Back to Basics", "Comme on vibre", "Dancefloor Bastards",
			"Des milliards", "Fluidité", "Frapper la terre", "I Feel Fine",
			"La morale de la fable", "Où il veut", "Retourner la France",
			"Spread It", "Wall of Shame", "À qui je nuis me pardonne",
		} {
			files = append(files, "Sidilarsen - "+title+" (Live).flac")
		}
		m := NewTrackMatcherByName(tracks, files)
		got := map[uint32]bool{}
		for _, f := range files {
			t := m.MatchByName(f)
			Expect(t).NotTo(BeNil(), f)
			got[t.ID] = true
		}
		Expect(got).To(HaveLen(13))
		Expect(got).NotTo(HaveKey(uint32(5)))
		Expect(m.MatchByName("Sidilarsen - Des milliards (Live).flac").ID).
			To(Equal(uint32(14)))
	})

	It("keeps a live bonus file apart from the studio track", func() {
		tracks := []*ent.Track{
			{ID: 1, Disc: 1, Position: 1, Title: "Spread It"},
			{ID: 2, Disc: 1, Position: 2, Title: "Go Fast"},
		}
		m := NewTrackMatcherByName(tracks, []string{
			"Go Fast.flac", "Spread It (Live).flac",
		})
		Expect(m.MatchByName("Go Fast.flac").ID).To(Equal(uint32(2)))
		Expect(m.MatchByName("Spread It (Live).flac")).To(BeNil())
	})

	It("agrees with a track its number reaches under another spelling", func() {
		t := &ent.Track{ID: 7, Disc: 1, Position: 7, Title: "Un élan du cœur"}
		Expect(TrackTitleAgrees(t, "Un élan de cœur")).To(BeTrue())
		Expect(TrackTitleAgrees(t, "Où il veut")).To(BeFalse())
	})

	It("does not read another title or number as a misspelling", func() {
		tracks := []*ent.Track{
			{ID: 1, Disc: 1, Position: 1, Title: "Fiddler's Day: Part One"},
			{ID: 2, Disc: 1, Position: 2, Title: "Interlude 1"},
			{ID: 3, Disc: 1, Position: 3, Title: "Fire"},
		}
		m := NewTrackMatcherByName(tracks, []string{
			"Bonus Song.flac", "Interlude 2.flac", "Fir.flac",
		})
		Expect(m.MatchByName("Bonus Song.flac")).To(BeNil())
		Expect(m.MatchByName("Interlude 2.flac")).To(BeNil())
		Expect(m.MatchByName("Fir.flac")).To(BeNil())
	})
})

var _ = Describe("ReleaseTitleLabel", Label("unit", "library"), func() {
	It("reads the label every titled file ends with", func() {
		Expect(ReleaseTitleLabel([]string{
			"Back to Basics (Live)", "Comme on vibre (live)", "", "Fluidité [Live]",
		})).To(Equal("live"))
		Expect(ReleaseTitleLabel([]string{
			"Regret (Remastered 2011)", "New Land (Remastered  2011)",
		})).To(Equal("remastered 2011"))
	})

	It("reads none when a file lacks it or carries another", func() {
		Expect(ReleaseTitleLabel([]string{"Spread It (Live)", "Go Fast"})).
			To(BeEmpty())
		Expect(ReleaseTitleLabel([]string{"Spread It (Live)", "Go Fast (Demo)"})).
			To(BeEmpty())
		Expect(ReleaseTitleLabel([]string{"Spread It (Live)"})).To(BeEmpty())
	})

	It("strips only that label", func() {
		Expect(StripTitleLabel("Où il veut (Live)", "live")).To(Equal("Où il veut"))
		Expect(StripTitleLabel("Où il veut (Demo)", "live")).
			To(Equal("Où il veut (Demo)"))
		Expect(StripTitleLabel("(Live)", "live")).To(Equal("(Live)"))
		Expect(StripTitleLabel("Où il veut (Live)", "")).
			To(Equal("Où il veut (Live)"))
	})
})

var _ = Describe("TitleNear", Label("unit", "library"), func() {
	DescribeTable("reads two spellings of one title as one",
		func(a, b string) {
			Expect(TitleNear(a, b)).To(BeTrue())
			Expect(TitleNear(b, a)).To(BeTrue())
		},
		Entry("a misspelt word", "Un élan de cœur", "Un élan du cœur"),
		Entry("a dropped letter", "Cardiotronic", "Cardiotonic"),
		Entry("the same words in another order",
			"Heretik System Remix", "Remix Heretik System"),
		Entry("a store's label", "Dancefloor Bastards [Explicit]",
			"Dancefloor Bastards"),
		Entry("a store's clean label", "Go Fast (Clean Version)", "Go Fast"),
	)

	DescribeTable("keeps two titles apart",
		func(a, b string) {
			Expect(TitleNear(a, b)).To(BeFalse())
			Expect(TitleNear(b, a)).To(BeFalse())
		},
		Entry("another number", "Interlude 1", "Interlude 2"),
		Entry("a short word", "Fire", "Fir"),
		Entry("another title", "Bonus Song", "Fiddler's Day: Part One"),
		Entry("another recording", "Dancefloor Bastards (Live)",
			"Dancefloor Bastards"),
		Entry("a word more", "Heretik System Remix", "Heretik System"),
		Entry("a live version of a long title",
			"Les enfants de la rage (Live)", "Les enfants de la rage"),
		Entry("a remix of a long title",
			"Méditerranée damnée (Remix)", "Méditerranée damnée"),
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

var _ = Describe("CueImage", Label("unit", "library"), func() {
	DescribeTable("audio files beside a CUE sheet, against the album's tracks",
		func(paths []string, tracks int, want bool) {
			Expect(CueImage(paths, tracks)).To(Equal(want))
		},
		Entry(
			"flac image",
			[]string{
				"Album/Album.flac",
				"Album/Album.cue",
				"Album/Album.log",
			},
			10,
			true,
		),
		Entry("ape image, a format the library never imports",
			[]string{"Album/CD.ape", "Album/CD.cue"}, 10, true),
		Entry(
			"one image per disc folder",
			[]string{
				"A/CD1/a.flac",
				"A/CD1/a.cue",
				"A/CD2/b.flac",
				"A/CD2/b.cue",
			},
			20,
			true,
		),
		Entry(
			"one image per vinyl side beside one sheet",
			[]string{
				"Rammstein – Zeit [2LP] 2022.cue",
				"Side A.flac",
				"Side B.flac",
				"Side C.flac",
				"Side D.flac",
			},
			12,
			true,
		),
		Entry("tracks not fetched yet: one file beside a sheet",
			[]string{"A/A.flac", "A/A.cue"}, 0, true),
		Entry("tracks not fetched yet: sides cannot be told from tracks",
			[]string{"A/Side A.flac", "A/Side B.flac", "A/A.cue"}, 0, false),
		Entry("a one-track album's image is its track",
			[]string{"A/A.flac", "A/A.cue"}, 1, false),
		Entry("tracks with a sheet",
			[]string{"A/01.flac", "A/02.flac", "A/A.cue"}, 2, false),
		Entry("one file, no sheet", []string{"A/A.flac"}, 10, false),
		Entry("sheet in another folder",
			[]string{"A/A.flac", "A/Scans/A.cue"}, 10, false),
		Entry("OS separators", []string{`A\A.wv`, `A\A.CUE`}, 10, true),
	)
})

var _ = Describe("ConvertsWAV", Label("unit", "library"), func() {
	DescribeTable("needs ffmpeg enabled and found, and the switch on",
		func(enabled, convert, ffprobe bool, ffmpegPath string, want bool) {
			configtest.Setup(map[string]any{
				"ffmpeg":  map[string]any{"enabled": enabled},
				"library": map[string]any{"convert_wav": convert},
			})
			p := mockffmpeg.NewMockProber(GinkgoT())
			p.EXPECT().Available().Return(ffprobe).Maybe()
			p.EXPECT().FFmpegPath().Return(ffmpegPath).Maybe()
			Expect(ConvertsWAV(p)).To(Equal(want))
		},
		Entry("all on", true, true, true, "/bin/ffmpeg", true),
		Entry("ffmpeg disabled", false, true, true, "/bin/ffmpeg", false),
		Entry("switch off", true, false, true, "/bin/ffmpeg", false),
		Entry("no ffprobe", true, true, false, "/bin/ffmpeg", false),
		Entry("no ffmpeg binary", true, true, true, "", false),
	)

	It("is false without a prober", func() {
		configtest.Setup()
		Expect(ConvertsWAV(nil)).To(BeFalse())
	})
})

var _ = Describe("IsReleaseAudio", Label("unit", "library"), func() {
	DescribeTable("audio files, and WAV only when it converts",
		func(p string, wav, want bool) {
			Expect(IsReleaseAudio(p, wav)).To(Equal(want))
		},
		Entry("flac", "a/01.flac", false, true),
		Entry("wav, converting", "a/01 x.WAV", true, true),
		Entry("wav, not converting", "a/01 x.wav", false, false),
		Entry("hidden wav", "a/.01.wav", true, false),
		Entry("cue", "a/x.cue", true, false),
	)
})
