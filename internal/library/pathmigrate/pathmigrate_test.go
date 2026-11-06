package pathmigrate

import (
	"bytes"
	"context"
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stretchr/testify/mock"

	"github.com/datahearth/streamline/ent"
	entmediafile "github.com/datahearth/streamline/ent/mediafile"
	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/db"
	dbmocks "github.com/datahearth/streamline/internal/db/mocks"
	"github.com/datahearth/streamline/internal/library/roothealth"
	"github.com/datahearth/streamline/internal/testutil/configtest"
	"github.com/datahearth/streamline/internal/testutil/dbtest"
)

var _ = Describe("Service", Label("unit", "library"), func() {
	var (
		ctx   context.Context
		store *dbmocks.MockStore
		svc   *Service
		root  string
		dest  string
	)

	// touch creates path with a parent tree so os.Stat sees it, and returns it.
	touch := func(path string) string {
		GinkgoHelper()
		Expect(os.MkdirAll(filepath.Dir(path), 0o755)).To(Succeed())
		Expect(os.WriteFile(path, []byte("x"), 0o644)).To(Succeed())
		return path
	}

	BeforeEach(func() {
		ctx = context.Background()
		tmp := GinkgoT().TempDir()
		root = filepath.Join(tmp, "media", "movies")
		dest = filepath.Join(tmp, "data", "movies")
		configtest.SetupFile(map[string]any{
			"library": map[string]any{"movie_path": root},
		})
		store = dbmocks.NewMockStore(GinkgoT())
		svc = NewService(store)
	})

	Describe("Roots", func() {
		// expectCounts stubs the per-root queries Roots always makes, so each
		// spec only states the rows it cares about.
		expectCounts := func(movieFiles []*ent.MediaFile, movieTotal int) {
			GinkgoHelper()
			store.EXPECT().ListMediaFilesByPathPrefix(mock.Anything, root).
				Return(movieFiles, nil).Once()
			store.EXPECT().CountMovieMediaFiles(mock.Anything).
				Return(movieTotal, nil).Once()
			store.EXPECT().
				ListMediaFilesByPathPrefix(mock.Anything, mock.Anything).
				Return(nil, nil).Times(4)
			store.EXPECT().CountEpisodeMediaFiles(mock.Anything).
				Return(0, nil).Once()
			store.EXPECT().
				ListDownloadRecordsByPathPrefix(mock.Anything, mock.Anything).
				Return(nil, nil).Once()
			store.EXPECT().
				ListTorrentSessionsByPathPrefix(mock.Anything, mock.Anything).
				Return(nil, nil).Once()
			store.EXPECT().CountLiveDownloadRecords(mock.Anything).
				Return(0, nil).Once()
			store.EXPECT().CountTorrentSessions(mock.Anything).
				Return(0, nil).Once()
			store.EXPECT().CountRootFiles(mock.Anything, mock.Anything).
				Return(0, nil).Times(3)
		}

		It("lists all six roots", func() {
			expectCounts(nil, 0)

			states, err := svc.Roots(ctx)

			Expect(err).NotTo(HaveOccurred())
			roots := make([]Root, len(states))
			for i, st := range states {
				roots[i] = st.Root
			}
			Expect(roots).To(ConsistOf(
				RootMovies,
				RootSeries,
				RootDownloads,
				RootMusic,
				RootEbooks,
				RootAudiobooks,
			))
		})

		It("counts the paths stored under each configured root", func() {
			expectCounts([]*ent.MediaFile{
				{ID: 1, Path: filepath.Join(root, "Dune (2021)/d.mkv")},
			}, 1)

			states, err := svc.Roots(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(states).To(HaveLen(6))
			Expect(states[0].Root).To(Equal(RootMovies))
			Expect(states[0].Path).To(Equal(root))
			Expect(states[0].Tracked).To(Equal(1))
			Expect(states[0].Total).To(Equal(1))
		})

		It("separates a drifted root from one that is simply empty", func() {
			// A file exists, but under a prefix the config no longer names.
			expectCounts([]*ent.MediaFile{
				{ID: 1, Path: "/somewhere/else/Dune (2021)/d.mkv"},
			}, 1)

			states, err := svc.Roots(ctx)
			Expect(err).NotTo(HaveOccurred())
			// Movies drifted: nothing under the root, but files exist.
			Expect(states[0].Tracked).To(BeZero())
			Expect(states[0].Total).To(Equal(1))
			// Downloads are merely idle — both counts zero, no drift.
			Expect(states[2].Root).To(Equal(RootDownloads))
			Expect(states[2].Tracked).To(BeZero())
			Expect(states[2].Total).To(BeZero())
		})
	})

	Describe("WarnOnDrift", func() {
		// expectDrift stubs Roots so only the named root drifts: rows exist
		// for it, none of them under the configured path.
		expectDrift := func(drifted Root) {
			GinkgoHelper()
			movieTotal, downloadTotal, musicTotal := 0, 0, 0
			switch drifted {
			case RootMovies:
				movieTotal = 2
			case RootMusic:
				musicTotal = 2
			default:
				downloadTotal = 2
			}
			store.EXPECT().CountRootFiles(mock.Anything, db.RootMusic).
				Return(musicTotal, nil).Once()
			store.EXPECT().CountRootFiles(mock.Anything, db.RootEbooks).
				Return(0, nil).Once()
			store.EXPECT().CountRootFiles(mock.Anything, db.RootAudiobooks).
				Return(0, nil).Once()
			store.EXPECT().
				ListMediaFilesByPathPrefix(mock.Anything, mock.Anything).
				Return(nil, nil).Times(5)
			store.EXPECT().CountMovieMediaFiles(mock.Anything).
				Return(movieTotal, nil).Once()
			store.EXPECT().CountEpisodeMediaFiles(mock.Anything).
				Return(0, nil).Once()
			store.EXPECT().
				ListDownloadRecordsByPathPrefix(mock.Anything, mock.Anything).
				Return(nil, nil).Once()
			store.EXPECT().
				ListTorrentSessionsByPathPrefix(mock.Anything, mock.Anything).
				Return(nil, nil).Once()
			store.EXPECT().CountLiveDownloadRecords(mock.Anything).
				Return(downloadTotal, nil).Once()
			store.EXPECT().CountTorrentSessions(mock.Anything).
				Return(0, nil).Once()
		}

		DescribeTable("warns about a drifted media root without promising a prune",
			func(drifted Root) {
				expectDrift(drifted)

				var buf bytes.Buffer
				GinkgoWriter.TeeTo(&buf)
				DeferCleanup(GinkgoWriter.ClearTeeWriters)

				svc.WarnOnDrift(ctx)
				Expect(
					buf.String(),
				).To(ContainSubstring("library.root=" + string(drifted)))
				Expect(buf.String()).To(ContainSubstring("held offline"))
				Expect(buf.String()).NotTo(ContainSubstring("pruned"))
			},
			Entry("movies", RootMovies),
			Entry("music", RootMusic),
		)

		// Download rows are never pruned on drift — hygiene walks media files
		// only — so the media-root message would be a false alarm here.
		It("warns about imports, not pruning, for a drifted download root", func() {
			expectDrift(RootDownloads)

			var buf bytes.Buffer
			GinkgoWriter.TeeTo(&buf)
			DeferCleanup(GinkgoWriter.ClearTeeWriters)

			svc.WarnOnDrift(ctx)
			Expect(buf.String()).To(ContainSubstring("downloads cannot import"))
			Expect(buf.String()).NotTo(ContainSubstring("held offline"))
		})
	})

	Describe("Preview", func() {
		It("rewrites every path under the root onto the new one", func() {
			files := []*ent.MediaFile{
				{ID: 1, Path: touch(filepath.Join(dest, "Dune (2021)/d.mkv"))},
				{ID: 2, Path: touch(filepath.Join(dest, "Heat (1995)/h.mkv"))},
			}
			// Rows still carry the old root; the files are already at the new one.
			files[0].Path = filepath.Join(root, "Dune (2021)/d.mkv")
			files[1].Path = filepath.Join(root, "Heat (1995)/h.mkv")
			store.EXPECT().ListMediaFilesByPathPrefix(mock.Anything, root).
				Return(files, nil).Once()

			p, err := svc.Preview(ctx, Params{Root: RootMovies, To: dest})
			Expect(err).NotTo(HaveOccurred())
			Expect(p.Total).To(Equal(2))
			Expect(p.Skipped).To(BeZero())
			Expect(p.CanMove).To(BeTrue())
			Expect(p.Samples).To(ConsistOf(
				Rewrite{
					From: filepath.Join(root, "Dune (2021)/d.mkv"),
					To:   filepath.Join(dest, "Dune (2021)/d.mkv"),
				},
				Rewrite{
					From: filepath.Join(root, "Heat (1995)/h.mkv"),
					To:   filepath.Join(dest, "Heat (1995)/h.mkv"),
				},
			))
		})

		It("counts rows whose file is not at the destination as skipped", func() {
			files := []*ent.MediaFile{
				{ID: 1, Path: filepath.Join(root, "Gone (2001)/g.mkv")},
			}
			store.EXPECT().ListMediaFilesByPathPrefix(mock.Anything, root).
				Return(files, nil).Once()

			p, err := svc.Preview(ctx, Params{Root: RootMovies, To: dest})
			Expect(err).NotTo(HaveOccurred())
			Expect(p.Total).To(Equal(1))
			Expect(p.Skipped).To(Equal(1))
		})

		It("ignores sibling roots that merely share a prefix", func() {
			files := []*ent.MediaFile{
				{ID: 1, Path: touch(filepath.Join(dest, "Dune (2021)/d.mkv"))},
				{ID: 2, Path: root + "2/Other (2020)/o.mkv"},
			}
			files[0].Path = filepath.Join(root, "Dune (2021)/d.mkv")
			store.EXPECT().ListMediaFilesByPathPrefix(mock.Anything, root).
				Return(files, nil).Once()

			p, err := svc.Preview(ctx, Params{Root: RootMovies, To: dest})
			Expect(err).NotTo(HaveOccurred())
			Expect(p.Total).To(Equal(1))
		})

		It("rejects a relative destination", func() {
			_, err := svc.Preview(ctx, Params{Root: RootMovies, To: "movies"})
			Expect(err).To(MatchError(ErrInvalidPath))
		})

		It("rejects a destination equal to the current root", func() {
			_, err := svc.Preview(ctx, Params{Root: RootMovies, To: root})
			Expect(err).To(MatchError(ErrInvalidPath))
		})

		It("rejects an unknown root", func() {
			_, err := svc.Preview(ctx, Params{Root: "photos", To: dest})
			Expect(err).To(MatchError(ErrUnknownRoot))
		})

		DescribeTable("only re-points the music and book roots",
			func(r Root) {
				_, err := svc.Preview(
					ctx,
					Params{Root: r, To: dest, MoveFiles: true},
				)
				Expect(err).To(MatchError(ErrMoveUnsupported))

				store.EXPECT().
					ListMediaFilesByPathPrefix(mock.Anything, mock.Anything).
					Return(nil, nil).
					Once()
				p, err := svc.Preview(ctx, Params{Root: r, To: dest})
				Expect(err).NotTo(HaveOccurred())
				Expect(p.CanMove).To(BeFalse())
			},
			Entry("music", RootMusic),
			Entry("ebooks", RootEbooks),
			Entry("audiobooks", RootAudiobooks),
		)

		It("refuses to move files for the download root", func() {
			_, err := svc.Preview(ctx, Params{
				Root: RootDownloads, To: dest, MoveFiles: true,
			})
			Expect(err).To(MatchError(ErrMoveUnsupported))
		})
	})

	Describe("Start", func() {
		It("re-points rows whose files are already at the destination", func() {
			touch(filepath.Join(dest, "Dune (2021)/d.mkv"))
			files := []*ent.MediaFile{
				{ID: 1, Path: filepath.Join(root, "Dune (2021)/d.mkv")},
			}
			store.EXPECT().ListMediaFilesByPathPrefix(mock.Anything, root).
				Return(files, nil).Once()
			store.EXPECT().UpdateMediaFilePath(
				mock.Anything,
				uint32(1),
				filepath.Join(dest, "Dune (2021)/d.mkv"),
			).Return(nil).Once()

			started, err := svc.Start(ctx, Params{Root: RootMovies, To: dest})
			Expect(err).NotTo(HaveOccurred())
			Expect(started.Total).To(Equal(1))
			Expect(started.From).To(Equal(root))
			Expect(started.To).To(Equal(dest))

			Eventually(func() bool { return svc.Status().Running }).
				Should(BeFalse())
			status := svc.Status()
			Expect(status.Error).To(BeEmpty())
			Expect(status.Done).To(Equal(1))
			Expect(status.Skipped).To(BeZero())
			Expect(config.Get().Library.MoviePath).To(Equal(dest))
		})

		It("carries the configured root along when a parent is migrated", func() {
			// /media/movies under /media → /data leaves the root at
			// /data/movies, not at /data.
			parent := filepath.Dir(root)
			dest := filepath.Join(GinkgoT().TempDir(), "data")
			moved := filepath.Join(dest, filepath.Base(root))
			touch(filepath.Join(moved, "Dune (2021)/d.mkv"))
			store.EXPECT().ListMediaFilesByPathPrefix(mock.Anything, parent).
				Return([]*ent.MediaFile{
					{ID: 1, Path: filepath.Join(root, "Dune (2021)/d.mkv")},
				}, nil).Once()
			store.EXPECT().UpdateMediaFilePath(
				mock.Anything, uint32(1), filepath.Join(moved, "Dune (2021)/d.mkv"),
			).Return(nil).Once()

			_, err := svc.Start(ctx, Params{
				Root: RootMovies, From: parent, To: dest,
			})
			Expect(err).NotTo(HaveOccurred())

			Eventually(func() bool { return svc.Status().Running }).
				Should(BeFalse())
			Expect(svc.Status().Error).To(BeEmpty())
			Expect(config.Get().Library.MoviePath).To(Equal(moved))
		})

		It("leaves the configured root alone when a subtree is migrated", func() {
			sub := filepath.Join(root, "4K")
			dest := filepath.Join(GinkgoT().TempDir(), "4k")
			touch(filepath.Join(dest, "Dune (2021)/d.mkv"))
			store.EXPECT().ListMediaFilesByPathPrefix(mock.Anything, sub).
				Return([]*ent.MediaFile{
					{ID: 1, Path: filepath.Join(sub, "Dune (2021)/d.mkv")},
				}, nil).Once()
			store.EXPECT().UpdateMediaFilePath(
				mock.Anything, uint32(1), filepath.Join(dest, "Dune (2021)/d.mkv"),
			).Return(nil).Once()

			_, err := svc.Start(ctx, Params{
				Root: RootMovies, From: sub, To: dest,
			})
			Expect(err).NotTo(HaveOccurred())

			Eventually(func() bool { return svc.Status().Running }).
				Should(BeFalse())
			Expect(svc.Status().Error).To(BeEmpty())
			Expect(config.Get().Library.MoviePath).To(Equal(root))
		})

		It("leaves rows alone when the file is missing", func() {
			files := []*ent.MediaFile{
				{ID: 1, Path: filepath.Join(root, "Gone (2001)/g.mkv")},
			}
			store.EXPECT().ListMediaFilesByPathPrefix(mock.Anything, root).
				Return(files, nil).Once()

			_, err := svc.Start(ctx, Params{Root: RootMovies, To: dest})
			Expect(err).NotTo(HaveOccurred())

			Eventually(func() bool { return svc.Status().Running }).
				Should(BeFalse())
			status := svc.Status()
			Expect(status.Done).To(BeZero())
			Expect(status.Skipped).To(Equal(1))
		})

		It("moves the file when asked to", func() {
			src := touch(filepath.Join(root, "Dune (2021)/d.mkv"))
			target := filepath.Join(dest, "Dune (2021)/d.mkv")
			store.EXPECT().ListMediaFilesByPathPrefix(mock.Anything, root).
				Return([]*ent.MediaFile{{ID: 1, Path: src}}, nil).Once()
			store.EXPECT().
				UpdateMediaFilePath(mock.Anything, uint32(1), target).
				Return(nil).Once()

			_, err := svc.Start(ctx, Params{
				Root: RootMovies, To: dest, MoveFiles: true,
			})
			Expect(err).NotTo(HaveOccurred())

			Eventually(func() bool { return svc.Status().Running }).
				Should(BeFalse())
			Expect(svc.Status().Error).To(BeEmpty())
			Expect(target).To(BeAnExistingFile())
			Expect(src).NotTo(BeAnExistingFile())
		})

		It("re-points download records and torrent sessions together", func() {
			downloads := filepath.Join(GinkgoT().TempDir(), "downloads")
			newDownloads := filepath.Join(GinkgoT().TempDir(), "torrents")
			configtest.SetupFile(map[string]any{
				"library": map[string]any{"download_path": downloads},
			})
			touch(filepath.Join(newDownloads, "Dune.2021/d.mkv"))
			store.EXPECT().
				ListDownloadRecordsByPathPrefix(mock.Anything, downloads).
				Return([]*ent.DownloadRecord{
					{ID: 7, SavePath: filepath.Join(downloads, "Dune.2021")},
				}, nil).Once()
			store.EXPECT().
				ListTorrentSessionsByPathPrefix(mock.Anything, downloads).
				Return([]*ent.TorrentSession{
					{
						InfoHash: "abc",
						SavePath: filepath.Join(downloads, "Dune.2021"),
					},
				}, nil).Once()
			store.EXPECT().SetDownloadRecordSavePath(
				mock.Anything, uint32(7), filepath.Join(newDownloads, "Dune.2021"),
			).Return(nil).Once()
			store.EXPECT().SetTorrentSessionSavePath(
				mock.Anything, "abc", filepath.Join(newDownloads, "Dune.2021"),
			).Return(nil).Once()

			_, err := svc.Start(ctx, Params{
				Root: RootDownloads, To: newDownloads,
			})
			Expect(err).NotTo(HaveOccurred())

			Eventually(func() bool { return svc.Status().Running }).
				Should(BeFalse())
			status := svc.Status()
			Expect(status.Error).To(BeEmpty())
			Expect(status.Done).To(Equal(2))
		})

		It("rejects a second migration while one is running", func() {
			touch(filepath.Join(dest, "Dune (2021)/d.mkv"))
			files := []*ent.MediaFile{
				{ID: 1, Path: filepath.Join(root, "Dune (2021)/d.mkv")},
			}
			store.EXPECT().ListMediaFilesByPathPrefix(mock.Anything, root).
				Return(files, nil).Twice()
			release := make(chan struct{})
			store.EXPECT().
				UpdateMediaFilePath(mock.Anything, uint32(1), mock.Anything).
				RunAndReturn(func(context.Context, uint32, string) error {
					<-release
					return nil
				}).Once()

			_, err := svc.Start(ctx, Params{Root: RootMovies, To: dest})
			Expect(err).NotTo(HaveOccurred())
			Eventually(func() bool { return svc.Status().Running }).Should(BeTrue())

			_, err = svc.Start(ctx, Params{Root: RootMovies, To: dest})
			Expect(err).To(MatchError(ErrMigrationRunning))

			close(release)
			Eventually(func() bool { return svc.Status().Running }).Should(BeFalse())
		})

		It("stops and reports the error when a move fails", func() {
			src := filepath.Join(root, "Dune (2021)/d.mkv")
			touch(src)
			// A destination directory that is really a file makes MkdirAll fail.
			touch(filepath.Join(dest, "Dune (2021)"))
			store.EXPECT().ListMediaFilesByPathPrefix(mock.Anything, root).
				Return([]*ent.MediaFile{{ID: 1, Path: src}}, nil).Once()

			_, err := svc.Start(ctx, Params{
				Root: RootMovies, To: dest, MoveFiles: true,
			})
			Expect(err).NotTo(HaveOccurred())

			Eventually(func() bool { return svc.Status().Running }).
				Should(BeFalse())
			Expect(svc.Status().Done).To(BeZero())
			Expect(svc.Status().Error).To(ContainSubstring("move"))
			Expect(config.Get().Library.MoviePath).To(Equal(root))
		})
	})
})

var _ = Describe("Service on real rows", Label("integration", "library"), func() {
	DescribeTable("re-points a music or book root and brings it back online",
		func(
			r Root,
			dbRoot db.LibraryRoot,
			key string,
			configured func(config.LibraryConfig) string,
			own func(client *ent.Client, c *ent.MediaFileCreate) *ent.MediaFileCreate,
		) {
			ctx := context.Background()
			tmp := GinkgoT().TempDir()
			from := filepath.Join(tmp, "old")
			to := filepath.Join(tmp, "new")
			Expect(os.MkdirAll(from, 0o755)).To(Succeed())
			Expect(os.MkdirAll(to, 0o755)).To(Succeed())
			Expect(
				os.WriteFile(filepath.Join(to, "file"), []byte("x"), 0o644),
			).To(Succeed())
			configtest.SetupFile(
				map[string]any{"library": map[string]any{key: from}},
			)
			client := dbtest.SetupTestDB(ctx)
			DeferCleanup(client.Close)
			store := db.New(client)
			roothealth.Register(store)
			DeferCleanup(func() { roothealth.Register(nil) })
			row := own(client, client.MediaFile.Create().
				SetPath(filepath.Join(from, "file")).SetSize(1)).SaveX(ctx)
			Expect(roothealth.Offline(ctx, dbRoot)).NotTo(BeNil())

			svc := NewService(store)
			_, err := svc.Start(ctx, Params{Root: r, To: to})
			Expect(err).NotTo(HaveOccurred())
			Eventually(func() bool { return svc.Status().Running }).Should(BeFalse())

			Expect(svc.Status().Error).To(BeEmpty())
			Expect(
				client.MediaFile.GetX(ctx, row.ID).Path,
			).To(Equal(filepath.Join(to, "file")))
			Expect(configured(config.Get().Library)).To(Equal(to))
			Expect(roothealth.Offline(ctx, dbRoot)).To(BeNil())
		},
		Entry("music", RootMusic, db.RootMusic, "music_path",
			func(l config.LibraryConfig) string { return l.MusicPath },
			func(client *ent.Client, c *ent.MediaFileCreate) *ent.MediaFileCreate {
				ctx := context.Background()
				artist := client.Artist.Create().SetMbid("a").SetName("A").SaveX(ctx)
				album := client.Album.Create().
					SetMbid("b").
					SetTitle("B").
					SetArtist(artist).
					SaveX(ctx)
				return c.SetTrack(client.Track.Create().
					SetTitle("T").
					SetDisc(1).SetPosition(1).SetAlbum(album).SaveX(ctx))
			}),
		Entry("ebooks", RootEbooks, db.RootEbooks, "ebook_path",
			func(l config.LibraryConfig) string { return l.EbookPath },
			func(client *ent.Client, c *ent.MediaFileCreate) *ent.MediaFileCreate {
				book := client.Book.Create().
					SetHardcoverID(1).
					SetTitle("B").
					SaveX(context.Background())
				return c.SetBook(book).SetBookKind(entmediafile.BookKindEbook)
			}),
		Entry("audiobooks", RootAudiobooks, db.RootAudiobooks, "audiobook_path",
			func(l config.LibraryConfig) string { return l.AudiobookPath },
			func(client *ent.Client, c *ent.MediaFileCreate) *ent.MediaFileCreate {
				book := client.Book.Create().
					SetHardcoverID(1).
					SetTitle("B").
					SaveX(context.Background())
				return c.SetBook(book).SetBookKind(entmediafile.BookKindAudiobook)
			}),
	)
})
