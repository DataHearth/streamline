package book

import (
	"context"
	"os"
	"path/filepath"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stretchr/testify/mock"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/book"
	"github.com/datahearth/streamline/ent/mediafile"
	"github.com/datahearth/streamline/internal/download"
	msmocks "github.com/datahearth/streamline/internal/mediaserver/mocks"
	"github.com/datahearth/streamline/internal/metadata"
)

var _ = Describe("Book lifecycle", Label("unit", "integration", "books"), func() {
	var f *fixture

	BeforeEach(func() { f = newFixture() })

	write := func(path string) string {
		GinkgoHelper()
		Expect(os.MkdirAll(filepath.Dir(path), 0o755)).To(Succeed())
		Expect(os.WriteFile(path, []byte("x"), 0o644)).To(Succeed())
		return path
	}
	attach := func(b *ent.Book, kind mediafile.BookKind, path string) *ent.MediaFile {
		GinkgoHelper()
		return f.client.MediaFile.Create().
			SetPath(path).SetSize(1).SetQuality("EPUB").SetFormat("epub").
			SetBookID(b.ID).SetBookKind(kind).SaveX(f.ctx)
	}
	addSeries := func(n int) *ent.BookSeries {
		GinkgoHelper()
		f.meta.EXPECT().GetSeries(anyCtx, uint32(50)).
			Return(seriesRecord("One Piece", n), nil).Once()
		want := []uint32{}
		recs := []*metadata.BookRecord{}
		for i := 1; i <= n; i++ {
			want = append(want, 1000+uint32(i))
			recs = append(recs, volumeRec(1000+uint32(i)))
		}
		f.meta.EXPECT().GetBooks(anyCtx, ids(want...)).Return(recs, nil).Once()
		s, err := f.svc.AddSeries(f.ctx, AddSeriesParams{HardcoverID: 50})
		Expect(err).NotTo(HaveOccurred())
		return s
	}

	Describe("DeleteBook", func() {
		It(
			"removes the book, its files when asked, its poster and the people left with nothing",
			func() {
				b := f.addBook(1, "Elantris", "")
				epub := write(
					filepath.Join(
						f.dir,
						"ebooks",
						"Brandon Sanderson",
						"Elantris (2005).epub",
					),
				)
				attach(b, mediafile.BookKindEbook, epub)

				Expect(f.svc.DeleteBook(f.ctx, b.ID, true)).To(Succeed())

				Expect(epub).NotTo(BeAnExistingFile())
				Expect(f.client.Book.Query().CountX(f.ctx)).To(BeZero())
				Expect(f.client.Author.Query().CountX(f.ctx)).To(BeZero())
				Expect(
					f.posters.removals(),
				).To(ContainElement(fetch{kind: "books", id: b.ID}))
				Expect(f.posters.removals()).To(HaveLen(2))
			},
		)

		It("asks the media servers to rescan once its files are gone", func() {
			ms := msmocks.NewMockRefresher(GinkgoT())
			f.svc.ms = ms
			done := make(chan struct{}, 2)
			ms.EXPECT().RefreshAll(mock.Anything, "book", mock.Anything).
				RunAndReturn(func(context.Context, string, string) error {
					done <- struct{}{}
					return nil
				}).Times(2)
			b := f.addBook(1, "Elantris", "")
			attach(b, mediafile.BookKindEbook,
				write(filepath.Join(f.dir, "ebooks", "x.epub")))

			Expect(f.svc.DeleteBook(f.ctx, b.ID, true)).To(Succeed())

			Eventually(done).Should(Receive())
			Eventually(done).Should(Receive())
		})

		It("keeps the files unless asked", func() {
			b := f.addBook(1, "Elantris", "")
			epub := write(filepath.Join(f.dir, "ebooks", "x.epub"))
			attach(b, mediafile.BookKindEbook, epub)

			Expect(f.svc.DeleteBook(f.ctx, b.ID, false)).To(Succeed())

			Expect(epub).To(BeAnExistingFile())
		})

		It("keeps a person another book still credits", func() {
			b := f.addBook(1, "Elantris", "")
			f.addBook(2, "Warbreaker", "")

			Expect(f.svc.DeleteBook(f.ctx, b.ID, false)).To(Succeed())

			Expect(f.client.Author.Query().CountX(f.ctx)).To(Equal(1))
			Expect(f.posters.removals()).To(HaveLen(1))
		})

		It("refuses a volume, which goes with its series", func() {
			s := addSeries(1)

			err := f.svc.DeleteBook(f.ctx, s.Edges.Volumes[0].ID, false)

			Expect(err).To(MatchError(ErrSeriesVolume))
			Expect(f.client.Book.Query().CountX(f.ctx)).To(Equal(1))
		})

		It("answers a book that does not exist", func() {
			Expect(
				f.svc.DeleteBook(f.ctx, 404, false),
			).To(MatchError(ErrBookNotFound))
		})
	})

	Describe("DeleteSeries", func() {
		It(
			"removes the volumes, their files in both libraries and every poster",
			func() {
				s := addSeries(2)
				cbz := write(
					filepath.Join(f.dir, "ebooks", "Oda", "One Piece", "v1.cbz"),
				)
				m4b := write(filepath.Join(f.dir, "audio", "Oda", "v2", "a.m4b"))
				attach(s.Edges.Volumes[0], mediafile.BookKindEbook, cbz)
				attach(s.Edges.Volumes[1], mediafile.BookKindAudiobook, m4b)

				Expect(f.svc.DeleteSeries(f.ctx, s.ID, true)).To(Succeed())

				Expect(cbz).NotTo(BeAnExistingFile())
				Expect(m4b).NotTo(BeAnExistingFile())
				Expect(f.client.Book.Query().CountX(f.ctx)).To(BeZero())
				Expect(f.client.BookSeries.Query().CountX(f.ctx)).To(BeZero())
				Expect(f.client.Author.Query().CountX(f.ctx)).To(BeZero())
				removed := f.posters.removals()
				Expect(
					removed,
				).To(ContainElement(fetch{kind: "books", id: s.Edges.Volumes[0].ID}))
				Expect(
					removed,
				).To(ContainElement(fetch{kind: "books", id: s.Edges.Volumes[1].ID}))
			},
		)

		It("answers a series that does not exist", func() {
			Expect(
				f.svc.DeleteSeries(f.ctx, 404, false),
			).To(MatchError(ErrSeriesNotFound))
		})
	})

	Describe("rename", func() {
		It(
			"moves an ebook to the template path with its own extension, then finds nothing left to do",
			func() {
				b := f.addBook(1, "Elantris", "")
				old := write(filepath.Join(f.dir, "ebooks", "wrong", "name.EPUB"))
				attach(b, mediafile.BookKindEbook, old)
				want := filepath.Join(
					f.dir,
					"ebooks",
					"Brandon Sanderson",
					"Elantris (2005).epub",
				)

				preview, err := f.svc.RenameBook(f.ctx, b.ID, true)
				Expect(err).NotTo(HaveOccurred())
				Expect(preview.Operations).To(HaveLen(1))
				Expect(preview.Operations[0].To).To(Equal(want))
				Expect(old).To(BeAnExistingFile())

				applied, err := f.svc.RenameBook(f.ctx, b.ID, false)
				Expect(err).NotTo(HaveOccurred())
				Expect(applied.Operations).To(HaveLen(1))
				Expect(want).To(BeAnExistingFile())
				Expect(old).NotTo(BeAnExistingFile())
				Expect(filepath.Dir(old)).NotTo(BeADirectory())
				Expect(f.client.MediaFile.Query().OnlyX(f.ctx).Path).To(Equal(want))

				again, err := f.svc.RenameBook(f.ctx, b.ID, false)
				Expect(err).NotTo(HaveOccurred())
				Expect(again.Operations).To(BeEmpty())
			},
		)

		It("names the file after the slot's edition and its language", func() {
			b := f.addBook(1, "Elantris", "")
			var fr *ent.BookEdition
			for _, e := range b.Edges.Editions {
				if e.Language == "fr" {
					fr = e
				}
			}
			Expect(
				f.store.SetBookSlotEdition(f.ctx, b.ID, "ebook", fr.ID),
			).To(Succeed())
			attach(
				b,
				mediafile.BookKindEbook,
				write(filepath.Join(f.dir, "ebooks", "x.epub")),
			)

			plan, err := f.svc.RenameBook(f.ctx, b.ID, true)

			Expect(err).NotTo(HaveOccurred())
			Expect(plan.Operations[0].To).To(Equal(
				filepath.Join(
					f.dir,
					"ebooks",
					"Brandon Sanderson",
					"Elantris (fr) (2005).epub",
				),
			))
		})

		It(
			"moves an audiobook folder whole, every file keeping its name and place",
			func() {
				b := f.addBook(1, "Elantris", "")
				old := filepath.Join(f.dir, "audio", "dump")
				one := write(filepath.Join(old, "Part 01.mp3"))
				two := write(filepath.Join(old, "Disc 2", "Part 01.mp3"))
				attach(b, mediafile.BookKindAudiobook, one)
				attach(b, mediafile.BookKindAudiobook, two)
				dir := filepath.Join(
					f.dir,
					"audio",
					"Brandon Sanderson",
					"Elantris (2005)",
				)

				plan, err := f.svc.RenameBook(f.ctx, b.ID, false)

				Expect(err).NotTo(HaveOccurred())
				Expect(plan.Operations).To(HaveLen(2))
				Expect(filepath.Join(dir, "Part 01.mp3")).To(BeAnExistingFile())
				Expect(
					filepath.Join(dir, "Disc 2", "Part 01.mp3"),
				).To(BeAnExistingFile())
				Expect(old).NotTo(BeADirectory())
			},
		)

		It("leaves a file whose target is taken where it is", func() {
			b := f.addBook(1, "Elantris", "")
			old := write(filepath.Join(f.dir, "ebooks", "x.epub"))
			attach(b, mediafile.BookKindEbook, old)
			taken := write(
				filepath.Join(
					f.dir,
					"ebooks",
					"Brandon Sanderson",
					"Elantris (2005).epub",
				),
			)

			plan, err := f.svc.RenameBook(f.ctx, b.ID, false)

			Expect(err).NotTo(HaveOccurred())
			Expect(plan.Operations).To(BeEmpty())
			Expect(old).To(BeAnExistingFile())
			Expect(taken).To(BeAnExistingFile())
		})

		It("names a series volume with the series template", func() {
			s := addSeries(2)
			attach(
				s.Edges.Volumes[0],
				mediafile.BookKindEbook,
				write(filepath.Join(f.dir, "ebooks", "a.cbz")),
			)
			attach(
				s.Edges.Volumes[1],
				mediafile.BookKindEbook,
				write(filepath.Join(f.dir, "ebooks", "b.cbz")),
			)

			plan, err := f.svc.RenameSeries(f.ctx, s.ID, false)

			Expect(err).NotTo(HaveOccurred())
			Expect(plan.Operations).To(HaveLen(2))
			dir := filepath.Join(f.dir, "ebooks", "Eiichiro Oda", "One Piece")
			Expect(
				filepath.Join(dir, "One Piece - Vol. 01.cbz"),
			).To(BeAnExistingFile())
			Expect(
				filepath.Join(dir, "One Piece - Vol. 02.cbz"),
			).To(BeAnExistingFile())
		})

		It("answers a book or series that does not exist", func() {
			_, err := f.svc.RenameBook(f.ctx, 404, true)
			Expect(err).To(MatchError(ErrBookNotFound))
			_, err = f.svc.RenameSeries(f.ctx, 404, true)
			Expect(err).To(MatchError(ErrSeriesNotFound))
		})
	})

	Describe("Progress", func() {
		It("reports the live percentage of each slot being downloaded", func() {
			b := f.addBook(1, "Elantris", "")
			rec := f.client.DownloadRecord.Create().SetTitle("x").SetBookID(b.ID).
				SetBookKind("audiobook").
				SetSavePath("/dl").SetStatus("downloading").SaveX(f.ctx)
			f.dl.EXPECT().Queue(anyCtx).Return(download.QueueSnapshot{
				Items: []download.QueueEntry{{RecordID: rec.ID, Progress: 0.426}},
			}, nil).Once()

			got := f.svc.Progress(f.ctx, []uint32{b.ID})

			Expect(
				got,
			).To(Equal(map[uint32]map[string]float64{b.ID: {"audiobook": 43}}))
		})

		It("does not ask the queue when nothing is in flight", func() {
			b := f.addBook(1, "Elantris", "")
			Expect(f.svc.Progress(f.ctx, []uint32{b.ID})).To(BeEmpty())
			Expect(f.svc.Progress(f.ctx, nil)).To(BeEmpty())
		})

		It("gives no figure when the queue cannot be read", func() {
			b := f.addBook(1, "Elantris", "")
			f.client.DownloadRecord.Create().SetTitle("x").SetBookID(b.ID).
				SetBookKind("ebook").
				SetSavePath("/dl").SetStatus("downloading").SaveX(f.ctx)
			f.dl.EXPECT().
				Queue(anyCtx).
				Return(download.QueueSnapshot{}, os.ErrDeadlineExceeded).
				Once()

			Expect(f.svc.Progress(f.ctx, []uint32{b.ID})).To(BeEmpty())
		})
	})

	Describe("List and Counts", func() {
		It("pages the shelf and carries the progress of a downloading book", func() {
			b := f.addBook(1, "Elantris", "")
			f.addBook(2, "Warbreaker", "")
			f.client.Book.UpdateOneID(b.ID).
				SetEbookStatus(book.EbookStatusDownloading).
				ExecX(f.ctx)
			rec := f.client.DownloadRecord.Create().SetTitle("x").SetBookID(b.ID).
				SetBookKind("ebook").
				SetSavePath("/dl").SetStatus("downloading").SaveX(f.ctx)
			f.dl.EXPECT().Queue(anyCtx).Return(download.QueueSnapshot{
				Items: []download.QueueEntry{{RecordID: rec.ID, Progress: 0.5}},
			}, nil).Once()

			page, err := f.svc.List(
				f.ctx,
				ListParams{Sort: "title", Page: 1, Limit: 1},
			)

			Expect(err).NotTo(HaveOccurred())
			Expect(page.Total).To(Equal(uint32(2)))
			Expect(page.Rows).To(HaveLen(1))
			Expect(page.Rows[0].Title).To(Equal("Elantris"))
			Expect(page.Progress).To(Equal(map[uint32]float64{b.ID: 50}))

			counts, err := f.svc.Counts(f.ctx, ListParams{})
			Expect(err).NotTo(HaveOccurred())
			Expect(counts.Total).To(Equal(uint32(2)))
			Expect(counts.Downloading).To(Equal(uint32(1)))
			Expect(counts.Wanted).To(Equal(uint32(1)))
		})
	})
})

