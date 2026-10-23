package db

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/ent"
)

var _ = Describe("Store.ListUpcomingAlbums", Label("integration", "db"), func() {
	var (
		ctx    context.Context
		client *ent.Client
		store  *DB
		artist *ent.Artist
		from   time.Time
		to     time.Time
	)

	BeforeEach(func() {
		ctx = context.Background()
		var err error
		client, err = Open(ctx, ":memory:")
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { client.Close() })
		store = New(client)
		artist = client.Artist.Create().
			SetMbid("artist-1").
			SetName("Nirvana").
			SaveX(ctx)
		from = time.Now().UTC().Truncate(time.Minute)
		to = from.Add(7 * 24 * time.Hour)
	})

	seed := func(title string, monitored bool, date *time.Time) {
		GinkgoHelper()
		c := client.Album.Create().
			SetMbid(title + "-mbid").
			SetTitle(title).
			SetMonitored(monitored).
			SetArtist(artist)
		if date != nil {
			c = c.SetReleaseDate(*date)
		}
		c.SaveX(ctx)
	}

	titles := func(albums []*ent.Album) []string {
		out := make([]string, 0, len(albums))
		for _, a := range albums {
			out = append(out, a.Title)
		}
		return out
	}

	It("returns monitored albums in [from,to), oldest first", func() {
		late := from.Add(5 * 24 * time.Hour)
		early := from.Add(24 * time.Hour)
		before := from.Add(-time.Hour)
		seed("late", true, &late)
		seed("early", true, &early)
		seed("unmonitored", false, &early)
		seed("past", true, &before)
		seed("at-to", true, &to)
		seed("undated", true, nil)

		got, err := store.ListUpcomingAlbums(ctx, from, to)
		Expect(err).NotTo(HaveOccurred())
		Expect(titles(got)).To(Equal([]string{"early", "late"}))
		Expect(got[0].Edges.Artist).NotTo(BeNil())
		Expect(got[0].Edges.Artist.Name).To(Equal("Nirvana"))
	})
})
