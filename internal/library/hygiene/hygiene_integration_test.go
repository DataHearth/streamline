package hygiene

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stretchr/testify/mock"

	"github.com/datahearth/streamline/ent"
	entalbum "github.com/datahearth/streamline/ent/album"
	entbook "github.com/datahearth/streamline/ent/book"
	entepisode "github.com/datahearth/streamline/ent/episode"
	entmediaevent "github.com/datahearth/streamline/ent/mediaevent"
	entmediafile "github.com/datahearth/streamline/ent/mediafile"
	entmovie "github.com/datahearth/streamline/ent/movie"
	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/db"
	"github.com/datahearth/streamline/internal/events"
	"github.com/datahearth/streamline/internal/library/roothealth"
	"github.com/datahearth/streamline/internal/metadata"
	metamocks "github.com/datahearth/streamline/internal/metadata/mocks"
	"github.com/datahearth/streamline/internal/testutil/configtest"
	"github.com/datahearth/streamline/internal/testutil/dbtest"
)

// unmountingStore removes files from disk once the sweep has decided which roots
// to skip, the moment a mount dies partway through a tick.
type unmountingStore struct {
	db.Store
	paths []string
}

func (s *unmountingStore) ListMediaFilesForDrift(
	ctx context.Context,
	afterID uint32,
	limit int,
	skip ...db.LibraryRoot,
) ([]db.DriftRow, error) {
	for _, p := range s.paths {
		Expect(os.Remove(p)).To(Succeed())
	}
	s.paths = nil
	return s.Store.ListMediaFilesForDrift(ctx, afterID, limit, skip...)
}

