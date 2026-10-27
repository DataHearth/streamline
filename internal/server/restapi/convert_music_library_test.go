package restapi

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/internal/media/music"
)

var _ = Describe(
	"music library conversion",
	Label("unit", "server", "music"),
	func() {
		u32 := func(v uint32) *uint32 { return &v }
		str := func(v string) *string { return &v }

		It("carries an album's artists, owner first", func() {
			v := music.AlbumView{
				Album: &ent.Album{ID: 3, Title: "Self Esteem / Down"},
				Artists: []music.PersonView{
					{Name: "The Offspring", MBID: "a-off", ArtistID: 1},
					{Name: "311", MBID: "a-311", ArtistID: 2},
				},
			}
			got := toMusicAlbumSummary(v, 2)
			Expect(got.Artists).NotTo(BeNil())
			Expect(*got.Artists).To(Equal([]MusicPerson{
				{Name: "The Offspring", Mbid: str("a-off"), ArtistId: u32(1)},
				{Name: "311", Mbid: str("a-311"), ArtistId: u32(2)},
			}))
			Expect(toMusicAlbum(v, 2).Artists).To(Equal(got.Artists))
		})

		It("carries a track's artists with their library ids", func() {
			got := toMusicTrack(music.TrackView{
				Track: &ent.Track{ID: 9, Title: "Self Esteem"},
				Artists: []music.PersonView{
					{Name: "311", MBID: "a-311", ArtistID: 2},
				},
			})
			Expect(got.Artists).NotTo(BeNil())
			Expect(*got.Artists).To(Equal([]MusicPerson{
				{Name: "311", Mbid: str("a-311"), ArtistId: u32(2)},
			}))
		})
	},
)
