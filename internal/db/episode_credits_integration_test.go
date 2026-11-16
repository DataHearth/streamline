package db

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/credit"
	"github.com/datahearth/streamline/ent/episode"
	"github.com/datahearth/streamline/ent/person"
	"github.com/datahearth/streamline/internal/metadata"
)

var _ = Describe("Episode credits", Label("integration", "db"), func() {
	var (
		ctx    context.Context
		client *ent.Client
		store  *DB
		showID uint32
		season uint32
	)

	BeforeEach(func() {
		ctx = context.Background()
		var err error
		client, err = Open(ctx, ":memory:")
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { client.Close() })
		store = New(client)
		showID = client.TVShow.Create().
			SetTitle("Show").SetOriginalTitle("Show").SetYear(2020).
			SetTvdbID(1).SaveX(ctx).ID
		season = client.Season.Create().
			SetNumber(1).
			SetTvShowID(showID).
			SaveX(ctx).
			ID
	})

	newEpisode := func(number uint16, tvdbID uint32, aired time.Time) uint32 {
		GinkgoHelper()
		return client.Episode.Create().
			SetNumber(number).SetTvdbID(tvdbID).SetAirDate(aired).
			SetSeasonID(season).SaveX(ctx).ID
	}

	stamp := func(id uint32, at time.Time) {
		GinkgoHelper()
		client.Episode.UpdateOneID(id).SetCreditsFetchedAt(at).ExecX(ctx)
	}

	Describe("EpisodesNeedingCredits", func() {
		It(
			"returns aired episodes never read or read within a month of airing",
			func() {
				now := time.Now()
				never := newEpisode(1, 11, now.AddDate(0, -6, 0))
				early := newEpisode(2, 12, now.AddDate(0, 0, -40))
				stamp(early, now.AddDate(0, 0, -35))
				settled := newEpisode(3, 13, now.AddDate(0, -6, 0))
				stamp(settled, now.AddDate(0, -4, 0))
				newEpisode(4, 14, now.AddDate(0, 0, 7))
				newEpisode(5, 0, now.AddDate(0, -1, 0))

				refs, err := store.EpisodesNeedingCredits(ctx, now)
				Expect(err).NotTo(HaveOccurred())
				Expect(refs).To(ConsistOf(
					EpisodeCreditsRef{ID: never, TVDBID: 11},
					EpisodeCreditsRef{ID: early, TVDBID: 12},
				))
			},
		)
	})

	Describe("ReplaceEpisodeCredits", func() {
		It(
			"replaces the episode's credits, leaves the series' alone and stamps it",
			func() {
				ep := newEpisode(1, 11, time.Now().AddDate(0, -1, 0))
				Expect(
					store.ReplaceCast(
						ctx,
						CastOwnerSeries,
						showID,
						[]metadata.CastMember{
							{TVDBID: 1, Name: "Lead", Character: "Hero"},
						},
					),
				).To(Succeed())
				Expect(store.ReplaceEpisodeCredits(ctx, ep, []metadata.CastMember{
					{TVDBID: 2, Name: "Visitor", Character: "Cop"},
				})).To(Succeed())
				Expect(store.ReplaceEpisodeCredits(ctx, ep, []metadata.CastMember{
					{
						TVDBID:     3,
						Name:       "Dee Rector",
						Job:        "Director",
						Department: "Directing",
					},
				})).To(Succeed())

				onEpisode := client.Credit.Query().
					Where(credit.HasEpisodeWith(episode.IDEQ(ep))).
					WithPerson().AllX(ctx)
				Expect(onEpisode).To(ConsistOf(And(
					HaveField("Job", "Director"),
					HaveField("Department", "Directing"),
				)))
				cast, err := store.TitleCast(ctx, CastOwnerSeries, showID)
				Expect(err).NotTo(HaveOccurred())
				Expect(cast).To(ConsistOf(HaveField("Name", "Lead")))
				Expect(client.Episode.GetX(ctx, ep).CreditsFetchedAt).NotTo(BeNil())
			},
		)

		It("clears the credits and stamps the episode on an empty list", func() {
			ep := newEpisode(1, 11, time.Now().AddDate(0, -1, 0))
			Expect(store.ReplaceEpisodeCredits(ctx, ep, []metadata.CastMember{
				{TVDBID: 2, Name: "Visitor", Character: "Cop"},
			})).To(Succeed())
			Expect(store.ReplaceEpisodeCredits(ctx, ep, nil)).To(Succeed())

			Expect(client.Credit.Query().
				Where(credit.HasEpisodeWith(episode.IDEQ(ep))).
				CountX(ctx)).To(BeZero())
			Expect(client.Episode.GetX(ctx, ep).CreditsFetchedAt).NotTo(BeNil())
		})
	})

	Describe("people", func() {
		It(
			"folds a guest's episodes into one series credit, off the People list",
			func() {
				first := newEpisode(1, 11, time.Now().AddDate(0, -2, 0))
				second := newEpisode(2, 12, time.Now().AddDate(0, -1, 0))
				guest := []metadata.CastMember{
					{TVDBID: 2, Name: "Visitor", Character: "Cop"},
				}
				Expect(store.ReplaceEpisodeCredits(ctx, first, guest)).To(Succeed())
				Expect(store.ReplaceEpisodeCredits(ctx, second, guest)).To(Succeed())
				Expect(
					store.ReplaceCast(
						ctx,
						CastOwnerSeries,
						showID,
						[]metadata.CastMember{
							{TVDBID: 1, Name: "Lead", Character: "Hero"},
							{TVDBID: 4, Name: "Penn", Job: "Writer"},
						},
					),
				).To(Succeed())

				listed, err := store.ListPeople(ctx, ListPeopleParams{Limit: 10})
				Expect(err).NotTo(HaveOccurred())
				Expect(
					listed,
				).To(ConsistOf(HaveField("Name", "Lead"), HaveField("Name", "Penn")))

				visitorID := client.Person.Query().
					Where(person.NameEQ("Visitor")).OnlyIDX(ctx)
				credits, err := store.PersonCredits(ctx, visitorID)
				Expect(err).NotTo(HaveOccurred())
				Expect(credits.Series).To(ConsistOf(And(
					HaveField("Series.ID", showID),
					HaveField("Character", "Cop"),
				)))
			},
		)

		It(
			"lists the people with a photo whom the given owner kinds credit",
			func() {
				ep := newEpisode(1, 11, time.Now().AddDate(0, -1, 0))
				Expect(store.ReplaceEpisodeCredits(ctx, ep, []metadata.CastMember{
					{
						TVDBID:     2,
						Name:       "Visitor",
						ProfileURL: "https://example.org/v.jpg",
					},
					{TVDBID: 3, Name: "Faceless"},
				})).To(Succeed())

				onEpisodes, err := store.PeopleWithPhotos(ctx, CastOwnerEpisode)
				Expect(err).NotTo(HaveOccurred())
				Expect(
					onEpisodes,
				).To(ConsistOf(HaveField("ProfileURL", "https://example.org/v.jpg")))
				onMovies, err := store.PeopleWithPhotos(ctx, CastOwnerMovie)
				Expect(err).NotTo(HaveOccurred())
				Expect(onMovies).To(BeEmpty())
			},
		)
	})
})
