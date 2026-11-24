package library

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/ent"
)

var _ = Describe("TitleMatches", Label("unit", "library"), func() {
	It("ignores case + punctuation", func() {
		Expect(TitleMatches("the.matrix", "The Matrix")).To(BeTrue())
	})
	It("strips leading article 'the'", func() {
		Expect(TitleMatches("Matrix", "The Matrix")).To(BeTrue())
	})
	It("strips leading article 'a'", func() {
		Expect(TitleMatches("Bug's Life", "A Bug's Life")).To(BeTrue())
	})
	It("returns false for genuine difference", func() {
		Expect(TitleMatches("The Matrix", "The Matrix Reloaded")).To(BeFalse())
	})
	It("folds accents a scene release never carries", func() {
		Expect(TitleMatches("Détective Conan", "Detective Conan")).To(BeTrue())
	})
	It("romanizes a title written in Cyrillic alone", func() {
		Expect(TitleMatches("Брат 2", "Brat 2")).To(BeTrue())
		Expect(TitleMatches("Сказка", "Брат")).To(BeFalse())
	})
	It("keeps a mark that is part of its letter", func() {
		Expect(TitleMatches("ばか", "はか")).To(BeFalse())
		Expect(TitleMatches("Чёрный", "Черныи")).To(BeTrue())
	})
	It("reads a two-script name by its Latin letters", func() {
		Expect(TitleMatches("Левиафан / Leviathan", "Leviathan")).To(BeTrue())
	})
})

var _ = Describe("TitleNamesSameWork", Label("unit", "library"), func() {
	It("accepts an exact match", func() {
		Expect(TitleNamesSameWork("Narcos", "Narcos")).To(BeTrue())
	})
	It("accepts release tags the parser could not cut", func() {
		Expect(TitleNamesSameWork("Narcos INTEGRALE FRENCH", "Narcos")).
			To(BeTrue())
		Expect(TitleNamesSameWork("Breaking Bad COMPLETE", "Breaking Bad")).
			To(BeTrue())
	})
	It("refuses a longer work built on the same name", func() {
		Expect(TitleNamesSameWork("Narcos Mexico", "Narcos")).To(BeFalse())
		Expect(TitleNamesSameWork("The Matrix Reloaded", "The Matrix")).
			To(BeFalse())
	})
	It("reaches a title held under the provider's fuller name", func() {
		Expect(TitleNamesSameWork(
			"Demon Slayer Kimetsu no Yaiba Infinity Castle",
			"Demon Slayer: Kimetsu no Yaiba Infinity Castle Chapter 1",
		)).To(BeTrue())
	})
})

var _ = Describe("MatchEpisode", Label("unit", "library"), func() {
	It("never reads an anime special's number as an absolute one", func() {
		seasons := []*ent.Season{{
			Number: 1,
			Edges: ent.SeasonEdges{Episodes: []*ent.Episode{
				{ID: 105, Number: 5, AbsoluteNumber: 5},
			}},
		}}
		Expect(MatchEpisode(Parse("Show.S00E05.1080p.WEB-GRP"), seasons, true)).
			To(BeNil())
	})

	seasons := []*ent.Season{
		{Number: 1, Edges: ent.SeasonEdges{Episodes: []*ent.Episode{
			{Number: 1, AbsoluteNumber: 1},
			{Number: 2, AbsoluteNumber: 2},
		}}},
		{Number: 2, Edges: ent.SeasonEdges{Episodes: []*ent.Episode{
			{Number: 1, AbsoluteNumber: 3},
		}}},
	}

	It("matches on season+episode for non-anime", func() {
		ep := MatchEpisode(ParseResult{Season: 1, Episode: 2}, seasons, false)
		Expect(ep).NotTo(BeNil())
		Expect(ep.AbsoluteNumber).To(Equal(uint16(2)))
	})

	It("matches on absolute number for anime", func() {
		ep := MatchEpisode(ParseResult{AbsoluteNumber: 3}, seasons, true)
		Expect(ep).NotTo(BeNil())
		Expect(ep.Number).To(Equal(uint16(1)))
	})

	It("returns nil when nothing matches", func() {
		Expect(
			MatchEpisode(ParseResult{Season: 9, Episode: 9}, seasons, false),
		).To(BeNil())
	})

	// TLC and Tsundere-Raws list a release as S02E03 and name the file inside
	// the torrent S02E15 — the absolute number in SxxExx form. Refusing it
	// fails the grab with ErrNoWantedFiles before any client is contacted.
	It(
		"reads an anime SxxExx as an absolute number when the season lacks it",
		func() {
			_, ep := MatchEpisodeInSeason(
				ParseResult{Season: 2, Episode: 3}, seasons, true,
			)
			Expect(ep).NotTo(BeNil())
			Expect(ep.AbsoluteNumber).To(Equal(uint16(3)))
			Expect(ep.Number).To(Equal(uint16(1)))
		},
	)

	It("prefers a real season+episode over the absolute fallback", func() {
		season, ep := MatchEpisodeInSeason(
			ParseResult{Season: 1, Episode: 2}, seasons, true,
		)
		Expect(ep).NotTo(BeNil())
		Expect(season).To(Equal(uint16(1)))
		Expect(ep.AbsoluteNumber).To(Equal(uint16(2)))
	})

	It("does not apply the absolute fallback to non-anime", func() {
		_, ep := MatchEpisodeInSeason(
			ParseResult{Season: 2, Episode: 3}, seasons, false,
		)
		Expect(ep).To(BeNil())
	})
})

