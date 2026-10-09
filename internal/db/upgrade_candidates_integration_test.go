package db

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/album"
	"github.com/datahearth/streamline/ent/downloadrecord"
	"github.com/datahearth/streamline/ent/mediafile"
)

var _ = Describe("Upgrade candidates", Label("integration", "db"), func() {
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

	Describe("albums", func() {
		var artist *ent.Artist

		BeforeEach(func() {
			var err error
			artist, err = store.CreateArtist(ctx, CreateArtistParams{
				MBID: "a-1", Name: "Nirvana", Monitored: true,
				Albums: []AlbumSeed{
					{
						MBID:  "rg-1",
						Title: "Nevermind",
						Type:  "album",
						Tracks: []TrackSeed{
							{MBID: "t-1", Title: "Drain You", Disc: 1, Position: 1},
						},
					},
					{
						MBID:  "rg-2",
						Title: "In Utero",
						Type:  "album",
						Tracks: []TrackSeed{
							{
								MBID:     "t-2",
								Title:    "Heart-Shaped Box",
								Disc:     1,
								Position: 1,
							},
						},
					},
				},
			})
			Expect(err).NotTo(HaveOccurred())
		})

		albumByMBID := func(mbid string) *ent.Album {
			GinkgoHelper()
			return client.Artist.GetX(ctx, artist.ID).QueryAlbums().
				Where(album.Mbid(mbid)).OnlyX(ctx)
		}

		It(
			"lists only monitored albums that hold a file, tracks and files loaded",
			func() {
				a := albumByMBID("rg-1")
				tracks := client.Album.GetX(ctx, a.ID).QueryTracks().AllX(ctx)
				client.MediaFile.Create().SetPath("/x/a.flac").SetSize(1).
					SetQuality("lossless").
					SetFormat("flac").SetTrackID(tracks[0].ID).SaveX(ctx)

				got, err := store.ListUpgradeCandidateAlbums(ctx)

				Expect(err).NotTo(HaveOccurred())
				Expect(got).To(HaveLen(1))
				Expect(got[0].Mbid).To(Equal("rg-1"))
				Expect(got[0].Edges.Artist).NotTo(BeNil())
				Expect(got[0].Edges.Tracks).To(HaveLen(1))
				Expect(got[0].Edges.Tracks[0].Edges.MediaFiles).To(HaveLen(1))
				Expect(
					got[0].Edges.Tracks[0].Edges.MediaFiles[0].Quality,
				).To(Equal("lossless"))
			},
		)

		It(
			"drops an unmonitored album and one a live record already covers",
			func() {
				one, two := albumByMBID("rg-1"), albumByMBID("rg-2")
				for _, a := range []*ent.Album{one, two} {
					tracks := client.Album.GetX(ctx, a.ID).QueryTracks().AllX(ctx)
					client.MediaFile.Create().
						SetPath("/x/" + a.Mbid + ".flac").
						SetSize(1).
						SetQuality("high").
						SetFormat("mp3").
						SetTrackID(tracks[0].ID).
						SaveX(ctx)
				}
				client.Album.UpdateOneID(one.ID).SetMonitored(false).ExecX(ctx)
				client.DownloadRecord.Create().SetTitle("Nirvana - In Utero").
					SetAlbumID(two.ID).SetSavePath("/dl").
					SetStatus(downloadrecord.StatusDownloading).SaveX(ctx)

				got, err := store.ListUpgradeCandidateAlbums(ctx)

				Expect(err).NotTo(HaveOccurred())
				Expect(got).To(BeEmpty())
			},
		)

		It(
			"flags the newest downloading record of the album and nothing else",
			func() {
				a := albumByMBID("rg-1")
				old := client.DownloadRecord.Create().
					SetTitle("old").
					SetAlbumID(a.ID).
					SetSavePath("/dl").
					SetStatus(downloadrecord.StatusCompleted).
					SaveX(ctx)
				live := client.DownloadRecord.Create().
					SetTitle("live").
					SetAlbumID(a.ID).
					SetSavePath("/dl").
					SetStatus(downloadrecord.StatusDownloading).
					SaveX(ctx)
				other := client.DownloadRecord.Create().SetTitle("other").
					SetAlbumID(albumByMBID("rg-2").ID).SetSavePath("/dl").
					SetStatus(downloadrecord.StatusDownloading).SaveX(ctx)

				Expect(store.SetLiveAlbumRecordReplaceMode(
					ctx, a.ID, downloadrecord.ReplaceModeUpgrades,
				)).To(Succeed())

				Expect(client.DownloadRecord.GetX(ctx, live.ID).ReplaceMode).
					To(Equal(downloadrecord.ReplaceModeUpgrades))
				Expect(client.DownloadRecord.GetX(ctx, old.ID).ReplaceMode).
					To(Equal(downloadrecord.ReplaceModeNone))
				Expect(client.DownloadRecord.GetX(ctx, other.ID).ReplaceMode).
					To(Equal(downloadrecord.ReplaceModeNone))
			},
		)

		It("reports an album with no downloading record", func() {
			err := store.SetLiveAlbumRecordReplaceMode(
				ctx, albumByMBID("rg-1").ID, downloadrecord.ReplaceModeUpgrades,
			)
			Expect(ent.IsNotFound(err)).To(BeTrue())
		})
	})

	Describe("books", func() {
		var (
			author *ent.Author
			bk     *ent.Book
		)

		BeforeEach(func() {
			author = client.Author.Create().
				SetHardcoverID(1).
				SetName("Sanderson").
				SaveX(ctx)
			bk = client.Book.Create().SetHardcoverID(2).SetTitle("Elantris").
				SetAuthor(author).SaveX(ctx)
		})

		addFile := func(kind mediafile.BookKind, quality string) {
			GinkgoHelper()
			client.MediaFile.Create().SetPath("/b/" + quality).SetSize(1).
				SetQuality(quality).
				SetFormat("x").SetBookID(bk.ID).SetBookKind(kind).SaveX(ctx)
		}

		It("lists a book holding a file of a slot nothing is coming for", func() {
			addFile(mediafile.BookKindEbook, "AZW3")

			got, err := store.ListUpgradeCandidateBooks(ctx)

			Expect(err).NotTo(HaveOccurred())
			Expect(got).To(HaveLen(1))
			Expect(got[0].Edges.Author).NotTo(BeNil())
			Expect(got[0].Edges.MediaFiles).To(HaveLen(1))
		})

		It(
			"drops a book with no file, and one whose only file's slot is in flight",
			func() {
				got, err := store.ListUpgradeCandidateBooks(ctx)
				Expect(err).NotTo(HaveOccurred())
				Expect(got).To(BeEmpty())

				addFile(mediafile.BookKindEbook, "AZW3")
				client.DownloadRecord.Create().SetTitle("x").SetBookID(bk.ID).
					SetBookKind(downloadrecord.BookKindEbook).SetSavePath("/dl").
					SetStatus(downloadrecord.StatusImporting).SaveX(ctx)
				got, err = store.ListUpgradeCandidateBooks(ctx)
				Expect(err).NotTo(HaveOccurred())
				Expect(got).To(BeEmpty())
			},
		)

		It("keeps a book whose other slot is the one in flight", func() {
			addFile(mediafile.BookKindEbook, "AZW3")
			client.DownloadRecord.Create().SetTitle("x").SetBookID(bk.ID).
				SetBookKind(downloadrecord.BookKindAudiobook).SetSavePath("/dl").
				SetStatus(downloadrecord.StatusDownloading).SaveX(ctx)

			got, err := store.ListUpgradeCandidateBooks(ctx)

			Expect(err).NotTo(HaveOccurred())
			Expect(got).To(HaveLen(1))
		})

		It("drops the books of an unmonitored author", func() {
			client.Author.UpdateOneID(author.ID).SetMonitored(false).ExecX(ctx)
			addFile(mediafile.BookKindEbook, "AZW3")

			got, err := store.ListUpgradeCandidateBooks(ctx)

			Expect(err).NotTo(HaveOccurred())
			Expect(got).To(BeEmpty())
		})

		It("flags the newest downloading record of one slot", func() {
			ebook := client.DownloadRecord.Create().SetTitle("e").SetBookID(bk.ID).
				SetBookKind(downloadrecord.BookKindEbook).SetSavePath("/dl").
				SetStatus(downloadrecord.StatusDownloading).SaveX(ctx)
			audio := client.DownloadRecord.Create().SetTitle("a").SetBookID(bk.ID).
				SetBookKind(downloadrecord.BookKindAudiobook).SetSavePath("/dl").
				SetStatus(downloadrecord.StatusDownloading).SaveX(ctx)

			Expect(store.SetLiveBookRecordReplaceMode(
				ctx,
				bk.ID,
				downloadrecord.BookKindEbook,
				downloadrecord.ReplaceModeUpgrades,
			)).To(Succeed())

			Expect(client.DownloadRecord.GetX(ctx, ebook.ID).ReplaceMode).
				To(Equal(downloadrecord.ReplaceModeUpgrades))
			Expect(client.DownloadRecord.GetX(ctx, audio.ID).ReplaceMode).
				To(Equal(downloadrecord.ReplaceModeNone))
		})
	})
})
