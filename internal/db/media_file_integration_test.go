package db

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/episode"
	entmovie "github.com/datahearth/streamline/ent/movie"
	"github.com/datahearth/streamline/internal/ffmpeg"
)

var _ = Describe("MediaFile store", Label("integration", "db"), func() {
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

	// createMovieFile makes a movie with a unique TmdbID and one probe-less
	// media file linked to it — the fixture the probe specs below use to seed
	// multiple rows per spec (ListUnprobedMediaFiles' ordering/limit specs).
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

		It("leaves probed_at nil when no probe info is given", func() {
			mf := createMovieFile()
			Expect(mf.ProbedAt).To(BeNil())
		})

		It("persists probe info and stamps probed_at when given", func() {
			tmdbSeq++
			m, err := store.CreateMovie(ctx, CreateMovieParams{
				Title: "Dune", OriginalTitle: "Dune", Year: 2021, TmdbID: tmdbSeq,
				Status: entmovie.StatusWanted, QualityProfile: "HD",
			})
			Expect(err).NotTo(HaveOccurred())

			mf, err := store.CreateMediaFile(ctx, CreateMediaFileParams{
				Path: "/lib/dune2.mkv", Size: 1, MovieID: m.ID,
				Probe: &ffmpeg.Info{
					Container: "mp4", DurationSec: 1200, VideoCodec: "hevc",
					Width: 3840, Height: 2160, AudioCodec: "eac3",
					AudioChannels: 6, BitrateBPS: 20_000_000,
				},
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(mf.Container).To(Equal("mp4"))
			Expect(mf.DurationSeconds).To(Equal(uint32(1200)))
			Expect(mf.VideoCodec).To(Equal("hevc"))
			Expect(mf.Width).To(Equal(uint16(3840)))
			Expect(mf.Height).To(Equal(uint16(2160)))
			Expect(mf.AudioCodec).To(Equal("eac3"))
			Expect(mf.AudioChannels).To(Equal(uint8(6)))
			Expect(mf.Bitrate).To(Equal(uint32(20_000_000)))
			Expect(mf.ProbedAt).NotTo(BeNil())
		})
	})

	Describe("BumpMediaFilesLastSeen", func() {
		It(
			"stamps last_seen_at and clears missing_since for the whole batch",
			func() {
				a, b := createMovieFile(), createMovieFile()
				first, err := store.MarkMediaFileMissing(ctx, a.ID)
				Expect(err).NotTo(HaveOccurred())
				Expect(first).To(BeTrue())

				Expect(store.BumpMediaFilesLastSeen(
					ctx, []uint32{a.ID, b.ID},
				)).To(Succeed())

				for _, id := range []uint32{a.ID, b.ID} {
					row, err := client.MediaFile.Get(ctx, id)
					Expect(err).NotTo(HaveOccurred())
					Expect(row.LastSeenAt).NotTo(BeNil())
					Expect(row.MissingSince).To(BeNil())
				}
			},
		)
	})

	Describe("StartMediaFileGraceClock", func() {
		// The grace-start write leaving missing_since alone is what keeps
		// MarkMediaFileMissing from reporting "first" twice — and each
		// "first" is a drift_detected event.
		It("does not re-arm MarkMediaFileMissing", func() {
			mf := createMovieFile()

			first, err := store.MarkMediaFileMissing(ctx, mf.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(first).To(BeTrue())

			Expect(store.StartMediaFileGraceClock(ctx, mf.ID)).To(Succeed())

			again, err := store.MarkMediaFileMissing(ctx, mf.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(again).To(BeFalse())

			row, err := client.MediaFile.Get(ctx, mf.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(row.LastSeenAt).NotTo(BeNil())
			Expect(row.MissingSince).NotTo(BeNil())
		})
	})

	Describe("StampMediaFileProbe", func() {
		It("stores probe info and stamps probed_at", func() {
			mf := createMovieFile()
			info := &ffmpeg.Info{
				Container: "matroska", DurationSec: 5400, VideoCodec: "h264",
				Width: 1920, Height: 1080, AudioCodec: "aac",
				AudioChannels: 2, BitrateBPS: 8_000_000,
			}
			Expect(
				store.StampMediaFileProbe(ctx, mf.ID, mf.Path, info),
			).To(Succeed())

			got, err := store.FindMediaFileByID(ctx, mf.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(got.Container).To(Equal("matroska"))
			Expect(got.DurationSeconds).To(Equal(uint32(5400)))
			Expect(got.VideoCodec).To(Equal("h264"))
			Expect(got.Width).To(Equal(uint16(1920)))
			Expect(got.Height).To(Equal(uint16(1080)))
			Expect(got.AudioCodec).To(Equal("aac"))
			Expect(got.AudioChannels).To(Equal(uint8(2)))
			Expect(got.Bitrate).To(Equal(uint32(8_000_000)))
			Expect(got.ProbedAt).NotTo(BeNil())
		})

		It(
			"stamps probed_at alone on a failed probe, leaving every probe column untouched",
			func() {
				mf := createMovieFile()
				Expect(
					store.StampMediaFileProbe(ctx, mf.ID, mf.Path, nil),
				).To(Succeed())

				got, err := store.FindMediaFileByID(ctx, mf.ID)
				Expect(err).NotTo(HaveOccurred())
				Expect(got.ProbedAt).NotTo(BeNil())
				Expect(got.Container).To(BeEmpty())
				Expect(got.DurationSeconds).To(BeZero())
				Expect(got.VideoCodec).To(BeEmpty())
				Expect(got.Width).To(BeZero())
				Expect(got.Height).To(BeZero())
				Expect(got.AudioCodec).To(BeEmpty())
				Expect(got.AudioChannels).To(BeZero())
				Expect(got.Bitrate).To(BeZero())
			},
		)
	})

	Describe("ListUnprobedMediaFiles", func() {
		It("excludes rows that already have a probed_at", func() {
			a := createMovieFile()
			b := createMovieFile()
			Expect(store.StampMediaFileProbe(ctx, a.ID, a.Path, nil)).To(Succeed())

			rows, err := store.ListUnprobedMediaFiles(ctx, 10)
			Expect(err).NotTo(HaveOccurred())
			Expect(rows).To(HaveLen(1))
			Expect(rows[0].ID).To(Equal(b.ID))
		})

		It("returns unprobed rows ordered oldest first", func() {
			a := createMovieFile()
			b := createMovieFile()
			c := createMovieFile()

			rows, err := store.ListUnprobedMediaFiles(ctx, 10)
			Expect(err).NotTo(HaveOccurred())
			Expect(rows).To(HaveLen(3))
			ids := make([]uint32, len(rows))
			for i, r := range rows {
				ids[i] = r.ID
			}
			Expect(ids).To(Equal([]uint32{a.ID, b.ID, c.ID}))
		})

		It("respects the limit, returning the oldest rows", func() {
			a := createMovieFile()
			b := createMovieFile()
			createMovieFile() // c: newest, excluded by the limit

			rows, err := store.ListUnprobedMediaFiles(ctx, 2)
			Expect(err).NotTo(HaveOccurred())
			Expect(rows).To(HaveLen(2))
			ids := make([]uint32, len(rows))
			for i, r := range rows {
				ids[i] = r.ID
			}
			Expect(ids).To(Equal([]uint32{a.ID, b.ID}))
		})
	})

	Describe("ListAllMediaFilesWithOwners", func() {
		It("eager-loads both the movie and the episode edge", func() {
			m, err := store.CreateMovie(ctx, CreateMovieParams{
				Title: "Dune", OriginalTitle: "Dune", Year: 2021, TmdbID: 999,
				Status: entmovie.StatusWanted, QualityProfile: "HD",
			})
			Expect(err).NotTo(HaveOccurred())
			_, err = store.CreateMediaFile(ctx, CreateMediaFileParams{
				Path: "/lib/dune.mkv", Size: 1, MovieID: m.ID,
			})
			Expect(err).NotTo(HaveOccurred())

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
			_, err = store.CreateMediaFile(ctx, CreateMediaFileParams{
				Path: "/lib/bear.mkv", Size: 1, EpisodeID: ep.ID,
			})
			Expect(err).NotTo(HaveOccurred())

			rows, err := store.ListAllMediaFilesWithOwners(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(rows).To(HaveLen(2))

			owners := map[string]uint32{}
			for _, r := range rows {
				switch {
				case r.Edges.Movie != nil:
					owners["movie"] = r.Edges.Movie.ID
				case r.Edges.Episode != nil:
					owners["episode"] = r.Edges.Episode.ID
				}
			}
			Expect(owners).To(Equal(map[string]uint32{
				"movie": m.ID, "episode": ep.ID,
			}))
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
