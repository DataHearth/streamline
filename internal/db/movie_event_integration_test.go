package db

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/movieevent"
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

	createEvent := func(movieID uint32, t movieevent.Type, ago time.Duration) {
		GinkgoHelper()
		client.MovieEvent.Create().
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
		Expect(res.Events[0].Type).To(Equal(movieevent.Type("imported")))
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
			Types: []movieevent.Type{"drift_detected"},
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(res.Events).To(HaveLen(1))
		Expect(res.Events[0].Type).To(Equal(movieevent.Type("drift_detected")))
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

		ids := func(rs []*ent.MovieEvent) []uint32 {
			out := make([]uint32, 0, len(rs))
			for _, r := range rs {
				out = append(out, r.ID)
			}
			return out
		}
		Expect(ids(page2.Events)).NotTo(ContainElements(ids(page1.Events)))
	})
})
