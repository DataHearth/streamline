package bulkimport

import (
	"context"
	"errors"
	"path/filepath"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stretchr/testify/mock"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/album"
	entimportscan "github.com/datahearth/streamline/ent/importscan"
	entimportscanalbum "github.com/datahearth/streamline/ent/importscanalbum"
	"github.com/datahearth/streamline/ent/mediafile"
	"github.com/datahearth/streamline/internal/db"
	"github.com/datahearth/streamline/internal/media/music"
	"github.com/datahearth/streamline/internal/metadata"
	metamocks "github.com/datahearth/streamline/internal/metadata/mocks"
	postersmocks "github.com/datahearth/streamline/internal/posters/mocks"
	"github.com/datahearth/streamline/internal/testutil/configtest"
	"github.com/datahearth/streamline/internal/testutil/dbtest"
)

var _ = Describe(
	"Music commit (adopt album)",
	Label("integration", "bulkimport"),
	func() {
		var (
			ctx    context.Context
			root   string
			client *ent.Client
			store  db.Store
			mb     *metamocks.MockMusicProvider
			svc    *Service
		)

		BeforeEach(func() {
			ctx = context.Background()
			root = GinkgoT().TempDir()
			configtest.Setup(map[string]any{
				"library": map[string]any{"music_path": root},
			})
			client = dbtest.SetupTestDB(ctx)
			DeferCleanup(client.Close)
			store = db.New(client)
			mb = metamocks.NewMockMusicProvider(GinkgoT())
			covers := postersmocks.NewMockManager(GinkgoT())
			covers.EXPECT().Path("albums", mock.Anything).
				Return(filepath.Join(root, "no-cover")).Maybe()
			covers.EXPECT().
				Put(mock.Anything, "albums", mock.Anything, mock.Anything).
				Return(nil).
				Maybe()
			covers.EXPECT().
				Fetch(mock.Anything, "albums", mock.Anything, mock.Anything).
				Return(nil).Maybe()
			svc = NewService(
				store, nil, nil, nil, nil, nil, nil, root, root,
				mb, music.NewService(store, mb, covers, nil, nil, nil, nil, nil),
				nil,
				nil,
			)
		})

		expectDiscography := func(artistMBID string, rgs map[string][]string) {
			GinkgoHelper()
			infos := make([]metadata.ReleaseGroupInfo, 0, len(rgs))
			for rg, titles := range rgs {
				infos = append(infos, metadata.ReleaseGroupInfo{
					MBID: rg, Title: rg, Type: metadata.AlbumTypeAlbum,
				})
				tracks := make([]metadata.TrackInfo, len(titles))
				for i, t := range titles {
					tracks[i] = metadata.TrackInfo{
						MBID: rg + t, Title: t, Disc: 1, Position: uint16(i + 1),
					}
				}
				// Maybe: the add queues every album for the background worker,
				// which races the commit's own hydration of the album it adopts.
				mb.EXPECT().GetReleaseGroup(mock.Anything, rg).
					Return(&metadata.ReleaseGroupDetails{
						MBID:            rg,
						Title:           rg,
						ReleaseMBID:     rg + "-rel",
						Tracks:          tracks,
						CreditsComplete: true,
					}, nil).Maybe()
			}
			mb.EXPECT().GetArtist(mock.Anything, artistMBID).
				Return(&metadata.ArtistDetails{
					MBID:          artistMBID,
					Name:          "Nirvana",
					ReleaseGroups: infos,
				}, nil).Once()
		}

		folder := func(name string, fixtures ...string) string {
			GinkgoHelper()
			dir := filepath.Join(root, name)
			for i, f := range fixtures {
				copyFixture(f, filepath.Join(dir, string(rune('a'+i))+".mp3"))
			}
			return dir
		}

		commit := func(rows ...db.CreateImportScanAlbumParams) *ent.ImportScan {
			GinkgoHelper()
			scan, err := store.CreateImportScan(ctx, db.CreateImportScanParams{
				SourcePath: root,
				Kind:       entimportscan.KindMusic,
				Mode:       entimportscan.ModeInPlace,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(store.UpdateImportScanStatus(
				ctx,
				scan.ID,
				entimportscan.StatusAwaitingReview,
				db.UpdateScanStatusOpts{},
			)).To(Succeed())
			Expect(
				store.BulkCreateImportScanAlbums(ctx, scan.ID, rows),
			).To(Succeed())
			svc.runCommitMusic(ctx, scan)
			scan, err = store.FindImportScan(ctx, scan.ID)
			Expect(err).NotTo(HaveOccurred())
			return scan
		}

		outcomeOf := func(folderPath string) *ent.ImportScanAlbum {
			GinkgoHelper()
			row, err := client.ImportScanAlbum.Query().
				Where(entimportscanalbum.FolderPathEQ(folderPath)).Only(ctx)
			Expect(err).NotTo(HaveOccurred())
			return row
		}

		confirmed := func(dir, rg, artist string) db.CreateImportScanAlbumParams {
			return db.CreateImportScanAlbumParams{
				FolderPath:       dir,
				Classification:   entimportscanalbum.ClassificationConfirmed,
				ReleaseGroupMBID: rg,
				ArtistMBID:       artist,
				FileCount:        1,
			}
		}

		It("adds a new artist unmonitored and adopts the files in place", func() {
			expectDiscography("a-1", map[string][]string{
				"rg-1": {"Smells Like Teen Spirit"},
				"rg-2": {"Other"},
			})
			dir := folder("Nirvana/Nevermind", "tagged.mp3")

			scan := commit(confirmed(dir, "rg-1", "a-1"))

			Expect(scan.Status).To(Equal(entimportscan.StatusCompleted))
			Expect(scan.CommitSuccessCount).To(Equal(uint32(1)))
			Expect(scan.CommitFailedCount).To(BeZero())

			alb, err := client.Album.Query().Where(album.Mbid("rg-1")).Only(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(alb.Monitored).To(BeTrue())
			Expect(alb.Status).To(Equal(album.StatusAvailable))
			other, err := client.Album.Query().Where(album.Mbid("rg-2")).Only(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(other.Monitored).To(BeFalse())
			Expect(other.Status).To(Equal(album.StatusWanted))

			files, err := client.MediaFile.Query().All(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(files).To(HaveLen(1))
			Expect(files[0].Path).To(Equal(filepath.Join(dir, "a.mp3")))
			Expect(files[0].Source).To(Equal(mediafile.SourceOrphan))
			Expect(files[0].Format).To(Equal("mp3"))

			row := outcomeOf(dir)
			Expect(row.Outcome).To(Equal(entimportscanalbum.OutcomeCreated))
			Expect(row.CreatedAlbumID).To(HaveValue(Equal(alb.ID)))
		})

		It("reuses an artist the library already holds", func() {
			held, err := store.CreateArtist(ctx, db.CreateArtistParams{
				MBID: "a-1",
				Name: "Nirvana",
				Albums: []db.AlbumSeed{
					{MBID: "rg-1", Title: "Nevermind", Type: "album"},
				},
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(
				store.SetAlbumHydration(
					ctx,
					held.Edges.Albums[0].ID,
					db.HydrationParams{
						Tracks: []db.TrackSeed{
							{
								MBID:     "t-1",
								Title:    "Smells Like Teen Spirit",
								Disc:     1,
								Position: 1,
							},
						},
					},
					time.Now(),
				),
			).To(Succeed())
			dir := folder("Nevermind", "tagged.mp3")

			scan := commit(confirmed(dir, "rg-1", "a-1"))

			Expect(scan.CommitSuccessCount).To(Equal(uint32(1)))
			n, err := client.Artist.Query().Count(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(n).To(Equal(1))
		})

		It("adds the artist once for several albums", func() {
			expectDiscography("a-1", map[string][]string{
				"rg-1": {"Smells Like Teen Spirit"},
				"rg-2": {"Smells Like Teen Spirit"},
			})

			scan := commit(
				confirmed(folder("one", "tagged.mp3"), "rg-1", "a-1"),
				confirmed(folder("two", "tagged.mp3"), "rg-2", "a-1"),
			)

			Expect(scan.CommitSuccessCount).To(Equal(uint32(2)))
		})

		It("reports files that match no track and leaves the album wanted", func() {
			expectDiscography("a-1", map[string][]string{
				"rg-1": {"Smells Like Teen Spirit", "In Bloom"},
			})
			dir := folder("Nevermind", "tagged.mp3", "untagged.mp3", "untagged.mp3")

			commit(confirmed(dir, "rg-1", "a-1"))

			row := outcomeOf(dir)
			Expect(row.Outcome).To(Equal(entimportscanalbum.OutcomeCreated))
			Expect(row.OutcomeMessage).To(ContainSubstring("2 files unmatched"))
			n, err := client.MediaFile.Query().Count(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(n).To(Equal(1))
			alb, err := client.Album.Query().Where(album.Mbid("rg-1")).Only(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(alb.Status).To(Equal(album.StatusWanted))
		})

		It(
			"adopts one file per track when two files resolve to the same track",
			func() {
				expectDiscography("a-1", map[string][]string{
					"rg-1": {"Smells Like Teen Spirit"},
				})
				dir := folder("Nevermind", "tagged.mp3", "tagged.mp3")

				commit(confirmed(dir, "rg-1", "a-1"))

				row := outcomeOf(dir)
				Expect(row.Outcome).To(Equal(entimportscanalbum.OutcomeCreated))
				Expect(row.OutcomeMessage).To(Equal("1 files unmatched"))
				n, err := client.MediaFile.Query().Count(ctx)
				Expect(err).NotTo(HaveOccurred())
				Expect(n).To(Equal(1))
				alb, err := client.Album.Query().Where(album.Mbid("rg-1")).Only(ctx)
				Expect(err).NotTo(HaveOccurred())
				Expect(alb.Status).To(Equal(album.StatusAvailable))
			},
		)

		It("counts a track with no file once, not once per missing file", func() {
			expectDiscography("a-1", map[string][]string{
				"rg-1": {"Smells Like Teen Spirit", "In Bloom"},
			})
			dir := folder("Nevermind", "tagged.mp3", "tagged.mp3")

			commit(confirmed(dir, "rg-1", "a-1"))

			Expect(outcomeOf(dir).OutcomeMessage).To(
				Equal("1 files unmatched; 1 of 2 tracks have no file"))
		})

		It("keeps one file per track when a copy is committed twice", func() {
			expectDiscography("a-1", map[string][]string{
				"rg-1": {"Smells Like Teen Spirit"},
			})
			dir := folder("Nevermind", "tagged.mp3", "tagged.mp3")
			commit(confirmed(dir, "rg-1", "a-1"))
			commit(confirmed(dir, "rg-1", "a-1"))

			n, err := client.MediaFile.Query().Count(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(n).To(Equal(1))
		})

		It("does not duplicate files on a second commit of the same folder", func() {
			expectDiscography("a-1", map[string][]string{
				"rg-1": {"Smells Like Teen Spirit"},
			})
			dir := folder("Nevermind", "tagged.mp3")
			commit(confirmed(dir, "rg-1", "a-1"))
			scan := commit(confirmed(dir, "rg-1", "a-1"))

			Expect(scan.CommitSuccessCount).To(Equal(uint32(1)))
			n, err := client.MediaFile.Query().Count(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(n).To(Equal(1))
		})

		It(
			"leaves skipped albums alone and fails only the album that errors",
			func() {
				expectDiscography("a-1", map[string][]string{
					"rg-1": {"Smells Like Teen Spirit"},
				})
				mb.EXPECT().GetArtist(mock.Anything, "a-bad").
					Return(nil, errors.New("musicbrainz 503")).Once()

				good := folder("good", "tagged.mp3")
				bad := folder("bad", "tagged.mp3")
				skipped := folder("skipped", "tagged.mp3")
				skippedRow := confirmed(skipped, "rg-1", "a-1")

				scan, err := store.CreateImportScan(ctx, db.CreateImportScanParams{
					SourcePath: root,
					Kind:       entimportscan.KindMusic,
					Mode:       entimportscan.ModeInPlace,
				})
				Expect(err).NotTo(HaveOccurred())
				Expect(
					store.BulkCreateImportScanAlbums(
						ctx,
						scan.ID,
						[]db.CreateImportScanAlbumParams{
							confirmed(bad, "rg-bad", "a-bad"),
							confirmed(good, "rg-1", "a-1"),
							skippedRow,
						},
					),
				).To(Succeed())
				_, err = store.BulkUpdateImportScanAlbumDecisions(
					ctx, scan.ID, entimportscanalbum.DecisionSkip, "",
					[]uint32{outcomeOf(skipped).ID},
				)
				Expect(err).NotTo(HaveOccurred())
				svc.runCommitMusic(ctx, scan)

				done, err := store.FindImportScan(ctx, scan.ID)
				Expect(err).NotTo(HaveOccurred())
				Expect(done.Status).To(Equal(entimportscan.StatusCompleted))
				Expect(done.CommitSuccessCount).To(Equal(uint32(1)))
				Expect(done.CommitFailedCount).To(Equal(uint32(1)))
				Expect(
					outcomeOf(bad).Outcome,
				).To(Equal(entimportscanalbum.OutcomeFailed))
				Expect(
					outcomeOf(bad).OutcomeMessage,
				).To(ContainSubstring("musicbrainz 503"))
				Expect(
					outcomeOf(good).Outcome,
				).To(Equal(entimportscanalbum.OutcomeCreated))
				Expect(
					outcomeOf(skipped).Outcome,
				).To(Equal(entimportscanalbum.OutcomePending))
			},
		)

		It(
			"fails an album whose release group is missing from the discography",
			func() {
				expectDiscography("a-1", map[string][]string{
					"rg-1": {"Smells Like Teen Spirit"},
				})
				dir := folder("Nevermind", "tagged.mp3")

				commit(confirmed(dir, "rg-9", "a-1"))

				row := outcomeOf(dir)
				Expect(row.Outcome).To(Equal(entimportscanalbum.OutcomeFailed))
				Expect(
					row.OutcomeMessage,
				).To(ContainSubstring("not in the artist's discography"))
			},
		)
	},
)
