package db

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/album"
	"github.com/datahearth/streamline/ent/book"
	"github.com/datahearth/streamline/ent/downloadrecord"
	"github.com/datahearth/streamline/ent/mediafile"
)

var _ = Describe(
	"RevertOrphanedDownloadingAlbumsAndBooks",
	Label("integration", "db"),
	func() {
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

		It(
			"reverts an album nothing is downloading and leaves one in flight alone",
			func() {
				_, err := store.CreateArtist(ctx, CreateArtistParams{
					MBID: "a-1", Name: "Nirvana",
					Albums: []AlbumSeed{
						{
							MBID:      "rg-1",
							Title:     "Nevermind",
							Type:      "album",
							Monitored: true,
						},
						{
							MBID:      "rg-2",
							Title:     "In Utero",
							Type:      "album",
							Monitored: true,
						},
					},
				})
				Expect(err).NotTo(HaveOccurred())
				stranded := client.Album.Query().Where(album.Mbid("rg-1")).OnlyX(ctx)
				live := client.Album.Query().Where(album.Mbid("rg-2")).OnlyX(ctx)
				for _, a := range []*ent.Album{stranded, live} {
					client.Album.UpdateOneID(a.ID).
						SetStatus(album.StatusDownloading).
						ExecX(ctx)
				}
				client.DownloadRecord.Create().
					SetTitle("refused").
					SetSavePath("/dl").
					SetAlbumID(stranded.ID).
					SetStatus(downloadrecord.StatusFailed).
					SaveX(ctx)
				client.DownloadRecord.Create().SetTitle("live").SetSavePath("/dl").
					SetAlbumID(live.ID).
					SetStatus(downloadrecord.StatusDownloading).SaveX(ctx)

				n, err := store.RevertOrphanedDownloadingAlbumsAndBooks(ctx)

				Expect(err).NotTo(HaveOccurred())
				Expect(n).To(Equal(1))
				Expect(
					client.Album.GetX(ctx, stranded.ID).Status,
				).To(Equal(album.StatusWanted))
				Expect(
					client.Album.GetX(ctx, live.ID).Status,
				).To(Equal(album.StatusDownloading))
			},
		)

		It(
			"reverts each book slot on its own, to available when it holds a file",
			func() {
				bk := client.Book.Create().SetHardcoverID(2).SetTitle("Elantris").
					SetAuthorName("Sanderson").
					SetEbookStatus(book.EbookStatusDownloading).
					SetAudiobookStatus(book.AudiobookStatusDownloading).
					SaveX(ctx)
				client.MediaFile.Create().
					SetPath("/b/e.epub").
					SetSize(1).
					SetQuality("EPUB").
					SetFormat("epub").
					SetBookID(bk.ID).
					SetBookKind(mediafile.BookKindEbook).
					SaveX(ctx)
				client.DownloadRecord.Create().SetTitle("audio").SetSavePath("/dl").
					SetBookID(bk.ID).SetBookKind(downloadrecord.BookKindAudiobook).
					SetStatus(downloadrecord.StatusDownloading).SaveX(ctx)

				n, err := store.RevertOrphanedDownloadingAlbumsAndBooks(ctx)

				Expect(err).NotTo(HaveOccurred())
				Expect(n).To(Equal(1))
				got := client.Book.GetX(ctx, bk.ID)
				Expect(got.EbookStatus).To(Equal(book.EbookStatusAvailable))
				Expect(
					got.AudiobookStatus,
				).To(Equal(book.AudiobookStatusDownloading))
			},
		)
	},
)
