package db

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/ent"
	entmovie "github.com/datahearth/streamline/ent/movie"
)

var _ = Describe("Store.UpcomingReleases", Label("integration", "db"), func() {
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

	seed := func(title string, tmdbID uint32, status entmovie.Status, drd *time.Time) {
		GinkgoHelper()
		c := client.Movie.Create().
			SetTitle(title).
			SetOriginalTitle(title).
			SetYear(2025).
			SetTmdbID(tmdbID).
			SetStatus(status)
		if drd != nil {
			c = c.SetDigitalReleaseDate(*drd)
		}
		c.SaveX(ctx)
	}

	It(
		"returns wanted movies inside the [from,to) window, ordered by drd asc",
		func() {
			now := time.Now().UTC().Truncate(time.Minute)
			d2 := now.Add(2 * 24 * time.Hour)
			d4 := now.Add(4 * 24 * time.Hour)
			d10 := now.Add(10 * 24 * time.Hour)
			dPast := now.Add(-24 * time.Hour)

			seed("Inside-Late", 1, entmovie.StatusWanted, &d4)
			seed("Inside-Early", 2, entmovie.StatusWanted, &d2)
			seed("Outside-Future", 3, entmovie.StatusWanted, &d10)
			seed("Outside-Past", 4, entmovie.StatusWanted, &dPast)

			got, err := store.UpcomingReleases(ctx, now, now.Add(7*24*time.Hour))
			Expect(err).NotTo(HaveOccurred())
			Expect(got).To(HaveLen(2))
			Expect(got[0].Title).To(Equal("Inside-Early"))
			Expect(got[1].Title).To(Equal("Inside-Late"))
		},
	)

	It("excludes non-wanted statuses even when drd is in window", func() {
		now := time.Now().UTC().Truncate(time.Minute)
		d3 := now.Add(3 * 24 * time.Hour)

		seed("Available", 1, entmovie.StatusAvailable, &d3)
		seed("Wanted", 2, entmovie.StatusWanted, &d3)

		got, err := store.UpcomingReleases(ctx, now, now.Add(7*24*time.Hour))
		Expect(err).NotTo(HaveOccurred())
		Expect(got).To(HaveLen(1))
		Expect(got[0].Title).To(Equal("Wanted"))
	})

	It("excludes wanted movies with no digital_release_date set", func() {
		now := time.Now().UTC().Truncate(time.Minute)

		seed("NoDRD", 1, entmovie.StatusWanted, nil)

		got, err := store.UpcomingReleases(ctx, now, now.Add(7*24*time.Hour))
		Expect(err).NotTo(HaveOccurred())
		Expect(got).To(BeEmpty())
	})
})