var _ = Describe("Derived states", Label("unit", "books"), func() {
	now := time.Now()
	slot := func(monitored bool, status string) *ent.Book {
		return &ent.Book{
			EbookMonitored:     monitored,
			EbookStatus:        book.EbookStatus(status),
			AudiobookMonitored: monitored,
			AudiobookStatus:    book.AudiobookStatus(status),
		}
	}

	DescribeTable(
		"a slot's state",
		func(monitored bool, status, want string) {
			Expect(SlotState(slot(monitored, status), slotEbook)).To(Equal(want))
			Expect(SlotState(slot(monitored, status), slotAudiobook)).To(Equal(want))
		},
		Entry("monitored wanted", true, "wanted", StateWanted),
		Entry("monitored skipped", true, "skipped", StateWanted),
		Entry("monitored available", true, "available", StateAvailable),
		Entry("monitored downloading", true, "downloading", StateDownloading),
		Entry("monitored paused is in flight", true, "paused", StateDownloading),
		Entry("unmonitored wanted", false, "wanted", StateUnmonitored),
		Entry("unmonitored skipped", false, "skipped", StateUnmonitored),
		Entry("unmonitored paused", false, "paused", StateUnmonitored),
		Entry(
			"unmonitored available keeps its file",
			false,
			"available",
			StateAvailable,
		),
		Entry(
			"unmonitored downloading is not cancelled",
			false,
			"downloading",
			StateDownloading,
		),
	)

	It("derives a book's status over the slots that are not unmonitored", func() {
		b := &ent.Book{
			EbookMonitored: true, EbookStatus: book.EbookStatusAvailable,
			AudiobookMonitored: true, AudiobookStatus: book.AudiobookStatusWanted,
		}
		Expect(Status(b, now)).To(Equal(StateWanted))
		b.AudiobookStatus = book.AudiobookStatusDownloading
		Expect(Status(b, now)).To(Equal(StateDownloading))
		b.AudiobookMonitored, b.AudiobookStatus = false, book.AudiobookStatusSkipped
		Expect(Status(b, now)).To(Equal(StateAvailable))
		b.EbookMonitored, b.EbookStatus = false, book.EbookStatusSkipped
		Expect(Status(b, now)).To(Equal(StateAvailable))
	})

	It("ignores the slots of a book that is not released yet", func() {
		future := now.Add(time.Hour)
		b := &ent.Book{
			EbookMonitored: true, EbookStatus: book.EbookStatusWanted,
			ReleaseDate: &future,
		}
		Expect(Status(b, now)).To(Equal(StateAvailable))
	})

	It("derives a volume's status, upcoming while its date is ahead", func() {
		future := now.Add(time.Hour)
		v := slot(true, "wanted")
		Expect(VolumeStatus(v, now)).To(Equal(StateWanted))
		v.EbookStatus = book.EbookStatusDownloading
		Expect(VolumeStatus(v, now)).To(Equal(StateDownloading))
		v.EbookMonitored = false
		v.EbookStatus = book.EbookStatusSkipped
		Expect(VolumeStatus(v, now)).To(Equal(StateAvailable))
		v.ReleaseDate = &future
		Expect(VolumeStatus(v, now)).To(Equal(VolumeUpcoming))
	})

	It("derives a series' status from its hydrated, released volumes", func() {
		refreshed, future := now, now.Add(time.Hour)
		wanted := slot(true, "wanted")
		wanted.LastRefreshedAt = &refreshed
		stub := slot(true, "downloading")
		upcoming := slot(true, "downloading")
		upcoming.LastRefreshedAt, upcoming.ReleaseDate = &refreshed, &future
		s := &ent.BookSeries{
			Monitor: "all",
			Edges: ent.BookSeriesEdges{
				Volumes: []*ent.Book{wanted, stub, upcoming},
			},
		}

		Expect(SeriesStatus(s, now)).To(Equal(StateWanted))
		Expect(Hydrating(s)).To(BeTrue())
		s.Monitor = "none"
		Expect(SeriesStatus(s, now)).To(Equal(StateAvailable))
		s.Monitor = "all"
		wanted.EbookStatus = book.EbookStatusDownloading
		Expect(SeriesStatus(s, now)).To(Equal(StateDownloading))
	})
})