var _ = Describe("NamesSpecial", Label("unit", "library"), func() {
	who := []string{"Doctor Who (2005)", "Doctor Who"}
	gear := []string{"Top Gear"}
	show := []string{"Show", "The Show Original"}

	DescribeTable("names the special by its own title",
		func(release string, shows []string, special string) {
			Expect(NamesSpecial(release, shows, []string{special})).To(BeZero())
		},
		Entry("title right after the show",
			"Doctor.Who.The.Day.of.the.Doctor", who, "The Day of the Doctor"),
		Entry("year between the show and the title",
			"Doctor.Who.2005.The.Day.of.the.Doctor", who, "The Day of the Doctor"),
		Entry("year after the title",
			"Doctor.Who.The.Day.of.the.Doctor.2013.1080p.BluRay.x264-GRP",
			who, "The Day of the Doctor"),
		Entry("a file name",
			"Doctor.Who.The.Day.of.the.Doctor.mkv", who, "The Day of the Doctor"),
		Entry("a fansub tag and brackets",
			"[Group] Show - The Title Here [1080p]", show, "The Title Here"),
		Entry("OVA before the title",
			"Show.OVA.The.Title.Here.1080p.WEB-GRP", show, "The Title Here"),
		Entry("Special before the title",
			"Show.Special.The.Title.Here.1080p.WEB-GRP", show, "The Title Here"),
		Entry("a title opening with the word special",
			"Show.Special.Delivery.1080p.WEB-GRP", show, "Special Delivery"),
		Entry("spaces, accents, case and a tag",
			"Kaamelott - Le Dernier Duel (Spécial) MULTI 1080p",
			[]string{"Kaamelott"}, "Le dernier duel"),
		Entry("a title opening with an A that is no article",
			"Show.Abba.1080p.WEB-GRP", show, "Abba"),
		Entry("a title opening with An that is no article",
			"Show.Anna.720p.HDTV-GRP", show, "Anna"),
		Entry("a show's alias", "The.Show.Original.The.Title.Here.720p.HDTV-GRP",
			show, "The Title Here"),
		Entry("the show's article dropped",
			"Show.Original.The.Title.Here.720p.HDTV-GRP", show, "The Title Here"),
		Entry(
			"the title's article dropped",
			"Doctor.Who.Day.of.the.Doctor.720p.HDTV-GRP",
			who,
			"The Day of the Doctor",
		),
		Entry("punctuation and an apostrophe",
			"Show.Chesters.Mill.Whats.Next.720p.HDTV-GRP",
			show, "Chester's Mill: What's Next?"),
		Entry("an ampersand for and",
			"Show.Cops.and.Robbers.720p.HDTV-GRP", show, "Cops & Robbers"),
		Entry("a part spelled out",
			"Top.Gear.Africa.Special.Part.1.720p.HDTV-GRP", gear,
			"Africa Special, Part One"),
		Entry("a part in brackets",
			"Top.Gear.Africa.Special.Part.01.720p.HDTV-GRP", gear,
			"Africa Special (1)"),
		Entry("a title that repeats the show's name",
			"Top.Gear.Polar.Special.720p.HDTV-GRP", gear, "Top Gear: Polar Special"),
		Entry("the word special only in the release",
			"Top.Gear.Patagonia.Special.720p.HDTV-GRP", gear, "Patagonia"),
		Entry("the word special only in the library",
			"Top.Gear.Patagonia.720p.HDTV-GRP", gear, "Patagonia Special"),
		Entry("a numbered special under another number",
			"Top.Gear.S00E57.Patagonia.Special.720p.HDTV-GRP", gear,
			"Patagonia Special"),
		Entry("an episode 0 of a season",
			"Doctor.Who.S07E00.The.Day.of.the.Doctor.720p.HDTV-GRP", who,
			"The Day of the Doctor"),
		Entry("a special under a season token",
			"Show.S05.Special.The.Title.Here.1080p.WEB-GRP", show, "The Title Here"),
		Entry("release tags after the title",
			"Show.The.Title.Here.FRENCH.REPACK.2019.720p.HDTV-GRP",
			show, "The Title Here"),
		Entry("a title ending in a year",
			"Show.Live.Aid.1985.720p.HDTV-GRP", show, "Live Aid 1985"),
	)

	DescribeTable(
		"names the special in more spellings",
		func(release string, shows []string, special string) {
			Expect(NamesSpecial(release, shows, []string{special})).To(BeZero())
		},
		Entry(
			"cut at a source tag",
			"Show.The.Title.Here.HDTV-G",
			show,
			"The Title Here",
		),
		Entry(
			"cut at a codec tag",
			"Show.The.Title.Here.x264-G",
			show,
			"The Title Here",
		),
		Entry("a curly apostrophe",
			"Show.Chesters.Mill.720p.HDTV-GRP", show, "Chester’s Mill"),
		Entry(
			"pt for part",
			"Top.Gear.Africa.Special.Pt.2.720p.HDTV-GRP",
			gear,
			"Africa Special Part 2",
		),
		Entry("part as a word of the title",
			"Show.Part.of.Me.720p.HDTV-GRP", show, "Part of Me"),
		Entry("the article a", "Show.Night.to.Remember.720p.HDTV-GRP",
			show, "A Night to Remember"),
		Entry("the article an", "Show.Evening.Out.720p.HDTV-GRP",
			show, "An Evening Out"),
		Entry(
			"an article after the repeated show name",
			"Top.Gear.Polar.Special.720p.HDTV-GRP",
			gear,
			"Top Gear: The Polar Special",
		),
		Entry(
			"the show's name repeated in another case",
			"Top.Gear.Polar.Special.720p.HDTV-GRP",
			gear,
			"TOP GEAR - Polar Special",
		),
		Entry("a decomposed accent",
			"Show.Pokemon.Origins.720p.HDTV-GRP", show, "Poke\u0301mon Origins"),
		Entry(
			"a three-digit special number",
			"Top.Gear.S00E100.Patagonia.Special.720p.HDTV-GRP",
			gear,
			"Patagonia Special",
		),
		Entry("OAD before the title",
			"Show.OAD.The.Title.Here.720p.HDTV-GRP", show, "The Title Here"),
		Entry("a number after the marker",
			"[Grp] Show OAD 01 - The Title Here [1080p]", show, "The Title Here"),
		Entry("a title of five characters in another script",
			"Show.進撃の巨人.1080p.WEB-GRP", show, "進撃の巨人"),
	)

	// The season's other titles decide a match that is not word for word.
	DescribeTable("picks among the season's specials",
		func(release string, titles []string, want int) {
			Expect(NamesSpecial(release, show, titles)).To(Equal(want))
		},
		Entry("a year no sibling carries",
			"Show.Christmas.Special.2011.720p.HDTV-GRP",
			[]string{"Christmas Special", "Christmas Special 2012"}, -1),
		Entry("the sibling's year",
			"Show.Christmas.Special.2012.720p.HDTV-GRP",
			[]string{"Christmas Special", "Christmas Special 2012"}, 1),
		Entry("the sibling's year, held as a suffix",
			"Show.Christmas.Special.2012.720p.HDTV-GRP",
			[]string{"Christmas Special", "Christmas Special (2012)"}, 1),
		Entry("no year",
			"Show.Christmas.Special.720p.HDTV-GRP",
			[]string{"Christmas Special", "Christmas Special 2012"}, 0),
		Entry("a tag word the sibling carries",
			"Show.The.Reunion.Extended.720p.HDTV-GRP",
			[]string{"The Reunion", "The Reunion (Extended)"}, 1),
		Entry("no tag word",
			"Show.The.Reunion.720p.HDTV-GRP",
			[]string{"The Reunion", "The Reunion (Extended)"}, 0),
		Entry("the word special a sibling carries",
			"Show.Polar.Special.720p.HDTV-GRP",
			[]string{"Polar", "Polar Special"}, 1),
		Entry("two specials titled alike",
			"Show.Recap.720p.HDTV-GRP", []string{"Recap", "Gag Reel", "Recap"}, -1),
		Entry("a year dropped, no sibling in the way",
			"Show.Christmas.Special.720p.HDTV-GRP",
			[]string{"Gag Reel", "Christmas Special 2012"}, 1),
		Entry("a year added, no sibling in the way",
			"Show.Christmas.Special.2012.720p.HDTV-GRP",
			[]string{"Gag Reel", "Christmas Special"}, 1),
		Entry("another year, the sibling not held",
			"Show.Christmas.Special.2011.720p.HDTV-GRP",
			[]string{"Gag Reel", "Christmas Special 2012"}, -1),
		Entry("another year before the word special",
			"Show.Live.2019.Special.720p.HDTV-GRP", []string{"Live 2020"}, -1),
		Entry("an untitled sibling",
			"Show.The.Title.Here.720p.HDTV-GRP", []string{"", "The Title Here"}, 1),
	)

	DescribeTable("refuses",
		func(release string, shows []string, special string) {
			Expect(NamesSpecial(release, shows, []string{special})).To(Equal(-1))
		},
		Entry("a later part",
			"Top.Gear.Africa.Special.Part.10.720p.HDTV-GRP", gear,
			"Africa Special Part 1"),
		Entry("a part of a title held whole",
			"Top.Gear.Africa.Special.Part.1.720p.HDTV-GRP", gear, "Africa Special"),
		Entry("a generic title, another number",
			"Show.2010.Episode.10.720p.HDTV-GRP", show, "Episode 1"),
		Entry("a generic title, the same number",
			"Show.Episode.1.720p.HDTV-GRP", show, "Episode 1"),
		Entry("Special N", "Show.Special.2.720p.HDTV-GRP", show, "Special 2"),
		Entry("Part N", "Show.Part.2.720p.HDTV-GRP", show, "Part 2"),
		Entry("Chapter N", "Show.Chapter.2.720p.HDTV-GRP", show, "Chapter Two"),
		Entry("OVA N", "Show.OVA.2.720p.HDTV-GRP", show, "OVA 2"),
		Entry(
			"Pilot inside a word",
			"Show.Pilots.Wanted.720p.HDTV-GRP",
			show,
			"Pilot",
		),
		Entry("Pilot", "Show.Pilot.720p.HDTV-GRP", show, "Pilot"),
		Entry("Extras", "Show.Extras.720p.HDTV-GRP", show, "Extras"),
		Entry("Trailer", "Show.Trailer.720p.HDTV-GRP", show, "Trailer"),
		Entry("a title under four characters",
			"Show.Pie.1080p.WEB-GRP", show, "Pie"),
		Entry("a title under four characters past its article",
			"Show.The.Day.1080p.WEB-GRP", show, "The Day"),
		Entry("a longer title on the special's",
			"Show.2010.The.Return.of.the.King.720p.HDTV-GRP", show, "The Return"),
		Entry("a sequel number",
			"Show.The.Movie.2.2021.720p.HDTV-GRP", show, "The Movie"),
		Entry("another work built on the show's name",
			"Doctor.Who.2005.Confidential.The.Day.of.the.Doctor.720p.HDTV-GRP",
			who, "The Day of the Doctor"),
		Entry("a trailer",
			"Doctor.Who.2005.The.Day.of.the.Doctor.Trailer.720p.HDTV-GRP",
			who, "The Day of the Doctor"),
		Entry("deleted scenes",
			"Doctor.Who.2005.The.Day.of.the.Doctor.Deleted.Scenes.720p.HDTV-GRP",
			who, "The Day of the Doctor"),
		Entry("a soundtrack",
			"Doctor.Who.2005.The.Day.of.the.Doctor.OST.FLAC-GRP",
			who, "The Day of the Doctor"),
		Entry("a release group",
			"Show.2010.Some.Other.Special.720p.HDTV.x264-DIMENSION",
			show, "Dimension"),
		Entry("a release group and no tag",
			"Show.2010.Some.Other.Special-RiSE", show, "Rise"),
		Entry("a source tag",
			"Show.2010.Some.Other.Special.1080p.Blu-ray.x264-GRP", show, "Blu-ray"),
		Entry("a source tag as the whole name", "Show.BluRay-GRP", show, "Blu-ray"),
		Entry("a codec tag", "Show.2010.H264-GRP", show, "H264"),
		Entry(
			"an edition tag",
			"Show.2010.Extended.720p.HDTV-GRP",
			show,
			"Extended",
		),
		Entry("inside a word, sunrise",
			"Show.2010.Sunrise.720p.HDTV-GRP", show, "Rise"),
		Entry(
			"another show",
			"Torchwood.The.Day.of.the.Doctor.1080p-GRP",
			who,
			"The Day of the Doctor",
		),
		Entry("the special's title missing",
			"Doctor.Who.2005.1080p.BluRay.x264-GRP", who, "The Day of the Doctor"),
		Entry("the title only inside the show's own name",
			"Doctor.Who.2005.1080p.BluRay-GRP", who, "Doctor"),
		Entry("a regular episode carrying the title",
			"Doctor.Who.S07E05.The.Day.of.the.Doctor.720p.HDTV-GRP", who,
			"The Day of the Doctor"),
		Entry("a season pack carrying the title",
			"Show.S00.The.Title.Here.720p.HDTV-GRP", show, "The Title Here"),
		Entry("a complete pack carrying the title",
			"Show.The.Title.Here.COMPLETE.720p.HDTV-GRP", show, "The Title Here"),
		Entry(
			"a pack with its special",
			"Show.S01.+.The.Title.Here.Special.720p.HDTV-GRP",
			show,
			"The Title Here",
		),
		Entry("an empty show name",
			"The.Title.Here.720p.HDTV-GRP", []string{""}, "The Title Here"),
		Entry("two trailing years",
			"Show.The.Title.Here.2012.2013.720p.HDTV-GRP", show, "The Title Here"),
		Entry("a special titled as its show",
			"Top.Gear.Top.Gear.720p.HDTV-GRP", gear, "Top Gear"),
		Entry("a title of two characters in another script",
			"Show.進撃.1080p.WEB-GRP", show, "進撃"),
		Entry("a source tag spelled in two words",
			"Show.Blu-ray.720p.x264-GRP", show, "Blu-ray"),
		Entry("no show name at all",
			"The.Title.Here.720p.HDTV-GRP", nil, "The Title Here"),
	)
})

