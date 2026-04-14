package db

import (
	"context"

	"github.com/datahearth/streamline/ent/episode"
	"github.com/datahearth/streamline/ent/tvshow"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("TV schema fields", Label("unit", "db"), func() {
	var ctx context.Context

	BeforeEach(func() {
		ctx = context.Background()
	})

	It("creates a show with the new fields and defaults", func() {
		client, err := Open(ctx, ":memory:")
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { Expect(client.Close()).To(Succeed()) })

		show := client.TVShow.Create().
			SetTitle("The Black Sea").
			SetYear(2023).
			SetTvdbID(123456).
			SetSeriesStatus(tvshow.SeriesStatusContinuing).
			SetType(tvshow.TypeAnime).
			SetNetwork("Halcyon").
			SetGenres([]string{"Drama", "Mystery"}).
			SetRating(8.4).
			SaveX(ctx)

		Expect(show.Monitored).To(BeTrue())
		Expect(show.Type).To(Equal(tvshow.TypeAnime))
		Expect(show.Genres).To(ConsistOf("Drama", "Mystery"))
	})

	It("creates a season + episode with monitored defaults", func() {
		client, err := Open(ctx, ":memory:")
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { Expect(client.Close()).To(Succeed()) })

		show := client.TVShow.Create().
			SetTitle("X").SetYear(2020).SetTvdbID(1).SaveX(ctx)
		season := client.Season.Create().
			SetNumber(1).SetTvShow(show).SaveX(ctx)
		ep := client.Episode.Create().
			SetNumber(1).SetAbsoluteNumber(13).SetSeason(season).SaveX(ctx)

		Expect(season.Monitored).To(BeTrue())
		Expect(ep.Monitored).To(BeTrue())
		Expect(ep.AbsoluteNumber).To(Equal(uint16(13)))
		Expect(ep.Status).To(Equal(episode.StatusWanted))
	})
})
