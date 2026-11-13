package restapi

import (
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/ent"
	entmediafile "github.com/datahearth/streamline/ent/mediafile"
	"github.com/datahearth/streamline/internal/media/music"
	"github.com/datahearth/streamline/internal/testutil/configtest"
)

var _ = Describe(
	"music library conversion",
	Label("unit", "server", "music"),
	func() {
		u32 := func(v uint32) *uint32 { return &v }
		str := func(v string) *string { return &v }

		It("places an artist under the music root configured now", func() {
			configtest.Setup(map[string]any{
				"library": map[string]any{"music_path": "/srv/music"},
			})
			view := music.ArtistView{
				Artist: &ent.Artist{ID: 1, Name: "AC/DC"},
			}

			Expect(toMusicArtist(view).Path).To(Equal("/srv/music/AC-DC"))
			Expect(toMusicArtistDetail(&view).Path).To(Equal("/srv/music/AC-DC"))
		})

		It("marks a track only when the view says its file is missing", func() {
			track := music.TrackView{Track: &ent.Track{ID: 1}, HasFile: true}
			Expect(toMusicTrack(track).MissingFromDisk).To(BeNil())

			track.MissingFromDisk = true
			Expect(toMusicTrack(track).MissingFromDisk).To(HaveValue(BeTrue()))
		})

		It(
			"marks a book slot only when all its files are confirmed missing",
			func() {
				at := time.Now()
				kind := entmediafile.BookKindEbook
				confirmed := &ent.MediaFile{
					Path: "/b/a.epub", BookKind: kind,
					MissingSince: &at, MissingConfirmedAt: &at,
				}
				b := &ent.Book{
					Edges: ent.BookEdges{MediaFiles: []*ent.MediaFile{confirmed}},
				}
				Expect(
					slotFile(b, string(kind)).MissingFromDisk,
				).To(HaveValue(BeTrue()))

				b.Edges.MediaFiles = []*ent.MediaFile{
					confirmed,
					{Path: "/b/a.pdf", BookKind: kind},
				}
				Expect(slotFile(b, string(kind)).MissingFromDisk).To(BeNil())

				b.Edges.MediaFiles = []*ent.MediaFile{
					{Path: "/b/a.epub", BookKind: kind, MissingSince: &at},
				}
				Expect(slotFile(b, string(kind)).MissingFromDisk).To(BeNil())
			},
		)

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

		It(
			"names a co-album's owner as its artist, whichever page it is read on",
			func() {
				v := music.AlbumView{Album: &ent.Album{
					ID: 3, Edges: ent.AlbumEdges{Artist: &ent.Artist{ID: 1}},
				}}
				Expect(toMusicAlbumSummary(v, 2).ArtistId).To(Equal(uint32(1)))
				Expect(toMusicAlbum(v, 2).ArtistId).To(Equal(uint32(1)))
			},
		)

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
