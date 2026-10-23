package db

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/ent"
	entmovie "github.com/datahearth/streamline/ent/movie"
)

var _ = Describe("People (cast)", Label("integration", "db"), func() {
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

	seedMovie := func(title string, tmdbID uint32) *ent.Movie {
		GinkgoHelper()
		m, err := store.CreateMovie(ctx, CreateMovieParams{
			Title:         title,
			OriginalTitle: title,
			Year:          2020,
			TmdbID:        tmdbID,
			Status:        entmovie.StatusWanted,
		})
		Expect(err).NotTo(HaveOccurred())
		return m
	}

	seedShow := func(title string, tvdbID uint32) *ent.TVShow {
		GinkgoHelper()
		s, err := store.CreateTVShow(ctx, CreateTVShowParams{
			Title:  title,
			Year:   2020,
			TvdbID: tvdbID,
		})
		Expect(err).NotTo(HaveOccurred())
		return s
	}

	// seedPerson writes the identity a TVDB-sourced cast member has at rest:
	// no provider id at all, only a name.
	seedPerson := func(name string) *ent.Person {
		GinkgoHelper()
		p, err := client.Person.Create().SetName(name).Save(ctx)
		Expect(err).NotTo(HaveOccurred())
		return p
	}

	creditMovie := func(p *ent.Person, m *ent.Movie, character string) {
		GinkgoHelper()
		_, err := client.Credit.Create().
			SetPerson(p).
			SetMovie(m).
			SetCharacter(character).
			Save(ctx)
		Expect(err).NotTo(HaveOccurred())
	}

	creditShow := func(p *ent.Person, s *ent.TVShow, character string) {
		GinkgoHelper()
		_, err := client.Credit.Create().
			SetPerson(p).
			SetTvShow(s).
			SetCharacter(character).
			Save(ctx)
		Expect(err).NotTo(HaveOccurred())
	}

	Describe("ListPeople", func() {
		It("keeps two provider-less series actors apart", func() {
			first := seedShow("Gamma", 3)
			second := seedShow("Delta", 4)
			zach := seedPerson("Zachary Chasseriaud")
			nina := seedPerson("Nina Meurisse")
			creditShow(zach, first, "Léo")
			creditShow(nina, second, "Claire")

			people, total, err := store.ListPeople(ctx, ListPeopleParams{Limit: 10})
			Expect(err).NotTo(HaveOccurred())
			Expect(total).To(Equal(uint32(2)))
			Expect(people).To(HaveLen(2))
			Expect(people[0].TMDBID).To(BeZero())
			Expect(people[1].TMDBID).To(BeZero())
			Expect(people[0].ID).NotTo(Equal(people[1].ID))
			Expect([]string{people[0].Name, people[1].Name}).
				To(ConsistOf("Zachary Chasseriaud", "Nina Meurisse"))
			Expect(people[0].Credits).To(Equal(uint32(1)))
			Expect(people[1].Credits).To(Equal(uint32(1)))
		})

		It("counts credits across movies and series and orders by credits", func() {
			alpha := seedMovie("Alpha", 1)
			beta := seedMovie("Beta", 2)
			gamma := seedShow("Gamma", 3)
			delta := seedShow("Delta", 4)
			ada, err := client.Person.Create().
				SetName("Ada Lovelace").
				SetTmdbID(10).
				SetProfileURL("https://img/ada.jpg").
				Save(ctx)
			Expect(err).NotTo(HaveOccurred())
			bob := seedPerson("Bob Stone")
			creditMovie(ada, alpha, "Herself")
			creditMovie(ada, beta, "Herself")
			creditShow(ada, gamma, "The Analyst")
			creditMovie(bob, alpha, "Bob")
			creditShow(bob, delta, "Bob")

			people, total, err := store.ListPeople(ctx, ListPeopleParams{Limit: 10})
			Expect(err).NotTo(HaveOccurred())
			Expect(total).To(Equal(uint32(2)))
			Expect(people).To(HaveLen(2))
			Expect(people[0].ID).To(Equal(ada.ID))
			Expect(people[0].TMDBID).To(Equal(uint32(10)))
			Expect(people[0].ProfileURL).To(Equal("https://img/ada.jpg"))
			Expect(people[0].Credits).To(Equal(uint32(3)))
			Expect(people[1].ID).To(Equal(bob.ID))
			Expect(people[1].Credits).To(Equal(uint32(2)))
		})

		It("counts a person credited twice on one title once", func() {
			alpha := seedMovie("Alpha", 1)
			ada := seedPerson("Ada Lovelace")
			creditMovie(ada, alpha, "Herself")
			creditMovie(ada, alpha, "Narrator")

			people, total, err := store.ListPeople(ctx, ListPeopleParams{Limit: 10})
			Expect(err).NotTo(HaveOccurred())
			Expect(total).To(Equal(uint32(1)))
			Expect(people[0].Credits).To(Equal(uint32(1)))
		})

		It("matches a folded name, so beatrice finds Béatrice", func() {
			alpha := seedMovie("Alpha", 1)
			beta := seedMovie("Beta", 2)
			creditMovie(seedPerson("Béatrice Dalle"), alpha, "Betty")
			creditMovie(seedPerson("Bob Stone"), beta, "Bob")

			people, total, err := store.ListPeople(ctx, ListPeopleParams{
				Query: "beatrice",
				Limit: 10,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(total).To(Equal(uint32(1)))
			Expect(people).To(HaveLen(1))
			Expect(people[0].Name).To(Equal("Béatrice Dalle"))
		})

		It("pages with limit and offset against the unpaged total", func() {
			alpha := seedMovie("Alpha", 1)
			beta := seedMovie("Beta", 2)
			ada := seedPerson("Ada Lovelace")
			creditMovie(ada, alpha, "Herself")
			creditMovie(ada, beta, "Herself")
			creditMovie(seedPerson("Bob Stone"), alpha, "Bob")
			creditMovie(seedPerson("Cleo Rain"), beta, "Cleo")

			page, total, err := store.ListPeople(ctx, ListPeopleParams{
				Limit:  1,
				Offset: 1,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(total).To(Equal(uint32(3)))
			Expect(page).To(HaveLen(1))
			// Ada leads on two credits; the tail is ordered by name.
			Expect(page[0].Name).To(Equal("Bob Stone"))
		})

		It("leaves out a person nothing credits", func() {
			seedMovie("Alpha", 1)
			seedShow("Gamma", 3)
			seedPerson("Ada Lovelace")

			people, total, err := store.ListPeople(ctx, ListPeopleParams{Limit: 10})
			Expect(err).NotTo(HaveOccurred())
			Expect(total).To(BeZero())
			Expect(people).To(BeEmpty())
		})
	})

	Describe("PersonCredits", func() {
		It("returns the movie and series credits with their characters", func() {
			alpha := seedMovie("Alpha", 1)
			beta := seedMovie("Beta", 2)
			gamma := seedShow("Gamma", 3)
			ada, err := client.Person.Create().
				SetName("Ada Lovelace").
				SetTvdbID(77).
				SetProfileURL("https://img/ada.jpg").
				Save(ctx)
			Expect(err).NotTo(HaveOccurred())
			creditMovie(ada, alpha, "Herself")
			creditShow(ada, gamma, "The Analyst")
			creditMovie(seedPerson("Bob Stone"), beta, "Bob")

			credits, err := store.PersonCredits(ctx, ada.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(credits.ID).To(Equal(ada.ID))
			Expect(credits.TVDBID).To(Equal(uint32(77)))
			Expect(credits.TMDBID).To(BeZero())
			Expect(credits.Name).To(Equal("Ada Lovelace"))
			Expect(credits.ProfileURL).To(Equal("https://img/ada.jpg"))
			Expect(credits.Movies).To(HaveLen(1))
			Expect(credits.Movies[0].Movie.Title).To(Equal("Alpha"))
			Expect(credits.Movies[0].Character).To(Equal("Herself"))
			Expect(credits.Series).To(HaveLen(1))
			Expect(credits.Series[0].Series.Title).To(Equal("Gamma"))
			Expect(credits.Series[0].Character).To(Equal("The Analyst"))
		})

		It("keeps two provider-less namesakes' credits apart", func() {
			gamma := seedShow("Gamma", 3)
			delta := seedShow("Delta", 4)
			first := seedPerson("Zachary Chasseriaud")
			second := seedPerson("Nina Meurisse")
			creditShow(first, gamma, "Léo")
			creditShow(second, delta, "Claire")

			credits, err := store.PersonCredits(ctx, first.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(credits.Series).To(HaveLen(1))
			Expect(credits.Series[0].Series.Title).To(Equal("Gamma"))
			Expect(credits.Movies).To(BeEmpty())
		})

		It("reports ErrPersonNotFound for an id with no row", func() {
			alpha := seedMovie("Alpha", 1)
			creditMovie(seedPerson("Ada Lovelace"), alpha, "Herself")

			_, err := store.PersonCredits(ctx, 999)
			Expect(err).To(MatchError(ErrPersonNotFound))
		})
	})
})