var _ = Describe("SpecialQuery", Label("unit", "library"), func() {
	DescribeTable("builds the text asked of an indexer",
		func(show, special, want string) {
			Expect(SpecialQuery(show, special)).To(Equal(want))
		},
		Entry("year suffix and punctuation dropped",
			"Doctor Who (2005)", "The Day of the Doctor (2013)",
			"Doctor Who The Day of the Doctor"),
		Entry("apostrophes closed up, other marks spaced",
			"Show", "Chester's Mill: What's Next?", "Show Chesters Mill Whats Next"),
		Entry("the show's name said once",
			"Top Gear", "Top Gear: Polar Special", "Top Gear Polar Special"),
		Entry("a generic title", "Show", "Episode 3", ""),
		Entry("a short title", "Show", "Pie", ""),
		Entry("no title", "Show", "  ", ""),
		Entry("no show", "", "The Title Here", ""),
		Entry("the show's name repeated in another case",
			"Top Gear", "TOP GEAR: Polar Special", "Top Gear Polar Special"),
		Entry("a special titled as its show", "Top Gear", "Top Gear", ""),
	)
})

var _ = Describe("SpecialTitles", Label("unit", "library"), func() {
	show := &ent.TVShow{Edges: ent.TVShowEdges{Seasons: []*ent.Season{
		{Number: 1, Edges: ent.SeasonEdges{Episodes: []*ent.Episode{
			{ID: 11, Title: "Pilot"},
		}}},
		{Number: 0, Edges: ent.SeasonEdges{Episodes: []*ent.Episode{
			{ID: 1, Title: "Gag Reel"},
			{ID: 2, Title: "The Lost Night"},
			{ID: 3, Title: "Recap"},
		}}},
	}}}

	It("lists a special's title first, then its siblings'", func() {
		Expect(SpecialTitles(show, 2)).To(Equal(
			[]string{"The Lost Night", "Gag Reel", "Recap"},
		))
	})

	It("lists nothing for a regular episode", func() {
		Expect(SpecialTitles(show, 11)).To(BeNil())
	})
})

var _ = Describe("ShowNames", Label("unit", "library"), func() {
	It("lists the title, the original title and every alias", func() {
		Expect(ShowNames(&ent.TVShow{
			Title: "Money Heist", OriginalTitle: "La casa de papel",
			Aliases: []string{"Haus des Geldes", "La Casa de Papel"},
		})).To(Equal([]string{
			"Money Heist", "La casa de papel", "Haus des Geldes", "La Casa de Papel",
		}))
	})
})
