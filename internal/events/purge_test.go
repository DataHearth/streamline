package events

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/movieevent"
	"github.com/datahearth/streamline/internal/db"
)

var _ = Describe("PurgeOldEvents", Label("integration", "events"), func() {
	var (
		ctx    context.Context
		client *ent.Client
	)

	BeforeEach(func() {
		ctx = context.Background()
		var err error
		client, err = db.Open(ctx, ":memory:")
		Expect(err).NotTo(HaveOccurred())
		Register(client)
		DeferCleanup(func() {
			defaultClient = nil
			Expect(client.Close()).To(Succeed())
		})
	})

	It("deletes events older than the retention window", func() {
		movie := client.Movie.Create().
			SetTitle("Old").
			SetOriginalTitle("Old").SetYear(2020).SetTmdbID(1).SaveX(ctx)

		old := client.MovieEvent.Create().
			SetType(movieevent.Type(TypeGrabbed)).
			SetMovieID(movie.ID).
			SetCreateTime(time.Now().Add(-100 * 24 * time.Hour)).
			SaveX(ctx)
		recent := client.MovieEvent.Create().
			SetType(movieevent.Type(TypeImported)).
			SetMovieID(movie.ID).
			SetCreateTime(time.Now().Add(-1 * 24 * time.Hour)).
			SaveX(ctx)

		n, err := PurgeOldEvents(ctx, 90*24*time.Hour)
		Expect(err).NotTo(HaveOccurred())
		Expect(n).To(Equal(1))

		survivors, err := client.MovieEvent.Query().IDs(ctx)
		Expect(err).NotTo(HaveOccurred())
		Expect(survivors).To(ConsistOf(recent.ID))
		_ = old
	})

	It("returns 0 when nothing is past retention", func() {
		movie := client.Movie.Create().
			SetTitle("Fresh").
			SetOriginalTitle("Fresh").SetYear(2025).SetTmdbID(2).SaveX(ctx)
		client.MovieEvent.Create().
			SetType(movieevent.Type(TypeImported)).
			SetMovieID(movie.ID).
			SaveX(ctx)

		n, err := PurgeOldEvents(ctx, 24*time.Hour)
		Expect(err).NotTo(HaveOccurred())
		Expect(n).To(Equal(0))
	})
})
