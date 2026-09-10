package db

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/credit"
	"github.com/datahearth/streamline/ent/movie"
	"github.com/datahearth/streamline/ent/person"
	"github.com/datahearth/streamline/ent/tvshow"
	"github.com/datahearth/streamline/internal/metadata"
)

var _ = Describe("Store.ReplaceCast", Label("integration", "db"), func() {
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

	newMovie := func(title string, tmdbID uint32) uint32 {
		GinkgoHelper()
		return client.Movie.Create().
			SetTitle(title).SetOriginalTitle(title).SetYear(1999).
			SetTmdbID(tmdbID).SaveX(ctx).ID
	}

	newShow := func(title string, tvdbID uint32) uint32 {
		GinkgoHelper()
		return client.TVShow.Create().
			SetTitle(title).SetOriginalTitle(title).SetYear(1999).
			SetTvdbID(tvdbID).SaveX(ctx).ID
	}

	movieCredits := func(id uint32) []*ent.Credit {
		GinkgoHelper()
		return client.Credit.Query().
			Where(credit.HasMovieWith(movie.IDEQ(id))).
			WithPerson().
			Order(ent.Asc(credit.FieldOrder)).
			AllX(ctx)
	}

	showCredits := func(id uint32) []*ent.Credit {
		GinkgoHelper()
		return client.Credit.Query().
			Where(credit.HasTvShowWith(tvshow.IDEQ(id))).
			WithPerson().
			Order(ent.Asc(credit.FieldOrder)).
			AllX(ctx)
	}

	Context("person identity", func() {
		It("matches an existing person on tmdb_id", func() {
			first := newMovie("Fight Club", 550)
			second := newMovie("Se7en", 807)

			Expect(
				store.ReplaceCast(ctx, CastOwnerMovie, first, []metadata.CastMember{
					{TMDBID: 819, Name: "Edward Norton", Character: "The Narrator"},
				}),
			).To(Succeed())
			// A different spelling of the name on the second title must not
			// matter: the id decides.
			Expect(
				store.ReplaceCast(ctx, CastOwnerMovie, second, []metadata.CastMember{
					{TMDBID: 819, Name: "Ed Norton", Character: "Somerset"},
				}),
			).To(Succeed())

			Expect(client.Person.Query().CountX(ctx)).To(Equal(1))
			p := client.Person.Query().OnlyX(ctx)
			Expect(p.Name).To(Equal("Edward Norton"))
			Expect(movieCredits(first)[0].Edges.Person.ID).To(Equal(p.ID))
			Expect(movieCredits(second)[0].Edges.Person.ID).To(Equal(p.ID))
		})

		It("matches an existing person on tvdb_id", func() {
			first := newShow("Firefly", 78874)
			second := newShow("Serenity", 78875)

			Expect(
				store.ReplaceCast(ctx, CastOwnerSeries, first, []metadata.CastMember{
					{TVDBID: 237586, Name: "Nathan Fillion", Character: "Mal"},
				}),
			).To(Succeed())
			Expect(
				store.ReplaceCast(
					ctx,
					CastOwnerSeries,
					second,
					[]metadata.CastMember{
						{TVDBID: 237586, Name: "Nathan Fillion", Character: "Mal"},
					},
				),
			).To(Succeed())

			Expect(client.Person.Query().CountX(ctx)).To(Equal(1))
			p := client.Person.Query().OnlyX(ctx)
			Expect(p.TvdbID).To(Equal(uint32(237586)))
			Expect(p.TmdbID).To(BeZero())
		})

		It("matches an id-less person on the folded name", func() {
			first := newMovie("Bridget Jones's Diary", 634)
			second := newMovie("Chicago", 1574)

			Expect(
				store.ReplaceCast(ctx, CastOwnerMovie, first, []metadata.CastMember{
					{Name: "Renée Zellweger", Character: "Bridget"},
				}),
			).To(Succeed())
			Expect(
				store.ReplaceCast(ctx, CastOwnerMovie, second, []metadata.CastMember{
					{Name: "renee zellweger", Character: "Roxie"},
				}),
			).To(Succeed())

			Expect(client.Person.Query().CountX(ctx)).To(Equal(1))
			Expect(
				client.Person.Query().OnlyX(ctx).Name,
			).To(Equal("Renée Zellweger"))
		})

		It("keeps two people apart when their ids contradict", func() {
			first := newMovie("Captain America", 1771)
			second := newMovie("Snowpiercer", 110415)

			Expect(
				store.ReplaceCast(ctx, CastOwnerMovie, first, []metadata.CastMember{
					{TMDBID: 16828, Name: "Chris Evans"},
				}),
			).To(Succeed())
			Expect(
				store.ReplaceCast(ctx, CastOwnerMovie, second, []metadata.CastMember{
					{TMDBID: 99999, Name: "Chris Evans"},
				}),
			).To(Succeed())

			Expect(client.Person.Query().CountX(ctx)).To(Equal(2))
			ids := client.Person.Query().Order(ent.Asc(person.FieldID)).AllX(ctx)
			Expect(ids[0].TmdbID).To(Equal(uint32(16828)))
			Expect(ids[1].TmdbID).To(Equal(uint32(99999)))
		})

		It("creates a person with whatever ids are known", func() {
			id := newMovie("Arrival", 329865)
			Expect(store.ReplaceCast(ctx, CastOwnerMovie, id, []metadata.CastMember{
				{TMDBID: 1245, Name: "Amy Adams", ProfileURL: "https://i/amy.jpg"},
			})).To(Succeed())

			p := client.Person.Query().OnlyX(ctx)
			Expect(p.TmdbID).To(Equal(uint32(1245)))
			Expect(p.TvdbID).To(BeZero())
			Expect(p.ProfileURL).To(Equal("https://i/amy.jpg"))
		})
	})

	Context("enrichment", func() {
		It("gives a name-only person their provider id on the next refresh", func() {
			show := newShow("Firefly", 78874)

			Expect(
				store.ReplaceCast(ctx, CastOwnerSeries, show, []metadata.CastMember{
					{Name: "Gina Torres", Character: "Zoe"},
				}),
			).To(Succeed())
			before := client.Person.Query().OnlyX(ctx)
			Expect(before.TvdbID).To(BeZero())

			Expect(
				store.ReplaceCast(ctx, CastOwnerSeries, show, []metadata.CastMember{
					{
						TVDBID:     237587,
						Name:       "Gina Torres",
						Character:  "Zoe",
						ProfileURL: "https://i/gina.jpg",
					},
				}),
			).To(Succeed())

			Expect(client.Person.Query().CountX(ctx)).To(Equal(1))
			after := client.Person.Query().OnlyX(ctx)
			Expect(after.ID).To(Equal(before.ID))
			Expect(after.TvdbID).To(Equal(uint32(237587)))
			Expect(after.ProfileURL).To(Equal("https://i/gina.jpg"))
		})

		It("fills the other provider's id without touching the one set", func() {
			show := newShow("Firefly", 78874)
			film := newMovie("Serenity", 16320)

			Expect(
				store.ReplaceCast(ctx, CastOwnerSeries, show, []metadata.CastMember{
					{TVDBID: 237586, Name: "Nathan Fillion"},
				}),
			).To(Succeed())
			Expect(
				store.ReplaceCast(ctx, CastOwnerMovie, film, []metadata.CastMember{
					{TMDBID: 12074, Name: "Nathan Fillion"},
				}),
			).To(Succeed())

			Expect(client.Person.Query().CountX(ctx)).To(Equal(1))
			p := client.Person.Query().OnlyX(ctx)
			Expect(p.TvdbID).To(Equal(uint32(237586)))
			Expect(p.TmdbID).To(Equal(uint32(12074)))
		})

		It("never overwrites a non-zero id with a different one", func() {
			film := newMovie("Serenity", 16320)
			Expect(
				store.ReplaceCast(ctx, CastOwnerMovie, film, []metadata.CastMember{
					{TMDBID: 12074, TVDBID: 237586, Name: "Nathan Fillion"},
				}),
			).To(Succeed())
			Expect(
				store.ReplaceCast(ctx, CastOwnerMovie, film, []metadata.CastMember{
					{TMDBID: 12074, TVDBID: 999999, Name: "Nathan Fillion"},
				}),
			).To(Succeed())

			p := client.Person.Query().OnlyX(ctx)
			Expect(p.TvdbID).To(Equal(uint32(237586)))
		})

		It("leaves a stored profile url alone when the refresh omits it", func() {
			film := newMovie("Arrival", 329865)
			Expect(
				store.ReplaceCast(ctx, CastOwnerMovie, film, []metadata.CastMember{
					{
						TMDBID:     1245,
						Name:       "Amy Adams",
						ProfileURL: "https://i/amy.jpg",
					},
				}),
			).To(Succeed())
			Expect(
				store.ReplaceCast(ctx, CastOwnerMovie, film, []metadata.CastMember{
					{TMDBID: 1245, Name: "Amy Adams"},
				}),
			).To(Succeed())

			Expect(client.Person.Query().OnlyX(ctx).ProfileURL).
				To(Equal("https://i/amy.jpg"))
		})
	})

	Context("replacement", func() {
		cast := []metadata.CastMember{
			{TMDBID: 287, Name: "Brad Pitt", Character: "Tyler Durden"},
			{TMDBID: 819, Name: "Edward Norton", Character: "The Narrator"},
		}

		It("does not duplicate credits on a second call", func() {
			film := newMovie("Fight Club", 550)
			Expect(store.ReplaceCast(ctx, CastOwnerMovie, film, cast)).To(Succeed())
			Expect(store.ReplaceCast(ctx, CastOwnerMovie, film, cast)).To(Succeed())

			Expect(movieCredits(film)).To(HaveLen(2))
			Expect(client.Person.Query().CountX(ctx)).To(Equal(2))
		})

		It("drops credits the refreshed cast no longer lists", func() {
			film := newMovie("Fight Club", 550)
			Expect(store.ReplaceCast(ctx, CastOwnerMovie, film, cast)).To(Succeed())
			Expect(
				store.ReplaceCast(ctx, CastOwnerMovie, film, cast[:1]),
			).To(Succeed())

			credits := movieCredits(film)
			Expect(credits).To(HaveLen(1))
			Expect(credits[0].Edges.Person.Name).To(Equal("Brad Pitt"))
			// The dropped person survives their credit — they are still
			// credited elsewhere in the library, or will be again.
			Expect(client.Person.Query().CountX(ctx)).To(Equal(2))
		})

		It("stores billing order as the provider's position", func() {
			film := newMovie("Fight Club", 550)
			Expect(store.ReplaceCast(ctx, CastOwnerMovie, film, cast)).To(Succeed())

			credits := movieCredits(film)
			Expect(credits[0].Order).To(Equal(uint8(0)))
			Expect(credits[0].Character).To(Equal("Tyler Durden"))
			Expect(credits[1].Order).To(Equal(uint8(1)))
			Expect(credits[1].Character).To(Equal("The Narrator"))
		})

		It("leaves stored credits alone when the incoming cast is empty", func() {
			film := newMovie("Fight Club", 550)
			Expect(store.ReplaceCast(ctx, CastOwnerMovie, film, cast)).To(Succeed())
			Expect(store.ReplaceCast(ctx, CastOwnerMovie, film, nil)).To(Succeed())

			Expect(movieCredits(film)).To(HaveLen(2))
		})

		It("rejects an unknown owner kind", func() {
			film := newMovie("Fight Club", 550)
			err := store.ReplaceCast(ctx, CastOwner("episode"), film, cast)
			Expect(err).To(MatchError(errUnknownCastOwner))
		})
	})

	Context("owner kinds", func() {
		It("keeps a movie's and a series' credits independent", func() {
			film := newMovie("Serenity", 16320)
			show := newShow("Firefly", 78874)
			member := []metadata.CastMember{
				{TMDBID: 12074, Name: "Nathan Fillion", Character: "Mal"},
			}

			Expect(
				store.ReplaceCast(ctx, CastOwnerMovie, film, member),
			).To(Succeed())
			Expect(
				store.ReplaceCast(ctx, CastOwnerSeries, show, member),
			).To(Succeed())
			// Replacing one side must not sweep the other's rows.
			Expect(
				store.ReplaceCast(ctx, CastOwnerMovie, film, member),
			).To(Succeed())

			filmCredits := movieCredits(film)
			seriesCredits := showCredits(show)
			Expect(filmCredits).To(HaveLen(1))
			Expect(seriesCredits).To(HaveLen(1))
			Expect(filmCredits[0].Edges.Person.ID).
				To(Equal(seriesCredits[0].Edges.Person.ID))
			Expect(filmCredits[0].QueryTvShow().CountX(ctx)).To(BeZero())
			Expect(seriesCredits[0].QueryMovie().CountX(ctx)).To(BeZero())
		})
	})

	Context("wired into the metadata write paths", func() {
		It("writes credits when a movie is created and refreshed", func() {
			m, err := store.CreateMovie(ctx, CreateMovieParams{
				Title:         "Fight Club",
				OriginalTitle: "Fight Club",
				TmdbID:        550,
				Status:        movie.StatusWanted,
				Cast: []metadata.CastMember{
					{TMDBID: 287, Name: "Brad Pitt", Character: "Tyler Durden"},
				},
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(movieCredits(m.ID)).To(HaveLen(1))

			Expect(store.UpdateMovieMetadata(ctx, m.ID, UpdateMovieMetadataParams{
				Title:         "Fight Club",
				OriginalTitle: "Fight Club",
				Cast: []metadata.CastMember{
					{TMDBID: 287, Name: "Brad Pitt", Character: "Tyler Durden"},
					{TMDBID: 819, Name: "Edward Norton", Character: "The Narrator"},
				},
			})).To(Succeed())
			Expect(movieCredits(m.ID)).To(HaveLen(2))
		})

		It("writes credits when a show is created and refreshed", func() {
			sh, err := store.CreateTVShow(ctx, CreateTVShowParams{
				Title:         "Firefly",
				OriginalTitle: "Firefly",
				TvdbID:        78874,
				Cast: []metadata.CastMember{
					{TVDBID: 237586, Name: "Nathan Fillion", Character: "Mal"},
				},
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(showCredits(sh.ID)).To(HaveLen(1))

			Expect(store.UpdateTVShowMetadata(ctx, sh.ID, UpdateTVShowMetadataParams{
				Title:         "Firefly",
				OriginalTitle: "Firefly",
				Cast: []metadata.CastMember{
					{TVDBID: 237586, Name: "Nathan Fillion", Character: "Mal"},
					{TVDBID: 237587, Name: "Gina Torres", Character: "Zoe"},
				},
			})).To(Succeed())
			Expect(showCredits(sh.ID)).To(HaveLen(2))
		})
	})
})

var _ = Describe("Store.TitleCast", Label("integration", "db"), func() {
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

	It("returns a movie's cast in billing order, each with its person id", func() {
		id := client.Movie.Create().
			SetTitle("Fight Club").SetOriginalTitle("Fight Club").
			SetYear(1999).SetTmdbID(550).SaveX(ctx).ID
		Expect(store.ReplaceCast(ctx, CastOwnerMovie, id, []metadata.CastMember{
			{
				TMDBID:     287,
				Name:       "Brad Pitt",
				Character:  "Tyler Durden",
				ProfileURL: "https://img/bp.jpg",
			},
			{TMDBID: 819, Name: "Edward Norton", Character: "The Narrator"},
		})).To(Succeed())

		cast, err := store.TitleCast(ctx, CastOwnerMovie, id)
		Expect(err).NotTo(HaveOccurred())
		Expect(cast).To(HaveLen(2))
		Expect(cast[0].Name).To(Equal("Brad Pitt"))
		Expect(cast[0].Character).To(Equal("Tyler Durden"))
		Expect(cast[0].TMDBID).To(Equal(uint32(287)))
		Expect(cast[0].ProfileURL).To(Equal("https://img/bp.jpg"))
		Expect(cast[1].Name).To(Equal("Edward Norton"))

		// The person id is the API's only key on a cast entry, so it has to be
		// the persons row and not the provider id it happens to carry.
		Expect(cast[0].PersonID).To(Equal(
			client.Person.Query().Where(person.TmdbIDEQ(287)).OnlyX(ctx).ID,
		))
		Expect(cast[1].PersonID).NotTo(Equal(cast[0].PersonID))
	})

	It("carries the tvdb id of a series actor TMDB never named", func() {
		id := client.TVShow.Create().
			SetTitle("Firefly").SetOriginalTitle("Firefly").
			SetYear(2002).SetTvdbID(78874).SaveX(ctx).ID
		Expect(store.ReplaceCast(ctx, CastOwnerSeries, id, []metadata.CastMember{
			{TVDBID: 237586, Name: "Nathan Fillion", Character: "Mal"},
		})).To(Succeed())

		cast, err := store.TitleCast(ctx, CastOwnerSeries, id)
		Expect(err).NotTo(HaveOccurred())
		Expect(cast).To(HaveLen(1))
		Expect(cast[0].TVDBID).To(Equal(uint32(237586)))
		Expect(cast[0].TMDBID).To(BeZero())
		Expect(cast[0].PersonID).NotTo(BeZero())
	})

	It("returns nothing for a title nothing credits", func() {
		id := client.Movie.Create().
			SetTitle("Se7en").SetOriginalTitle("Se7en").
			SetYear(1995).SetTmdbID(807).SaveX(ctx).ID

		cast, err := store.TitleCast(ctx, CastOwnerMovie, id)
		Expect(err).NotTo(HaveOccurred())
		Expect(cast).To(BeEmpty())
	})

	It("rejects an owner kind it has no credit edge for", func() {
		_, err := store.TitleCast(ctx, CastOwner("album"), 1)
		Expect(err).To(MatchError(errUnknownCastOwner))
	})
})
