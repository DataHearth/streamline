package library

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/ent"
)

var _ = Describe("music names", Label("unit", "library"), func() {
	hoshino := &ent.Artist{Name: "星野源", SortName: "Hoshino, Gen"}
	episode := &ent.Album{Title: "エピソード", Aliases: []string{"Episode"}}
	ic3peak := &ent.Artist{Name: "IC3PEAK", SortName: "IC3PEAK"}
	skazka := &ent.Album{Title: "Сказка", Aliases: []string{"Fairytale", "Skazka"}}

	It("reads a sort name forwards", func() {
		Expect(ArtistNames(hoshino)).To(Equal([]string{"星野源", "Gen Hoshino"}))
		Expect(ArtistNames(&ent.Artist{
			Name: "Ado", SortName: "Ado", Aliases: []string{"アド"},
		})).To(Equal([]string{"Ado", "アド"}))
		Expect(ArtistNames(ic3peak)).To(Equal([]string{"IC3PEAK"}))
		Expect(
			ArtistNames(
				&ent.Artist{Name: "The Glitch Mob", SortName: "Glitch Mob, The"},
			),
		).
			To(Equal([]string{"The Glitch Mob"}))
		Expect(
			ArtistNames(
				&ent.Artist{
					Name:     "Earth, Wind & Fire",
					SortName: "Earth, Wind & Fire",
				},
			),
		).
			To(Equal([]string{"Earth, Wind & Fire"}))
	})

	DescribeTable("a release under a sort name or an alias",
		func(name string, artist *ent.Artist, album *ent.Album, names bool) {
			Expect(ReleaseNamesAlbum(name, artist, album)).To(Equal(names))
			Expect(ReleaseNamesOtherAlbum(name, artist, album)).To(Equal(!names))
		},
		Entry("romanized artist, translated title",
			"Gen Hoshino - Episode (2011) [FLAC]", hoshino, episode, true),
		Entry("scene style",
			"Gen.Hoshino.Episode.2011.FLAC-GRP", hoshino, episode, true),
		Entry("own script", "星野源 - エピソード [FLAC]", hoshino, episode, true),
		Entry("that artist's other album",
			"Gen Hoshino - Stranger (2013) [FLAC]", hoshino, episode, false),
		Entry("a translated alias",
			"IC3PEAK - Fairytale (2018) [FLAC]", ic3peak, skazka, true),
		Entry("another album",
			"IC3PEAK - Kiss of Death - 2022 (FLAC)", ic3peak, skazka, false),
	)

	It("finds a pack by an artist's sort name", func() {
		Expect(ReleaseNamedByArtist("Gen Hoshino - Discography [FLAC]", hoshino)).
			To(BeTrue())
		Expect(ReleaseNamedByArtist("Gen Hoshi - Discography [FLAC]", hoshino)).
			To(BeFalse())
	})

	It("matches a file to a track by the title another release gives it", func() {
		tracks := []*ent.Track{
			{
				ID:       1,
				Disc:     1,
				Position: 1,
				Title:    "Считалочка",
				Aliases:  []string{"Sčitaločka"},
			},
			{
				ID:       2,
				Disc:     1,
				Position: 2,
				Title:    "Привет",
				Aliases:  []string{"Privet"},
			},
		}
		Expect(matchTitle(tracks, 1, "Privet")).To(Equal(tracks[1]))
		Expect(matchTitle(tracks, 1, "Scitalocka")).To(Equal(tracks[0]))
		Expect(matchTitle(tracks, 1, "Poka")).To(BeNil())
		Expect(TrackTitleAgrees(tracks[1], "Privet")).To(BeTrue())
		Expect(TrackTitleAgrees(tracks[1], "Sčitaločka")).To(BeFalse())
	})

	It("matches a pack folder and a feed title by an alias", func() {
		Expect(MatchPackDir("/dl/pack/2018 - Skazka", []*ent.Album{skazka})).
			To(Equal(skazka))
		Expect(AlbumTitleNamed("Fairytale", skazka)).To(BeTrue())
		Expect(AlbumTitleNamed("Kiss of Death", skazka)).To(BeFalse())
	})
})
