package db

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/mediaevent"
)

var _ = Describe("Store.RecentActivity", Label("integration", "db"), func() {
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

	createMovie := func(title string, tmdbID uint32) uint32 {
		GinkgoHelper()
		m := client.Movie.Create().
			SetTitle(title).SetOriginalTitle(title).SetYear(2024).SetTmdbID(tmdbID).
			SaveX(ctx)
		return m.ID
	}

	createEvent := func(movieID uint32, t mediaevent.Type, ago time.Duration) {
		GinkgoHelper()
		client.MediaEvent.Create().
			SetMovieID(movieID).
			SetType(t).
			SetCreateTime(time.Now().Add(-ago)).
			SaveX(ctx)
	}

	It("returns events newest-first with movie eager-loaded", func() {
		mid := createMovie("Fight Club", 550)
		createEvent(mid, "grabbed", 2*time.Minute)
		createEvent(mid, "imported", 1*time.Minute)

		res, err := store.RecentActivity(ctx, ActivityFilter{})
		Expect(err).NotTo(HaveOccurred())
		Expect(res.Events).To(HaveLen(2))
		Expect(res.Events[0].Type).To(Equal(mediaevent.Type("imported")))
		Expect(res.Events[0].Edges.Movie).NotTo(BeNil())
		Expect(res.Events[0].Edges.Movie.Title).To(Equal("Fight Club"))
		Expect(res.NextCursor).To(BeEmpty())
	})

	It("filters by type", func() {
		mid := createMovie("Inception", 27205)
		createEvent(mid, "grabbed", 3*time.Minute)
		createEvent(mid, "drift_detected", 2*time.Minute)
		createEvent(mid, "imported", 1*time.Minute)

		res, err := store.RecentActivity(ctx, ActivityFilter{
			Types: []mediaevent.Type{"drift_detected"},
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(res.Events).To(HaveLen(1))
		Expect(res.Events[0].Type).To(Equal(mediaevent.Type("drift_detected")))
	})

	It("filters by movie id", func() {
		mid1 := createMovie("A", 100)
		mid2 := createMovie("B", 200)
		createEvent(mid1, "imported", 1*time.Minute)
		createEvent(mid2, "imported", 1*time.Minute)

		filter := mid1
		res, err := store.RecentActivity(ctx, ActivityFilter{MovieID: &filter})
		Expect(err).NotTo(HaveOccurred())
		Expect(res.Events).To(HaveLen(1))
		Expect(res.Events[0].Edges.Movie.ID).To(Equal(mid1))
	})

	It("matches the events of an album the artist is co-credited on", func() {
		owner := client.Artist.Create().SetMbid("a1").SetName("311").SaveX(ctx)
		guest := client.Artist.Create().
			SetMbid("a2").
			SetName("The Offspring").
			SaveX(ctx)
		split := client.Album.Create().SetMbid("rg1").SetTitle("Self Esteem / Down").
			SetArtist(owner).AddCoArtists(guest).SaveX(ctx)
		client.MediaEvent.Create().SetType("imported").SetAlbum(split).SaveX(ctx)

		res, err := store.RecentActivity(ctx, ActivityFilter{ArtistID: &guest.ID})
		Expect(err).NotTo(HaveOccurred())
		Expect(res.Events).To(HaveLen(1))
	})

	It("matches an artist's own rows and its albums'", func() {
		gojira := client.Artist.Create().SetMbid("a1").SetName("Gojira").SaveX(ctx)
		other := client.Artist.Create().SetMbid("a2").SetName("Other").SaveX(ctx)
		magma := client.Album.Create().
			SetMbid("rg1").SetTitle("Magma").SetArtist(gojira).SaveX(ctx)
		elsewhere := client.Album.Create().
			SetMbid("rg2").SetTitle("Else").SetArtist(other).SaveX(ctx)
		client.MediaEvent.Create().SetType("grabbed").SetArtist(gojira).SaveX(ctx)
		client.MediaEvent.Create().SetType("imported").SetAlbum(magma).SaveX(ctx)
		client.MediaEvent.Create().SetType("imported").SetAlbum(elsewhere).SaveX(ctx)

		res, err := store.RecentActivity(ctx, ActivityFilter{ArtistID: &gojira.ID})
		Expect(err).NotTo(HaveOccurred())
		Expect(res.Events).To(HaveLen(2))
		var album *ent.Album
		for _, e := range res.Events {
			if e.Edges.Album != nil {
				album = e.Edges.Album
			}
		}
		Expect(album).NotTo(BeNil())
		Expect(album.Edges.Artist).NotTo(BeNil(),
			"the row names the artist beside the album")
		Expect(album.Edges.Artist.Name).To(Equal("Gojira"))
	})

	It("matches a book, and a series with its volumes", func() {
		series := client.BookSeries.Create().
			SetHardcoverID(9).SetTitle("Astérix").SaveX(ctx)
		vol := client.Book.Create().
			SetHardcoverID(1).SetTitle("Astérix le Gaulois").
			SetSeries(series).SaveX(ctx)
		alone := client.Book.Create().
			SetHardcoverID(2).SetTitle("Exhalation").SaveX(ctx)
		client.MediaEvent.Create().SetType("added").SetBookSeries(series).SaveX(ctx)
		client.MediaEvent.Create().SetType("imported").SetBook(vol).SaveX(ctx)
		client.MediaEvent.Create().SetType("imported").SetBook(alone).SaveX(ctx)

		res, err := store.RecentActivity(
			ctx,
			ActivityFilter{BookSeriesID: &series.ID},
		)
		Expect(err).NotTo(HaveOccurred())
		Expect(res.Events).To(HaveLen(2))

		res, err = store.RecentActivity(ctx, ActivityFilter{BookID: &alone.ID})
		Expect(err).NotTo(HaveOccurred())
		Expect(res.Events).To(HaveLen(1))
		Expect(res.Events[0].Edges.Book.ID).To(Equal(alone.ID))

		res, err = store.RecentActivity(ctx, ActivityFilter{BookID: &vol.ID})
		Expect(err).NotTo(HaveOccurred())
		Expect(res.Events).To(HaveLen(1))
		Expect(res.Events[0].Edges.Book.Edges.Series).NotTo(BeNil(),
			"a volume's row names its series")
	})

	It("pages stably with cursors", func() {
		mid := createMovie("Z", 999)
		for i := range 5 {
			createEvent(mid, "grabbed", time.Duration(5-i)*time.Minute)
		}

		page1, err := store.RecentActivity(ctx, ActivityFilter{Limit: 2})
		Expect(err).NotTo(HaveOccurred())
		Expect(page1.Events).To(HaveLen(2))
		Expect(page1.NextCursor).NotTo(BeEmpty())

		page2, err := store.RecentActivity(
			ctx,
			ActivityFilter{Limit: 2, Cursor: page1.NextCursor},
		)
		Expect(err).NotTo(HaveOccurred())
		Expect(page2.Events).To(HaveLen(2))

		ids := func(rs []*ent.MediaEvent) []uint32 {
			out := make([]uint32, 0, len(rs))
			for _, r := range rs {
				out = append(out, r.ID)
			}
			return out
		}
		Expect(ids(page2.Events)).NotTo(ContainElements(ids(page1.Events)))
	})
})
