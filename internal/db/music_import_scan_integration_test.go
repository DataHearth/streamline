package db

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/album"
	entimportscan "github.com/datahearth/streamline/ent/importscan"
	entimportscanalbum "github.com/datahearth/streamline/ent/importscanalbum"
	"github.com/datahearth/streamline/ent/mediafile"
	"github.com/datahearth/streamline/ent/schema"
)

var _ = Describe("Music import scan store", Label("integration", "db"), func() {
	var (
		ctx    context.Context
		client *ent.Client
		store  *DB
	)

	BeforeEach(func() {
		ctx = context.Background()
		var err error
		client, err = Open(ctx, ":memory:")
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { client.Close() })
		store = New(client)
	})

	newScan := func() *ent.ImportScan {
		GinkgoHelper()
		scan, err := store.CreateImportScan(ctx, CreateImportScanParams{
			SourcePath: "/music",
			Mode:       entimportscan.ModeInPlace,
			Kind:       entimportscan.KindMusic,
		})
		Expect(err).NotTo(HaveOccurred())
		return scan
	}

	seedAlbum := func() *ent.Album {
		GinkgoHelper()
		_, err := store.CreateArtist(ctx, CreateArtistParams{
			MBID: "a-1", Name: "Nirvana", Monitored: false,
			Albums: []AlbumSeed{{
				MBID: "rg-1", Title: "Nevermind", Type: "album",
				Tracks: []TrackSeed{
					{MBID: "t-2", Title: "In Bloom", Disc: 1, Position: 2},
					{
						MBID:     "t-1",
						Title:    "Smells Like Teen Spirit",
						Disc:     1,
						Position: 1,
					},
				},
			}},
		})
		Expect(err).NotTo(HaveOccurred())
		a, err := store.FindAlbumByMBID(ctx, "rg-1")
		Expect(err).NotTo(HaveOccurred())
		Expect(a).NotTo(BeNil())
		return a
	}

	Describe("import_scan_album rows", func() {
		It(
			"creates rows, lists pending folders, filters the commit list and records outcomes",
			func() {
				scan := newScan()
				Expect(store.UpdateImportScanStatus(
					ctx,
					scan.ID,
					entimportscan.StatusAwaitingReview,
					UpdateScanStatusOpts{},
				)).To(Succeed())
				existing := uint32(9)
				Expect(
					store.BulkCreateImportScanAlbums(
						ctx,
						scan.ID,
						[]CreateImportScanAlbumParams{
							{
								FolderPath:       "/music/a",
								TaggedArtist:     "Nirvana",
								TaggedAlbum:      "Nevermind",
								Classification:   entimportscanalbum.ClassificationConfirmed,
								ReleaseGroupMBID: "rg-1",
								ArtistMBID:       "a-1",
								FileCount:        12,
								Candidates: []schema.ScannedAlbumCandidate{
									{ReleaseGroupMBID: "rg-1"},
								},
							},
							{
								FolderPath:      "/music/b",
								Classification:  entimportscanalbum.ClassificationExisting,
								ExistingAlbumID: &existing,
							},
							{
								FolderPath:     "/music/c",
								Classification: entimportscanalbum.ClassificationAmbiguous,
							},
							{
								FolderPath:     "/music/d",
								Classification: entimportscanalbum.ClassificationUnmatched,
							},
						},
					),
				).To(Succeed())

				folders, err := store.ListPendingImportScanAlbumFolders(ctx)
				Expect(err).NotTo(HaveOccurred())
				Expect(
					folders,
				).To(ConsistOf("/music/a", "/music/b", "/music/c", "/music/d"))

				rows, err := store.ListImportScanAlbumsForCommit(ctx, scan.ID)
				Expect(err).NotTo(HaveOccurred())
				Expect(rows).To(HaveLen(2))

				all, err := client.ImportScanAlbum.Query().All(ctx)
				Expect(err).NotTo(HaveOccurred())
				var ambiguous, unmatched uint32
				for _, r := range all {
					switch r.FolderPath {
					case "/music/c":
						ambiguous = r.ID
					case "/music/d":
						unmatched = r.ID
					}
				}

				n, err := store.BulkUpdateImportScanAlbumDecisions(
					ctx, scan.ID, entimportscanalbum.DecisionAccept,
					entimportscanalbum.ClassificationAmbiguous, nil,
				)
				Expect(err).NotTo(HaveOccurred())
				Expect(n).To(Equal(1))
				n, err = store.BulkUpdateImportScanAlbumDecisions(
					ctx,
					scan.ID,
					entimportscanalbum.DecisionSkip,
					"",
					[]uint32{unmatched},
				)
				Expect(err).NotTo(HaveOccurred())
				Expect(n).To(Equal(1))

				rows, err = store.ListImportScanAlbumsForCommit(ctx, scan.ID)
				Expect(err).NotTo(HaveOccurred())
				Expect(rows).To(HaveLen(3))

				Expect(store.UpdateImportScanAlbumOutcome(
					ctx,
					ambiguous,
					entimportscanalbum.OutcomeCreated,
					UpdateScanAlbumOutcomeOpts{
						Message:        "1 files unmatched",
						CreatedAlbumID: 4,
					},
				)).To(Succeed())
				got, err := client.ImportScanAlbum.Get(ctx, ambiguous)
				Expect(err).NotTo(HaveOccurred())
				Expect(got.Outcome).To(Equal(entimportscanalbum.OutcomeCreated))
				Expect(got.OutcomeMessage).To(Equal("1 files unmatched"))
				Expect(got.CreatedAlbumID).To(HaveValue(Equal(uint32(4))))
			},
		)

		It(
			"excludes a scan no longer awaiting review from the pending folders",
			func() {
				scan := newScan()
				Expect(
					store.BulkCreateImportScanAlbums(
						ctx,
						scan.ID,
						[]CreateImportScanAlbumParams{
							{
								FolderPath:     "/music/a",
								Classification: entimportscanalbum.ClassificationUnmatched,
							},
						},
					),
				).To(Succeed())
				folders, err := store.ListPendingImportScanAlbumFolders(ctx)
				Expect(err).NotTo(HaveOccurred())
				Expect(folders).To(BeEmpty())
			},
		)
	})

	Describe("album lookup", func() {
		It("indexes albums by release-group mbid and orders tracks", func() {
			a := seedAlbum()
			idx, err := store.AlbumMBIDIndex(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(idx).To(HaveKeyWithValue("rg-1", a.ID))
			Expect(a.Edges.Tracks).To(HaveLen(2))
			Expect(a.Edges.Tracks[0].Position).To(Equal(uint16(1)))
		})

		It("returns nil, nil on a miss", func() {
			a, err := store.FindAlbumByMBID(ctx, "nope")
			Expect(err).NotTo(HaveOccurred())
			Expect(a).To(BeNil())
		})
	})

	Describe("AdoptAlbumFiles", func() {
		It("marks the album available only once every track has a file", func() {
			a := seedAlbum()
			t1, t2 := a.Edges.Tracks[0].ID, a.Edges.Tracks[1].ID

			Expect(store.AdoptAlbumFiles(ctx, a.ID, []AdoptAlbumFile{
				{
					TrackID: t1,
					Path:    "/music/01.flac",
					Quality: "flac",
					Format:  "flac",
					Size:    10,
				},
			})).To(Succeed())
			got, err := client.Album.Get(ctx, a.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(got.Monitored).To(BeTrue())
			Expect(got.Status).To(Equal(album.StatusWanted))

			Expect(store.AdoptAlbumFiles(ctx, a.ID, []AdoptAlbumFile{
				{
					TrackID: t2,
					Path:    "/music/02.flac",
					Quality: "flac",
					Format:  "flac",
					Size:    20,
				},
			})).To(Succeed())
			got, err = client.Album.Get(ctx, a.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(got.Status).To(Equal(album.StatusAvailable))

			files, err := client.MediaFile.Query().All(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(files).To(HaveLen(2))
			for _, f := range files {
				Expect(f.Source).To(Equal(mediafile.SourceOrphan))
			}
		})

		It("never moves an album out of downloading", func() {
			a := seedAlbum()
			Expect(client.Album.UpdateOneID(a.ID).
				SetStatus(album.StatusDownloading).Exec(ctx)).To(Succeed())

			Expect(store.AdoptAlbumFiles(ctx, a.ID, []AdoptAlbumFile{
				{
					TrackID: a.Edges.Tracks[0].ID,
					Path:    "/music/01.flac",
					Format:  "flac",
					Size:    1,
				},
				{
					TrackID: a.Edges.Tracks[1].ID,
					Path:    "/music/02.flac",
					Format:  "flac",
					Size:    1,
				},
			})).To(Succeed())

			got, err := client.Album.Get(ctx, a.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(got.Status).To(Equal(album.StatusDownloading))
		})

		It("rolls everything back when one file fails", func() {
			a := seedAlbum()
			err := store.AdoptAlbumFiles(ctx, a.ID, []AdoptAlbumFile{
				{
					TrackID: a.Edges.Tracks[0].ID,
					Path:    "/music/01.flac",
					Format:  "flac",
					Size:    10,
				},
				{TrackID: 9999, Path: "/music/02.flac", Format: "flac", Size: 10},
			})
			Expect(err).To(HaveOccurred())

			n, err := client.MediaFile.Query().Count(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(n).To(BeZero())
			got, err := client.Album.Get(ctx, a.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(got.Monitored).To(BeFalse())
			Expect(got.Status).To(Equal(album.StatusWanted))
		})
	})
})
