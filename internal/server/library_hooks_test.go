package server

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/internal/extension"
	"github.com/datahearth/streamline/internal/testutil/dbtest"
)

var _ = Describe("registerLibraryHooks", Label("unit"), func() {
	var (
		ctx    context.Context
		client *ent.Client
		got    []extension.LibraryChange
	)

	BeforeEach(func() {
		ctx = context.Background()
		client = dbtest.SetupTestDB(ctx)
		DeferCleanup(client.Close)
		got = nil
		registerLibraryHooks(client, func(c extension.LibraryChange) {
			got = append(got, c)
		})
	})

	movie := func() *ent.Movie {
		GinkgoHelper()
		return client.Movie.Create().SetTitle("M").SetOriginalTitle("M").
			SetYear(2024).SetTmdbID(1).SaveX(ctx)
	}

	It("reports a movie that is added, updated and deleted", func() {
		m := movie()
		client.Movie.UpdateOneID(m.ID).SetTitle("N").ExecX(ctx)
		client.Movie.DeleteOneID(m.ID).ExecX(ctx)
		want := extension.LibraryChange{Movies: []uint32{m.ID}}
		Expect(got).To(Equal([]extension.LibraryChange{want, want, want}))
	})

	It("reports a series that is added and deleted", func() {
		show := client.TVShow.Create().SetTitle("S").SetOriginalTitle("S").
			SetYear(2002).SetTvdbID(1).SaveX(ctx)
		client.TVShow.DeleteOneID(show.ID).ExecX(ctx)
		want := extension.LibraryChange{Series: []uint32{show.ID}}
		Expect(got).To(Equal([]extension.LibraryChange{want, want}))
	})

	It("reports the owner of a video file that appears, moves and goes", func() {
		m := movie()
		show := client.TVShow.Create().SetTitle("S").SetOriginalTitle("S").
			SetYear(2002).SetTvdbID(1).SaveX(ctx)
		season := client.Season.Create().SetNumber(1).SetTvShowID(show.ID).
			SaveX(ctx)
		ep := client.Episode.Create().SetNumber(1).SetSeasonID(season.ID).
			SaveX(ctx)
		got = nil

		f := client.MediaFile.Create().SetPath("/m/a.mkv").SetSize(1).
			SetMovie(m).SaveX(ctx)
		e := client.MediaFile.Create().SetPath("/s/e.mkv").SetSize(1).
			SetEpisode(ep).SaveX(ctx)
		client.MediaFile.UpdateOneID(f.ID).SetPath("/m/b.mkv").ExecX(ctx)
		client.MediaFile.UpdateOneID(e.ID).SetMissingSince(time.Now()).
			ExecX(ctx)
		client.MediaFile.UpdateOneID(f.ID).SetProbedAt(time.Now()).ExecX(ctx)
		client.MediaFile.DeleteOneID(e.ID).ExecX(ctx)

		ofMovie := extension.LibraryChange{Movies: []uint32{m.ID}}
		ofEpisode := extension.LibraryChange{Episodes: []uint32{ep.ID}}
		Expect(got).To(Equal([]extension.LibraryChange{
			ofMovie, ofEpisode, ofMovie, ofEpisode, {}, ofEpisode,
		}), "a probe is not a change an extension reads")
	})
})
