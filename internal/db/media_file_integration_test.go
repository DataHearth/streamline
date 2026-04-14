package db

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/episode"
	entmovie "github.com/datahearth/streamline/ent/movie"
)

var _ = Describe("MediaFile store", Label("integration", "db"), func() {
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

	Describe("CreateMediaFile", func() {
		It("persists the file linked to a movie", func() {
			m, err := store.CreateMovie(ctx, CreateMovieParams{
				Title: "Dune", OriginalTitle: "Dune", Year: 2021, TmdbID: 999,
				Status: entmovie.StatusWanted, QualityProfile: "HD",
			})
			Expect(err).NotTo(HaveOccurred())

			mf, err := store.CreateMediaFile(ctx, CreateMediaFileParams{
				Path: "/lib/dune.mkv", Size: 1024,
				Quality: "1080p", Format: "mkv", ReleaseGroup: "GROUP",
				MovieID: m.ID,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(mf.Path).To(Equal("/lib/dune.mkv"))
		})

		When("the MovieID points to a non-existent row", func() {
			It("returns a constraint error from the FK violation", func() {
				_, err := store.CreateMediaFile(ctx, CreateMediaFileParams{
					Path: "/x.mkv", Size: 1, MovieID: 99999,
				})
				Expect(err).To(HaveOccurred())
				Expect(ent.IsConstraintError(err)).To(BeTrue())
			})
		})
	})

	Describe("DeleteMediaFileAndRevertEpisode", func() {
		It("deletes the media_file row and reverts the episode to wanted", func() {
			ad := time.Now()
			show, err := store.CreateTVShow(ctx, CreateTVShowParams{
				Title: "The Bear", Year: 2022, TvdbID: 7777,
				Seasons: []SeasonSeed{
					{
						Number: 1,
						Episodes: []EpisodeSeed{
							{Number: 1, Title: "System", AirDate: &ad},
						},
					},
				},
			})
			Expect(err).NotTo(HaveOccurred())
			ep := show.Edges.Seasons[0].Edges.Episodes[0]
			_, err = client.Episode.UpdateOneID(ep.ID).
				SetStatus(episode.StatusAvailable).Save(ctx)
			Expect(err).NotTo(HaveOccurred())

			mf, err := client.MediaFile.Create().
				SetPath("/lib/ep.mkv").SetSize(1).SetEpisodeID(ep.ID).Save(ctx)
			Expect(err).NotTo(HaveOccurred())

			err = store.DeleteMediaFileAndRevertEpisode(ctx, mf.ID, ep.ID)
			Expect(err).NotTo(HaveOccurred())

			_, ferr := client.MediaFile.Get(ctx, mf.ID)
			Expect(ent.IsNotFound(ferr)).To(BeTrue())
			reloaded, err := client.Episode.Get(ctx, ep.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(reloaded.Status).To(Equal(episode.StatusWanted))
		})
	})
})
