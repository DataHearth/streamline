package book

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/datahearth/streamline/ent/mediafile"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/ent/book"
	"github.com/datahearth/streamline/ent/downloadrecord"
	"github.com/datahearth/streamline/internal/db"
	"github.com/datahearth/streamline/internal/download"
	"github.com/datahearth/streamline/internal/upload"
)

var _ = Describe("uploads", Label("unit", "books"), func() {
	var (
		f      *fixture
		called int
		recv   ReceiveFunc
		dir    string
	)

	BeforeEach(func() {
		f = newFixture()
		called = 0
		dir = GinkgoT().TempDir()
		recv = func(_ context.Context, _ upload.Kind) (*upload.Staged, error) {
			called++
			Expect(os.WriteFile(filepath.Join(dir, "a.epub"), []byte("x"), 0o600)).
				To(Succeed())
			return &upload.Staged{
				Dir:   dir,
				Files: []upload.File{{Path: "a.epub", Size: 1}},
			}, nil
		}
	})

	It("refuses a stub book before receiving", func() {
		stub := f.client.Book.Create().
			SetHardcoverID("9").
			SetTitle("Stub").
			SaveX(f.ctx)

		_, err := f.svc.UploadBook(f.ctx, stub.ID, "ebook", UploadOptions{}, recv)

		Expect(err).To(MatchError(ErrNotHydrated))
		Expect(called).To(BeZero())
	})

	It("refuses a slot with a live record before receiving", func() {
		b := f.addBook(1, "Elantris", "both")
		_, err := f.store.CreateDownloadRecord(f.ctx, db.CreateDownloadRecordParams{
			Title: "x", Status: downloadrecord.StatusDownloading,
			BookID: b.ID, BookKind: downloadrecord.BookKindEbook,
		})
		Expect(err).NotTo(HaveOccurred())

		_, err = f.svc.UploadBook(f.ctx, b.ID, "ebook", UploadOptions{}, recv)

		Expect(err).To(MatchError(upload.ErrConflict))
		Expect(called).To(BeZero())
	})

	It("fills a slot whose file is missing from disk without replace", func() {
		b := f.addBook(1, "Elantris", "both")
		f.client.MediaFile.Create().
			SetPath("/books/gone.epub").SetSize(1).SetBookID(b.ID).
			SetBookKind(mediafile.BookKindEbook).SetMissingSince(time.Now()).
			ExecX(f.ctx)

		_, err := f.svc.UploadBook(f.ctx, b.ID, "ebook", UploadOptions{}, recv)

		Expect(err).NotTo(HaveOccurred())
	})

	It("files an uploaded record and marks the slot downloading", func() {
		b := f.addBook(1, "Elantris", "both")

		rec, err := f.svc.UploadBook(f.ctx, b.ID, "ebook",
			UploadOptions{Replace: true}, recv)

		Expect(err).NotTo(HaveOccurred())
		Expect(called).To(Equal(1))
		Expect(rec.Uploaded).To(BeTrue())
		Expect(rec.Title).To(Equal("Upload"))
		Expect(rec.SavePath).To(Equal(dir))
		Expect(rec.Status).To(Equal(downloadrecord.StatusImporting))
		Expect(rec.BookKind).To(Equal(downloadrecord.BookKindEbook))
		got := f.reloadBook(b.ID)
		Expect(got.EbookStatus).To(Equal(book.EbookStatusDownloading))
		Expect(got.AudiobookStatus).To(Equal(book.AudiobookStatusWanted))
	})

	It("lets one of several simultaneous uploads through", func() {
		b := f.addBook(1, "Elantris", "both")
		dirs := make(chan string, 8)
		results := make(chan error, cap(dirs))
		for range cap(dirs) {
			dirs <- GinkgoT().TempDir()
		}
		var wg sync.WaitGroup
		for range cap(dirs) {
			wg.Go(func() {
				dir := <-dirs
				_, err := f.svc.UploadBook(f.ctx, b.ID, "ebook", UploadOptions{},
					func(context.Context, upload.Kind) (*upload.Staged, error) {
						return &upload.Staged{
							Dir:   dir,
							Files: []upload.File{{Path: "a.epub", Size: 1}},
						}, nil
					})
				results <- err
			})
		}
		wg.Wait()
		close(results)

		var ok, busy int
		for err := range results {
			switch {
			case err == nil:
				ok++
			case errors.Is(err, upload.ErrConflict):
				busy++
			}
		}
		Expect(ok).To(Equal(1))
		Expect(busy).To(Equal(cap(results) - 1))
	})

	It("answers a busy slot from the preview", func() {
		b := f.addBook(1, "Elantris", "both")
		_, err := f.store.CreateDownloadRecord(f.ctx, db.CreateDownloadRecordParams{
			Title: "x", Status: downloadrecord.StatusHeld,
			BookID: b.ID, BookKind: downloadrecord.BookKindEbook,
		})
		Expect(err).NotTo(HaveOccurred())

		_, err = f.svc.BookUploadPreview(
			f.ctx,
			b.ID,
			"ebook",
			[]download.ReleaseFile{
				{Path: "a.epub", Size: 10},
			},
		)
		Expect(err).To(MatchError(upload.ErrConflict))
	})

	It("previews the one ebook the import would keep", func() {
		b := f.addBook(1, "Elantris", "both")

		got, err := f.svc.BookUploadPreview(
			f.ctx,
			b.ID,
			"ebook",
			[]download.ReleaseFile{
				{Path: "a.epub", Size: 10},
				{Path: "a.pdf", Size: 20},
				{Path: "a.mp3", Size: 30},
				{Path: "notes.txt", Size: 1},
			},
		)

		Expect(err).NotTo(HaveOccurred())
		outcomes := map[string]string{}
		for _, v := range got {
			outcomes[v.Path] = v.Outcome
		}
		Expect(outcomes).To(Equal(map[string]string{
			"a.epub":    "kept",
			"a.pdf":     "ignored",
			"a.mp3":     "refused",
			"notes.txt": "ignored",
		}))
	})
})