var _ = Describe("hygiene end-to-end", Label("integration", "hygiene"), func() {
	var (
		ctx       context.Context
		tmpDir    string
		entClient *ent.Client
		store     db.Store
		meta      *metamocks.MockProvider
		svc       *Service
	)

	BeforeEach(func() {
		ctx = context.Background()
		tmpDir = GinkgoT().TempDir()
		cfg := configtest.Setup(map[string]any{
			"library": map[string]any{
				"movie_path":          tmpDir,
				"movie_naming":        "{title} ({year})/{title}.{ext}",
				"import_mode":         "copy",
				"import_max_attempts": 3,
				"drift_grace_ticks":   3,
			},
		})
		entClient = dbtest.SetupTestDB(ctx)
		DeferCleanup(entClient.Close)
		// The drift specs below assert the MediaEvent rows a sweep writes, and
		// events.Record needs the package default client the server wires up.
		events.Register(entClient)
		store = db.New(entClient)
		meta = metamocks.NewMockProvider(GinkgoT())
		svc = New(
			store,
			meta,
			metamocks.NewMockTVProvider(GinkgoT()),
			&cfg.Library,
		)
	})

	It("adopts an orphan where it lies for a tracked movie", func() {
		// The orphan is already inside the library, so it is tracked at its own
		// path. Transferring it under import_mode would hardlink a second name
		// beside it, and deleting the movie later would remove only the tracked
		// one.
		srcDir := filepath.Join(tmpDir, "incoming")
		Expect(os.MkdirAll(srcDir, 0o755)).To(Succeed())
		orphan := filepath.Join(srcDir, "Inception.2010.1080p.BluRay.mkv")
		Expect(os.WriteFile(orphan, make([]byte, 60*1024*1024), 0o644)).To(Succeed())

		_, err := store.CreateMovie(ctx, db.CreateMovieParams{
			Title:         "Inception",
			OriginalTitle: "Inception",
			Year:          2010,
			TmdbID:        27205,
			Status:        entmovie.StatusWanted,
		})
		Expect(err).NotTo(HaveOccurred())

		meta.EXPECT().SearchMovie(mock.Anything, "Inception", uint16(2010)).
			Return([]metadata.MovieResult{
				{TMDBID: 27205, Title: "Inception", Year: 2010},
			}, nil).Once()

		Expect(svc.RunOrphanScan(ctx)).To(Succeed())
		Expect(svc.RunOrphanScan(ctx)).To(Succeed())

		movie, err := store.FindMovieByTMDBID(ctx, 27205)
		Expect(err).NotTo(HaveOccurred())
		Expect(string(movie.Status)).To(Equal("available"))
		files, err := store.ListMediaFilesByMovieID(ctx, movie.ID)
		Expect(err).NotTo(HaveOccurred())
		Expect(files).To(HaveLen(1))
		Expect(files[0].Path).To(Equal(orphan))
		Expect(filepath.Join(tmpDir, "Inception (2010)")).NotTo(BeAnExistingFile())

		_, total, err := store.ListImportScans(ctx, 0, 100)
		Expect(err).NotTo(HaveOccurred())
		Expect(total).To(BeZero())
	})

	It(
		"appends to the existing review queue instead of a new scan each run",
		func() {
			// Orphans trickle in across runs (a migrated *arr library classifies
			// over several scans). Every run must fold new orphans into the one
			// open review queue for the directory — one import entry per directory,
			// not a fresh scan on every restart.
			meta.EXPECT().SearchMovie(mock.Anything, mock.Anything, mock.Anything).
				Return(nil, nil) // everything unmatched → queued for review

			a := filepath.Join(tmpDir, "First Unmatched File.mkv")
			Expect(os.WriteFile(a, make([]byte, 60*1024*1024), 0o644)).To(Succeed())
			Expect(svc.RunOrphanScan(ctx)).To(Succeed())

			// A second orphan shows up; the first is already pending review.
			b := filepath.Join(tmpDir, "Second Unmatched File.mkv")
			Expect(os.WriteFile(b, make([]byte, 60*1024*1024), 0o644)).To(Succeed())
			Expect(svc.RunOrphanScan(ctx)).To(Succeed())

			scans, total, err := store.ListImportScans(ctx, 0, 100)
			Expect(err).NotTo(HaveOccurred())
			Expect(total).To(Equal(uint32(1)), "one review entry per directory")

			_, fileCount, err := store.FilterImportScanFiles(
				ctx, db.FilterImportScanFilesParams{ScanID: scans[0].ID},
			)
			Expect(err).NotTo(HaveOccurred())
			Expect(fileCount).To(Equal(uint32(2)), "both orphans in the one scan")
		},
	)

	It("reverts a Movie when its file disappears past the grace window", func() {
		path := filepath.Join(tmpDir, "movie.mkv")
		Expect(os.WriteFile(path, []byte("data"), 0o644)).To(Succeed())

		m, err := store.CreateMovie(ctx, db.CreateMovieParams{
			Title:         "Gone",
			OriginalTitle: "Gone",
			Year:          2024,
			TmdbID:        1234,
			Status:        entmovie.StatusAvailable,
		})
		Expect(err).NotTo(HaveOccurred())
		mf, err := store.CreateMediaFile(ctx, db.CreateMediaFileParams{
			MovieID: m.ID, Path: path, Size: 4,
		})
		Expect(err).NotTo(HaveOccurred())

		Expect(os.Remove(path)).To(Succeed())

		By("only starting the clock on the tick that first finds it gone")
		Expect(svc.RunDriftCheck(ctx, time.Hour)).To(Succeed())
		refreshed, err := store.FindMovieByID(ctx, m.ID)
		Expect(err).NotTo(HaveOccurred())
		Expect(string(refreshed.Status)).To(Equal("available"))

		By("reverting on a later tick past the grace window")
		Expect(entClient.MediaFile.UpdateOneID(mf.ID).
			SetMissingSince(time.Now().Add(-4 * time.Hour)).
			Exec(ctx)).To(Succeed())
		Expect(svc.RunDriftCheck(ctx, time.Hour)).To(Succeed())

		refreshed, err = store.FindMovieByID(ctx, m.ID)
		Expect(err).NotTo(HaveOccurred())
		Expect(string(refreshed.Status)).To(Equal("wanted"))

		for _, t := range []entmediaevent.Type{
			entmediaevent.TypeDriftDetected,
			entmediaevent.TypeDriftConfirmed,
		} {
			evs, err := entClient.MediaEvent.Query().
				Where(entmediaevent.TypeEQ(t)).
				WithMovie().
				All(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(evs).To(HaveLen(1), string(t))
			Expect(evs[0].Edges.Movie).NotTo(BeNil(), "movie drift stays per movie")
			Expect(evs[0].Edges.Movie.ID).To(Equal(m.ID))
			Expect(evs[0].Payload["path"]).To(Equal(path))
		}
	})

	It("reverts an Episode when its file disappears past the grace window", func() {
		path := filepath.Join(tmpDir, "episode.mkv")
		Expect(os.WriteFile(path, []byte("data"), 0o644)).To(Succeed())

		air := time.Now()
		show, err := store.CreateTVShow(ctx, db.CreateTVShowParams{
			Title: "Gone Show", Year: 2024, TvdbID: 4321,
			Seasons: []db.SeasonSeed{
				{
					Number: 1,
					Episodes: []db.EpisodeSeed{
						{Number: 1, Title: "Pilot", AirDate: &air},
					},
				},
			},
		})
		Expect(err).NotTo(HaveOccurred())
		ep := show.Edges.Seasons[0].Edges.Episodes[0]
		Expect(store.SetEpisodeStatus(ctx, ep.ID, entepisode.StatusAvailable)).
			To(Succeed())

		mf, err := store.CreateMediaFile(ctx, db.CreateMediaFileParams{
			EpisodeID: ep.ID, Path: path, Size: 4,
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(entClient.MediaFile.UpdateOneID(mf.ID).
			SetMissingSince(time.Now().Add(-2 * time.Hour)).
			Exec(ctx)).To(Succeed())

		Expect(os.Remove(path)).To(Succeed())

		Expect(svc.RunDriftCheck(ctx, time.Millisecond)).To(Succeed())

		_, err = store.FindMediaFileByID(ctx, mf.ID)
		Expect(ent.IsNotFound(err)).To(BeTrue())
		refreshed, err := entClient.Episode.Get(ctx, ep.ID)
		Expect(err).NotTo(HaveOccurred())
		Expect(string(refreshed.Status)).To(Equal("wanted"))
	})

	It("folds a whole show's missing files into one event per type", func() {
		air := time.Now()
		show, err := store.CreateTVShow(ctx, db.CreateTVShowParams{
			Title: "Vanished", Year: 2024, TvdbID: 5555,
			Seasons: []db.SeasonSeed{
				{
					Number: 1,
					Episodes: []db.EpisodeSeed{
						{Number: 1, Title: "One", AirDate: &air},
						{Number: 2, Title: "Two", AirDate: &air},
					},
				},
				{
					Number: 2,
					Episodes: []db.EpisodeSeed{
						{Number: 1, Title: "Three", AirDate: &air},
					},
				},
			},
		})
		Expect(err).NotTo(HaveOccurred())

		// Staggered so the aggregate's missing_since is provably the earliest
		// of the three and not just whichever row happened to be walked first.
		oldest := time.Now().Add(-5 * time.Hour).UTC().Truncate(time.Second)
		var fileIDs []uint32
		for _, season := range show.Edges.Seasons {
			for _, ep := range season.Edges.Episodes {
				Expect(store.SetEpisodeStatus(
					ctx, ep.ID, entepisode.StatusAvailable,
				)).To(Succeed())
				path := filepath.Join(tmpDir, fmt.Sprintf(
					"Vanished S%02dE%02d.mkv", season.Number, ep.Number,
				))
				Expect(os.WriteFile(path, []byte("data"), 0o644)).To(Succeed())
				mf, err := store.CreateMediaFile(ctx, db.CreateMediaFileParams{
					EpisodeID: ep.ID, Path: path, Size: 4,
				})
				Expect(err).NotTo(HaveOccurred())
				Expect(os.Remove(path)).To(Succeed())
				fileIDs = append(fileIDs, mf.ID)
			}
		}

		Expect(svc.RunDriftCheck(ctx, time.Millisecond)).To(Succeed())
		for offset, id := range fileIDs {
			Expect(entClient.MediaFile.UpdateOneID(id).
				SetMissingSince(oldest.Add(time.Duration(offset) * time.Hour)).
				Exec(ctx)).To(Succeed())
		}
		Expect(svc.RunDriftCheck(ctx, time.Millisecond)).To(Succeed())

		for _, t := range []entmediaevent.Type{
			entmediaevent.TypeDriftDetected,
			entmediaevent.TypeDriftConfirmed,
		} {
			evs, err := entClient.MediaEvent.Query().
				Where(entmediaevent.TypeEQ(t)).
				WithTvShow().
				WithEpisode().
				All(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(evs).To(HaveLen(1), string(t))
			Expect(evs[0].Edges.Episode).To(BeNil(), "aggregated, not per episode")
			Expect(evs[0].Edges.TvShow).NotTo(BeNil())
			Expect(evs[0].Edges.TvShow.ID).To(Equal(show.ID))
			Expect(evs[0].Payload["seasons"]).To(HaveExactElements(
				BeEquivalentTo(1), BeEquivalentTo(2),
			))
			Expect(evs[0].Payload["episodes"]).To(BeEquivalentTo(3))
			Expect(evs[0].Payload).NotTo(HaveKey("path"))
		}

		confirmed, err := entClient.MediaEvent.Query().
			Where(entmediaevent.TypeEQ(entmediaevent.TypeDriftConfirmed)).
			Only(ctx)
		Expect(err).NotTo(HaveOccurred())
		since, err := time.Parse(
			time.RFC3339Nano, confirmed.Payload["missing_since"].(string),
		)
		Expect(err).NotTo(HaveOccurred())
		Expect(since.UTC()).To(BeTemporally("==", oldest))
	})

	Describe("an offline library root", func() {
		var (
			musicDir string
			alb      *ent.Album
			movie    *ent.Movie
			lost     *ent.MediaFile
		)

		trackFile := func(name string, position uint16, since *time.Time) *ent.MediaFile {
			GinkgoHelper()
			tr := entClient.Track.Create().
				SetTitle(name).
				SetDisc(1).SetPosition(position).SetAlbum(alb).SaveX(ctx)
			return entClient.MediaFile.Create().
				SetPath(filepath.Join(musicDir, name)).SetSize(4).
				SetTrack(tr).SetNillableMissingSince(since).SaveX(ctx)
		}
		driftEventCount := func() int {
			GinkgoHelper()
			return entClient.MediaEvent.Query().
				Where(entmediaevent.HasAlbum()).
				CountX(ctx)
		}
		missingSince := func(mf *ent.MediaFile) *time.Time {
			GinkgoHelper()
			return entClient.MediaFile.GetX(ctx, mf.ID).MissingSince
		}

		BeforeEach(func() {
			musicDir = GinkgoT().TempDir()
			configtest.Setup(map[string]any{"library": map[string]any{
				"movie_path": tmpDir, "music_path": musicDir,
			}})
			roothealth.Register(store)
			DeferCleanup(func() { roothealth.Register(nil) })

			artist := entClient.Artist.Create().
				SetMbid("artist").
				SetName("Artist").
				SaveX(ctx)
			alb = entClient.Album.Create().
				SetMbid("album").SetTitle("Album").SetArtist(artist).
				SetStatus(entalbum.StatusAvailable).SaveX(ctx)

			var err error
			movie, err = store.CreateMovie(ctx, db.CreateMovieParams{
				Title: "Control", OriginalTitle: "Control", Year: 2020, TmdbID: 1,
				Status: entmovie.StatusAvailable,
			})
			Expect(err).NotTo(HaveOccurred())
			kept := filepath.Join(tmpDir, "kept.mkv")
			Expect(os.WriteFile(kept, []byte("data"), 0o644)).To(Succeed())
			entClient.MediaFile.Create().
				SetPath(kept).
				SetSize(4).
				SetMovie(movie).
				SaveX(ctx)
			lost = entClient.MediaFile.Create().
				SetPath(filepath.Join(tmpDir, "lost.mkv")).
				SetSize(4).SetMovie(movie).SaveX(ctx)
		})

		Describe("accepting that files are gone", func() {
			// 25 rows sample at every offset but 4, 9, 14, 19 and 23, so a file
			// at offset 4 leaves the root offline while its row has a real file.
			seedWithUnsampledFile := func() (present *ent.MediaFile, gone []*ent.MediaFile) {
				GinkgoHelper()
				for i := range 25 {
					row := trackFile(fmt.Sprintf("%02d.flac", i), uint16(i+1), nil)
					if i == 4 {
						Expect(
							os.WriteFile(row.Path, []byte("data"), 0o644),
						).To(Succeed())
						present = row
						continue
					}
					gone = append(gone, row)
				}
				Expect(roothealth.Offline(ctx, db.RootMusic)).NotTo(BeNil())
				return present, gone
			}
			confirmed := func() []*ent.MediaEvent {
				GinkgoHelper()
				return entClient.MediaEvent.Query().Where(
					entmediaevent.TypeEQ(entmediaevent.TypeDriftConfirmed),
					entmediaevent.HasAlbum(),
				).AllX(ctx)
			}

			It("removes only rows whose file is gone, ignoring grace", func() {
				present, gone := seedWithUnsampledFile()

				removed, err := svc.AcceptMissing(ctx, db.RootMusic)

				Expect(err).NotTo(HaveOccurred())
				Expect(removed).To(Equal(len(gone)))
				Expect(
					entClient.MediaFile.Query().
						Where(entmediafile.HasTrack()).
						IDsX(ctx),
				).
					To(ConsistOf(present.ID))
				Expect(
					entClient.Album.GetX(ctx, alb.ID).Status,
				).To(Equal(entalbum.StatusWanted))
				Expect(confirmed()).To(HaveLen(1))
			})

			It("leaves the root online afterwards", func() {
				seedWithUnsampledFile()

				_, err := svc.AcceptMissing(ctx, db.RootMusic)

				Expect(err).NotTo(HaveOccurred())
				Expect(roothealth.Offline(ctx, db.RootMusic)).To(BeNil())
			})

			It("refuses a root that is online and removes nothing", func() {
				_, err := svc.AcceptMissing(ctx, db.RootMovies)

				Expect(err).To(MatchError(ErrRootOnline))
				Expect(store.FindMediaFileByID(ctx, lost.ID)).NotTo(BeNil())
			})

			It("refuses a second call straight after a successful one", func() {
				trackFile("01.flac", 1, nil)
				removed, err := svc.AcceptMissing(ctx, db.RootMusic)
				Expect(err).NotTo(HaveOccurred())
				Expect(removed).To(Equal(1))

				_, err = svc.AcceptMissing(ctx, db.RootMusic)

				Expect(err).To(MatchError(ErrRootOnline))
			})

			It("keeps a row whose stat fails with a permission error", func() {
				locked := filepath.Join(musicDir, "locked")
				Expect(os.Mkdir(locked, 0o755)).To(Succeed())
				unreadable := filepath.Join(locked, "01.flac")
				Expect(os.WriteFile(unreadable, []byte("data"), 0o644)).To(Succeed())
				Expect(os.Chmod(locked, 0)).To(Succeed())
				DeferCleanup(
					func() { Expect(os.Chmod(locked, 0o755)).To(Succeed()) },
				)
				tr := entClient.Track.Create().
					SetTitle("x").SetDisc(1).SetPosition(1).SetAlbum(alb).SaveX(ctx)
				row := entClient.MediaFile.Create().
					SetPath(unreadable).SetSize(4).SetTrack(tr).SaveX(ctx)
				Expect(roothealth.Offline(ctx, db.RootMusic)).NotTo(BeNil())

				removed, err := svc.AcceptMissing(ctx, db.RootMusic)

				Expect(err).NotTo(HaveOccurred())
				Expect(removed).To(BeZero())
				Expect(store.FindMediaFileByID(ctx, row.ID)).NotTo(BeNil())
			})

			It("reverts a movie, an episode and a book slot", func() {
				entClient.MediaFile.Delete().
					Where(entmediafile.HasMovie()).
					ExecX(ctx)
				entClient.MediaFile.Create().
					SetPath("/gone/m.mkv").
					SetSize(4).
					SetMovie(movie).
					SaveX(ctx)
				show := entClient.TVShow.Create().
					SetTitle("S").
					SetYear(2020).
					SetTvdbID(1).
					SaveX(ctx)
				season := entClient.Season.Create().
					SetNumber(1).
					SetTvShow(show).
					SaveX(ctx)
				episode := entClient.Episode.Create().SetNumber(1).SetSeason(season).
					SetStatus(entepisode.StatusAvailable).SaveX(ctx)
				entClient.MediaFile.Create().
					SetPath("/gone/e.mkv").
					SetSize(4).
					SetEpisode(episode).
					SaveX(ctx)
				book := entClient.Book.Create().SetHardcoverID(1).SetTitle("B").
					SetEbookStatus(entbook.EbookStatusAvailable).SaveX(ctx)
				entClient.MediaFile.Create().
					SetPath("/gone/b.epub").
					SetSize(4).
					SetBook(book).
					SetBookKind(entmediafile.BookKindEbook).
					SaveX(ctx)

				for _, root := range []db.LibraryRoot{db.RootMovies, db.RootSeries, db.RootEbooks} {
					Expect(svc.AcceptMissing(ctx, root)).To(Equal(1))
				}

				Expect(
					entClient.Movie.GetX(ctx, movie.ID).Status,
				).To(Equal(entmovie.StatusWanted))
				Expect(
					entClient.Episode.GetX(ctx, episode.ID).Status,
				).To(Equal(entepisode.StatusWanted))
				Expect(
					entClient.Book.GetX(ctx, book.ID).EbookStatus,
				).To(Equal(entbook.EbookStatusWanted))
			})
		})

		It("leaves an offline root's rows untouched", func() {
			gone := trackFile("01.flac", 1, nil)

			Expect(svc.RunDriftCheck(ctx, time.Millisecond)).To(Succeed())

			Expect(missingSince(gone)).To(BeNil())
			Expect(driftEventCount()).To(BeZero())
			Expect(
				entClient.Album.GetX(ctx, alb.ID).Status,
			).To(Equal(entalbum.StatusAvailable))
			Expect(missingSince(lost)).NotTo(BeNil())
		})

		It("does not revert a past-grace row while its root is offline", func() {
			old := time.Now().Add(-48 * time.Hour)
			gone := trackFile("01.flac", 1, &old)

			Expect(svc.RunDriftCheck(ctx, time.Millisecond)).To(Succeed())

			Expect(store.FindMediaFileByID(ctx, gone.ID)).NotTo(BeNil())
			Expect(
				entClient.Album.GetX(ctx, alb.ID).Status,
			).To(Equal(entalbum.StatusAvailable))
			Expect(driftEventCount()).To(BeZero())
		})

		It("starts no revert for a root that goes offline mid-sweep", func() {
			paths := []string{
				filepath.Join(musicDir, "01.flac"),
				filepath.Join(musicDir, "02.flac"),
			}
			for _, p := range paths {
				Expect(os.WriteFile(p, []byte("data"), 0o644)).To(Succeed())
			}
			first, second := trackFile(
				"01.flac",
				1,
				nil,
			), trackFile(
				"02.flac",
				2,
				nil,
			)
			sweep := New(
				&unmountingStore{Store: store, paths: paths},
				meta, metamocks.NewMockTVProvider(GinkgoT()),
				&config.Get().Library,
			)

			Expect(sweep.RunDriftCheck(ctx, time.Millisecond)).To(Succeed())

			started := missingSince(first)
			Expect(started).NotTo(BeNil())
			Expect(missingSince(second)).NotTo(BeNil())
			Expect(
				entClient.Album.GetX(ctx, alb.ID).Status,
			).To(Equal(entalbum.StatusAvailable))

			Expect(svc.RunDriftCheck(ctx, time.Nanosecond)).To(Succeed())

			Expect(store.FindMediaFileByID(ctx, first.ID)).NotTo(BeNil())
			Expect(missingSince(first)).To(HaveValue(BeTemporally("==", *started)))
			Expect(
				entClient.Album.GetX(ctx, alb.ID).Status,
			).To(Equal(entalbum.StatusAvailable))
		})

		It("clears the clocks when the root returns", func() {
			old := time.Now().Add(-48 * time.Hour)
			gone := trackFile("01.flac", 1, &old)
			Expect(svc.RunDriftCheck(ctx, time.Hour)).To(Succeed())
			Expect(missingSince(gone)).NotTo(BeNil())

			Expect(os.WriteFile(gone.Path, []byte("data"), 0o644)).To(Succeed())
			Expect(svc.RunDriftCheck(ctx, time.Hour)).To(Succeed())

			Expect(missingSince(gone)).To(BeNil())
		})

		It("skips the movie orphan scan while movies are offline", func() {
			entClient.MediaFile.Delete().ExecX(ctx)
			row := entClient.MediaFile.Create().
				SetPath(filepath.Join(musicDir, "elsewhere.mkv")).
				SetSize(4).SetMovie(movie).SaveX(ctx)
			orphan := filepath.Join(tmpDir, "Orphan.2010.1080p.BluRay.mkv")
			Expect(
				os.WriteFile(orphan, make([]byte, 60*1024*1024), 0o644),
			).To(Succeed())

			Expect(svc.RunOrphanScan(ctx)).To(Succeed())
			_, total, err := store.ListImportScans(ctx, 0, 100)
			Expect(err).NotTo(HaveOccurred())
			Expect(total).To(BeZero())

			Expect(os.WriteFile(row.Path, []byte("data"), 0o644)).To(Succeed())
			meta.EXPECT().SearchMovie(mock.Anything, "Orphan", uint16(2010)).
				Return(nil, nil).Once()
			Expect(svc.RunOrphanScan(ctx)).To(Succeed())
		})

		It("skips the series orphan scan while series are offline", func() {
			seriesDir := GinkgoT().TempDir()
			cfg := configtest.Setup(map[string]any{"library": map[string]any{
				"series_path": seriesDir,
			}})
			show := entClient.TVShow.Create().
				SetTitle("Tracked").
				SetYear(2020).
				SetTvdbID(1).
				SaveX(ctx)
			season := entClient.Season.Create().
				SetNumber(1).
				SetTvShow(show).
				SaveX(ctx)
			episode := entClient.Episode.Create().
				SetNumber(1).
				SetSeason(season).
				SaveX(ctx)
			entClient.MediaFile.Create().
				SetPath(filepath.Join(musicDir, "elsewhere.mkv")).
				SetSize(4).SetEpisode(episode).SaveX(ctx)
			folder := filepath.Join(seriesDir, "Breaking Bad")
			Expect(os.MkdirAll(folder, 0o755)).To(Succeed())
			Expect(os.WriteFile(
				filepath.Join(
					folder,
					"Breaking Bad S01E01.mkv",
				),
				make([]byte, 60*1024*1024),
				0o644,
			)).To(Succeed())
			scan := New(
				store,
				meta,
				metamocks.NewMockTVProvider(GinkgoT()),
				&cfg.Library,
			)

			Expect(scan.RunSeriesOrphanScan(ctx)).To(Succeed())

			_, total, err := store.ListImportScans(ctx, 0, 100)
			Expect(err).NotTo(HaveOccurred())
			Expect(total).To(BeZero())
		})
	})

	Describe("track and book files", func() {
		var missingSince time.Time

		// file writes a row whose file is already gone past the grace window.
		file := func(name string, set func(*ent.MediaFileCreate)) *ent.MediaFile {
			GinkgoHelper()
			c := entClient.MediaFile.Create().
				SetPath(filepath.Join(tmpDir, name)).
				SetSize(4).
				SetMissingSince(missingSince)
			set(c)
			return c.SaveX(ctx)
		}
		driftEvents := func(t entmediaevent.Type) []*ent.MediaEvent {
			GinkgoHelper()
			return entClient.MediaEvent.Query().
				Where(entmediaevent.TypeEQ(t)).
				WithAlbum().
				WithBook().
				AllX(ctx)
		}

		BeforeEach(func() {
			missingSince = time.Now().Add(-2 * time.Hour).UTC()
		})

		Describe("of an album", func() {
			var (
				alb      *ent.Album
				newTrack func(position uint16) *ent.Track
			)

			BeforeEach(func() {
				owner := entClient.Artist.Create().
					SetMbid("artist-mbid").SetName("Artist").
					SaveX(ctx)
				alb = entClient.Album.Create().
					SetMbid("album-mbid").SetTitle("Album").
					SetArtist(owner).SetStatus(entalbum.StatusAvailable).
					SaveX(ctx)
				newTrack = func(position uint16) *ent.Track {
					return entClient.Track.Create().
						SetTitle(fmt.Sprintf("Track %d", position)).
						SetDisc(1).SetPosition(position).SetAlbum(alb).
						SaveX(ctx)
				}
			})

			It(
				"sends an available album back to wanted, keeping its other tracks",
				func() {
					gone := file(
						"01.flac",
						func(c *ent.MediaFileCreate) { c.SetTrack(newTrack(1)) },
					)
					kept := filepath.Join(tmpDir, "02.flac")
					Expect(os.WriteFile(kept, []byte("data"), 0o644)).To(Succeed())
					other := entClient.MediaFile.Create().
						SetPath(kept).SetSize(4).SetTrack(newTrack(2)).SaveX(ctx)

					Expect(svc.RunDriftCheck(ctx, time.Millisecond)).To(Succeed())

					_, err := store.FindMediaFileByID(ctx, gone.ID)
					Expect(ent.IsNotFound(err)).To(BeTrue())
					_, err = store.FindMediaFileByID(ctx, other.ID)
					Expect(err).NotTo(HaveOccurred())
					Expect(entClient.Album.GetX(ctx, alb.ID).Status).
						To(Equal(entalbum.StatusWanted))
				},
			)

			It("reports one event per album for the whole sweep", func() {
				file(
					"01.flac",
					func(c *ent.MediaFileCreate) { c.SetTrack(newTrack(1)) },
				)
				file(
					"02.flac",
					func(c *ent.MediaFileCreate) { c.SetTrack(newTrack(2)) },
				)

				Expect(svc.RunDriftCheck(ctx, time.Millisecond)).To(Succeed())

				confirmed := driftEvents(entmediaevent.TypeDriftConfirmed)
				Expect(confirmed).To(HaveLen(1))
				Expect(confirmed[0].Edges.Album).NotTo(BeNil())
				Expect(confirmed[0].Edges.Album.ID).To(Equal(alb.ID))
				Expect(confirmed[0].Payload["tracks"]).To(BeEquivalentTo(2))
				Expect(confirmed[0].Payload).To(HaveKey("missing_since"))
				Expect(driftEvents(entmediaevent.TypeFileRemoved)).To(BeEmpty(),
					"a diagnosed disappearance is not also a bare removal")
			})

			It("leaves an album it does not hold as available alone", func() {
				entClient.Album.UpdateOneID(alb.ID).
					SetStatus(entalbum.StatusDownloading).ExecX(ctx)
				gone := file(
					"01.flac",
					func(c *ent.MediaFileCreate) { c.SetTrack(newTrack(1)) },
				)

				Expect(svc.RunDriftCheck(ctx, time.Millisecond)).To(Succeed())

				_, err := store.FindMediaFileByID(ctx, gone.ID)
				Expect(ent.IsNotFound(err)).To(BeTrue())
				Expect(entClient.Album.GetX(ctx, alb.ID).Status).
					To(Equal(entalbum.StatusDownloading))
			})

			It(
				"reports a file inside the grace window without touching the album",
				func() {
					row := entClient.MediaFile.Create().
						SetPath(filepath.Join(tmpDir, "01.flac")).
						SetSize(4).SetTrack(newTrack(1)).SaveX(ctx)

					Expect(svc.RunDriftCheck(ctx, time.Hour)).To(Succeed())

					_, err := store.FindMediaFileByID(ctx, row.ID)
					Expect(err).NotTo(HaveOccurred())
					Expect(entClient.Album.GetX(ctx, alb.ID).Status).
						To(Equal(entalbum.StatusAvailable))
					detected := driftEvents(entmediaevent.TypeDriftDetected)
					Expect(detected).To(HaveLen(1))
					Expect(detected[0].Edges.Album.ID).To(Equal(alb.ID))
					Expect(detected[0].Payload["tracks"]).To(BeEquivalentTo(1))
				},
			)
		})

		Describe("of a book", func() {
			var b *ent.Book

			BeforeEach(func() {
				b = entClient.Book.Create().
					SetHardcoverID(1).SetTitle("Book").
					SetEbookStatus(entbook.EbookStatusAvailable).
					SetAudiobookStatus(entbook.AudiobookStatusAvailable).
					SaveX(ctx)
			})

			inSlot := func(kind entmediafile.BookKind) func(*ent.MediaFileCreate) {
				return func(c *ent.MediaFileCreate) { c.SetBook(b).SetBookKind(kind) }
			}

			It("sends only the emptied slot back to wanted", func() {
				gone := file("book.epub", inSlot(entmediafile.BookKindEbook))
				kept := filepath.Join(tmpDir, "part1.m4b")
				Expect(os.WriteFile(kept, []byte("data"), 0o644)).To(Succeed())
				entClient.MediaFile.Create().SetPath(kept).SetSize(4).
					SetBook(b).SetBookKind(entmediafile.BookKindAudiobook).SaveX(ctx)

				Expect(svc.RunDriftCheck(ctx, time.Millisecond)).To(Succeed())

				_, err := store.FindMediaFileByID(ctx, gone.ID)
				Expect(ent.IsNotFound(err)).To(BeTrue())
				got := entClient.Book.GetX(ctx, b.ID)
				Expect(got.EbookStatus).To(Equal(entbook.EbookStatusWanted))
				Expect(
					got.AudiobookStatus,
				).To(Equal(entbook.AudiobookStatusAvailable))
				confirmed := driftEvents(entmediaevent.TypeDriftConfirmed)
				Expect(confirmed).To(HaveLen(1))
				Expect(confirmed[0].Edges.Book.ID).To(Equal(b.ID))
				Expect(confirmed[0].Payload["book_kind"]).To(Equal("ebook"))
			})

			It("keeps a slot available while it still holds a file", func() {
				// A grab into a slot holding files fails on the file already
				// there, so wanted would only burn a download.
				gone := file("part1.m4b", inSlot(entmediafile.BookKindAudiobook))
				kept := filepath.Join(tmpDir, "part2.m4b")
				Expect(os.WriteFile(kept, []byte("data"), 0o644)).To(Succeed())
				entClient.MediaFile.Create().SetPath(kept).SetSize(4).
					SetBook(b).SetBookKind(entmediafile.BookKindAudiobook).SaveX(ctx)

				Expect(svc.RunDriftCheck(ctx, time.Millisecond)).To(Succeed())

				_, err := store.FindMediaFileByID(ctx, gone.ID)
				Expect(ent.IsNotFound(err)).To(BeTrue())
				Expect(entClient.Book.GetX(ctx, b.ID).AudiobookStatus).
					To(Equal(entbook.AudiobookStatusAvailable))
				confirmed := driftEvents(entmediaevent.TypeDriftConfirmed)
				Expect(
					confirmed,
				).To(HaveLen(1), "the row left, so the owner is told")
			})

			It(
				"folds a whole audiobook into one event and empties the slot",
				func() {
					file("part1.m4b", inSlot(entmediafile.BookKindAudiobook))
					file("part2.m4b", inSlot(entmediafile.BookKindAudiobook))

					Expect(svc.RunDriftCheck(ctx, time.Millisecond)).To(Succeed())

					Expect(entClient.Book.GetX(ctx, b.ID).AudiobookStatus).
						To(Equal(entbook.AudiobookStatusWanted))
					confirmed := driftEvents(entmediaevent.TypeDriftConfirmed)
					Expect(confirmed).To(HaveLen(1))
					Expect(confirmed[0].Payload["files"]).To(BeEquivalentTo(2))
					Expect(confirmed[0].Payload["book_kind"]).To(Equal("audiobook"))
				},
			)
		})
	})

	It("deletes a media_file row whose owner is gone", func() {
		path := filepath.Join(tmpDir, "orphan.mkv")
		Expect(os.WriteFile(path, []byte("data"), 0o644)).To(Succeed())

		mf, err := entClient.MediaFile.Create().
			SetPath(path).
			SetSize(4).
			SetMissingSince(time.Now().Add(-2 * time.Hour)).
			Save(ctx)
		Expect(err).NotTo(HaveOccurred())
		Expect(os.Remove(path)).To(Succeed())

		Expect(svc.RunDriftCheck(ctx, time.Millisecond)).To(Succeed())

		_, err = store.FindMediaFileByID(ctx, mf.ID)
		Expect(ent.IsNotFound(err)).To(BeTrue())
	})
})

var _ = Describe(
	"Service.RunSeriesOrphanScan (integration)",
	Label("integration", "hygiene"),
	func() {
		var (
			ctx       context.Context
			tmpDir    string
			entClient *ent.Client
			store     db.Store
			tvmeta    *metamocks.MockTVProvider
			svc       *Service
		)

		BeforeEach(func() {
			ctx = context.Background()
			tmpDir = GinkgoT().TempDir()
			cfg := configtest.Setup(map[string]any{
				"library": map[string]any{
					"series_path": tmpDir,
					"import_mode": "copy",
				},
			})
			entClient = dbtest.SetupTestDB(ctx)
			DeferCleanup(entClient.Close)
			store = db.New(entClient)
			tvmeta = metamocks.NewMockTVProvider(GinkgoT())
			svc = New(store, metamocks.NewMockProvider(GinkgoT()), tvmeta,
				&cfg.Library)
		})

		placeShow := func(show, file string) {
			dir := filepath.Join(tmpDir, show)
			Expect(os.MkdirAll(dir, 0o755)).To(Succeed())
			Expect(
				os.WriteFile(
					filepath.Join(dir, file),
					make([]byte, 60*1024*1024),
					0o644,
				),
			).To(Succeed())
		}

		It("folds shows found across runs into one series scan", func() {
			tvmeta.EXPECT().SearchSeries(mock.Anything, "Breaking Bad").
				Return([]metadata.TVResult{{TVDBID: 81189, Title: "Breaking Bad", Year: 2008}}, nil).
				Once()
			tvmeta.EXPECT().SearchSeries(mock.Anything, "The Wire").
				Return([]metadata.TVResult{{TVDBID: 79126, Title: "The Wire", Year: 2002}}, nil).
				Once()

			placeShow("Breaking Bad", "Breaking Bad S01E01.mkv")
			Expect(svc.RunSeriesOrphanScan(ctx)).To(Succeed())

			// Second run with a new folder must fold into the same open review scan.
			placeShow("The Wire", "The Wire S01E01.mkv")
			Expect(svc.RunSeriesOrphanScan(ctx)).To(Succeed())

			scans, total, err := store.ListImportScans(ctx, 0, 50)
			Expect(err).NotTo(HaveOccurred())
			Expect(total).To(Equal(uint32(1)))

			shows, showTotal, err := store.ListImportScanShows(
				ctx, db.ListImportScanShowsParams{ScanID: scans[0].ID})
			Expect(err).NotTo(HaveOccurred())
			Expect(showTotal).To(Equal(uint32(2)))
			Expect([]string{shows[0].FolderPath, shows[1].FolderPath}).To(ConsistOf(
				filepath.Join(tmpDir, "Breaking Bad"),
				filepath.Join(tmpDir, "The Wire"),
			))
		})
	},
)
