package events

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/downloadrecord"
	"github.com/datahearth/streamline/ent/importscanfile"
	"github.com/datahearth/streamline/ent/importscanshow"
	"github.com/datahearth/streamline/ent/mediaevent"
	"github.com/datahearth/streamline/ent/schema"
	"github.com/datahearth/streamline/internal/db"
)

var _ = Describe("hooks via Register", Label("integration", "events"), func() {
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

	It("emits grabbed on DownloadRecord.Create", func() {
		movie := client.Movie.Create().
			SetTitle("Fight Club").
			SetOriginalTitle("Fight Club").
			SetYear(1999).
			SetTmdbID(550).
			SaveX(ctx)

		client.DownloadRecord.Create().
			SetMovieID(movie.ID).
			SetTitle("Fight.Club.1999.1080p.BluRay-X").
			SetSize(4_000_000_000).
			SaveX(ctx)

		evs := eventsOfType(ctx, client, TypeGrabbed)
		Expect(evs).To(HaveLen(1))
		Expect(
			evs[0].Payload,
		).To(HaveKeyWithValue("release_title", "Fight.Club.1999.1080p.BluRay-X"))
	})

	It("emits download_completed on status transition to completed", func() {
		movie := client.Movie.Create().
			SetTitle("Inception").
			SetOriginalTitle("Inception").
			SetYear(2010).
			SetTmdbID(27205).
			SaveX(ctx)

		dl := client.DownloadRecord.Create().
			SetMovieID(movie.ID).
			SetTitle("Inception.2010.1080p").
			SaveX(ctx)

		// Importing, not completed: completed is stamped once the file is
		// already filed, where MediaFile.Create records imported instead.
		client.DownloadRecord.UpdateOne(dl).
			SetStatus(downloadrecord.StatusImporting).
			SaveX(ctx)

		types := allEventTypes(ctx, client)
		Expect(types).To(ConsistOf(
			mediaevent.Type(TypeAdded),
			mediaevent.Type(TypeGrabbed),
			mediaevent.Type(TypeDownloadCompleted),
		))
	})

	It("does not emit download_completed when a record reaches completed", func() {
		movie := client.Movie.Create().
			SetTitle("Dune").
			SetOriginalTitle("Dune").
			SetYear(2021).
			SetTmdbID(438631).
			SaveX(ctx)

		dl := client.DownloadRecord.Create().
			SetMovieID(movie.ID).
			SetTitle("Dune.2021.2160p").
			SaveX(ctx)

		client.DownloadRecord.UpdateOne(dl).
			SetStatus(downloadrecord.StatusCompleted).
			SaveX(ctx)

		Expect(eventsOfType(ctx, client, TypeDownloadCompleted)).To(BeEmpty())
	})

	It("emits import_held_for_review with the checks that failed", func() {
		movie := client.Movie.Create().
			SetTitle("Heat").
			SetOriginalTitle("Heat").
			SetYear(1995).
			SetTmdbID(949).
			SaveX(ctx)

		dl := client.DownloadRecord.Create().
			SetMovieID(movie.ID).
			SetTitle("Heat.1995.1080p").
			SaveX(ctx)

		client.DownloadRecord.UpdateOne(dl).
			SetStatus(downloadrecord.StatusHeld).
			SetHoldReasons([]schema.HoldReason{
				{
					File:     "heat.mkv",
					Check:    "duration",
					Expected: "170m",
					Actual:   "3m",
				},
			}).
			SaveX(ctx)

		held := eventsOfType(ctx, client, TypeImportHeld)
		Expect(held).To(HaveLen(1))
		Expect(held[0].Payload).To(HaveKeyWithValue("held_count", float64(1)))
		Expect(held[0].Payload).To(HaveKey("held_checks"))
	})

	It("emits download_failed with reason payload", func() {
		movie := client.Movie.Create().
			SetTitle("Tenet").
			SetOriginalTitle("Tenet").
			SetYear(2020).
			SetTmdbID(577922).
			SaveX(ctx)

		dl := client.DownloadRecord.Create().
			SetMovieID(movie.ID).
			SetTitle("Tenet.2020").
			SaveX(ctx)

		client.DownloadRecord.UpdateOne(dl).
			SetStatus(downloadrecord.StatusFailed).
			SetFailureReason("seed timeout").
			SaveX(ctx)

		failed, err := client.MediaEvent.Query().
			Where(mediaevent.TypeEQ(mediaevent.Type(TypeDownloadFailed))).
			Only(ctx)
		Expect(err).NotTo(HaveOccurred())
		Expect(failed.Payload).To(HaveKeyWithValue("reason", "seed timeout"))
	})

	It("emits imported on MediaFile.Create with source payload", func() {
		movie := client.Movie.Create().
			SetTitle("Anora").
			SetOriginalTitle("Anora").
			SetYear(2024).
			SetTmdbID(1064213).
			SaveX(ctx)

		client.MediaFile.Create().
			SetMovieID(movie.ID).
			SetPath("/lib/Anora.mkv").
			SetSize(4_000_000_000).
			SetSource("orphan").
			SaveX(ctx)

		evs := eventsOfType(ctx, client, TypeImported)
		Expect(evs).To(HaveLen(1))
		Expect(evs[0].Payload).To(HaveKeyWithValue("source", "orphan"))
	})

	It(
		"emits import_failed when ImportScanFile.outcome → failed and movie is attributable",
		func() {
			movie := client.Movie.Create().
				SetTitle("Drive").
				SetOriginalTitle("Drive").
				SetYear(2011).
				SetTmdbID(64690).
				SaveX(ctx)

			scan := client.ImportScan.Create().
				SetSourcePath("/import").
				SetMode("in_place").
				SaveX(ctx)

			f := client.ImportScanFile.Create().
				SetSourcePath("/import/Drive.2011.mkv").
				SetSize(2_500_000_000).
				SetScan(scan).
				SetExistingMovieID(movie.ID).
				SaveX(ctx)

			client.ImportScanFile.UpdateOne(f).
				SetOutcome(importscanfile.OutcomeFailed).
				SetOutcomeMessage("hardlink rejected").
				SaveX(ctx)

			failed, err := client.MediaEvent.Query().
				Where(mediaevent.TypeEQ(mediaevent.Type(TypeImportFailed))).
				Only(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(failed.Payload).To(HaveKeyWithValue("error", "hardlink rejected"))
		},
	)

	It("does not emit import_failed when no movie is attributable", func() {
		scan := client.ImportScan.Create().
			SetSourcePath("/import").
			SetMode("in_place").
			SaveX(ctx)

		f := client.ImportScanFile.Create().
			SetSourcePath("/import/unknown.mkv").
			SetSize(1).
			SetScan(scan).
			SaveX(ctx)

		client.ImportScanFile.UpdateOne(f).
			SetOutcome(importscanfile.OutcomeFailed).
			SaveX(ctx)

		count, err := client.MediaEvent.Query().
			Where(mediaevent.TypeEQ(mediaevent.Type(TypeImportFailed))).
			Count(ctx)
		Expect(err).NotTo(HaveOccurred())
		Expect(count).To(Equal(0))
	})

	It("records an episode-linked record against its episode", func() {
		show := client.TVShow.Create().
			SetTitle("The Bear").SetYear(2022).SetTvdbID(9999).SaveX(ctx)
		season := client.Season.Create().
			SetNumber(1).SetTvShowID(show.ID).SaveX(ctx)
		ep := client.Episode.Create().
			SetNumber(2).SetSeasonID(season.ID).SaveX(ctx)
		dl := client.DownloadRecord.Create().
			SetEpisodeID(ep.ID).SetTitle("The.Bear.S01E02").SaveX(ctx)

		client.DownloadRecord.UpdateOne(dl).
			SetStatus(downloadrecord.StatusImporting).SaveX(ctx)

		// The show's own `added` row is series-scoped and not part of this;
		// what matters is that every download-record event hangs off the
		// episode rather than the movie edge.
		rows := client.MediaEvent.Query().
			Where(mediaevent.TypeIn(
				mediaevent.Type(TypeGrabbed),
				mediaevent.Type(TypeDownloadCompleted),
			)).
			WithEpisode().WithMovie().AllX(ctx)
		Expect(rows).To(HaveLen(2))
		for _, r := range rows {
			Expect(r.Edges.Episode).NotTo(BeNil())
			Expect(r.Edges.Episode.ID).To(Equal(ep.ID))
			Expect(r.Edges.Movie).To(BeNil())
		}
	})

	It("records an episode import against its episode", func() {
		show := client.TVShow.Create().
			SetTitle("Severance").SetYear(2022).SetTvdbID(8888).SaveX(ctx)
		season := client.Season.Create().
			SetNumber(1).SetTvShowID(show.ID).SaveX(ctx)
		ep := client.Episode.Create().
			SetNumber(1).SetSeasonID(season.ID).SaveX(ctx)

		client.MediaFile.Create().
			SetPath("/tv/Severance/S01E01.mkv").
			SetSize(1).
			SetEpisodeID(ep.ID).
			SaveX(ctx)

		Expect(eventsOfType(ctx, client, TypeImported)).To(HaveLen(1))
	})

	It("records a failed series import against the series", func() {
		show := client.TVShow.Create().
			SetTitle("Andor").SetYear(2022).SetTvdbID(7777).SaveX(ctx)
		scan := client.ImportScan.Create().
			SetSourcePath("/import").
			SetMode("in_place").
			SaveX(ctx)
		row := client.ImportScanShow.Create().
			SetFolderPath("/import/Andor").
			SetScan(scan).
			SetExistingTvshowID(show.ID).
			SaveX(ctx)

		client.ImportScanShow.UpdateOne(row).
			SetOutcome(importscanshow.OutcomeFailed).
			SaveX(ctx)

		rows := client.MediaEvent.Query().
			Where(mediaevent.TypeEQ(mediaevent.Type(TypeImportFailed))).
			WithTvShow().AllX(ctx)
		Expect(rows).To(HaveLen(1))
		Expect(rows[0].Edges.TvShow.ID).To(Equal(show.ID))
	})

	It("emits added when a movie enters the library", func() {
		movie := client.Movie.Create().
			SetTitle("Sicario").
			SetOriginalTitle("Sicario").
			SetYear(2015).
			SetTmdbID(273481).
			SaveX(ctx)

		rows := client.MediaEvent.Query().
			Where(mediaevent.TypeEQ(mediaevent.Type(TypeAdded))).
			WithMovie().AllX(ctx)
		Expect(rows).To(HaveLen(1))
		Expect(rows[0].Edges.Movie.ID).To(Equal(movie.ID))
		Expect(rows[0].Payload).To(HaveKeyWithValue("title", "Sicario"))
	})

	It("emits file_removed with the path when a media file is deleted", func() {
		movie := client.Movie.Create().
			SetTitle("Arrival").
			SetOriginalTitle("Arrival").
			SetYear(2016).
			SetTmdbID(329865).
			SaveX(ctx)

		mf := client.MediaFile.Create().
			SetMovieID(movie.ID).
			SetPath("/lib/Arrival.mkv").
			SetSize(1).
			SaveX(ctx)

		client.MediaFile.DeleteOne(mf).ExecX(ctx)

		removed := eventsOfType(ctx, client, TypeFileRemoved)
		Expect(removed).To(HaveLen(1))
		Expect(removed[0].Payload).To(
			HaveKeyWithValue("path", "/lib/Arrival.mkv"),
		)
	})

	It("stays quiet on a delete made under SuppressFileRemoved", func() {
		movie := client.Movie.Create().
			SetTitle("Prisoners").
			SetOriginalTitle("Prisoners").
			SetYear(2013).
			SetTmdbID(146233).
			SaveX(ctx)

		mf := client.MediaFile.Create().
			SetMovieID(movie.ID).
			SetPath("/lib/Prisoners.mkv").
			SetSize(1).
			SaveX(ctx)

		// The drift sweep records its own diagnosed event for this deletion.
		client.MediaFile.DeleteOne(mf).ExecX(SuppressFileRemoved(ctx))

		Expect(eventsOfType(ctx, client, TypeFileRemoved)).To(BeEmpty())
	})
})

func allEventTypes(ctx context.Context, c *ent.Client) []mediaevent.Type {
	GinkgoHelper()
	rows, err := c.MediaEvent.Query().
		Order(ent.Asc(mediaevent.FieldCreateTime)).
		All(ctx)
	Expect(err).NotTo(HaveOccurred())
	out := make([]mediaevent.Type, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.Type)
	}
	return out
}

func eventsOfType(
	ctx context.Context, c *ent.Client, t Type,
) []*ent.MediaEvent {
	GinkgoHelper()
	rows, err := c.MediaEvent.Query().
		Where(mediaevent.TypeEQ(mediaevent.Type(t))).
		Order(ent.Asc(mediaevent.FieldCreateTime)).
		All(ctx)
	Expect(err).NotTo(HaveOccurred())
	return rows
}
