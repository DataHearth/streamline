package db

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/ent"
	entmovie "github.com/datahearth/streamline/ent/movie"
	"github.com/datahearth/streamline/internal/metadata"
)

var _ = Describe("Person details", Label("integration", "db"), func() {
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

	seedMovie := func(tmdbID uint32) *ent.Movie {
		GinkgoHelper()
		m, err := store.CreateMovie(ctx, CreateMovieParams{
			Title:         "Solaris",
			OriginalTitle: "Solaris",
			Year:          1972,
			TmdbID:        tmdbID,
			Status:        entmovie.StatusWanted,
		})
		Expect(err).NotTo(HaveOccurred())
		return m
	}

	credit := func(p *ent.Person, m *ent.Movie) {
		GinkgoHelper()
		_, err := client.Credit.Create().SetPerson(p).SetMovie(m).Save(ctx)
		Expect(err).NotTo(HaveOccurred())
	}

	details := metadata.PersonDetails{
		Biography:    "An actor.",
		KnownFor:     "Acting",
		Birthday:     "1951-09-25",
		Deathday:     "2016-12-27",
		PlaceOfBirth: "Burbank, California, USA",
		ProfileURL:   "https://img/mh.jpg",
		IMDbID:       "nm0000434",
		InstagramID:  "hamillhimself",
		TwitterID:    "HamillHimself",
	}

	Describe("PeopleNeedingDetails", func() {
		It("returns only the credited people with no fetch stamp", func() {
			m := seedMovie(1)
			fresh, err := client.Person.Create().
				SetName("Mark Hamill").SetTmdbID(1892).Save(ctx)
			Expect(err).NotTo(HaveOccurred())
			credit(fresh, m)

			out, err := store.PeopleNeedingDetails(ctx, CastOwnerMovie, m.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(out).To(HaveLen(1))
			Expect(out[0].ID).To(Equal(fresh.ID))
			Expect(out[0].TMDBID).To(Equal(uint32(1892)))
		})

		It("drops a person once their details have been saved", func() {
			m := seedMovie(2)
			p, err := client.Person.Create().
				SetName("Mark Hamill").SetTmdbID(1892).Save(ctx)
			Expect(err).NotTo(HaveOccurred())
			credit(p, m)

			Expect(store.SavePersonDetails(ctx, p.ID, details)).To(Succeed())

			out, err := store.PeopleNeedingDetails(ctx, CastOwnerMovie, m.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(out).To(BeEmpty())
		})

		It("ignores people credited on a different title", func() {
			mine, other := seedMovie(3), seedMovie(4)
			p, err := client.Person.Create().SetName("Ann Actress").Save(ctx)
			Expect(err).NotTo(HaveOccurred())
			credit(p, other)

			out, err := store.PeopleNeedingDetails(ctx, CastOwnerMovie, mine.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(out).To(BeEmpty())
		})

		It("rejects an unknown owner kind", func() {
			_, err := store.PeopleNeedingDetails(ctx, CastOwner("album"), 1)
			Expect(err).To(MatchError(errUnknownCastOwner))
		})
	})

	Describe("SavePersonDetails", func() {
		It("writes every field and stamps the fetch", func() {
			p, err := client.Person.Create().SetName("Mark Hamill").Save(ctx)
			Expect(err).NotTo(HaveOccurred())

			Expect(store.SavePersonDetails(ctx, p.ID, details)).To(Succeed())

			row, err := client.Person.Get(ctx, p.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(row.Biography).To(Equal("An actor."))
			Expect(row.KnownFor).To(Equal("Acting"))
			Expect(row.Birthday).To(Equal("1951-09-25"))
			Expect(row.Deathday).To(Equal("2016-12-27"))
			Expect(row.PlaceOfBirth).To(Equal("Burbank, California, USA"))
			Expect(row.ImdbID).To(Equal("nm0000434"))
			Expect(row.InstagramID).To(Equal("hamillhimself"))
			Expect(row.TwitterID).To(Equal("HamillHimself"))
			Expect(row.ProfileURL).To(Equal("https://img/mh.jpg"))
			Expect(row.DetailsFetchedAt).NotTo(BeNil())
		})

		It("keeps the stored portrait when the provider record has none", func() {
			p, err := client.Person.Create().
				SetName("Ann Actress").
				SetProfileURL("https://img/cast.jpg").
				Save(ctx)
			Expect(err).NotTo(HaveOccurred())

			Expect(store.SavePersonDetails(ctx, p.ID, metadata.PersonDetails{
				Biography: "A performer.",
			})).To(Succeed())

			row, err := client.Person.Get(ctx, p.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(row.ProfileURL).To(Equal("https://img/cast.jpg"))
			Expect(row.KnownFor).To(BeEmpty())
			Expect(row.DetailsFetchedAt).NotTo(BeNil())
		})
	})

	Describe("the API-facing person shapes", func() {
		It("carries the biographical fields onto both of them", func() {
			m := seedMovie(5)
			p, err := client.Person.Create().
				SetName("Mark Hamill").SetTmdbID(1892).Save(ctx)
			Expect(err).NotTo(HaveOccurred())
			credit(p, m)
			Expect(store.SavePersonDetails(ctx, p.ID, details)).To(Succeed())

			listed, total, err := store.ListPeople(ctx, ListPeopleParams{Limit: 10})
			Expect(err).NotTo(HaveOccurred())
			Expect(total).To(Equal(uint32(1)))
			Expect(listed[0].PersonBio).To(Equal(PersonBio{
				Biography:    details.Biography,
				KnownFor:     details.KnownFor,
				Birthday:     details.Birthday,
				Deathday:     details.Deathday,
				PlaceOfBirth: details.PlaceOfBirth,
				IMDbID:       details.IMDbID,
				InstagramID:  details.InstagramID,
				TwitterID:    details.TwitterID,
			}))

			credits, err := store.PersonCredits(ctx, p.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(credits.PersonBio).To(Equal(listed[0].PersonBio))
		})
	})
})
