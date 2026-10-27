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
		tr = &ent.Track{
			Title: "Lithium", Disc: 1, Position: 5, Mbid: "rec-5",
			ReleaseTrackMbid: "reltrack-5",
		}
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
			audiotags.KeyMBReleaseTrackID: {"reltrack-5"},
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

	It("tags a track with its own artists", func() {
		tr.Edges.Credits = []*ent.MusicCredit{
			{Kind: musiccredit.KindArtist, Name: "311", Mbid: "a-311"},
		}

		t := library.AlbumTrackTags(ar, al, tr)

		Expect(t[audiotags.KeyArtist]).To(Equal([]string{"311"}))
		Expect(t[audiotags.KeyArtists]).To(Equal([]string{"311"}))
		Expect(t[audiotags.KeyMBArtistID]).To(Equal([]string{"a-311"}))
		Expect(t).To(HaveKeyWithValue(audiotags.KeyArtistSort, BeNil()))
		Expect(t[audiotags.KeyAlbumArtist]).To(Equal([]string{"Nirvana"}))
	})

	It("keeps the album artist when the track is credited to it", func() {
		plain := library.AlbumTrackTags(ar, al, tr)
		tr.Edges.Credits = []*ent.MusicCredit{
			{Kind: musiccredit.KindArtist, Name: "Nirvana", Mbid: "art-1"},
		}

		Expect(library.AlbumTrackTags(ar, al, tr)).To(Equal(plain))
	})

	It("joins several track artists", func() {
		tr.Edges.Credits = []*ent.MusicCredit{
			{Kind: musiccredit.KindArtist, Name: "A", Mbid: "a"},
			{Kind: musiccredit.KindArtist, Name: "B", Mbid: "b"},
			{Kind: musiccredit.KindFeaturing, Name: "C", Mbid: "c"},
		}

		t := library.AlbumTrackTags(ar, al, tr)

		Expect(t[audiotags.KeyArtist]).To(Equal([]string{"A & B feat. C"}))
		Expect(t[audiotags.KeyArtists]).To(Equal([]string{"A", "B", "C"}))
		Expect(t[audiotags.KeyMBArtistID]).To(Equal([]string{"a", "b", "c"}))
	})

	It(
		"leaves the release details out for an album hydrated before they were fetched",
		func() {
			// NULL genres marks an album whose genres, dates and release
			// track ids were never fetched: writing what the row holds would
			// delete the file's GENRE and cut its DATE to a year.
			al.OriginalDate, al.EditionDate, al.Genres = "", "", nil
			tr.ReleaseTrackMbid = ""

			t := library.AlbumTrackTags(ar, al, tr)

			for _, k := range []string{
				audiotags.KeyDate, audiotags.KeyOriginalDate,
				audiotags.KeyGenre, audiotags.KeyMBReleaseTrackID,
			} {
				Expect(t).NotTo(HaveKey(k), k)
			}
			Expect(t[audiotags.KeyAlbum]).To(Equal([]string{"Nevermind"}))
		},
	)

	It("falls back to the artist genre when the release group has none", func() {
		al.Genres = []string{}

		Expect(library.AlbumTrackTags(ar, al, tr)[audiotags.KeyGenre]).
			To(Equal([]string{"Grunge"}))
	})

	It(
		"falls back to the first release's year when no date string is known",
		func() {
			al.OriginalDate, al.EditionDate = "", ""

			t := library.AlbumTrackTags(ar, al, tr)

			Expect(t[audiotags.KeyDate]).To(Equal([]string{"1991"}))
			Expect(t[audiotags.KeyOriginalDate]).To(Equal([]string{"1991"}))
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
	Context("credits", func() {
		BeforeEach(func() {
			at := time.Now()
			al.CreditsFetchedAt, al.CreditsVersion = &at, library.MusicCreditsVersion
			other := al.Edges.Tracks[0]
			other.Edges.Credits = []*ent.MusicCredit{
				{
					Kind: musiccredit.KindCredit,
					Role: musiccredit.RoleRemixer,
					Name: "Remixer Of Track One",
				},
			}
			al.Edges.Credits = []*ent.MusicCredit{
				{
					Kind: musiccredit.KindCredit,
					Role: musiccredit.RoleProducer,
					Name: "Butch Vig",
					Mbid: "a-butch",
				},
				{
					Kind: musiccredit.KindCredit,
					Role: musiccredit.RoleMastering,
					Name: "Howie Weinberg",
				},
				{
					Kind: musiccredit.KindCredit,
					Role: musiccredit.RoleArtwork,
					Name: "Robert Fisher",
				},
				{
					Kind: musiccredit.KindCredit,
					Role: musiccredit.RoleMix,
					Name: "Andy Wallace",
					Mbid: "a-andy",
				},
				{
					Kind: musiccredit.KindCredit,
					Role: musiccredit.RoleRemixer,
					Name: "Remixer Of Track One",
				},
				{
					Kind:        musiccredit.KindPerformer,
					Name:        "Album Personnel",
					Instruments: "drums",
				},
			}
			tr.Edges.Credits = []*ent.MusicCredit{
				{
					Kind: musiccredit.KindWriter,
					Role: musiccredit.RoleComposer,
					Name: "Kurt Cobain",
				},
				{
					Kind: musiccredit.KindWriter,
					Role: musiccredit.RoleComposer,
					Name: "Krist Novoselic",
				},
				{
					Kind: musiccredit.KindWriter,
					Role: musiccredit.RoleLyricist,
					Name: "Kurt Cobain",
				},
				{Kind: musiccredit.KindWriter, Name: "An Old Writer Row"},
				{
					Kind: musiccredit.KindWriter,
					Role: musiccredit.RoleArranger,
					Name: "An Orchestrator",
				},
				{
					Kind: musiccredit.KindCredit,
					Role: musiccredit.RoleMix,
					Name: "Andy Wallace",
					Mbid: "a-andy",
				},
				{
					Kind: musiccredit.KindCredit,
					Role: musiccredit.RoleConductor,
					Name: "A Conductor",
				},
				{
					Kind:        musiccredit.KindPerformer,
					Name:        "Kurt Cobain",
					Instruments: "guitar,lead vocals",
				},
				{
					Kind:        musiccredit.KindPerformer,
					Name:        "Dave Grohl",
					Instruments: "drums",
				},
				{Kind: musiccredit.KindPerformer, Name: "Someone"},
			}
		})

		It(
			"writes the track's writers, its recording's credits and the release's",
			func() {
				t := library.AlbumTrackTags(ar, al, tr)

				Expect(
					t[audiotags.KeyComposer],
				).To(Equal([]string{"Kurt Cobain", "Krist Novoselic"}))
				Expect(t[audiotags.KeyLyricist]).To(Equal([]string{"Kurt Cobain"}))
				Expect(
					t[audiotags.KeyWriter],
				).To(Equal([]string{"An Old Writer Row"}))
				Expect(
					t[audiotags.KeyArranger],
				).To(Equal([]string{"An Orchestrator"}))
				Expect(t[audiotags.KeyMixer]).To(Equal([]string{"Andy Wallace"}))
				Expect(t[audiotags.KeyConductor]).To(Equal([]string{"A Conductor"}))
				Expect(t[audiotags.KeyProducer]).To(Equal([]string{"Butch Vig"}))
				Expect(
					t[audiotags.KeyEngineer],
				).To(Equal([]string{"Howie Weinberg"}))
				Expect(t[audiotags.KeyPerformer]).To(Equal([]string{
					"Kurt Cobain (guitar)",
					"Kurt Cobain (lead vocals)",
					"Dave Grohl (drums)",
					"Someone",
				}))
				Expect(t).To(HaveKeyWithValue(audiotags.KeyRemixer, BeNil()),
					"another recording's credit is not the release's")
			},
		)

		It(
			"leaves the credit tags alone until the album's credits are current",
			func() {
				al.CreditsVersion = 0
				t := library.AlbumTrackTags(ar, al, tr)
				for _, k := range []string{
					audiotags.KeyComposer, audiotags.KeyPerformer, audiotags.KeyProducer,
				} {
					Expect(t).NotTo(HaveKey(k))
				}

				al.CreditsVersion, al.CreditsFetchedAt = library.MusicCreditsVersion, nil
				Expect(
					library.AlbumTrackTags(ar, al, tr),
				).NotTo(HaveKey(audiotags.KeyComposer))
			},
		)
	})
})
