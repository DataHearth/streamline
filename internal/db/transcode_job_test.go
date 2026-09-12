package db

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/ent"
	entmovie "github.com/datahearth/streamline/ent/movie"
	"github.com/datahearth/streamline/ent/transcodejob"
)

var _ = Describe("ResetRunningTranscodeJobs", Label("unit", "db"), func() {
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
		DeferCleanup(func() { Expect(client.Close()).To(Succeed()) })
		store = New(client)
		tmdbSeq = 0
	})

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

	It(
		"refunds the claim's attempt and clears started_at on a requeued job",
		func() {
			mf := createMovieFile()
			job, err := store.CreateTranscodeJob(ctx, mf.ID)
			Expect(err).NotTo(HaveOccurred())

			// Drive attempts to 2: claim, a non-terminal failure returns it to
			// queued without touching attempts, then claim again.
			_, err = store.ClaimNextTranscodeJob(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(store.FailTranscodeJob(ctx, job.ID, "transient", false)).
				To(Succeed())
			claimed, err := store.ClaimNextTranscodeJob(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(claimed.Attempts).To(Equal(uint8(2)))
			Expect(claimed.StartedAt).NotTo(BeNil())

			n, err := store.ResetRunningTranscodeJobs(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(n).To(Equal(1))

			got, err := client.TranscodeJob.Get(ctx, job.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(got.Status).To(Equal(transcodejob.StatusQueued))
			Expect(got.Attempts).To(Equal(uint8(1)))
			Expect(got.StartedAt).To(BeNil())
		},
	)

	It("leaves a queued job untouched", func() {
		mf := createMovieFile()
		job, err := store.CreateTranscodeJob(ctx, mf.ID)
		Expect(err).NotTo(HaveOccurred())

		n, err := store.ResetRunningTranscodeJobs(ctx)
		Expect(err).NotTo(HaveOccurred())
		Expect(n).To(BeZero())

		got, err := client.TranscodeJob.Get(ctx, job.ID)
		Expect(err).NotTo(HaveOccurred())
		Expect(got.Status).To(Equal(transcodejob.StatusQueued))
		Expect(got.Attempts).To(Equal(uint8(0)))
	})

	It("returns the count of rows it reset", func() {
		a := createMovieFile()
		b := createMovieFile()
		c := createMovieFile()
		_, err := store.CreateTranscodeJob(ctx, a.ID)
		Expect(err).NotTo(HaveOccurred())
		_, err = store.CreateTranscodeJob(ctx, b.ID)
		Expect(err).NotTo(HaveOccurred())
		_, err = store.CreateTranscodeJob(ctx, c.ID)
		Expect(err).NotTo(HaveOccurred())

		_, err = store.ClaimNextTranscodeJob(ctx)
		Expect(err).NotTo(HaveOccurred())
		_, err = store.ClaimNextTranscodeJob(ctx)
		Expect(err).NotTo(HaveOccurred())

		n, err := store.ResetRunningTranscodeJobs(ctx)
		Expect(err).NotTo(HaveOccurred())
		Expect(n).To(Equal(2))
	})
})
