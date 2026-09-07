package db

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/ent"
	entmovie "github.com/datahearth/streamline/ent/movie"
	"github.com/datahearth/streamline/ent/transcodejob"
	"github.com/datahearth/streamline/internal/ffmpeg"
)

var _ = Describe("TranscodeJob store", Label("integration", "db"), func() {
	var (
		ctx     context.Context
		client  *ent.Client
		store   *DB
		tmdbSeq uint32
	)

	BeforeEach(func() {
		ctx = context.Background()
		var err error
		client, err = Open(ctx, ":memory:")
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { client.Close() })
		store = New(client)
		tmdbSeq = 0
	})

	// createMovieFile makes a movie with a unique TmdbID and one media file
	// linked to it, mirroring media_file_integration_test.go's fixture.
	createMovieFile := func() *ent.MediaFile {
		GinkgoHelper()
		tmdbSeq++
		m, err := store.CreateMovie(ctx, CreateMovieParams{
			Title: "Dune", OriginalTitle: "Dune", Year: 2021, TmdbID: tmdbSeq,
			Status: entmovie.StatusWanted, QualityProfile: "HD",
		})
		Expect(err).NotTo(HaveOccurred())
		mf, err := store.CreateMediaFile(ctx, CreateMediaFileParams{
			Path: "/lib/dune.mkv", Size: 1024, MovieID: m.ID,
		})
		Expect(err).NotTo(HaveOccurred())
		return mf
	}

	// createEpisodeFile makes a show/season/episode and one media file linked
	// to the episode, for the owner-chain eager-load specs.
	createEpisodeFile := func() *ent.MediaFile {
		GinkgoHelper()
		tmdbSeq++
		ad := time.Now()
		show, err := store.CreateTVShow(ctx, CreateTVShowParams{
			Title: "The Bear", Year: 2022, TvdbID: tmdbSeq,
			Seasons: []SeasonSeed{
				{
					Number: 1,
					Episodes: []EpisodeSeed{
						{Number: 2, Title: "System", AirDate: &ad},
					},
				},
			},
		})
		Expect(err).NotTo(HaveOccurred())
		ep := show.Edges.Seasons[0].Edges.Episodes[0]
		mf, err := store.CreateMediaFile(ctx, CreateMediaFileParams{
			Path: "/lib/bear.mkv", Size: 1024, EpisodeID: ep.ID,
		})
		Expect(err).NotTo(HaveOccurred())
		return mf
	}

	Describe("CreateTranscodeJob", func() {
		It("creates a queued job for the media file", func() {
			mf := createMovieFile()
			job, err := store.CreateTranscodeJob(ctx, mf.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(job.Status).To(Equal(transcodejob.StatusQueued))
			Expect(job.Attempts).To(Equal(uint8(0)))
		})

		It("returns the existing queued/running job instead of duplicating", func() {
			mf := createMovieFile()
			first, err := store.CreateTranscodeJob(ctx, mf.ID)
			Expect(err).NotTo(HaveOccurred())

			second, err := store.CreateTranscodeJob(ctx, mf.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(second.ID).To(Equal(first.ID))

			n, err := client.TranscodeJob.Query().Count(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(n).To(Equal(1))
		})

		It("dedupes against a running job too", func() {
			mf := createMovieFile()
			_, err := store.CreateTranscodeJob(ctx, mf.ID)
			Expect(err).NotTo(HaveOccurred())
			claimed, err := store.ClaimNextTranscodeJob(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(claimed).NotTo(BeNil())

			again, err := store.CreateTranscodeJob(ctx, mf.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(again.ID).To(Equal(claimed.ID))

			n, err := client.TranscodeJob.Query().Count(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(n).To(Equal(1))
		})
	})

	Describe("ClaimNextTranscodeJob", func() {
		It(
			"claims the oldest queued job, bumping attempts and started_at, with owners loaded",
			func() {
				a := createMovieFile()
				b := createMovieFile()
				jobA, err := store.CreateTranscodeJob(ctx, a.ID)
				Expect(err).NotTo(HaveOccurred())
				_, err = store.CreateTranscodeJob(ctx, b.ID)
				Expect(err).NotTo(HaveOccurred())

				claimed, err := store.ClaimNextTranscodeJob(ctx)
				Expect(err).NotTo(HaveOccurred())
				Expect(claimed).NotTo(BeNil())
				Expect(claimed.ID).To(Equal(jobA.ID))
				Expect(claimed.Status).To(Equal(transcodejob.StatusRunning))
				Expect(claimed.Attempts).To(Equal(uint8(1)))
				Expect(claimed.StartedAt).NotTo(BeNil())
				Expect(claimed.Edges.MediaFile).NotTo(BeNil())
				Expect(claimed.Edges.MediaFile.Edges.Movie).NotTo(BeNil())
				Expect(claimed.Edges.MediaFile.Edges.Movie.ID).To(Equal(a.ID))
			},
		)

		It("loads the episode owner chain through season and show", func() {
			ef := createEpisodeFile()
			_, err := store.CreateTranscodeJob(ctx, ef.ID)
			Expect(err).NotTo(HaveOccurred())

			claimed, err := store.ClaimNextTranscodeJob(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(claimed).NotTo(BeNil())
			Expect(claimed.Edges.MediaFile.Edges.Episode).NotTo(BeNil())
			Expect(claimed.Edges.MediaFile.Edges.Episode.Edges.Season).NotTo(BeNil())
			Expect(
				claimed.Edges.MediaFile.Edges.Episode.Edges.Season.Edges.TvShow,
			).NotTo(BeNil())
		})

		It("returns nil, nil when there is nothing queued", func() {
			job, err := store.ClaimNextTranscodeJob(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(job).To(BeNil())
		})

		It(
			"breaks a create_time tie by id, claiming the lower id first",
			func() {
				// create_time is immutable once set (mixin.Time), so the tie is
				// forced at creation via the raw ent client rather than through
				// store.CreateTranscodeJob, which cannot pass one in.
				a := createMovieFile()
				b := createMovieFile()
				tied := time.Now()
				jobA, err := client.TranscodeJob.Create().
					SetMediaFileID(a.ID).SetCreateTime(tied).Save(ctx)
				Expect(err).NotTo(HaveOccurred())
				jobB, err := client.TranscodeJob.Create().
					SetMediaFileID(b.ID).SetCreateTime(tied).Save(ctx)
				Expect(err).NotTo(HaveOccurred())
				Expect(jobA.CreateTime).To(Equal(jobB.CreateTime))
				Expect(jobA.ID).To(BeNumerically("<", jobB.ID))

				claimed, err := store.ClaimNextTranscodeJob(ctx)
				Expect(err).NotTo(HaveOccurred())
				Expect(claimed).NotTo(BeNil())
				Expect(claimed.ID).To(Equal(jobA.ID))
			},
		)

		It(
			"returns nil, nil on a third claim once two jobs are already claimed",
			func() {
				a := createMovieFile()
				b := createMovieFile()
				_, err := store.CreateTranscodeJob(ctx, a.ID)
				Expect(err).NotTo(HaveOccurred())
				_, err = store.CreateTranscodeJob(ctx, b.ID)
				Expect(err).NotTo(HaveOccurred())

				_, err = store.ClaimNextTranscodeJob(ctx)
				Expect(err).NotTo(HaveOccurred())
				_, err = store.ClaimNextTranscodeJob(ctx)
				Expect(err).NotTo(HaveOccurred())

				third, err := store.ClaimNextTranscodeJob(ctx)
				Expect(err).NotTo(HaveOccurred())
				Expect(third).To(BeNil())
			},
		)
	})

	Describe("FailTranscodeJob", func() {
		It(
			"returns a non-terminal failure to queued with the error recorded",
			func() {
				mf := createMovieFile()
				job, err := store.CreateTranscodeJob(ctx, mf.ID)
				Expect(err).NotTo(HaveOccurred())
				_, err = store.ClaimNextTranscodeJob(ctx)
				Expect(err).NotTo(HaveOccurred())

				Expect(
					store.FailTranscodeJob(ctx, job.ID, "transient", false),
				).To(Succeed())

				got, err := client.TranscodeJob.Get(ctx, job.ID)
				Expect(err).NotTo(HaveOccurred())
				Expect(got.Status).To(Equal(transcodejob.StatusQueued))
				Expect(got.Error).To(Equal("transient"))
				Expect(got.FinishedAt).To(BeNil())
			},
		)

		It("terminally fails to failed with error and finished_at", func() {
			mf := createMovieFile()
			job, err := store.CreateTranscodeJob(ctx, mf.ID)
			Expect(err).NotTo(HaveOccurred())
			_, err = store.ClaimNextTranscodeJob(ctx)
			Expect(err).NotTo(HaveOccurred())

			Expect(store.FailTranscodeJob(ctx, job.ID, "boom", true)).To(Succeed())

			got, err := client.TranscodeJob.Get(ctx, job.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(got.Status).To(Equal(transcodejob.StatusFailed))
			Expect(got.Error).To(Equal("boom"))
			Expect(got.FinishedAt).NotTo(BeNil())
		})
	})

	Describe("CompleteTranscodeJob", func() {
		It("marks succeeded with sizes, finished_at, and clears any error", func() {
			mf := createMovieFile()
			job, err := store.CreateTranscodeJob(ctx, mf.ID)
			Expect(err).NotTo(HaveOccurred())
			_, err = store.ClaimNextTranscodeJob(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(
				store.FailTranscodeJob(ctx, job.ID, "retrying", false),
			).To(Succeed())
			_, err = store.ClaimNextTranscodeJob(ctx)
			Expect(err).NotTo(HaveOccurred())

			Expect(store.CompleteTranscodeJob(ctx, job.ID, 2000, 800)).To(Succeed())

			got, err := client.TranscodeJob.Get(ctx, job.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(got.Status).To(Equal(transcodejob.StatusSucceeded))
			Expect(got.SizeBefore).To(Equal(int64(2000)))
			Expect(got.SizeAfter).To(Equal(int64(800)))
			Expect(got.FinishedAt).NotTo(BeNil())
			Expect(got.Error).To(BeEmpty())
		})
	})

	Describe("RetryTranscodeJob", func() {
		It("resets a failed job to queued with attempts and error cleared", func() {
			mf := createMovieFile()
			job, err := store.CreateTranscodeJob(ctx, mf.ID)
			Expect(err).NotTo(HaveOccurred())
			_, err = store.ClaimNextTranscodeJob(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(store.FailTranscodeJob(ctx, job.ID, "boom", true)).To(Succeed())

			Expect(store.RetryTranscodeJob(ctx, job.ID)).To(Succeed())

			got, err := client.TranscodeJob.Get(ctx, job.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(got.Status).To(Equal(transcodejob.StatusQueued))
			Expect(got.Attempts).To(Equal(uint8(0)))
			Expect(got.Error).To(BeEmpty())
			Expect(got.FinishedAt).To(BeNil())
		})

		It("refuses to retry a job that is not failed", func() {
			mf := createMovieFile()
			job, err := store.CreateTranscodeJob(ctx, mf.ID)
			Expect(err).NotTo(HaveOccurred())

			err = store.RetryTranscodeJob(ctx, job.ID)
			Expect(err).To(MatchError(ErrTranscodeJobNotRetryable))
		})
	})

	Describe("MarkTranscodeJobCanceled", func() {
		It("cancels a queued job", func() {
			mf := createMovieFile()
			job, err := store.CreateTranscodeJob(ctx, mf.ID)
			Expect(err).NotTo(HaveOccurred())

			Expect(store.MarkTranscodeJobCanceled(ctx, job.ID)).To(Succeed())

			got, err := client.TranscodeJob.Get(ctx, job.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(got.Status).To(Equal(transcodejob.StatusCanceled))
			Expect(got.FinishedAt).NotTo(BeNil())
		})

		It("cancels a running job", func() {
			mf := createMovieFile()
			job, err := store.CreateTranscodeJob(ctx, mf.ID)
			Expect(err).NotTo(HaveOccurred())
			_, err = store.ClaimNextTranscodeJob(ctx)
			Expect(err).NotTo(HaveOccurred())

			Expect(store.MarkTranscodeJobCanceled(ctx, job.ID)).To(Succeed())

			got, err := client.TranscodeJob.Get(ctx, job.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(got.Status).To(Equal(transcodejob.StatusCanceled))
		})

		It("refuses to cancel a succeeded job", func() {
			mf := createMovieFile()
			job, err := store.CreateTranscodeJob(ctx, mf.ID)
			Expect(err).NotTo(HaveOccurred())
			_, err = store.ClaimNextTranscodeJob(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(store.CompleteTranscodeJob(ctx, job.ID, 100, 50)).To(Succeed())

			err = store.MarkTranscodeJobCanceled(ctx, job.ID)
			Expect(err).To(MatchError(ErrTranscodeJobNotCancelable))
		})
	})

	Describe("ResetRunningTranscodeJobs", func() {
		It("resets every running job back to queued", func() {
			a := createMovieFile()
			b := createMovieFile()
			c := createMovieFile()
			_, err := store.CreateTranscodeJob(ctx, a.ID)
			Expect(err).NotTo(HaveOccurred())
			_, err = store.CreateTranscodeJob(ctx, b.ID)
			Expect(err).NotTo(HaveOccurred())
			jobC, err := store.CreateTranscodeJob(ctx, c.ID)
			Expect(err).NotTo(HaveOccurred())

			_, err = store.ClaimNextTranscodeJob(ctx)
			Expect(err).NotTo(HaveOccurred())
			_, err = store.ClaimNextTranscodeJob(ctx)
			Expect(err).NotTo(HaveOccurred())

			n, err := store.ResetRunningTranscodeJobs(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(n).To(Equal(2))

			rows, err := client.TranscodeJob.Query().All(ctx)
			Expect(err).NotTo(HaveOccurred())
			for _, r := range rows {
				if r.ID == jobC.ID {
					Expect(r.Status).To(Equal(transcodejob.StatusQueued))
					continue
				}
				Expect(r.Status).To(Equal(transcodejob.StatusQueued))
			}
		})
	})

	Describe("ListTranscodeJobs", func() {
		It("lists newest-first with the episode owner chain loaded", func() {
			mf := createMovieFile()
			ef := createEpisodeFile()
			jobMovie, err := store.CreateTranscodeJob(ctx, mf.ID)
			Expect(err).NotTo(HaveOccurred())
			jobEpisode, err := store.CreateTranscodeJob(ctx, ef.ID)
			Expect(err).NotTo(HaveOccurred())

			rows, err := store.ListTranscodeJobs(ctx, 10)
			Expect(err).NotTo(HaveOccurred())
			Expect(rows).To(HaveLen(2))
			Expect(rows[0].ID).To(Equal(jobEpisode.ID))
			Expect(rows[1].ID).To(Equal(jobMovie.ID))

			Expect(rows[0].Edges.MediaFile.Edges.Episode).NotTo(BeNil())
			Expect(rows[0].Edges.MediaFile.Edges.Episode.Edges.Season).NotTo(BeNil())
			Expect(
				rows[0].Edges.MediaFile.Edges.Episode.Edges.Season.Edges.TvShow,
			).NotTo(BeNil())
			Expect(rows[1].Edges.MediaFile.Edges.Movie).NotTo(BeNil())
		})

		It("respects the limit", func() {
			a := createMovieFile()
			b := createMovieFile()
			_, err := store.CreateTranscodeJob(ctx, a.ID)
			Expect(err).NotTo(HaveOccurred())
			_, err = store.CreateTranscodeJob(ctx, b.ID)
			Expect(err).NotTo(HaveOccurred())

			rows, err := store.ListTranscodeJobs(ctx, 1)
			Expect(err).NotTo(HaveOccurred())
			Expect(rows).To(HaveLen(1))
		})
	})

	Describe("ListMediaFilesWithTranscodeOwners", func() {
		It("returns every media file with owner chains loaded", func() {
			createMovieFile()
			createEpisodeFile()

			rows, err := store.ListMediaFilesWithTranscodeOwners(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(rows).To(HaveLen(2))

			var sawMovie, sawEpisode bool
			for _, r := range rows {
				if r.Edges.Movie != nil {
					sawMovie = true
				}
				if r.Edges.Episode != nil {
					sawEpisode = true
					Expect(r.Edges.Episode.Edges.Season).NotTo(BeNil())
					Expect(r.Edges.Episode.Edges.Season.Edges.TvShow).NotTo(BeNil())
				}
			}
			Expect(sawMovie).To(BeTrue())
			Expect(sawEpisode).To(BeTrue())
		})
	})

	Describe("the media_file → transcode_jobs cascade", func() {
		It("lets a file that has jobs be deleted, taking them with it", func() {
			mf := createMovieFile()
			job, err := store.CreateTranscodeJob(ctx, mf.ID)
			Expect(err).NotTo(HaveOccurred())
			claimed, err := store.ClaimNextTranscodeJob(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(claimed).NotTo(BeNil())
			Expect(store.CompleteTranscodeJob(ctx, job.ID, 2000, 900)).
				To(Succeed())

			Expect(store.DeleteMediaFile(ctx, mf.ID)).To(Succeed())

			n, err := client.TranscodeJob.Query().Count(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(n).To(BeZero())
		})

		It("lets the owning movie be deleted through the file's jobs", func() {
			mf := createMovieFile()
			_, err := store.CreateTranscodeJob(ctx, mf.ID)
			Expect(err).NotTo(HaveOccurred())
			owner, err := client.MediaFile.QueryMovie(mf).Only(ctx)
			Expect(err).NotTo(HaveOccurred())

			Expect(store.DeleteMovie(ctx, owner.ID)).To(Succeed())

			n, err := client.TranscodeJob.Query().Count(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(n).To(BeZero())
		})
	})

	Describe("UpdateMediaFileAfterTranscode", func() {
		It("updates the file and restamps the probe from the output", func() {
			mf := createMovieFile()
			before := &ffmpeg.Info{
				Container: "matroska", DurationSec: 5400, VideoCodec: "h264",
				Width: 1920, Height: 1080, AudioCodec: "aac",
				AudioChannels: 2, BitrateBPS: 8_000_000, AudioTracks: 2,
				AudioLangs: "eng", SubLangs: "eng",
			}
			Expect(store.StampMediaFileProbe(ctx, mf.ID, mf.Path, before)).
				To(Succeed())

			after := &ffmpeg.Info{
				Container: "matroska", DurationSec: 5400, VideoCodec: "hevc",
				Width: 1920, Height: 1080, AudioCodec: "aac",
				AudioChannels: 2, BitrateBPS: 3_000_000, AudioTracks: 1,
				AudioLangs: "eng", SubLangs: "fra",
			}
			err := store.UpdateMediaFileAfterTranscode(
				ctx, mf.ID, "/lib/dune.hevc.mkv", 900, 2000, "mkv", after,
			)
			Expect(err).NotTo(HaveOccurred())

			got, err := store.FindMediaFileByID(ctx, mf.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(got.Path).To(Equal("/lib/dune.hevc.mkv"))
			Expect(got.Size).To(Equal(int64(900)))
			Expect(got.Format).To(Equal("mkv"))
			Expect(got.SizeBefore).To(Equal(int64(2000)))
			Expect(got.TranscodedAt).NotTo(BeNil())

			// The row describes the bytes that are now on disk, so nothing
			// scoring it falls back to the release name it was imported under.
			Expect(got.ProbedAt).NotTo(BeNil())
			Expect(got.VideoCodec).To(Equal("hevc"))
			Expect(got.Bitrate).To(Equal(uint32(3_000_000)))
			Expect(got.AudioTracks).To(Equal(uint8(1)))
			Expect(got.SubLangs).To(Equal("fra"))
			Expect(got.Width).To(Equal(uint16(1920)))
			Expect(got.Height).To(Equal(uint16(1080)))
		})
	})
})
