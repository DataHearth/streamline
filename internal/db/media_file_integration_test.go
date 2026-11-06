package db

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/album"
	"github.com/datahearth/streamline/ent/book"
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

	Describe("ListMediaFilesForDrift skipping a root", func() {
		It(
			"leaves the other roots' files in the walk when a book root is skipped",
			func() {
				movieFile := createMovieFile()
				bk := client.Book.Create().SetHardcoverID(2).SetTitle("Elantris").
					SetAuthorName("Sanderson").SaveX(ctx)
				client.MediaFile.Create().SetPath("/b/a.epub").SetSize(1).
					SetBookID(bk.ID).SetBookKind(mediafile.BookKindEbook).ExecX(ctx)
				audio := client.MediaFile.Create().SetPath("/b/a.m4b").SetSize(1).
					SetBookID(bk.ID).
					SetBookKind(mediafile.BookKindAudiobook).SaveX(ctx)

				rows, err := store.ListMediaFilesForDrift(ctx, 0, 10, RootEbooks)

				Expect(err).NotTo(HaveOccurred())
				ids := make([]uint32, len(rows))
				for i, row := range rows {
					ids[i] = row.ID
				}
				Expect(ids).To(ConsistOf(movieFile.ID, audio.ID))
			},
		)
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

	Describe("AllConfirmedMissing", func() {
		at := time.Now()
		confirmed := &ent.MediaFile{MissingSince: &at, MissingConfirmedAt: &at}

		It("is true only when every file is confirmed missing", func() {
			Expect(AllConfirmedMissing(nil)).To(BeFalse())
			Expect(AllConfirmedMissing([]*ent.MediaFile{confirmed})).To(BeTrue())
			Expect(
				AllConfirmedMissing([]*ent.MediaFile{confirmed, {}}),
			).To(BeFalse())
			Expect(AllConfirmedMissing([]*ent.MediaFile{
				confirmed, {MissingSince: &at},
			})).To(BeFalse())
		})
	})

	Describe("ForgetMediaFile", func() {
		withOwners := func(id uint32) *ent.MediaFile {
			GinkgoHelper()
			row, err := store.FindMediaFileWithOwners(ctx, id)
			Expect(err).NotTo(HaveOccurred())
			return row
		}
		expectGone := func(id uint32) {
			GinkgoHelper()
			Expect(client.MediaFile.Query().
				Where(mediafile.ID(id)).ExistX(ctx)).To(BeFalse())
		}

		It(
			"unmonitors a movie and sends it to wanted, out of the missing search",
			func() {
				mf := createMovieFile()
				m := client.MediaFile.GetX(ctx, mf.ID).QueryMovie().OnlyX(ctx)
				client.Movie.UpdateOne(m).
					SetStatus(entmovie.StatusAvailable).
					ExecX(ctx)

				Expect(store.ForgetMediaFile(ctx, withOwners(mf.ID))).To(Succeed())

				expectGone(mf.ID)
				got := client.Movie.GetX(ctx, m.ID)
				Expect(got.Status).To(Equal(entmovie.StatusWanted))
				Expect(got.Monitored).To(BeFalse())
				Expect(store.ListWantedMovies(ctx)).To(BeEmpty())
			},
		)

		It("keeps the status of a movie with another file on disk", func() {
			mf := createMovieFile()
			m := client.MediaFile.GetX(ctx, mf.ID).QueryMovie().OnlyX(ctx)
			client.Movie.UpdateOne(m).SetStatus(entmovie.StatusAvailable).ExecX(ctx)
			client.MediaFile.Create().SetPath("/lib/other.mkv").SetSize(1).
				SetMovie(m).ExecX(ctx)

			Expect(store.ForgetMediaFile(ctx, withOwners(mf.ID))).To(Succeed())

			got := client.Movie.GetX(ctx, m.ID)
			Expect(got.Status).To(Equal(entmovie.StatusAvailable))
			Expect(got.Monitored).To(BeFalse())
		})

		It("unmonitors and pins an episode, out of the missing search", func() {
			aired := time.Now().Add(-time.Hour)
			show := client.TVShow.Create().
				SetTitle("The Bear").SetYear(2022).SetTvdbID(1).SaveX(ctx)
			season := client.Season.Create().SetNumber(1).SetTvShow(show).SaveX(ctx)
			ep := client.Episode.Create().
				SetNumber(1).SetTitle("System").SetSeason(season).SetAirDate(aired).
				SetStatus(episode.StatusAvailable).SaveX(ctx)
			mf := client.MediaFile.Create().SetPath("/s/a.mkv").SetSize(1).
				SetEpisodeID(ep.ID).SaveX(ctx)

			Expect(store.ForgetMediaFile(ctx, withOwners(mf.ID))).To(Succeed())

			expectGone(mf.ID)
			got := client.Episode.GetX(ctx, ep.ID)
			Expect(got.Status).To(Equal(episode.StatusWanted))
			Expect(got.Monitored).To(BeFalse())
			Expect(got.MonitorPinned).To(BeTrue())
			Expect(store.ListEligibleEpisodesForSync(
				ctx, 10, time.Now(), time.Now(),
			)).To(BeEmpty())
		})

		Describe("of an album", func() {
			var (
				al *ent.Album
				mf *ent.MediaFile
			)

			BeforeEach(func() {
				artist := client.Artist.Create().
					SetMbid("a").SetName("Artist").SaveX(ctx)
				al = client.Album.Create().
					SetMbid("rg").SetTitle("Album").SetArtist(artist).
					SetStatus(album.StatusAvailable).
					SetMetadataFetchedAt(time.Now()).SaveX(ctx)
				tr := client.Track.Create().
					SetTitle("One").SetPosition(1).SetAlbum(al).SaveX(ctx)
				mf = client.MediaFile.Create().SetPath("/m/1.flac").SetSize(1).
					SetTrackID(tr.ID).SaveX(ctx)
			})

			It("unmonitors and pins it, out of the missing search", func() {
				Expect(store.ForgetMediaFile(ctx, withOwners(mf.ID))).To(Succeed())

				expectGone(mf.ID)
				got := client.Album.GetX(ctx, al.ID)
				Expect(got.Status).To(Equal(album.StatusWanted))
				Expect(got.Monitored).To(BeFalse())
				Expect(got.MonitorPinned).To(BeTrue())
				Expect(store.ListWantedAlbums(ctx, 10)).To(BeEmpty())
			})

			It("keeps the status of a downloading album", func() {
				client.Album.UpdateOne(al).
					SetStatus(album.StatusDownloading).
					ExecX(ctx)

				Expect(store.ForgetMediaFile(ctx, withOwners(mf.ID))).To(Succeed())

				got := client.Album.GetX(ctx, al.ID)
				Expect(got.Status).To(Equal(album.StatusDownloading))
				Expect(got.Monitored).To(BeFalse())
			})
		})

		Describe("of a book", func() {
			var (
				bk *ent.Book
				mf *ent.MediaFile
			)

			BeforeEach(func() {
				bk = client.Book.Create().SetHardcoverID(2).SetTitle("Elantris").
					SetAuthorName("Sanderson").SetLastRefreshedAt(time.Now()).
					SetEbookMonitored(true).
					SetEbookStatus(book.EbookStatusAvailable).
					SetAudiobookMonitored(true).
					SetAudiobookStatus(book.AudiobookStatusAvailable).SaveX(ctx)
				mf = client.MediaFile.Create().SetPath("/b/a.epub").SetSize(1).
					SetBookID(bk.ID).SetBookKind(mediafile.BookKindEbook).SaveX(ctx)
			})

			It(
				"unmonitors the slot, out of the missing search, leaving the other",
				func() {
					Expect(
						store.ForgetMediaFile(ctx, withOwners(mf.ID)),
					).To(Succeed())

					expectGone(mf.ID)
					got := client.Book.GetX(ctx, bk.ID)
					Expect(got.EbookMonitored).To(BeFalse())
					Expect(got.EbookStatus).To(Equal(book.EbookStatusWanted))
					Expect(got.AudiobookMonitored).To(BeTrue())
					Expect(
						got.AudiobookStatus,
					).To(Equal(book.AudiobookStatusAvailable))
					Expect(store.ListWantedBooks(ctx, 10)).To(BeEmpty())
				},
			)

			It("keeps a slot holding another file available, unmonitored", func() {
				client.MediaFile.Create().SetPath("/b/a.pdf").SetSize(1).
					SetBookID(bk.ID).SetBookKind(mediafile.BookKindEbook).ExecX(ctx)

				Expect(store.ForgetMediaFile(ctx, withOwners(mf.ID))).To(Succeed())

				got := client.Book.GetX(ctx, bk.ID)
				Expect(got.EbookMonitored).To(BeFalse())
				Expect(got.EbookStatus).To(Equal(book.EbookStatusAvailable))
			})
		})
	})

	Describe("ListReplacedMissingFiles", func() {
		confirm := func(id uint32) {
			GinkgoHelper()
			client.MediaFile.UpdateOneID(id).
				SetMissingSince(time.Now()).
				SetMissingConfirmedAt(time.Now()).ExecX(ctx)
		}
		secondFile := func(mf *ent.MediaFile) *ent.MediaFile {
			GinkgoHelper()
			movieID := client.MediaFile.GetX(ctx, mf.ID).QueryMovie().OnlyIDX(ctx)
			return client.MediaFile.Create().SetPath("/lib/new.mkv").SetSize(1).
				SetMovieID(movieID).SaveX(ctx)
		}

		It("lists a confirmed row whose movie holds another file", func() {
			mf := createMovieFile()
			confirm(mf.ID)
			secondFile(mf)
			confirm(createMovieFile().ID)

			Expect(store.ListReplacedMissingFiles(ctx)).To(ConsistOf(mf.ID))
		})

		It("leaves a row still inside grace alone", func() {
			mf := createMovieFile()
			client.MediaFile.UpdateOneID(mf.ID).
				SetMissingSince(time.Now()).
				ExecX(ctx)
			secondFile(mf)

			Expect(store.ListReplacedMissingFiles(ctx)).To(BeEmpty())
		})

		It("does not take a file missing as well for a replacement", func() {
			mf := createMovieFile()
			confirm(mf.ID)
			client.MediaFile.UpdateOneID(secondFile(mf).ID).
				SetMissingSince(time.Now()).ExecX(ctx)

			Expect(store.ListReplacedMissingFiles(ctx)).To(BeEmpty())
		})

		It("leaves out a root being skipped", func() {
			mf := createMovieFile()
			confirm(mf.ID)
			secondFile(mf)

			Expect(store.ListReplacedMissingFiles(ctx, RootMovies)).To(BeEmpty())
		})

		It(
			"lists an ebook replaced within its slot, not a chapter of an audiobook",
			func() {
				b := client.Book.Create().
					SetHardcoverID(1).SetTitle("Elantris").SaveX(ctx)
				file := func(path string, kind mediafile.BookKind) *ent.MediaFile {
					return client.MediaFile.Create().SetPath(path).SetSize(1).
						SetBookID(b.ID).SetBookKind(kind).SaveX(ctx)
				}
				ebook := file("/b/old.epub", mediafile.BookKindEbook)
				confirm(ebook.ID)
				file("/b/new.epub", mediafile.BookKindEbook)
				chapter := file("/a/1.m4b", mediafile.BookKindAudiobook)
				confirm(chapter.ID)
				file("/a/2.m4b", mediafile.BookKindAudiobook)

				Expect(store.ListReplacedMissingFiles(ctx)).To(ConsistOf(ebook.ID))
			},
		)
	})

	Describe("ListMissingFiles", func() {
		It("leaves out a row with no owner", func() {
			client.MediaFile.Create().SetPath("/lib/none.mkv").SetSize(1).
				SetMissingSince(time.Now()).
				SetMissingConfirmedAt(time.Now()).ExecX(ctx)

			rows, total, err := store.ListMissingFiles(ctx, 10, 0)
			Expect(err).NotTo(HaveOccurred())
			Expect(total).To(BeZero())
			Expect(rows).To(BeEmpty())
		})

		It("pages the confirmed rows only, with the names a title needs", func() {
			confirmed := createMovieFile()
			createMovieFile()
			inGrace := createMovieFile()
			client.MediaFile.UpdateOneID(inGrace.ID).
				SetMissingSince(time.Now()).ExecX(ctx)
			client.MediaFile.UpdateOneID(confirmed.ID).
				SetMissingSince(time.Now()).
				SetMissingConfirmedAt(time.Now()).ExecX(ctx)

			artist := client.Artist.Create().
				SetMbid("a").SetName("Artist").SaveX(ctx)
			al := client.Album.Create().
				SetMbid("rg").SetTitle("Album").SetArtist(artist).SaveX(ctx)
			tr := client.Track.Create().
				SetTitle("One").SetPosition(3).SetAlbum(al).SaveX(ctx)
			track := client.MediaFile.Create().SetPath("/m/3.flac").SetSize(1).
				SetTrackID(tr.ID).SetMissingSince(time.Now()).
				SetMissingConfirmedAt(time.Now().Add(time.Minute)).SaveX(ctx)

			rows, total, err := store.ListMissingFiles(ctx, 1, 0)
			Expect(err).NotTo(HaveOccurred())
			Expect(total).To(Equal(2))
			Expect(rows).To(HaveLen(1))
			Expect(rows[0].ID).To(Equal(confirmed.ID))
			Expect(rows[0].Edges.Movie.Title).To(Equal("Dune"))

			rows, _, err = store.ListMissingFiles(ctx, 10, 1)
			Expect(err).NotTo(HaveOccurred())
			Expect(rows).To(HaveLen(1))
			Expect(rows[0].ID).To(Equal(track.ID))
			got := rows[0].Edges.Track
			Expect(got.Title).To(Equal("One"))
			Expect(got.Position).To(BeEquivalentTo(3))
			Expect(got.Edges.Album.Title).To(Equal("Album"))
			Expect(got.Edges.Album.Edges.Artist.Name).To(Equal("Artist"))
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

	Describe("MovieHasMediaFile", func() {
		It("does not count a missing file, so the orphan scan can adopt", func() {
			mf := createMovieFile()
			tmdb := client.MediaFile.GetX(ctx, mf.ID).QueryMovie().OnlyX(ctx).TmdbID
			Expect(store.MovieHasMediaFile(ctx, tmdb)).To(BeTrue())

			client.MediaFile.UpdateOneID(mf.ID).
				SetMissingSince(time.Now()).
				ExecX(ctx)

			Expect(store.MovieHasMediaFile(ctx, tmdb)).To(BeFalse())
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

	DescribeTable("a removed file leaves a title on its way in alone",
		func(st entmovie.Status) {
			mf := createMovieFile()
			m := client.MediaFile.GetX(ctx, mf.ID).QueryMovie().OnlyX(ctx)
			client.Movie.UpdateOneID(m.ID).SetStatus(st).ExecX(ctx)

			Expect(
				store.DeleteMediaFileAndRevertMovie(ctx, mf.ID, m.ID),
			).To(Succeed())

			Expect(client.Movie.GetX(ctx, m.ID).Status).To(Equal(st))
		},
		Entry("downloading", entmovie.StatusDownloading),
		Entry("importing", entmovie.StatusImporting),
	)

	DescribeTable("a removed file leaves an episode on its way in alone",
		func(st episode.Status) {
			show, err := store.CreateTVShow(ctx, CreateTVShowParams{
				Title: "The Bear", Year: 2022, TvdbID: 7777,
				Seasons: []SeasonSeed{{Number: 1, Episodes: []EpisodeSeed{
					{Number: 1, Title: "System"},
				}}},
			})
			Expect(err).NotTo(HaveOccurred())
			ep := show.Edges.Seasons[0].Edges.Episodes[0]
			client.Episode.UpdateOneID(ep.ID).SetStatus(st).ExecX(ctx)
			mf := client.MediaFile.Create().
				SetPath("/lib/ep.mkv").SetSize(1).SetEpisodeID(ep.ID).SaveX(ctx)

			Expect(
				store.DeleteMediaFileAndRevertEpisode(ctx, mf.ID, ep.ID),
			).To(Succeed())

			Expect(client.Episode.GetX(ctx, ep.ID).Status).To(Equal(st))
		},
		Entry("downloading", episode.StatusDownloading),
		Entry("importing", episode.StatusImporting),
		Entry("paused", episode.StatusPaused),
	)

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
