package music

import (
	"errors"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stretchr/testify/mock"

	"github.com/datahearth/streamline/internal/metadata"
)

var _ = Describe("Artist lookup", Label("unit", "integration", "music"), func() {
	var e *env

	BeforeEach(func() {
		e = newEnv(true)
	})

	date := func(y int) *time.Time {
		t := time.Date(y, 6, 1, 0, 0, 0, 0, time.UTC)
		return &t
	}

	nirvana := func() *metadata.ArtistDetails {
		return &metadata.ArtistDetails{
			MBID: "mbid-n", Name: "Nirvana", SortName: "Nirvana",
			Disambiguation: "90s US grunge band", Type: "group", Genre: "Grunge",
			Area: "Aberdeen, United States", Since: 1987,
			Genres:     []string{"Grunge", "Rock"},
			DeezerID:   415,
			WikidataID: "Q11649",
			Members: []metadata.ArtistMemberInfo{
				{Name: "Dave Grohl", MBID: "m-1"},
				{Name: "Kurt Cobain", MBID: "m-2", Ended: true},
			},
			ReleaseGroups: []metadata.ReleaseGroupInfo{
				{
					MBID:        "rg-1",
					Title:       "Bleach",
					Type:        metadata.AlbumTypeAlbum,
					ReleaseDate: date(1989),
				},
				{MBID: "rg-3", Title: "Undated", Type: metadata.AlbumTypeOther},
				{
					MBID:        "rg-2",
					Title:       "Nevermind",
					Type:        metadata.AlbumTypeAlbum,
					ReleaseDate: date(1991),
				},
			},
		}
	}

	Describe("SearchReleaseGroups", func() {
		hit := func(mbid string) metadata.ReleaseGroupSearchResult {
			return metadata.ReleaseGroupSearchResult{
				MBID: mbid, Title: mbid, Type: metadata.AlbumTypeAlbum,
				ArtistMBID: "mbid-n", ArtistName: "Nirvana",
			}
		}

		It("flags the albums the library holds and remembers the answer", func() {
			e.seedArtist("Nirvana", albumSeed{mbid: "rg-held", title: "Held"})
			e.provider.EXPECT().
				SearchReleaseGroupsFreeText(mock.Anything, "Nirvana").
				Return([]metadata.ReleaseGroupSearchResult{
					hit("rg-held"), hit("rg-new"),
				}, nil).
				Once()

			hits, err := e.svc.SearchReleaseGroups(e.ctx, "Nirvana")
			Expect(err).NotTo(HaveOccurred())
			Expect(hits).To(HaveLen(2))
			Expect(hits[0].AlreadyAdded).To(BeTrue())
			Expect(hits[1].AlreadyAdded).To(BeFalse())

			again, err := e.svc.SearchReleaseGroups(e.ctx, "  nirvana ")
			Expect(err).NotTo(HaveOccurred())
			Expect(again).To(HaveLen(2))
		})

		It("passes a provider failure through and does not cache it", func() {
			e.provider.EXPECT().SearchReleaseGroupsFreeText(mock.Anything, "x1").
				Return(nil, &metadata.RateLimitedError{RetryAfter: time.Minute}).
				Once()
			_, err := e.svc.SearchReleaseGroups(e.ctx, "x1")
			Expect(err).To(MatchError(metadata.ErrRateLimited))

			e.provider.EXPECT().SearchReleaseGroupsFreeText(mock.Anything, "x1").
				Return(nil, nil).Once()
			hits, err := e.svc.SearchReleaseGroups(e.ctx, "x1")
			Expect(err).NotTo(HaveOccurred())
			Expect(hits).To(BeEmpty())
		})
	})

	Describe("SearchArtists", func() {
		It("flags the hits already in the library with one query", func() {
			a, _ := e.seedArtist("Nirvana")
			e.provider.EXPECT().SearchArtists(mock.Anything, "nirvana").Return(
				[]metadata.ArtistResult{
					{MBID: a.Mbid, Name: "Nirvana", Score: 100},
					{MBID: "other", Name: "Nirvana Tribute", Score: 60},
				}, nil).Once()

			hits, err := e.svc.SearchArtists(e.ctx, "nirvana")
			Expect(err).NotTo(HaveOccurred())
			Expect(hits).To(HaveLen(2))
			Expect(hits[0].AlreadyAdded).To(BeTrue())
			Expect(hits[0].LibraryID).To(Equal(a.ID))
			Expect(hits[1].AlreadyAdded).To(BeFalse())
			Expect(hits[1].LibraryID).To(BeZero())
			Expect(hits[1].Score).To(Equal(uint8(60)))
		})

		It("passes a provider failure through", func() {
			e.provider.EXPECT().SearchArtists(mock.Anything, "x").
				Return(nil, &metadata.RateLimitedError{RetryAfter: time.Minute}).
				Once()
			_, err := e.svc.SearchArtists(e.ctx, "x")
			Expect(err).To(MatchError(metadata.ErrRateLimited))
		})
	})

	Describe("LookupArtist", func() {
		It("answers the hit fields and the detail in one object", func() {
			e.provider.EXPECT().
				GetArtist(mock.Anything, "mbid-n").
				Return(nirvana(), nil).
				Once()
			e.overviews.EXPECT().Overviews(mock.Anything, "Q11649", []string{"en"}).
				Return(map[string]metadata.Overview{"en": {Text: "A band."}}, nil).
				Once()

			d, err := e.svc.LookupArtist(e.ctx, "mbid-n", "en")
			Expect(err).NotTo(HaveOccurred())
			Expect(d.MBID).To(Equal("mbid-n"))
			Expect(d.Name).To(Equal("Nirvana"))
			Expect(d.Disambiguation).To(Equal("90s US grunge band"))
			Expect(d.Type).To(Equal("group"))
			Expect(d.Area).To(Equal("Aberdeen, United States"))
			Expect(d.Since).To(Equal(uint16(1987)))
			Expect(d.Genre).To(Equal("Grunge"))
			Expect(d.Genres).To(Equal([]string{"Grunge", "Rock"}))
			Expect(d.Overview).To(Equal("A band."))
			Expect(d.AlreadyAdded).To(BeFalse())
			Expect(d.Score).To(BeZero())
		})

		It("lists only the current members", func() {
			e.provider.EXPECT().
				GetArtist(mock.Anything, "mbid-n").
				Return(nirvana(), nil).
				Once()
			e.overviews.EXPECT().
				Overviews(mock.Anything, mock.Anything, mock.Anything).
				Return(nil, nil).
				Once()

			d, err := e.svc.LookupArtist(e.ctx, "mbid-n", "en")
			Expect(err).NotTo(HaveOccurred())
			Expect(d.Members).To(Equal([]string{"Dave Grohl"}))
		})

		It("lists the release groups newest first, undated last", func() {
			e.provider.EXPECT().
				GetArtist(mock.Anything, "mbid-n").
				Return(nirvana(), nil).
				Once()
			e.overviews.EXPECT().
				Overviews(mock.Anything, mock.Anything, mock.Anything).
				Return(nil, nil).
				Once()

			d, err := e.svc.LookupArtist(e.ctx, "mbid-n", "en")
			Expect(err).NotTo(HaveOccurred())
			titles := make([]string, len(d.Releases))
			for i, r := range d.Releases {
				titles[i] = r.Title
			}
			Expect(titles).To(Equal([]string{"Nevermind", "Bleach", "Undated"}))
			Expect(d.Releases[0].Year).To(Equal(uint16(1991)))
			Expect(d.Releases[2].Year).To(BeZero())
			Expect(d.Releases[2].Type).To(Equal("other"))
		})

		It("serves the French overview, falling back to English", func() {
			e.provider.EXPECT().
				GetArtist(mock.Anything, "mbid-n").
				Return(nirvana(), nil).
				Once()
			e.overviews.EXPECT().
				Overviews(mock.Anything, "Q11649", []string{"fr", "en"}).
				Return(map[string]metadata.Overview{
					"fr": {Text: "Un groupe."}, "en": {Text: "A band."},
				}, nil).
				Once()
			d, err := e.svc.LookupArtist(e.ctx, "mbid-n", "fr")
			Expect(err).NotTo(HaveOccurred())
			Expect(d.Overview).To(Equal("Un groupe."))
		})

		It("falls back to English when French has no article", func() {
			e.provider.EXPECT().
				GetArtist(mock.Anything, "mbid-n").
				Return(nirvana(), nil).
				Once()
			e.overviews.EXPECT().
				Overviews(mock.Anything, "Q11649", []string{"fr", "en"}).
				Return(map[string]metadata.Overview{"en": {Text: "A band."}}, nil).
				Once()
			d, err := e.svc.LookupArtist(e.ctx, "mbid-n", "fr")
			Expect(err).NotTo(HaveOccurred())
			Expect(d.Overview).To(Equal("A band."))
		})

		It("treats an unknown language as English", func() {
			e.provider.EXPECT().
				GetArtist(mock.Anything, "mbid-n").
				Return(nirvana(), nil).
				Once()
			e.overviews.EXPECT().Overviews(mock.Anything, "Q11649", []string{"en"}).
				Return(nil, nil).Once()
			_, err := e.svc.LookupArtist(e.ctx, "mbid-n", "de")
			Expect(err).NotTo(HaveOccurred())
		})

		It("is not failed by Wikipedia", func() {
			e.provider.EXPECT().
				GetArtist(mock.Anything, "mbid-n").
				Return(nirvana(), nil).
				Once()
			e.overviews.EXPECT().
				Overviews(mock.Anything, mock.Anything, mock.Anything).
				Return(nil, errors.New("wikipedia down")).
				Once()
			d, err := e.svc.LookupArtist(e.ctx, "mbid-n", "en")
			Expect(err).NotTo(HaveOccurred())
			Expect(d.Overview).To(BeEmpty())
		})

		It("asks Wikipedia nothing when MusicBrainz links no Wikidata item", func() {
			d := nirvana()
			d.WikidataID = ""
			e.provider.EXPECT().
				GetArtist(mock.Anything, "mbid-n").
				Return(d, nil).
				Once()
			got, err := e.svc.LookupArtist(e.ctx, "mbid-n", "en")
			Expect(err).NotTo(HaveOccurred())
			Expect(got.Overview).To(BeEmpty())
		})

		It(
			"serves a second lookup from the cache but reads the library fresh",
			func() {
				e.provider.EXPECT().
					GetArtist(mock.Anything, "mbid-n").
					Return(nirvana(), nil).
					Once()
				e.overviews.EXPECT().
					Overviews(mock.Anything, mock.Anything, mock.Anything).
					Return(nil, nil).
					Once()

				first, err := e.svc.LookupArtist(e.ctx, "mbid-n", "en")
				Expect(err).NotTo(HaveOccurred())
				Expect(first.AlreadyAdded).To(BeFalse())

				row := e.client.Artist.Create().
					SetMbid("mbid-n").
					SetName("Nirvana").
					SaveX(e.ctx)
				second, err := e.svc.LookupArtist(e.ctx, "mbid-n", "en")
				Expect(err).NotTo(HaveOccurred())
				Expect(second.AlreadyAdded).To(BeTrue())
				Expect(second.LibraryID).To(Equal(row.ID))
			},
		)

		It("reports a missing artist and a rate limit", func() {
			e.provider.EXPECT().GetArtist(mock.Anything, "gone").
				Return(nil, metadata.ErrNotFound).Once()
			_, err := e.svc.LookupArtist(e.ctx, "gone", "en")
			Expect(err).To(MatchError(metadata.ErrNotFound))

			e.provider.EXPECT().GetArtist(mock.Anything, "busy").
				Return(nil, &metadata.RateLimitedError{RetryAfter: time.Minute}).
				Once()
			_, err = e.svc.LookupArtist(e.ctx, "busy", "en")
			Expect(err).To(MatchError(metadata.ErrRateLimited))
		})
	})
})
