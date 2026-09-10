package db

import (
	"context"
	"time"

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

var _ = Describe("Person and credit schema fields", Label("unit", "db"), func() {
	var ctx context.Context

	BeforeEach(func() {
		ctx = context.Background()
	})

	It("defaults both provider ids to zero", func() {
		client, err := Open(ctx, ":memory:")
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { Expect(client.Close()).To(Succeed()) })

		p := client.Person.Create().SetName("Ana Vidal").SaveX(ctx)

		Expect(p.TmdbID).To(BeZero())
		Expect(p.TvdbID).To(BeZero())
		Expect(p.ProfileURL).To(BeEmpty())
		Expect(p.Biography).To(BeEmpty())
		Expect(p.KnownFor).To(BeEmpty())
		Expect(p.Birthday).To(BeEmpty())
		Expect(p.Deathday).To(BeEmpty())
		Expect(p.PlaceOfBirth).To(BeEmpty())
		Expect(p.ImdbID).To(BeEmpty())
		Expect(p.InstagramID).To(BeEmpty())
		Expect(p.TwitterID).To(BeEmpty())
		Expect(p.DetailsFetchedAt).To(BeNil())
	})

	It("stamps details_fetched_at once the provider detail call succeeds", func() {
		client, err := Open(ctx, ":memory:")
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { Expect(client.Close()).To(Succeed()) })

		p := client.Person.Create().SetName("Ana Vidal").SaveX(ctx)
		Expect(p.DetailsFetchedAt).To(BeNil())

		fetchedAt := time.Now().UTC().Truncate(time.Second)
		p = p.Update().
			SetBiography("An actor known for...").
			SetKnownFor("Acting").
			SetBirthday("1984-05-02").
			SetDeathday("").
			SetPlaceOfBirth("Lisbon, Portugal").
			SetImdbID("nm1234567").
			SetInstagramID("ana.vidal").
			SetTwitterID("anavidal").
			SetDetailsFetchedAt(fetchedAt).
			SaveX(ctx)

		Expect(p.Biography).To(Equal("An actor known for..."))
		Expect(p.KnownFor).To(Equal("Acting"))
		Expect(p.Birthday).To(Equal("1984-05-02"))
		Expect(p.PlaceOfBirth).To(Equal("Lisbon, Portugal"))
		Expect(p.ImdbID).To(Equal("nm1234567"))
		Expect(p.InstagramID).To(Equal("ana.vidal"))
		Expect(p.TwitterID).To(Equal("anavidal"))
		Expect(p.DetailsFetchedAt).NotTo(BeNil())
		Expect(*p.DetailsFetchedAt).To(Equal(fetchedAt))
	})

	It("rejects an empty name", func() {
		client, err := Open(ctx, ":memory:")
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { Expect(client.Close()).To(Succeed()) })

		_, err = client.Person.Create().SetName("").Save(ctx)
		Expect(err).To(HaveOccurred())
	})

	It(
		"stores two provider-scoped people sharing the zero id of the other provider",
		func() {
			client, err := Open(ctx, ":memory:")
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(func() { Expect(client.Close()).To(Succeed()) })

			client.Person.Create().SetName("Ana Vidal").SetTmdbID(4021).SaveX(ctx)
			client.Person.Create().SetName("Ilya Koval").SetTvdbID(9911).SaveX(ctx)

			Expect(client.Person.Query().CountX(ctx)).To(Equal(2))
		},
	)

	It("credits a person on a movie and on a show, one owner each", func() {
		client, err := Open(ctx, ":memory:")
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { Expect(client.Close()).To(Succeed()) })

		person := client.Person.Create().
			SetName("Ana Vidal").
			SetTmdbID(4021).
			SaveX(ctx)
		movie := client.Movie.Create().
			SetTitle("The Long Wait").SetOriginalTitle("The Long Wait").
			SetYear(2024).SetTmdbID(700).SaveX(ctx)
		show := client.TVShow.Create().
			SetTitle("The Black Sea").SetYear(2023).SetTvdbID(123456).SaveX(ctx)

		movieCredit := client.Credit.Create().
			SetPerson(person).
			SetMovie(movie).SetCharacter("Nadia").SetOrder(1).SaveX(ctx)
		showCredit := client.Credit.Create().
			SetPerson(person).SetTvShow(show).SaveX(ctx)

		Expect(movieCredit.Character).To(Equal("Nadia"))
		Expect(movieCredit.Order).To(Equal(uint8(1)))
		Expect(showCredit.Character).To(BeEmpty())
		Expect(showCredit.Order).To(BeZero())

		Expect(movieCredit.QueryMovie().OnlyIDX(ctx)).To(Equal(movie.ID))
		Expect(movieCredit.QueryTvShow().CountX(ctx)).To(BeZero())
		Expect(showCredit.QueryTvShow().OnlyIDX(ctx)).To(Equal(show.ID))
		Expect(showCredit.QueryMovie().CountX(ctx)).To(BeZero())

		Expect(person.QueryCredits().CountX(ctx)).To(Equal(2))
		Expect(movie.QueryCredits().OnlyIDX(ctx)).To(Equal(movieCredit.ID))
		Expect(show.QueryCredits().OnlyIDX(ctx)).To(Equal(showCredit.ID))
	})

	It("requires a person on a credit", func() {
		client, err := Open(ctx, ":memory:")
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { Expect(client.Close()).To(Succeed()) })

		movie := client.Movie.Create().
			SetTitle("The Long Wait").SetOriginalTitle("The Long Wait").
			SetYear(2024).SetTmdbID(700).SaveX(ctx)

		_, err = client.Credit.Create().SetMovie(movie).Save(ctx)
		Expect(err).To(HaveOccurred())
	})
})
