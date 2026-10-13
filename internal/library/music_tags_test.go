package library_test

import (
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/album"
	"github.com/datahearth/streamline/ent/musiccredit"
	"github.com/datahearth/streamline/internal/library"
	"github.com/datahearth/streamline/internal/library/audiotags"
)

var _ = Describe("AlbumTrackTags", Label("unit", "library"), func() {
	var (
		ar  *ent.Artist
		al  *ent.Album
		tr  *ent.Track
		rel time.Time
	)

	BeforeEach(func() {
		rel = time.Date(1991, 1, 1, 0, 0, 0, 0, time.UTC)
		ar = &ent.Artist{
			Name: "Nirvana", SortName: "Nirvana", Mbid: "art-1", Genre: "Grunge",
		}
		tr = &ent.Track{Title: "Lithium", Disc: 1, Position: 5, Mbid: "rec-5"}
		al = &ent.Album{
			Mbid: "rg-1", ReleaseMbid: "rel-1", Title: "Nevermind",
			Type: album.TypeAlbum, ReleaseDate: &rel,
			OriginalDate: "1991-09-24", EditionDate: "2011-09-26",
			Genres: []string{"Grunge", "Alternative Rock"},
			Label:  "DGC", CatalogNumber: "DGCD-24425", Barcode: "720642442524",
			Country: "US", Media: "cd",
			Edges: ent.AlbumEdges{Tracks: []*ent.Track{
				{Disc: 1, Position: 1},
				{Disc: 1, Position: 2},
				tr,
				{Disc: 2, Position: 1},
			}},
		}
	})

	It("names every key the importer owns, from the database", func() {
		t := library.AlbumTrackTags(ar, al, tr)

		Expect(t).To(Equal(audiotags.Tags{
			audiotags.KeyTitle:            {"Lithium"},
			audiotags.KeyArtist:           {"Nirvana"},
			audiotags.KeyArtists:          {"Nirvana"},
			audiotags.KeyArtistSort:       {"Nirvana"},
			audiotags.KeyAlbum:            {"Nevermind"},
			audiotags.KeyAlbumArtist:      {"Nirvana"},
			audiotags.KeyAlbumArtistSort:  {"Nirvana"},
			audiotags.KeyTrackNumber:      {"5"},
			audiotags.KeyTrackTotal:       {"3"},
			audiotags.KeyDiscNumber:       {"1"},
			audiotags.KeyDiscTotal:        {"2"},
			audiotags.KeyDate:             {"2011-09-26"},
			audiotags.KeyOriginalDate:     {"1991-09-24"},
			audiotags.KeyGenre:            {"Grunge", "Alternative Rock"},
			audiotags.KeyLabel:            {"DGC"},
			audiotags.KeyCatalogNumber:    {"DGCD-24425"},
			audiotags.KeyBarcode:          {"720642442524"},
			audiotags.KeyReleaseCountry:   {"US"},
			audiotags.KeyMedia:            {"CD"},
			audiotags.KeyReleaseType:      {"album"},
			audiotags.KeyCompilation:      nil,
			audiotags.KeyMBArtistID:       {"art-1"},
			audiotags.KeyMBAlbumArtistID:  {"art-1"},
			audiotags.KeyMBAlbumID:        {"rel-1"},
			audiotags.KeyMBReleaseGroupID: {"rg-1"},
			audiotags.KeyMBRecordingID:    {"rec-5"},
		}))
	})

	It("credits featured artists on the track and drops the sort name", func() {
		tr.Edges.Credits = []*ent.MusicCredit{
			{Kind: musiccredit.KindWriter, Name: "Kurt Cobain"},
			{Kind: musiccredit.KindFeaturing, Name: "Guest One", Mbid: "g-1"},
			{Kind: musiccredit.KindFeaturing, Name: "Guest Two"},
			{Kind: musiccredit.KindFeaturing, Name: "Guest Three", Mbid: "g-3"},
		}

		t := library.AlbumTrackTags(ar, al, tr)

		Expect(t[audiotags.KeyArtist]).To(Equal([]string{
			"Nirvana feat. Guest One, Guest Two & Guest Three",
		}))
		Expect(t[audiotags.KeyArtists]).To(Equal([]string{
			"Nirvana", "Guest One", "Guest Two", "Guest Three",
		}))
		Expect(t[audiotags.KeyMBArtistID]).To(Equal([]string{"art-1", "g-1", "g-3"}))
		Expect(t).To(HaveKeyWithValue(audiotags.KeyArtistSort, BeNil()))
		Expect(t[audiotags.KeyAlbumArtistSort]).To(Equal([]string{"Nirvana"}))
	})

	It(
		"falls back to the first release's year and the artist genre for an album stored before them",
		func() {
			al.OriginalDate, al.EditionDate, al.Genres = "", "", nil

			t := library.AlbumTrackTags(ar, al, tr)

			Expect(t[audiotags.KeyDate]).To(Equal([]string{"1991"}))
			Expect(t[audiotags.KeyOriginalDate]).To(Equal([]string{"1991"}))
			Expect(t[audiotags.KeyGenre]).To(Equal([]string{"Grunge"}))
		},
	)

	It("uses the first release as the date when the edition has none", func() {
		al.EditionDate = ""

		Expect(library.AlbumTrackTags(ar, al, tr)[audiotags.KeyDate]).
			To(Equal([]string{"1991-09-24"}))
	})

	It("names keys it has no value for, empty, so stale values are deleted", func() {
		al.Label, al.Barcode, al.ReleaseDate, al.OriginalDate, al.EditionDate = "", "", nil, "", ""
		al.Genres, ar.Genre = []string{}, ""

		t := library.AlbumTrackTags(ar, al, tr)

		for _, k := range []string{
			audiotags.KeyLabel, audiotags.KeyBarcode, audiotags.KeyDate,
			audiotags.KeyOriginalDate, audiotags.KeyGenre,
		} {
			Expect(t).To(HaveKey(k))
			Expect(t[k]).To(BeEmpty(), k)
		}
	})

	It("writes no medium when the release group spans several", func() {
		al.Media = "cd,vinyl"

		Expect(library.AlbumTrackTags(ar, al, tr)[audiotags.KeyMedia]).To(BeEmpty())
	})

	DescribeTable("spells the release type as Picard does",
		func(typ album.Type, want string) {
			al.Type = typ
			Expect(library.AlbumTrackTags(ar, al, tr)[audiotags.KeyReleaseType]).
				To(Equal([]string{want}))
		},
		Entry("album", album.TypeAlbum, "album"),
		Entry("ep", album.TypeEp, "ep"),
		Entry("compilation", album.TypeCompilation, "album; compilation"),
		Entry("live", album.TypeLive, "album; live"),
	)

	It("marks a compilation only when the album artist is Various Artists", func() {
		al.Type = album.TypeCompilation
		Expect(
			library.AlbumTrackTags(ar, al, tr)[audiotags.KeyCompilation],
		).To(BeEmpty())

		ar.Mbid = library.VariousArtistsMBID
		Expect(library.AlbumTrackTags(ar, al, tr)[audiotags.KeyCompilation]).
			To(Equal([]string{"1"}))
	})
})
