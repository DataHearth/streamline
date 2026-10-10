package importer

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stretchr/testify/mock"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/book"
	"github.com/datahearth/streamline/ent/downloadrecord"
	"github.com/datahearth/streamline/ent/mediafile"
	"github.com/datahearth/streamline/internal/db"
	"github.com/datahearth/streamline/internal/library"
	msmocks "github.com/datahearth/streamline/internal/mediaserver/mocks"
	"github.com/datahearth/streamline/internal/testutil/configtest"
	"github.com/datahearth/streamline/internal/testutil/dbtest"
)

var _ = Describe("Worker book imports", Label("unit", "importer"), func() {
	var (
		ctx    context.Context
		client *ent.Client
		w      *Worker
		tmp    string
		ebooks string
		audio  string
		b      *ent.Book
	)

	write := func(dir, name, content string) string {
		GinkgoHelper()
		p := filepath.Join(dir, name)
		Expect(os.MkdirAll(filepath.Dir(p), 0o755)).To(Succeed())
		Expect(os.WriteFile(p, []byte(content), 0o644)).To(Succeed())
		return p
	}

	record := func(kind downloadrecord.BookKind, src string, mode downloadrecord.ReplaceMode) uint32 {
		GinkgoHelper()
		return client.DownloadRecord.Create().
			SetTitle("Elantris").
			SetBookID(b.ID).
			SetBookKind(kind).
			SetSavePath(src).
			SetReplaceMode(mode).
			SetStatus(downloadrecord.StatusImporting).
			SaveX(ctx).ID
	}

	run := func(id uint32) error {
		GinkgoHelper()
		err := w.runImport(ctx, id)
		if err != nil {
			w.handleOutcome(ctx, id, err)
		}
		return err
	}

	reload := func() *ent.Book {
		GinkgoHelper()
		return client.Book.GetX(ctx, b.ID)
	}

	BeforeEach(func() {
		ctx = context.Background()
		tmp = GinkgoT().TempDir()
		ebooks = filepath.Join(tmp, "ebooks")
		audio = filepath.Join(tmp, "audio")
		configtest.Setup(map[string]any{
			"library": map[string]any{
				"ebook_path":          ebooks,
				"audiobook_path":      audio,
				"import_mode":         "copy",
				"import_max_attempts": 3,
			},
			"book_quality_profiles": []map[string]any{{
				"name": "e",
				"ebook": map[string]any{
					"formats": []string{"EPUB", "MOBI"}, "preferred": "EPUB",
				},
				"audiobook": map[string]any{
					"formats": []string{"M4B", "MP3"}, "preferred": "M4B",
				},
			}},
			"book_quality_default_profiles": map[string]any{
				"novel": "e", "bd": "e", "comic": "e", "manga": "e",
			},
		})
		client = dbtest.SetupTestDB(ctx)
		DeferCleanup(client.Close)
		w = NewWorker(Deps{DB: db.New(client), Library: library.NewImportService()})

		b = client.Book.Create().
			SetHardcoverID(2).SetTitle("Elantris").
			SetReleaseDate(time.Date(2005, 4, 21, 0, 0, 0, 0, time.UTC)).
			SetAuthorName("Brandon Sanderson").
			SetReleaseYear(2005).
			SetLastRefreshedAt(time.Now()).
			SetEbookStatus(book.EbookStatusDownloading).
			SetAudiobookStatus(book.AudiobookStatusDownloading).
			SaveX(ctx)
	})

	It("asks the media servers to rescan the slot's root", func() {
		ms := msmocks.NewMockRefresher(GinkgoT())
		w = NewWorker(Deps{
			DB: db.New(client), Library: library.NewImportService(), MediaServer: ms,
		})
		ms.EXPECT().RefreshAll(mock.Anything, "book", ebooks).Return(nil).Once()
		src := filepath.Join(tmp, "dl")
		write(src, "elantris.epub", "epub bytes")

		Expect(run(record(
			downloadrecord.BookKindEbook, src, downloadrecord.ReplaceModeNone,
		))).To(Succeed())
	})

	It("imports one ebook file, ignoring the rest of the download", func() {
		src := filepath.Join(tmp, "dl")
		epub := write(src, "elantris.epub", "epub bytes")
		write(src, "elantris.nfo", "nfo")
		write(src, "readme.txt", "txt")
		id := record(
			downloadrecord.BookKindEbook,
			src,
			downloadrecord.ReplaceModeNone,
		)

		Expect(run(id)).To(Succeed())

		dst := filepath.Join(ebooks, "Brandon Sanderson", "Elantris (2005).epub")
		got, err := os.ReadFile(dst)
		Expect(err).NotTo(HaveOccurred())
		want, err := os.ReadFile(epub)
		Expect(err).NotTo(HaveOccurred())
		Expect(got).To(Equal(want))

		files := client.MediaFile.Query().AllX(ctx)
		Expect(files).To(HaveLen(1))
		Expect(files[0].Source).To(Equal(mediafile.SourceAuto))
		Expect(files[0].BookKind).To(Equal(mediafile.BookKindEbook))
		Expect(files[0].Quality).To(Equal("EPUB"))
		Expect(files[0].Format).To(Equal("epub"))
		Expect(files[0].Path).To(Equal(dst))
		Expect(reload().EbookStatus).To(Equal(book.EbookStatusAvailable))
		Expect(reload().AudiobookStatus).To(Equal(book.AudiobookStatusDownloading))
		Expect(client.DownloadRecord.GetX(ctx, id).Status).
			To(Equal(downloadrecord.StatusCompleted))
	})

	It("imports only the best format of the author's profile", func() {
		src := filepath.Join(tmp, "dl")
		write(src, "x.mobi", "mobi")
		write(src, "x.epub", "epub")
		write(src, "x.pdf", "pdf")
		id := record(
			downloadrecord.BookKindEbook,
			src,
			downloadrecord.ReplaceModeNone,
		)

		Expect(run(id)).To(Succeed())

		files := client.MediaFile.Query().AllX(ctx)
		Expect(files).To(HaveLen(1))
		Expect(files[0].Format).To(Equal("epub"))
		Expect(filepath.Join(ebooks, "Brandon Sanderson", "Elantris (2005).mobi")).
			NotTo(BeAnExistingFile())
	})

	It("fails terminally when only formats outside the profile are present", func() {
		src := filepath.Join(tmp, "dl")
		write(src, "x.pdf", "pdf")
		id := record(
			downloadrecord.BookKindEbook,
			src,
			downloadrecord.ReplaceModeNone,
		)

		Expect(run(id)).To(MatchError(library.ErrNoMedia))

		Expect(client.DownloadRecord.GetX(ctx, id).Status).
			To(Equal(downloadrecord.StatusFailed))
		Expect(reload().EbookStatus).To(Equal(book.EbookStatusWanted))
		Expect(reload().AudiobookStatus).To(Equal(book.AudiobookStatusDownloading))
	})

	It("imports every audiobook file into one folder", func() {
		src := filepath.Join(tmp, "dl")
		for i := 1; i <= 12; i++ {
			name := "Part " + pad(i) + ".mp3"
			write(src, name, "audio "+strconv.Itoa(i))
		}
		write(src, "cover.jpg", "jpg")
		id := record(
			downloadrecord.BookKindAudiobook,
			src,
			downloadrecord.ReplaceModeNone,
		)

		Expect(run(id)).To(Succeed())

		dir := filepath.Join(audio, "Brandon Sanderson", "Elantris (2005)")
		files := client.MediaFile.Query().AllX(ctx)
		Expect(files).To(HaveLen(12))
		for _, f := range files {
			Expect(f.BookKind).To(Equal(mediafile.BookKindAudiobook))
			Expect(f.Quality).To(Equal("MP3"))
			Expect(filepath.Dir(f.Path)).To(Equal(dir))
		}
		Expect(filepath.Join(dir, "Part 07.mp3")).To(BeAnExistingFile())
		Expect(filepath.Join(dir, "cover.jpg")).NotTo(BeAnExistingFile())
		Expect(reload().AudiobookStatus).To(Equal(book.AudiobookStatusAvailable))
		Expect(reload().EbookStatus).To(Equal(book.EbookStatusDownloading))
		Expect(client.DownloadRecord.GetX(ctx, id).Status).
			To(Equal(downloadrecord.StatusCompleted))
	})

	It("fails with ErrDestExists on an existing slot file without replace", func() {
		src := filepath.Join(tmp, "dl")
		write(src, "x.epub", "new")
		old := write(ebooks, "Brandon Sanderson/Elantris (2005).epub", "old")
		client.MediaFile.Create().
			SetPath(old).SetSize(3).SetQuality("epub").SetFormat("epub").
			SetBookID(b.ID).SetBookKind(mediafile.BookKindEbook).SaveX(ctx)
		id := record(
			downloadrecord.BookKindEbook,
			src,
			downloadrecord.ReplaceModeNone,
		)

		Expect(run(id)).To(MatchError(library.ErrDestExists))

		got, err := os.ReadFile(old)
		Expect(err).NotTo(HaveOccurred())
		Expect(string(got)).To(Equal("old"))
		Expect(client.DownloadRecord.GetX(ctx, id).Status).
			To(Equal(downloadrecord.StatusFailed))
	})

	It("replaces the slot's file when the record asks for it", func() {
		src := filepath.Join(tmp, "dl")
		write(src, "x.epub", "new")
		old := write(ebooks, "Brandon Sanderson/Elantris (2005).epub", "old")
		client.MediaFile.Create().
			SetPath(old).SetSize(3).SetQuality("epub").SetFormat("epub").
			SetBookID(b.ID).SetBookKind(mediafile.BookKindEbook).SaveX(ctx)
		id := record(
			downloadrecord.BookKindEbook,
			src,
			downloadrecord.ReplaceModeAll,
		)

		Expect(run(id)).To(Succeed())

		got, err := os.ReadFile(old)
		Expect(err).NotTo(HaveOccurred())
		Expect(string(got)).To(Equal("new"))
		Expect(old + replacedSuffix).NotTo(BeAnExistingFile())
		Expect(client.MediaFile.Query().CountX(ctx)).To(Equal(1))
	})

	It("replaces a whole audiobook folder and puts it back on failure", func() {
		dir := filepath.Join(audio, "Brandon Sanderson", "Elantris (2005)")
		oldFile := write(dir, "old.mp3", "old")
		client.MediaFile.Create().
			SetPath(oldFile).SetSize(3).SetQuality("mp3").SetFormat("mp3").
			SetBookID(b.ID).SetBookKind(mediafile.BookKindAudiobook).SaveX(ctx)

		empty := filepath.Join(tmp, "empty")
		Expect(os.MkdirAll(empty, 0o755)).To(Succeed())
		failing := record(
			downloadrecord.BookKindAudiobook,
			empty,
			downloadrecord.ReplaceModeAll,
		)
		Expect(run(failing)).To(MatchError(library.ErrNoMedia))
		Expect(oldFile).To(BeAnExistingFile())
		Expect(reload().AudiobookStatus).To(Equal(book.AudiobookStatusDownloading))

		src := filepath.Join(tmp, "dl")
		write(src, "new.mp3", "new")
		id := record(
			downloadrecord.BookKindAudiobook,
			src,
			downloadrecord.ReplaceModeAll,
		)
		Expect(run(id)).To(Succeed())

		Expect(oldFile + replacedSuffix).NotTo(BeAnExistingFile())
		files := client.MediaFile.Query().AllX(ctx)
		Expect(files).To(HaveLen(1))
		Expect(files[0].Path).To(Equal(filepath.Join(dir, "new.mp3")))
	})

	It("leaves the slot available when a failed import still has a file", func() {
		old := write(ebooks, "Brandon Sanderson/Elantris (2005).epub", "old")
		client.MediaFile.Create().
			SetPath(old).SetSize(3).SetQuality("epub").SetFormat("epub").
			SetBookID(b.ID).SetBookKind(mediafile.BookKindEbook).SaveX(ctx)
		client.Book.UpdateOneID(b.ID).
			SetEbookStatus(book.EbookStatusAvailable).
			ExecX(ctx)
		src := filepath.Join(tmp, "dl")
		write(src, "x.epub", "new")
		id := record(
			downloadrecord.BookKindEbook,
			src,
			downloadrecord.ReplaceModeNone,
		)

		Expect(run(id)).To(MatchError(library.ErrDestExists))

		Expect(reload().EbookStatus).To(Equal(book.EbookStatusAvailable))
	})
	Describe("a book not ready to be named", func() {
		It("parks the record held while the book has not been hydrated", func() {
			client.Book.UpdateOneID(b.ID).ClearLastRefreshedAt().ExecX(ctx)
			src := filepath.Join(tmp, "dl")
			write(src, "x.epub", "epub")
			id := record(
				downloadrecord.BookKindEbook,
				src,
				downloadrecord.ReplaceModeNone,
			)

			Expect(run(id)).To(Succeed())

			rec := client.DownloadRecord.GetX(ctx, id)
			Expect(rec.Status).To(Equal(downloadrecord.StatusHeld))
			Expect(
				rec.HoldReasons,
			).To(ContainElement(HaveField("Check", "metadata")))
			Expect(client.MediaFile.Query().CountX(ctx)).To(BeZero())
			Expect(ebooks).NotTo(BeADirectory())
		})

		It("parks the record held when the book has no author yet", func() {
			client.Book.UpdateOneID(b.ID).SetAuthorName("").ExecX(ctx)
			src := filepath.Join(tmp, "dl")
			write(src, "x.epub", "epub")
			id := record(
				downloadrecord.BookKindEbook,
				src,
				downloadrecord.ReplaceModeNone,
			)

			Expect(run(id)).To(Succeed())

			Expect(client.DownloadRecord.GetX(ctx, id).Status).
				To(Equal(downloadrecord.StatusHeld))
			Expect(ebooks).NotTo(BeADirectory())
		})
	})

	It("names a file from the slot edition and the profile of its series", func() {
		series := client.BookSeries.Create().
			SetHardcoverID(7).SetTitle("Mistborn").SetQualityProfile("e").SaveX(ctx)
		ed := client.BookEdition.Create().
			SetBook(b).SetHardcoverEditionID(70).SetLanguage("fr").
			SetTitle("L'Empire ultime").SetFormat("ebook").SaveX(ctx)
		client.Book.UpdateOneID(b.ID).
			SetSeries(series).SetSeriesPosition(1).SetEbookEdition(ed).ExecX(ctx)
		src := filepath.Join(tmp, "dl")
		write(src, "x.epub", "epub")
		id := record(
			downloadrecord.BookKindEbook,
			src,
			downloadrecord.ReplaceModeNone,
		)

		Expect(run(id)).To(Succeed())

		Expect(filepath.Join(
			ebooks, "Brandon Sanderson", "Mistborn", "Mistborn - Vol. 01.epub",
		)).To(BeAnExistingFile())
	})

	It("clears the replacing language once the replacement is placed", func() {
		old := write(ebooks, "Brandon Sanderson/Elantris (2005).epub", "old")
		client.MediaFile.Create().
			SetPath(old).SetSize(3).SetQuality("EPUB").SetFormat("epub").
			SetBookID(b.ID).SetBookKind(mediafile.BookKindEbook).SaveX(ctx)
		client.Book.UpdateOneID(b.ID).SetEbookReplacingLanguage("en").ExecX(ctx)
		src := filepath.Join(tmp, "dl")
		write(src, "x.epub", "new")
		id := record(
			downloadrecord.BookKindEbook,
			src,
			downloadrecord.ReplaceModeAll,
		)

		Expect(run(id)).To(Succeed())

		Expect(reload().EbookReplacingLanguage).To(BeEmpty())
		Expect(reload().EbookStatus).To(Equal(book.EbookStatusAvailable))
	})
})

func pad(i int) string {
	if i < 10 {
		return "0" + strconv.Itoa(i)
	}
	return strconv.Itoa(i)
}
