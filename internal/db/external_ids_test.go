package db

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/artist"
)

var _ = Describe("external ids", Label("integration", "db"), func() {
	var (
		ctx    context.Context
		client *ent.Client
		store  *DB
	)

	BeforeEach(func() {
		ctx = context.Background()
		var err error
		client, err = Open(ctx, ":memory:")
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { client.Close() })
		store = New(client)
	})

	It("keeps an owner's ids on a nil write, and replaces them on a map", func() {
		a, err := client.Artist.Create().
			SetMbid("a7f7df4a-77d8-4f12-8acd-5c60c93f4de8").
			SetName("Radiohead").SetSortName("Radiohead").SetPath("/m/r").
			SetMonitor(artist.MonitorNone).
			Save(ctx)
		Expect(err).NotTo(HaveOccurred())

		Expect(replaceExternalIDs(ctx, client, ExternalIDsOfArtist, a.ID,
			ExternalIDs{"discogs": "3840", "lastfm": "Radiohead", "spotify": ""},
		)).To(Succeed())
		Expect(replaceExternalIDs(ctx, client, ExternalIDsOfArtist, a.ID, nil)).
			To(Succeed())
		ids, err := store.TitleExternalIDs(ctx, ExternalIDsOfArtist, a.ID)
		Expect(err).NotTo(HaveOccurred())
		Expect(ids).To(Equal(ExternalIDs{"discogs": "3840", "lastfm": "Radiohead"}))

		Expect(replaceExternalIDs(ctx, client, ExternalIDsOfArtist, a.ID,
			ExternalIDs{"discogs": "3840", "not-a-source": "x"},
		)).To(Succeed())
		ids, err = store.TitleExternalIDs(ctx, ExternalIDsOfArtist, a.ID)
		Expect(err).NotTo(HaveOccurred())
		Expect(ids).To(Equal(ExternalIDs{"discogs": "3840"}))
	})

	It("reads many owners' ids in one call, keyed by owner", func() {
		a, err := client.Artist.Create().
			SetMbid("a7f7df4a-77d8-4f12-8acd-5c60c93f4de8").
			SetName("Radiohead").SetSortName("Radiohead").SetPath("/m/r").
			SetMonitor(artist.MonitorNone).
			Save(ctx)
		Expect(err).NotTo(HaveOccurred())
		mk := func(mbid, title string) *ent.Album {
			al, err := client.Album.Create().
				SetMbid(mbid).SetTitle(title).SetArtistID(a.ID).Save(ctx)
			Expect(err).NotTo(HaveOccurred())
			return al
		}
		ok := mk("b1392450-e666-3926-a536-22c65f834433", "OK Computer")
		kid := mk("0f1f4d2a-b3a1-3b06-9a2b-0e9fbdb87a6e", "Kid A")
		bends := mk("f2aaeb8e-8f2b-3b0c-8d1f-1a6e7f0a1b2c", "The Bends")

		Expect(replaceExternalIDs(ctx, client, ExternalIDsOfAlbum, ok.ID,
			ExternalIDs{"discogs": "21491", "spotify": "6dVIqQ8qmQ5GBnJ9shOYGE"},
		)).To(Succeed())
		Expect(replaceExternalIDs(ctx, client, ExternalIDsOfAlbum, kid.ID,
			ExternalIDs{"discogs": "21501"},
		)).To(Succeed())
		// The artist's own ids are another owner's and stay out.
		Expect(replaceExternalIDs(ctx, client, ExternalIDsOfArtist, a.ID,
			ExternalIDs{"discogs": "3840"},
		)).To(Succeed())

		got, err := store.ExternalIDsFor(ctx, ExternalIDsOfAlbum,
			[]uint32{ok.ID, kid.ID, bends.ID})
		Expect(err).NotTo(HaveOccurred())
		Expect(got).To(Equal(map[uint32]ExternalIDs{
			ok.ID:  {"discogs": "21491", "spotify": "6dVIqQ8qmQ5GBnJ9shOYGE"},
			kid.ID: {"discogs": "21501"},
		}))
	})

	It("lists hydrated rows without links after the cursor, and stamps them", func() {
		a, err := client.Artist.Create().
			SetMbid("a7f7df4a-77d8-4f12-8acd-5c60c93f4de8").
			SetName("Radiohead").SetSortName("Radiohead").SetPath("/m/r").
			SetMonitor(artist.MonitorNone).
			Save(ctx)
		Expect(err).NotTo(HaveOccurred())
		hydrated := client.Album.Create().
			SetMbid("b1392450-e666-3926-a536-22c65f834433").SetTitle("OK Computer").
			SetArtistID(a.ID).SetMetadataFetchedAt(time.Now()).SaveX(ctx)
		// A stub gets its links with its tracks.
		client.Album.Create().
			SetMbid("0f1f4d2a-b3a1-3b06-9a2b-0e9fbdb87a6e").SetTitle("Kid A").
			SetArtistID(a.ID).SaveX(ctx)

		albums, err := store.ListAlbumsAwaitingLinks(ctx, 0, 10)
		Expect(err).NotTo(HaveOccurred())
		Expect(albums).To(HaveLen(1))
		Expect(albums[0].ID).To(Equal(hydrated.ID))
		albums, err = store.ListAlbumsAwaitingLinks(ctx, hydrated.ID, 10)
		Expect(err).NotTo(HaveOccurred())
		Expect(albums).To(BeEmpty())

		Expect(store.SetAlbumLinks(ctx, hydrated.ID, nil, time.Now())).To(Succeed())
		albums, err = store.ListAlbumsAwaitingLinks(ctx, 0, 10)
		Expect(err).NotTo(HaveOccurred())
		Expect(albums).To(BeEmpty())

		artists, err := store.ListArtistsAwaitingLinks(ctx, 0, 10)
		Expect(err).NotTo(HaveOccurred())
		Expect(artists).To(HaveLen(1))
		Expect(store.SetArtistLinks(ctx, a.ID, ExternalIDs{"discogs": "3840"}, time.Now())).
			To(Succeed())
		artists, err = store.ListArtistsAwaitingLinks(ctx, 0, 10)
		Expect(err).NotTo(HaveOccurred())
		Expect(artists).To(BeEmpty())
	})

	It("lists only the editions a slot follows, until their links are read", func() {
		b, err := store.CreateBook(ctx, elantris(time.Now()))
		Expect(err).NotTo(HaveOccurred())
		// The English ebook (11) and audiobook (13) are followed; the French
		// ebook (12) is kept but no slot points at it, so it costs nothing.
		eds, err := store.ListEditionsAwaitingLinks(ctx, 0, 10)
		Expect(err).NotTo(HaveOccurred())
		hc := []uint32{}
		for _, e := range eds {
			hc = append(hc, e.HardcoverEditionID)
		}
		Expect(hc).To(ConsistOf(uint32(11), uint32(13)))

		// A stamped edition is never listed again, with links or without.
		Expect(store.SetEditionLinks(ctx, eds[0].ID,
			ExternalIDs{"goodreads": "34168383", "openlibrary": "OL27302768M"}, time.Now(),
		)).To(Succeed())
		Expect(store.SetEditionLinks(ctx, eds[1].ID, ExternalIDs{}, time.Now())).To(Succeed())
		left, err := store.ListEditionsAwaitingLinks(ctx, 0, 10)
		Expect(err).NotTo(HaveOccurred())
		Expect(left).To(BeEmpty())

		ids, err := store.ExternalIDsFor(ctx, ExternalIDsOfBookEdition, []uint32{eds[0].ID, eds[1].ID})
		Expect(err).NotTo(HaveOccurred())
		Expect(ids).To(Equal(map[uint32]ExternalIDs{
			eds[0].ID: {"goodreads": "34168383", "openlibrary": "OL27302768M"},
		}))
		_ = b
	})
})
