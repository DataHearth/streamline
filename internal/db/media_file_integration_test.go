package db

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/episode"
	"github.com/datahearth/streamline/ent/mediafile"
	entmovie "github.com/datahearth/streamline/ent/movie"
	"github.com/datahearth/streamline/ent/transcodejob"
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

		It("queues a transcode job when QueueTranscode is true", func() {
			m, err := store.CreateMovie(ctx, CreateMovieParams{
				Title: "Dune", OriginalTitle: "Dune", Year: 2021, TmdbID: 998,
				Status: entmovie.StatusWanted, QualityProfile: "HD",
			})
			Expect(err).NotTo(HaveOccurred())

			mf, err := store.CreateMediaFile(ctx, CreateMediaFileParams{
				Path: "/lib/dune3.mkv", Size: 1, MovieID: m.ID,
				QueueTranscode: true,
			})
			Expect(err).NotTo(HaveOccurred())

			n, err := client.TranscodeJob.Query().
				Where(transcodejob.HasMediaFileWith(mediafile.ID(mf.ID))).
				Count(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(n).To(Equal(1))
		})

		It("queues no transcode job when QueueTranscode is false", func() {
			mf := createMovieFile()

			n, err := client.TranscodeJob.Query().
				Where(transcodejob.HasMediaFileWith(mediafile.ID(mf.ID))).
				Count(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(n).To(Equal(0))
		})
	})

	Describe("ClearMediaFilesMissing", func() {
		It("re-arms MarkMediaFileMissing for the files it clears", func() {
			a, b := createMovieFile(), createMovieFile()
			for _, id := range []uint32{a.ID, b.ID} {
				first, err := store.MarkMediaFileMissing(ctx, id)
				Expect(err).NotTo(HaveOccurred())
				Expect(first).To(BeTrue())
			}

			Expect(store.ClearMediaFilesMissing(ctx, []uint32{a.ID})).To(Succeed())

			again, err := store.MarkMediaFileMissing(ctx, a.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(again).To(BeTrue())
			again, err = store.MarkMediaFileMissing(ctx, b.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(again).To(BeFalse())
		})
	})

	Describe("the missing file columns", func() {
		It("ClearMediaFilesMissing clears missing_confirmed_at too", func() {
			mf := createMovieFile()
			_, err := store.MarkMediaFileMissing(ctx, mf.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(store.ConfirmMediaFileMissing(ctx, mf.ID)).To(Succeed())

			Expect(store.ClearMediaFilesMissing(ctx, []uint32{mf.ID})).To(Succeed())

			got := client.MediaFile.GetX(ctx, mf.ID)
			Expect(got.MissingSince).To(BeNil())
			Expect(got.MissingConfirmedAt).To(BeNil())
		})

		It("ListMediaFilesForDrift returns missing_confirmed_at", func() {
			mf := createMovieFile()
			_, err := store.MarkMediaFileMissing(ctx, mf.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(store.ConfirmMediaFileMissing(ctx, mf.ID)).To(Succeed())

			rows, err := store.ListMediaFilesForDrift(ctx, 0, 10)
			Expect(err).NotTo(HaveOccurred())
			Expect(rows).To(HaveLen(1))
			Expect(rows[0].MissingConfirmedAt).NotTo(BeNil())
		})

		It("ConfirmMediaFileMissing keeps the first confirmation", func() {
			mf := createMovieFile()
			Expect(store.ConfirmMediaFileMissing(ctx, mf.ID)).To(Succeed())
			first := client.MediaFile.GetX(ctx, mf.ID).MissingConfirmedAt
			Expect(first).NotTo(BeNil())

			Expect(store.ConfirmMediaFileMissing(ctx, mf.ID)).To(Succeed())

			Expect(client.MediaFile.GetX(ctx, mf.ID).MissingConfirmedAt).
				To(Equal(first))
		})
	})

	Describe("HoldsOtherFile", func() {
		withOwners := func(id uint32) *ent.MediaFile {
			GinkgoHelper()
			row, err := store.FindMediaFileWithOwners(ctx, id)
			Expect(err).NotTo(HaveOccurred())
			return row
		}

		It("is true for another present file of the movie", func() {
			mf := createMovieFile()
			movieID := client.MediaFile.GetX(ctx, mf.ID).QueryMovie().OnlyIDX(ctx)
			client.MediaFile.Create().SetPath("/lib/other.mkv").SetSize(1).
				SetMovieID(movieID).ExecX(ctx)

			Expect(store.HoldsOtherFile(ctx, withOwners(mf.ID))).To(BeTrue())
		})

		It("is false when the other file is missing as well", func() {
			mf := createMovieFile()
			movieID := client.MediaFile.GetX(ctx, mf.ID).QueryMovie().OnlyIDX(ctx)
			client.MediaFile.Create().SetPath("/lib/other.mkv").SetSize(1).
				SetMovieID(movieID).SetMissingSince(time.Now()).ExecX(ctx)

			Expect(store.HoldsOtherFile(ctx, withOwners(mf.ID))).To(BeFalse())
		})

		It("is false for a file of another owner", func() {
			mf := createMovieFile()
			createMovieFile()

			Expect(store.HoldsOtherFile(ctx, withOwners(mf.ID))).To(BeFalse())
		})

		It("is true for another file of the episode", func() {
			show := client.TVShow.Create().
				SetTitle("The Bear").SetYear(2022).SetTvdbID(1).SaveX(ctx)
			season := client.Season.Create().SetNumber(1).SetTvShow(show).SaveX(ctx)
			ep := client.Episode.Create().
				SetNumber(1).SetTitle("System").SetSeason(season).SaveX(ctx)
			mf := client.MediaFile.Create().SetPath("/s/a.mkv").SetSize(1).
				SetEpisodeID(ep.ID).SaveX(ctx)
			client.MediaFile.Create().SetPath("/s/b.mkv").SetSize(1).
				SetEpisodeID(ep.ID).ExecX(ctx)

			Expect(store.HoldsOtherFile(ctx, withOwners(mf.ID))).To(BeTrue())
		})

		It("counts the same track only, not the album's other tracks", func() {
			artist := client.Artist.Create().
				SetMbid("a").SetName("Artist").SaveX(ctx)
			al := client.Album.Create().
				SetMbid("rg").SetTitle("Album").SetArtist(artist).SaveX(ctx)
			one := client.Track.Create().
				SetTitle("One").SetPosition(1).SetAlbum(al).SaveX(ctx)
			two := client.Track.Create().
				SetTitle("Two").SetPosition(2).SetAlbum(al).SaveX(ctx)
			mf := client.MediaFile.Create().SetPath("/m/1.flac").SetSize(1).
				SetTrackID(one.ID).SaveX(ctx)
			client.MediaFile.Create().SetPath("/m/2.flac").SetSize(1).
				SetTrackID(two.ID).ExecX(ctx)

			Expect(store.HoldsOtherFile(ctx, withOwners(mf.ID))).To(BeFalse())

			client.MediaFile.Create().SetPath("/m/1b.flac").SetSize(1).
				SetTrackID(one.ID).ExecX(ctx)

			Expect(store.HoldsOtherFile(ctx, withOwners(mf.ID))).To(BeTrue())
		})

		It("counts the same book slot only", func() {
			bk := client.Book.Create().SetHardcoverID(2).SetTitle("Elantris").
				SetAuthorName("Sanderson").SaveX(ctx)
			mf := client.MediaFile.Create().SetPath("/b/a.epub").SetSize(1).
				SetBookID(bk.ID).SetBookKind(mediafile.BookKindEbook).SaveX(ctx)
			client.MediaFile.Create().SetPath("/b/a.m4b").SetSize(1).
				SetBookID(bk.ID).SetBookKind(mediafile.BookKindAudiobook).ExecX(ctx)

			Expect(store.HoldsOtherFile(ctx, withOwners(mf.ID))).To(BeFalse())

			client.MediaFile.Create().SetPath("/b/a.pdf").SetSize(1).
				SetBookID(bk.ID).SetBookKind(mediafile.BookKindEbook).ExecX(ctx)

			Expect(store.HoldsOtherFile(ctx, withOwners(mf.ID))).To(BeTrue())
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

		It("leaves out a file that is neither a movie's nor an episode's", func() {
			client.MediaFile.Create().SetPath("/lib/01 - Track.flac").SetSize(1).
				SetFormat("flac").SaveX(ctx)
			movie := createMovieFile()

			rows, err := store.ListUnprobedMediaFiles(ctx, 10)
			Expect(err).NotTo(HaveOccurred())
			Expect(rows).To(HaveLen(1))
			Expect(rows[0].ID).To(Equal(movie.ID))
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

	Describe("ListUnprobedMediaFiles with a missing file", func() {
		It("leaves it out, so a backlog of them cannot starve the batch", func() {
			missing := createMovieFile()
			present := createMovieFile()
			client.MediaFile.UpdateOneID(missing.ID).
				SetMissingSince(time.Now()).ExecX(ctx)

			got, err := store.ListUnprobedMediaFiles(ctx, 1)
			Expect(err).NotTo(HaveOccurred())
			Expect(got).To(HaveLen(1))
			Expect(got[0].ID).To(Equal(present.ID))
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
