package db

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/album"
	"github.com/datahearth/streamline/ent/downloadrecord"
	"github.com/datahearth/streamline/ent/track"
)

var _ = Describe("Discography pack records", Label("integration", "db"), func() {
	var (
		ctx    context.Context
		client *ent.Client
		store  *DB
		a      *ent.Artist
		albums []*ent.Album
	)

	BeforeEach(func() {
		ctx = context.Background()
		var err error
		client, err = Open(ctx, ":memory:")
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { client.Close() })
		store = New(client)

		a, err = store.CreateArtist(ctx, CreateArtistParams{
			MBID: "a-1", Name: "Nirvana",
			Albums: []AlbumSeed{
				{MBID: "rg-1", Title: "Bleach", Monitored: true},
				{MBID: "rg-2", Title: "Nevermind", Monitored: true},
			},
		})
		Expect(err).NotTo(HaveOccurred())
		hydrateAlbum(ctx, store, "rg-1",
			TrackSeed{MBID: "t-1", Title: "Blew", Disc: 1, Position: 1})
		hydrateAlbum(ctx, store, "rg-2",
			TrackSeed{MBID: "t-2", Title: "Breed", Disc: 1, Position: 1})
		albums = client.Album.Query().Order(ent.Asc(album.FieldID)).AllX(ctx)
		client.Album.Update().SetStatus(album.StatusDownloading).ExecX(ctx)
	})

	createPack := func() *ent.DownloadRecord {
		GinkgoHelper()
		rec, err := store.CreateDownloadRecord(ctx, CreateDownloadRecordParams{
			Title: "Nirvana - Discography", Size: 1, TorrentHash: "pack-hash",
			Status:   downloadrecord.StatusImporting,
			ArtistID: a.ID, AlbumIDs: []uint32{albums[0].ID, albums[1].ID},
		})
		Expect(err).NotTo(HaveOccurred())
		return rec
	}

	It("files the record under the artist and links the albums", func() {
		rec := createPack()
		Expect(rec.QueryArtist().OnlyIDX(ctx)).To(Equal(a.ID))
		Expect(rec.QueryAlbums().CountX(ctx)).To(Equal(2))
		Expect(rec.QueryAlbum().ExistX(ctx)).To(BeFalse())
	})

	It(
		"hands the importer the artist and the linked albums with their tracks",
		func() {
			rec := createPack()
			got, err := store.FindImportingDownloadRecordByID(ctx, rec.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(got.Edges.Artist).NotTo(BeNil())
			Expect(got.Edges.Albums).To(HaveLen(2))
			Expect(got.Edges.Albums[0].Edges.Tracks).To(HaveLen(1))
			Expect(got.Edges.Albums[0].Edges.Artist).NotTo(BeNil())
		},
	)

	It("lists the live pack with its album ids for the queue", func() {
		createPack()
		rows, err := store.ListActiveDownloadRecords(ctx)
		Expect(err).NotTo(HaveOccurred())
		Expect(rows).To(HaveLen(1))
		Expect(rows[0].Edges.Artist).NotTo(BeNil())
		Expect(rows[0].Edges.Albums).To(HaveLen(2))
	})

	It("keeps the record while one album of the pack is recorded", func() {
		rec := createPack()
		Expect(store.RecordAlbumImportSuccess(ctx, RecordAlbumImportSuccessParams{
			RecordID: rec.ID, AlbumID: albums[0].ID, KeepRecord: true,
			Files: []AdoptAlbumFile{
				{
					TrackID: client.Track.Query().
						Where(track.MbidEQ("t-1")).
						OnlyIDX(ctx),
					Path:    "/m/blew.flac",
					Size:    10,
					Quality: "lossless",
					Format:  "flac",
				},
			},
		})).To(Succeed())
		Expect(client.DownloadRecord.GetX(ctx, rec.ID).Status).
			To(Equal(downloadrecord.StatusImporting))
		Expect(client.Album.GetX(ctx, albums[0].ID).Status).
			To(Equal(album.StatusAvailable))
		Expect(client.Album.GetX(ctx, albums[1].ID).Status).
			To(Equal(album.StatusDownloading))
	})

	It("completes a single album's record unless told to keep it", func() {
		rec, err := store.CreateDownloadRecord(ctx, CreateDownloadRecordParams{
			Title: "Bleach", Size: 1, Status: downloadrecord.StatusImporting,
			AlbumID: albums[0].ID,
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(store.RecordAlbumImportSuccess(ctx, RecordAlbumImportSuccessParams{
			RecordID: rec.ID, AlbumID: albums[0].ID,
		})).To(Succeed())
		Expect(client.DownloadRecord.GetX(ctx, rec.ID).Status).
			To(Equal(downloadrecord.StatusCompleted))
	})

	It(
		"returns every linked album without a file to wanted on a terminal failure",
		func() {
			rec := createPack()
			client.MediaFile.Create().SetPath("/m/blew.flac").SetSize(1).
				SetTrackID(client.Track.Query().Where(track.MbidEQ("t-1")).OnlyIDX(ctx)).
				ExecX(ctx)

			Expect(store.RecordImportFailure(ctx, RecordImportFailureParams{
				RecordID: rec.ID, Terminal: true, Reason: "boom", Attempts: 3,
				PackAlbumIDs: []uint32{albums[0].ID, albums[1].ID},
			})).To(Succeed())

			Expect(client.DownloadRecord.GetX(ctx, rec.ID).Status).
				To(Equal(downloadrecord.StatusFailed))
			Expect(client.Album.GetX(ctx, albums[0].ID).Status).
				To(Equal(album.StatusDownloading), "it holds a file already")
			Expect(client.Album.GetX(ctx, albums[1].ID).Status).
				To(Equal(album.StatusWanted))
		},
	)

	It("leaves the albums alone on a failure that will be retried", func() {
		rec := createPack()
		Expect(store.RecordImportFailure(ctx, RecordImportFailureParams{
			RecordID: rec.ID, Attempts: 1,
			PackAlbumIDs: []uint32{albums[0].ID, albums[1].ID},
		})).To(Succeed())
		Expect(client.Album.GetX(ctx, albums[1].ID).Status).
			To(Equal(album.StatusDownloading))
	})

	It("flags the artist's newest downloading record's replace mode", func() {
		rec, err := store.CreateDownloadRecord(ctx, CreateDownloadRecordParams{
			Title: "pack", Size: 1, Status: downloadrecord.StatusDownloading,
			ArtistID: a.ID, AlbumIDs: []uint32{albums[0].ID},
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(store.SetLiveArtistRecordReplaceMode(
			ctx, a.ID, downloadrecord.ReplaceModeAll,
		)).To(Succeed())
		Expect(client.DownloadRecord.GetX(ctx, rec.ID).ReplaceMode).
			To(Equal(downloadrecord.ReplaceModeAll))
	})
})
