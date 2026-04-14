package db

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/downloadrecord"
	"github.com/datahearth/streamline/ent/episode"
	entmovie "github.com/datahearth/streamline/ent/movie"
)

var _ = Describe("Download record store", Label("integration", "db"), func() {
	var (
		ctx     context.Context
		client  *ent.Client
		store   *DB
		movieID uint32
	)

	const clientName = "qb"

	BeforeEach(func() {
		ctx = context.Background()
		var err error
		client, err = Open(ctx, ":memory:")
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { client.Close() })
		store = New(client)

		m, err := store.CreateMovie(ctx, CreateMovieParams{
			Title: "Dune", OriginalTitle: "Dune", Year: 2021, TmdbID: 438631,
			Status: entmovie.StatusWanted, QualityProfile: "HD",
		})
		Expect(err).NotTo(HaveOccurred())
		movieID = m.ID
	})

	createRec := func(hash string, status downloadrecord.Status) *ent.DownloadRecord {
		GinkgoHelper()
		rec, err := store.CreateDownloadRecord(ctx, CreateDownloadRecordParams{
			Title: "t", Size: 1,
			TorrentHash: hash, Status: status,
			MovieID: movieID, DownloadClientName: clientName,
		})
		Expect(err).NotTo(HaveOccurred())
		return rec
	}

	Describe("CreateDownloadRecord", func() {
		It("persists with the given edges", func() {
			rec := createRec("abc", downloadrecord.StatusDownloading)
			Expect(rec.TorrentHash).To(Equal("abc"))
			mv, err := rec.QueryMovie().Only(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(mv.ID).To(Equal(movieID))
		})

		It("links the episode edge (not the movie) for a TV grab", func() {
			ad := time.Now()
			show, err := store.CreateTVShow(ctx, CreateTVShowParams{
				Title: "The Black Sea", Year: 2024, TvdbID: 9001,
				Seasons: []SeasonSeed{{
					Number: 3,
					Episodes: []EpisodeSeed{
						{Number: 1, Title: "Pilot", AirDate: &ad},
					},
				}},
			})
			Expect(err).NotTo(HaveOccurred())
			episodeID := show.Edges.Seasons[0].Edges.Episodes[0].ID

			rec, err := store.CreateDownloadRecord(ctx, CreateDownloadRecordParams{
				Title: "t", Size: 1, TorrentHash: "tv",
				Status:             downloadrecord.StatusDownloading,
				EpisodeID:          episodeID,
				DownloadClientName: clientName,
			})
			Expect(err).NotTo(HaveOccurred())

			ep, err := rec.QueryEpisode().Only(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(ep.ID).To(Equal(episodeID))

			_, err = rec.QueryMovie().Only(ctx)
			Expect(ent.IsNotFound(err)).To(BeTrue())
		})
	})

	Describe("ListDownloadingRecords", func() {
		It(
			"returns only status=downloading rows with download_client preloaded",
			func() {
				createRec("abc", downloadrecord.StatusDownloading)
				createRec("def", downloadrecord.StatusCompleted)

				items, err := store.ListDownloadingRecords(ctx)
				Expect(err).NotTo(HaveOccurred())
				Expect(items).To(HaveLen(1))
				Expect(items[0].DownloadClientName).To(Equal(clientName))
			},
		)
	})

	Describe("ListDownloadingRecordsWithMovie", func() {
		It("preloads client and movie", func() {
			createRec("abc", downloadrecord.StatusDownloading)
			createRec("def", downloadrecord.StatusCompleted)

			recs, err := store.ListDownloadingRecordsWithMovie(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(recs).To(HaveLen(1))
			Expect(recs[0].DownloadClientName).To(Equal(clientName))
			Expect(recs[0].Edges.Movie).NotTo(BeNil())
		})
	})

	Describe("UpdateDownloadRecordStatus", func() {
		It("updates the status", func() {
			rec := createRec("abc", downloadrecord.StatusDownloading)
			Expect(
				store.UpdateDownloadRecordStatus(
					ctx,
					rec.ID,
					downloadrecord.StatusImporting,
				),
			).To(Succeed())
			got, _ := client.DownloadRecord.Get(ctx, rec.ID)
			Expect(got.Status).To(Equal(downloadrecord.StatusImporting))
		})
	})

	Describe("ListImportingDownloadRecords", func() {
		It("returns only status=importing with edges preloaded", func() {
			createRec("abc", downloadrecord.StatusImporting)
			createRec("def", downloadrecord.StatusDownloading)

			items, err := store.ListImportingDownloadRecords(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(items).To(HaveLen(1))
			Expect(items[0].Edges.Movie).NotTo(BeNil())
			Expect(items[0].DownloadClientName).To(Equal(clientName))
		})
	})

	Describe("FindImportingDownloadRecordByID", func() {
		It("returns the matching importing row with edges preloaded", func() {
			rec := createRec("abc", downloadrecord.StatusImporting)
			got, err := store.FindImportingDownloadRecordByID(ctx, rec.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(got.Edges.Movie).NotTo(BeNil())
			Expect(got.DownloadClientName).To(Equal(clientName))
		})

		It("returns NotFound for a record that is not importing", func() {
			rec := createRec("abc", downloadrecord.StatusDownloading)
			_, err := store.FindImportingDownloadRecordByID(ctx, rec.ID)
			Expect(ent.IsNotFound(err)).To(BeTrue())
		})
	})

	Describe("SetDownloadRecordSavePath", func() {
		It("persists the path", func() {
			rec := createRec("abc", downloadrecord.StatusDownloading)
			Expect(
				store.SetDownloadRecordSavePath(ctx, rec.ID, "/data"),
			).To(Succeed())
			got, _ := client.DownloadRecord.Get(ctx, rec.ID)
			Expect(got.SavePath).To(Equal("/data"))
		})
	})

	Describe("RecordImportSuccess", func() {
		It("writes media file, completes record, marks movie available", func() {
			rec := createRec("abc", downloadrecord.StatusImporting)
			err := store.RecordImportSuccess(ctx, RecordImportSuccessParams{
				RecordID: rec.ID, MovieID: movieID,
				File: MediaFileRow{
					Path: "/lib/dune.mkv", Size: 1024,
					Quality: "1080p", Format: "mkv", ReleaseGroup: "GROUP",
				},
			})
			Expect(err).NotTo(HaveOccurred())

			got, _ := client.DownloadRecord.Get(ctx, rec.ID)
			Expect(got.Status).To(Equal(downloadrecord.StatusCompleted))

			m, _ := client.Movie.Get(ctx, movieID)
			Expect(m.Status).To(Equal(entmovie.StatusAvailable))

			count, _ := client.MediaFile.Query().Count(ctx)
			Expect(count).To(Equal(1))
		})

		When("the media file path is empty", func() {
			It("rolls back, leaving record + movie unchanged", func() {
				rec := createRec("abc", downloadrecord.StatusImporting)
				err := store.RecordImportSuccess(ctx, RecordImportSuccessParams{
					RecordID: rec.ID, MovieID: movieID,
					File: MediaFileRow{Path: "", Size: 1},
				})
				Expect(err).To(HaveOccurred())

				got, _ := client.DownloadRecord.Get(ctx, rec.ID)
				Expect(got.Status).To(Equal(downloadrecord.StatusImporting))

				m, _ := client.Movie.Get(ctx, movieID)
				Expect(m.Status).To(Equal(entmovie.StatusWanted))

				count, _ := client.MediaFile.Query().Count(ctx)
				Expect(count).To(Equal(0))
			})
		})
	})

	Describe("RecordImportFailure", func() {
		When("non-terminal", func() {
			It(
				"bumps attempts, leaves status importing, leaves movie wanted",
				func() {
					rec := createRec("abc", downloadrecord.StatusImporting)
					err := store.RecordImportFailure(ctx, RecordImportFailureParams{
						RecordID: rec.ID, MovieID: movieID,
						Terminal: false, Reason: "tmp", Attempts: 2,
					})
					Expect(err).NotTo(HaveOccurred())

					got, _ := client.DownloadRecord.Get(ctx, rec.ID)
					Expect(got.Status).To(Equal(downloadrecord.StatusImporting))
					Expect(got.ImportAttempts).To(Equal(uint8(2)))

					m, _ := client.Movie.Get(ctx, movieID)
					Expect(m.Status).To(Equal(entmovie.StatusWanted))
				},
			)
		})

		When("terminal", func() {
			It("flips record + movie to failed with reason", func() {
				rec := createRec("abc", downloadrecord.StatusImporting)
				err := store.RecordImportFailure(ctx, RecordImportFailureParams{
					RecordID: rec.ID, MovieID: movieID,
					Terminal: true, Reason: "bad file", Attempts: 5,
				})
				Expect(err).NotTo(HaveOccurred())

				got, _ := client.DownloadRecord.Get(ctx, rec.ID)
				Expect(got.Status).To(Equal(downloadrecord.StatusFailed))
				Expect(got.FailureReason).To(Equal("bad file"))

				m, _ := client.Movie.Get(ctx, movieID)
				Expect(m.Status).To(Equal(entmovie.StatusFailed))
				Expect(m.FailureReason).To(Equal("bad file"))
			})
		})
	})

	Describe("DeleteCompletedDownloadRecordsBefore", func() {
		It("deletes only completed records older than cutoff", func() {
			old := createRec("oldc", downloadrecord.StatusCompleted)
			_, err := client.DownloadRecord.UpdateOneID(old.ID).
				SetImportedAt(time.Now().Add(-40 * 24 * time.Hour)).Save(ctx)
			Expect(err).NotTo(HaveOccurred())
			fresh := createRec("newc", downloadrecord.StatusCompleted)
			_, err = client.DownloadRecord.UpdateOneID(fresh.ID).
				SetImportedAt(time.Now()).Save(ctx)
			Expect(err).NotTo(HaveOccurred())
			createRec("oldf", downloadrecord.StatusFailed)

			n, err := store.DeleteCompletedDownloadRecordsBefore(
				ctx, time.Now().Add(-30*24*time.Hour),
			)
			Expect(err).NotTo(HaveOccurred())
			Expect(n).To(Equal(1))

			_, err = client.DownloadRecord.Get(ctx, old.ID)
			Expect(ent.IsNotFound(err)).To(BeTrue())
			_, err = client.DownloadRecord.Get(ctx, fresh.ID)
			Expect(err).NotTo(HaveOccurred())
		})
	})

	Describe("DeleteFailedDownloadRecordsBefore", func() {
		It("deletes only failed records older than cutoff", func() {
			old := createRec("oldf", downloadrecord.StatusFailed)
			_, err := client.DownloadRecord.UpdateOneID(old.ID).
				SetUpdateTime(time.Now().Add(-20 * 24 * time.Hour)).Save(ctx)
			Expect(err).NotTo(HaveOccurred())
			fresh := createRec("newf", downloadrecord.StatusFailed)
			createRec("comp", downloadrecord.StatusCompleted)

			n, err := store.DeleteFailedDownloadRecordsBefore(
				ctx, time.Now().Add(-14*24*time.Hour),
			)
			Expect(err).NotTo(HaveOccurred())
			Expect(n).To(Equal(1))

			_, err = client.DownloadRecord.Get(ctx, old.ID)
			Expect(ent.IsNotFound(err)).To(BeTrue())
			_, err = client.DownloadRecord.Get(ctx, fresh.ID)
			Expect(err).NotTo(HaveOccurred())
		})
	})

	Describe("ListActiveDownloadRecords", func() {
		It("returns downloading+importing only with edges preloaded", func() {
			createRec("dl", downloadrecord.StatusDownloading)
			createRec("imp", downloadrecord.StatusImporting)
			createRec("done", downloadrecord.StatusCompleted)
			createRec("fail", downloadrecord.StatusFailed)

			recs, err := store.ListActiveDownloadRecords(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(recs).To(HaveLen(2))
			for _, r := range recs {
				Expect(r.Status).To(Or(
					Equal(downloadrecord.StatusDownloading),
					Equal(downloadrecord.StatusImporting)))
				Expect(r.Edges.Movie).NotTo(BeNil())
				Expect(r.DownloadClientName).To(Equal(clientName))
			}
		})
	})

	Describe("FindActiveDownloadRecordByID", func() {
		It("returns the in-flight record with edges", func() {
			rec := createRec("dl", downloadrecord.StatusDownloading)
			got, err := store.FindActiveDownloadRecordByID(ctx, rec.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(got.Edges.Movie).NotTo(BeNil())
			Expect(got.DownloadClientName).To(Equal(clientName))
		})

		It("returns NotFound for a terminal record", func() {
			rec := createRec("done", downloadrecord.StatusCompleted)
			_, err := store.FindActiveDownloadRecordByID(ctx, rec.ID)
			Expect(ent.IsNotFound(err)).To(BeTrue())
		})
	})

	Describe("ListDownloadHistory", func() {
		It("paginates completed+failed desc, excluding in-flight", func() {
			createRec("a", downloadrecord.StatusCompleted)
			createRec("b", downloadrecord.StatusFailed)
			createRec("c", downloadrecord.StatusCompleted)
			createRec("dl", downloadrecord.StatusDownloading)

			page1, err := store.ListDownloadHistory(ctx, 2, "")
			Expect(err).NotTo(HaveOccurred())
			Expect(page1.Records).To(HaveLen(2))
			Expect(page1.NextCursor).NotTo(BeEmpty())
			for _, r := range page1.Records {
				Expect(r.Status).To(Or(
					Equal(downloadrecord.StatusCompleted),
					Equal(downloadrecord.StatusFailed)))
				Expect(r.Edges.Movie).NotTo(BeNil())
			}

			page2, err := store.ListDownloadHistory(ctx, 2, page1.NextCursor)
			Expect(err).NotTo(HaveOccurred())
			Expect(page2.Records).To(HaveLen(1))
			Expect(page2.NextCursor).To(BeEmpty())
		})

		It("400-style error on a malformed cursor", func() {
			_, err := store.ListDownloadHistory(ctx, 10, "!!notbase64!!")
			Expect(err).To(MatchError(ContainSubstring("decode cursor")))
		})
	})

	Describe("DeleteDownloadRecord", func() {
		It("deletes one record and NotFounds when absent", func() {
			rec := createRec("x", downloadrecord.StatusFailed)
			Expect(store.DeleteDownloadRecord(ctx, rec.ID)).To(Succeed())
			_, err := client.DownloadRecord.Get(ctx, rec.ID)
			Expect(ent.IsNotFound(err)).To(BeTrue())
			Expect(ent.IsNotFound(
				store.DeleteDownloadRecord(ctx, rec.ID))).To(BeTrue())
		})
	})

	Describe("DeleteAllCompletedDownloadRecords", func() {
		It("removes every completed record, keeping failed", func() {
			createRec("c1", downloadrecord.StatusCompleted)
			createRec("c2", downloadrecord.StatusCompleted)
			createRec("f1", downloadrecord.StatusFailed)

			n, err := store.DeleteAllCompletedDownloadRecords(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(n).To(Equal(2))

			rest, err := store.ListDownloadHistory(ctx, 50, "")
			Expect(err).NotTo(HaveOccurred())
			for _, r := range rest.Records {
				Expect(r.Status).To(Equal(downloadrecord.StatusFailed))
			}
		})
	})

	Describe("RevertMovieToWantedIfNoFile", func() {
		It("reverts a file-less movie but leaves a movie with a file", func() {
			// movieID (from BeforeEach) has no media file — flip to available
			// then expect revert to wanted.
			_, err := client.Movie.UpdateOneID(movieID).
				SetStatus(entmovie.StatusAvailable).Save(ctx)
			Expect(err).NotTo(HaveOccurred())

			m2, err := store.CreateMovie(ctx, CreateMovieParams{
				Title:         "Arrival",
				OriginalTitle: "Arrival",
				Year:          2016,
				TmdbID:        329865,
				Status:        entmovie.StatusAvailable,
			})
			Expect(err).NotTo(HaveOccurred())
			_, err = client.MediaFile.Create().
				SetPath("/lib/arrival.mkv").SetSize(10).
				SetQuality("1080p").SetFormat("mkv").
				SetReleaseGroup("G").SetMovieID(m2.ID).Save(ctx)
			Expect(err).NotTo(HaveOccurred())

			Expect(store.RevertMovieToWantedIfNoFile(ctx, movieID)).To(Succeed())
			Expect(store.RevertMovieToWantedIfNoFile(ctx, m2.ID)).To(Succeed())

			a, _ := client.Movie.Get(ctx, movieID)
			b, _ := client.Movie.Get(ctx, m2.ID)
			Expect(a.Status).To(Equal(entmovie.StatusWanted))
			Expect(b.Status).To(Equal(entmovie.StatusAvailable))
		})
	})

	Describe("RevertOrphanedDownloadingEpisodes", func() {
		// seedDownloadingSeason creates a show with one season of n episodes,
		// all flipped to "downloading", and returns their IDs.
		seedDownloadingSeason := func(tvdb uint32, n int) []uint32 {
			GinkgoHelper()
			eps := make([]EpisodeSeed, n)
			for i := range eps {
				eps[i] = EpisodeSeed{Number: uint16(i + 1), Title: "E"}
			}
			show, err := store.CreateTVShow(ctx, CreateTVShowParams{
				Title: "Show", Year: 2020, TvdbID: tvdb,
				Seasons: []SeasonSeed{{Number: 1, Episodes: eps}},
			})
			Expect(err).NotTo(HaveOccurred())
			ids := make([]uint32, 0, n)
			for _, e := range show.Edges.Seasons[0].Edges.Episodes {
				_, err := client.Episode.UpdateOneID(e.ID).
					SetStatus(episode.StatusDownloading).Save(ctx)
				Expect(err).NotTo(HaveOccurred())
				ids = append(ids, e.ID)
			}
			return ids
		}

		It("reverts a stranded season pack with no active record", func() {
			ids := seedDownloadingSeason(7001, 3)

			n, err := store.RevertOrphanedDownloadingEpisodes(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(n).To(Equal(3))
			for _, id := range ids {
				e, _ := client.Episode.Get(ctx, id)
				Expect(e.Status).To(Equal(episode.StatusWanted))
			}
		})

		It("spares the whole season while it has an active record", func() {
			ids := seedDownloadingSeason(7002, 3)
			_, err := store.CreateDownloadRecord(ctx, CreateDownloadRecordParams{
				Title: "pack", Size: 1, TorrentHash: "h",
				Status:             downloadrecord.StatusDownloading,
				EpisodeID:          ids[0],
				DownloadClientName: clientName,
			})
			Expect(err).NotTo(HaveOccurred())

			n, err := store.RevertOrphanedDownloadingEpisodes(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(n).To(Equal(0))
			for _, id := range ids {
				e, _ := client.Episode.Get(ctx, id)
				Expect(e.Status).To(Equal(episode.StatusDownloading))
			}
		})

		It("never reverts an episode that already has a media file", func() {
			ids := seedDownloadingSeason(7003, 2)
			_, err := client.MediaFile.Create().
				SetPath("/lib/e1.mkv").SetSize(10).
				SetEpisodeID(ids[0]).Save(ctx)
			Expect(err).NotTo(HaveOccurred())

			n, err := store.RevertOrphanedDownloadingEpisodes(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(n).To(Equal(1)) // only the file-less episode reverts
			withFile, _ := client.Episode.Get(ctx, ids[0])
			fileless, _ := client.Episode.Get(ctx, ids[1])
			Expect(withFile.Status).To(Equal(episode.StatusDownloading))
			Expect(fileless.Status).To(Equal(episode.StatusWanted))
		})
	})

	Describe("SyncSeasonDownloadStateForRecord", func() {
		It("pauses then resumes a whole season's downloading episodes", func() {
			show, err := store.CreateTVShow(ctx, CreateTVShowParams{
				Title: "S", Year: 2020, TvdbID: 8001,
				Seasons: []SeasonSeed{{Number: 1, Episodes: []EpisodeSeed{
					{Number: 1, Title: "E1"}, {Number: 2, Title: "E2"},
				}}},
			})
			Expect(err).NotTo(HaveOccurred())
			epIDs := make([]uint32, 0, len(show.Edges.Seasons[0].Edges.Episodes))
			for _, e := range show.Edges.Seasons[0].Edges.Episodes {
				_, err := client.Episode.UpdateOneID(e.ID).
					SetStatus(episode.StatusDownloading).Save(ctx)
				Expect(err).NotTo(HaveOccurred())
				epIDs = append(epIDs, e.ID)
			}
			// One record links only the first episode (season-pack shape).
			rec, err := store.CreateDownloadRecord(ctx, CreateDownloadRecordParams{
				Title: "pack", Size: 1, TorrentHash: "h",
				Status:             downloadrecord.StatusDownloading,
				EpisodeID:          epIDs[0],
				DownloadClientName: clientName,
			})
			Expect(err).NotTo(HaveOccurred())

			Expect(store.SyncSeasonDownloadStateForRecord(ctx, rec.ID, true)).
				To(Succeed())
			for _, id := range epIDs {
				e, _ := client.Episode.Get(ctx, id)
				Expect(e.Status).To(Equal(episode.StatusPaused))
			}

			Expect(store.SyncSeasonDownloadStateForRecord(ctx, rec.ID, false)).
				To(Succeed())
			for _, id := range epIDs {
				e, _ := client.Episode.Get(ctx, id)
				Expect(e.Status).To(Equal(episode.StatusDownloading))
			}
		})

		It("is a no-op for a movie record", func() {
			rec := createRec("mh", downloadrecord.StatusDownloading)
			Expect(store.SyncSeasonDownloadStateForRecord(ctx, rec.ID, true)).
				To(Succeed())
		})
	})

	Describe("AllDownloadRecordHashes", func() {
		It("returns every non-empty torrent hash as a set", func() {
			createRec("H1", downloadrecord.StatusDownloading)
			createRec("H2", downloadrecord.StatusCompleted)
			createRec("", downloadrecord.StatusDownloading) // empty hash skipped

			set, err := store.AllDownloadRecordHashes(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(set).To(HaveKey("H1"))
			Expect(set).To(HaveKey("H2"))
			Expect(set).NotTo(HaveKey(""))
		})
	})

	Describe("CreateDownloadRecord adoption fields", func() {
		It("persists save_path, quality, and failure_reason when set", func() {
			rec, err := store.CreateDownloadRecord(ctx, CreateDownloadRecordParams{
				Title: "t", Size: 1, TorrentHash: "h",
				Status:        downloadrecord.StatusPending,
				MovieID:       movieID,
				SavePath:      "/data/t",
				Quality:       "1080p",
				FailureReason: "already have a file",
			})
			Expect(err).NotTo(HaveOccurred())

			got, _ := client.DownloadRecord.Get(ctx, rec.ID)
			Expect(got.SavePath).To(Equal("/data/t"))
			Expect(got.Quality).To(Equal("1080p"))
			Expect(got.FailureReason).To(Equal("already have a file"))
			Expect(got.Status).To(Equal(downloadrecord.StatusPending))
		})
	})

	Describe("LatestImportedRecordForMovie", func() {
		It("returns the most recent hash-carrying record for the movie", func() {
			createRec("old", downloadrecord.StatusCompleted)
			newest := createRec("new", downloadrecord.StatusCompleted)

			got, err := store.LatestImportedRecordForMovie(ctx, movieID)
			Expect(err).NotTo(HaveOccurred())
			Expect(got.ID).To(Equal(newest.ID))
		})

		It("returns NotFound when the movie has no hash-carrying record", func() {
			_, err := store.LatestImportedRecordForMovie(ctx, movieID)
			Expect(ent.IsNotFound(err)).To(BeTrue())
		})
	})

	Describe("DeleteStalePendingAdoptions", func() {
		// other-client pending: must never be touched when pruning "qb".
		otherPending := func(hash string) *ent.DownloadRecord {
			GinkgoHelper()
			rec, err := store.CreateDownloadRecord(ctx, CreateDownloadRecordParams{
				Title: "t", Size: 1, TorrentHash: hash,
				Status:             downloadrecord.StatusPending,
				MovieID:            movieID,
				DownloadClientName: "deluge",
			})
			Expect(err).NotTo(HaveOccurred())
			return rec
		}

		It("prunes only this client's pendings whose hash is gone", func() {
			live := createRec("live", downloadrecord.StatusPending)
			stale := createRec("stale", downloadrecord.StatusPending)
			active := createRec(
				"active",
				downloadrecord.StatusDownloading,
			) // not pending
			other := otherPending(
				"stale",
			) // different client

			n, err := store.DeleteStalePendingAdoptions(
				ctx,
				clientName,
				[]string{"live"},
			)
			Expect(err).NotTo(HaveOccurred())
			Expect(n).To(Equal(1))

			_, err = client.DownloadRecord.Get(ctx, stale.ID)
			Expect(ent.IsNotFound(err)).To(BeTrue())
			for _, keep := range []*ent.DownloadRecord{live, active, other} {
				_, err := client.DownloadRecord.Get(ctx, keep.ID)
				Expect(err).NotTo(HaveOccurred())
			}
		})

		It(
			"prunes every pending for the client when it reports no torrents",
			func() {
				createRec("a", downloadrecord.StatusPending)
				createRec("b", downloadrecord.StatusPending)
				other := otherPending("c")

				n, err := store.DeleteStalePendingAdoptions(ctx, clientName, nil)
				Expect(err).NotTo(HaveOccurred())
				Expect(n).To(Equal(2))

				_, err = client.DownloadRecord.Get(ctx, other.ID)
				Expect(err).NotTo(HaveOccurred())
			},
		)
	})

	Describe("ListPendingDownloadRecords / FindPendingDownloadRecordByID", func() {
		It("lists pending records and finds one by id, edges loaded", func() {
			pending := createRec("p", downloadrecord.StatusPending)
			createRec("d", downloadrecord.StatusDownloading)

			list, err := store.ListPendingDownloadRecords(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(list).To(HaveLen(1))
			Expect(list[0].ID).To(Equal(pending.ID))
			Expect(list[0].Edges.Movie).NotTo(BeNil())

			got, err := store.FindPendingDownloadRecordByID(ctx, pending.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(got.Edges.Movie).NotTo(BeNil())

			_, err = store.FindPendingDownloadRecordByID(ctx, 99999)
			Expect(ent.IsNotFound(err)).To(BeTrue())
		})
	})
})
