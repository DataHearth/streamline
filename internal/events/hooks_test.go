package events

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/downloadrecord"
	"github.com/datahearth/streamline/ent/importscanfile"
	"github.com/datahearth/streamline/ent/movieevent"
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

		evs := client.MovieEvent.Query().AllX(ctx)
		Expect(evs).To(HaveLen(1))
		Expect(evs[0].Type).To(Equal(movieevent.Type(TypeGrabbed)))
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

		client.DownloadRecord.UpdateOne(dl).
			SetStatus(downloadrecord.StatusCompleted).
			SaveX(ctx)

		types := allEventTypes(ctx, client)
		Expect(types).To(ConsistOf(
			movieevent.Type(TypeGrabbed),
			movieevent.Type(TypeDownloadCompleted),
		))
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

		failed, err := client.MovieEvent.Query().
			Where(movieevent.TypeEQ(movieevent.Type(TypeDownloadFailed))).
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

		evs := client.MovieEvent.Query().AllX(ctx)
		Expect(evs).To(HaveLen(1))
		Expect(evs[0].Type).To(Equal(movieevent.Type(TypeImported)))
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

			failed, err := client.MovieEvent.Query().
				Where(movieevent.TypeEQ(movieevent.Type(TypeImportFailed))).
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

		count, err := client.MovieEvent.Query().
			Where(movieevent.TypeEQ(movieevent.Type(TypeImportFailed))).
			Count(ctx)
		Expect(err).NotTo(HaveOccurred())
		Expect(count).To(Equal(0))
	})

	It("does not error or emit on an episode-linked record status change", func() {
		show := client.TVShow.Create().
			SetTitle("The Bear").SetYear(2022).SetTvdbID(9999).SaveX(ctx)
		season := client.Season.Create().
			SetNumber(1).SetTvShowID(show.ID).SaveX(ctx)
		ep := client.Episode.Create().
			SetNumber(2).SetSeasonID(season.ID).SaveX(ctx)
		dl := client.DownloadRecord.Create().
			SetEpisodeID(ep.ID).SetTitle("The.Bear.S01E02").SaveX(ctx)

		// Previously the hook's QueryMovie().OnlyID errored (no movie edge),
		// failing the update. SaveX panics if the hook returns an error.
		client.DownloadRecord.UpdateOne(dl).
			SetStatus(downloadrecord.StatusCompleted).SaveX(ctx)

		Expect(client.MovieEvent.Query().CountX(ctx)).To(Equal(0))
	})
})

func allEventTypes(ctx context.Context, c *ent.Client) []movieevent.Type {
	GinkgoHelper()
	rows, err := c.MovieEvent.Query().
		Order(ent.Asc(movieevent.FieldCreateTime)).
		All(ctx)
	Expect(err).NotTo(HaveOccurred())
	out := make([]movieevent.Type, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.Type)
	}
	return out
}
