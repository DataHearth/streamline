package book

import (
	"errors"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/book"
	"github.com/datahearth/streamline/ent/downloadrecord"
	"github.com/datahearth/streamline/ent/mediafile"
	"github.com/datahearth/streamline/internal/download"
	"github.com/datahearth/streamline/internal/indexer"
	"github.com/datahearth/streamline/internal/metadata"
	"github.com/datahearth/streamline/internal/testutil/configtest"
)

var _ = Describe("Book releases", Label("unit", "integration", "books"), func() {
	var f *fixture

	BeforeEach(func() { f = newFixture() })

	epub := indexer.SearchResult{
		Title:   "Brandon Sanderson - Elantris (2005) EPUB",
		Seeders: 5,
	}
	m4b := indexer.SearchResult{
		Title:   "Brandon Sanderson - Elantris Unabridged M4B 128kbps",
		Seeders: 9,
	}
	pdf := indexer.SearchResult{
		Title:   "Brandon Sanderson - Elantris PDF",
		Seeders: 50,
	}

	Describe("SearchBookReleases", func() {
		It(
			"searches by the slot edition's title, author and year, and scores against the profile",
			func() {
				b := f.addBook(1, "Elantris", "ebook")
				f.idx.EXPECT().
					SearchBook(anyCtx, "Brandon Sanderson", "Elantris", uint16(2005), mediafile.BookKindEbook).
					Return([]indexer.SearchResult{pdf, epub}, nil).Once()

				got, err := f.svc.SearchBookReleases(f.ctx, b.ID, "ebook")

				Expect(err).NotTo(HaveOccurred())
				Expect(got).To(HaveLen(2))
				// Accepted first, the rejected one last with its reason.
				Expect(got[0].Title).To(Equal(epub.Title))
				Expect(got[0].Slot).To(Equal("ebook"))
				Expect(got[0].Container).To(Equal("EPUB"))
				Expect(got[0].Rejected).To(BeFalse())
				Expect(got[0].Score).To(BeNumerically(">", 0))
				Expect(got[1].Rejected).To(BeTrue())
				Expect(got[1].Reason).To(Equal("PDF is not in the profile"))
			},
		)

		It(
			"queries the French edition's title when that is the slot's edition",
			func() {
				b := f.addBook(1, "Elantris", "ebook")
				fr := b.Edges.Editions[0]
				for _, e := range b.Edges.Editions {
					if e.Language == "fr" {
						fr = e
					}
				}
				Expect(
					f.store.SetBookSlotEdition(f.ctx, b.ID, "ebook", fr.ID),
				).To(Succeed())
				f.idx.EXPECT().
					SearchBook(anyCtx, "Brandon Sanderson", "Elantris (fr)", uint16(2008), mediafile.BookKindEbook).
					Return(nil, nil).Once()

				_, err := f.svc.SearchBookReleases(f.ctx, b.ID, "ebook")

				Expect(err).NotTo(HaveOccurred())
			},
		)

		It(
			"merges both slots when no kind is asked for, each release in its own slot",
			func() {
				b := f.addBook(1, "Elantris", "both")
				f.idx.EXPECT().
					SearchBook(anyCtx, "Brandon Sanderson", "Elantris", uint16(2005), mediafile.BookKindEbook).

					// An audiobook that leaked into the ebook categories belongs to
					// the other slot's search.
					Return([]indexer.SearchResult{epub, m4b}, nil).
					Once()
				f.idx.EXPECT().
					SearchBook(anyCtx, "Brandon Sanderson", "Elantris", uint16(2006), mediafile.BookKindAudiobook).
					Return([]indexer.SearchResult{m4b}, nil).
					Once()

				got, err := f.svc.SearchBookReleases(f.ctx, b.ID, "")

				Expect(err).NotTo(HaveOccurred())
				slots := map[string]string{}
				for _, r := range got {
					slots[r.Title] = r.Slot
				}
				Expect(
					slots,
				).To(Equal(map[string]string{epub.Title: "ebook", m4b.Title: "audiobook"}))
				Expect(got).To(HaveLen(2))
			},
		)

		It(
			"reads a stated audiobook rate and rejects one under the profile's floor",
			func() {
				b := f.addBook(1, "Elantris", "audiobook")
				low := indexer.SearchResult{Title: "Elantris Unabridged M4B 32kbps"}
				f.idx.EXPECT().
					SearchBook(anyCtx, "Brandon Sanderson", "Elantris", uint16(2006), mediafile.BookKindAudiobook).
					Return([]indexer.SearchResult{low, m4b}, nil).
					Once()

				got, err := f.svc.SearchBookReleases(f.ctx, b.ID, "audiobook")

				Expect(err).NotTo(HaveOccurred())
				Expect(got[0].BitrateKbps).To(Equal(uint32(128)))
				Expect(got[1].Rejected).To(BeTrue())
				Expect(
					got[1].Reason,
				).To(Equal("32 kbps is below the profile's minimum of 64"))
			},
		)

		It("rejects a collection", func() {
			b := f.addBook(1, "Elantris", "ebook")
			f.idx.EXPECT().
				SearchBook(anyCtx, "Brandon Sanderson", "Elantris", uint16(2005), mediafile.BookKindEbook).
				Return([]indexer.SearchResult{{Title: "Sanderson Complete Collection EPUB"}}, nil).
				Once()

			got, err := f.svc.SearchBookReleases(f.ctx, b.ID, "ebook")

			Expect(err).NotTo(HaveOccurred())
			Expect(got[0].Rejected).To(BeTrue())
			Expect(got[0].Reason).To(Equal("collection or box set"))
		})

		It("never filters on language interactively", func() {
			b := f.addBook(1, "Elantris", "ebook")
			french := indexer.SearchResult{
				Title: "Brandon Sanderson - Elantris FRENCH EPUB",
			}
			f.idx.EXPECT().
				SearchBook(anyCtx, "Brandon Sanderson", "Elantris", uint16(2005), mediafile.BookKindEbook).
				Return([]indexer.SearchResult{french}, nil).
				Once()

			got, err := f.svc.SearchBookReleases(f.ctx, b.ID, "ebook")

			Expect(err).NotTo(HaveOccurred())
			Expect(got[0].Rejected).To(BeFalse())
		})

		It(
			"resolves the profile of the book, then its series, then the default of its kind",
			func() {
				b := f.addBook(1, "Elantris", "ebook")
				// The kind's default is "std" (EPUB, CBZ); a comic default accepts CBZ only.
				f.client.Book.UpdateOneID(b.ID).SetKind(book.KindComic).ExecX(f.ctx)
				f.idx.EXPECT().
					SearchBook(anyCtx, "Brandon Sanderson", "Elantris", uint16(2005), mediafile.BookKindEbook).
					Return([]indexer.SearchResult{epub}, nil).
					Times(2)

				got, err := f.svc.SearchBookReleases(f.ctx, b.ID, "ebook")
				Expect(err).NotTo(HaveOccurred())
				Expect(got[0].Rejected).To(BeTrue())

				f.client.Book.UpdateOneID(b.ID).SetQualityProfile("std").ExecX(f.ctx)
				got, err = f.svc.SearchBookReleases(f.ctx, b.ID, "ebook")
				Expect(err).NotTo(HaveOccurred())
				Expect(got[0].Rejected).To(BeFalse())
			},
		)

		It(
			"follows the series' profile for a volume that has none of its own",
			func() {
				f.meta.EXPECT().GetSeries(anyCtx, uint32(50)).
					Return(seriesRecord("One Piece", 1), nil).Once()
				f.meta.EXPECT().GetBooks(anyCtx, ids(1001)).
					Return([]*metadata.BookRecord{volumeRec(1001)}, nil).Once()
				s, err := f.svc.AddSeries(
					f.ctx,
					AddSeriesParams{HardcoverID: 50, QualityProfile: "comics"},
				)
				Expect(err).NotTo(HaveOccurred())
				vol := s.Edges.Volumes[0]
				// The write-through has not reached this volume yet.
				f.client.Book.UpdateOneID(vol.ID).SetQualityProfile("").ExecX(f.ctx)
				cbz := indexer.SearchResult{Title: "Eiichiro Oda - Volume 1001 CBZ"}
				f.idx.EXPECT().
					SearchBook(anyCtx, "Eiichiro Oda", "Volume 1001", uint16(2005), mediafile.BookKindEbook).
					Return([]indexer.SearchResult{cbz, epub}, nil).
					Once()

				got, err := f.svc.SearchBookReleases(f.ctx, vol.ID, "ebook")

				Expect(err).NotTo(HaveOccurred())
				Expect(got[0].Title).To(Equal(cbz.Title))
				Expect(got[0].Rejected).To(BeFalse())
				Expect(got[1].Rejected).To(BeTrue())
			},
		)

		It("says so when no profile resolves", func() {
			b := f.addBook(1, "Elantris", "ebook")
			configtest.Setup(map[string]any{
				"book_quality_profiles":         []map[string]any{},
				"book_quality_default_profiles": map[string]any{},
			})

			_, err := f.svc.SearchBookReleases(f.ctx, b.ID, "ebook")

			Expect(err).To(MatchError(ErrNoQualityProfile))
		})

		It("refuses an unknown slot and an unknown book", func() {
			b := f.addBook(1, "Elantris", "ebook")
			_, err := f.svc.SearchBookReleases(f.ctx, b.ID, "paper")
			Expect(err).To(MatchError(ErrInvalidSlotKind))
			_, err = f.svc.SearchBookReleases(f.ctx, 404, "ebook")
			Expect(err).To(MatchError(ErrBookNotFound))
		})
	})

	Describe("GrabBookRelease", func() {
		It(
			"grabs for the slot the container fills and marks it downloading",
			func() {
				b := f.addBook(1, "Elantris", "both")
				f.dl.EXPECT().GrabBook(anyCtx, epub, b.ID, mediafile.BookKindEbook).
					Return(&ent.DownloadRecord{ID: 7}, nil).Once()

				Expect(
					f.svc.GrabBookRelease(f.ctx, b.ID, GrabParams{Result: epub}),
				).To(Succeed())

				got := f.reloadBook(b.ID)
				Expect(got.EbookStatus).To(Equal(book.EbookStatusDownloading))
				Expect(got.AudiobookStatus).To(Equal(book.AudiobookStatusWanted))
			},
		)

		It(
			"takes the slot from the query or the body when the container says nothing",
			func() {
				b := f.addBook(1, "Elantris", "both")
				opaque := indexer.SearchResult{Title: "Elantris"}
				f.dl.EXPECT().
					GrabBook(anyCtx, opaque, b.ID, mediafile.BookKindAudiobook).
					Return(&ent.DownloadRecord{}, nil).
					Once()

				Expect(f.svc.GrabBookRelease(f.ctx, b.ID, GrabParams{
					Kind: "audiobook", Result: opaque,
				})).To(Succeed())

				Expect(
					f.reloadBook(b.ID).AudiobookStatus,
				).To(Equal(book.AudiobookStatusDownloading))
			},
		)

		It("refuses a body whose slot disagrees with the container", func() {
			b := f.addBook(1, "Elantris", "both")

			err := f.svc.GrabBookRelease(
				f.ctx,
				b.ID,
				GrabParams{Slot: "ebook", Result: m4b},
			)
			Expect(err).To(MatchError(ErrSlotMismatch))
			err = f.svc.GrabBookRelease(
				f.ctx,
				b.ID,
				GrabParams{Kind: "audiobook", Result: epub},
			)
			Expect(err).To(MatchError(ErrSlotMismatch))

			Expect(f.reloadBook(b.ID).EbookStatus).To(Equal(book.EbookStatusWanted))
		})

		It("refuses when no slot can be told", func() {
			b := f.addBook(1, "Elantris", "both")
			err := f.svc.GrabBookRelease(
				f.ctx,
				b.ID,
				GrabParams{Result: indexer.SearchResult{Title: "Elantris"}},
			)
			Expect(err).To(MatchError(ErrInvalidSlotKind))
		})

		It("refuses a book that has not been hydrated", func() {
			stub := f.client.Book.Create().
				SetHardcoverID(9).
				SetTitle("Stub").
				SaveX(f.ctx)
			err := f.svc.GrabBookRelease(f.ctx, stub.ID, GrabParams{Result: epub})
			Expect(err).To(MatchError(ErrNotHydrated))
		})

		It("leaves an available slot alone", func() {
			b := f.addBook(1, "Elantris", "both")
			f.client.Book.UpdateOneID(b.ID).
				SetEbookStatus(book.EbookStatusAvailable).
				ExecX(f.ctx)
			f.dl.EXPECT().GrabBook(anyCtx, epub, b.ID, mediafile.BookKindEbook).
				Return(&ent.DownloadRecord{}, nil).Once()

			Expect(
				f.svc.GrabBookRelease(f.ctx, b.ID, GrabParams{Result: epub}),
			).To(Succeed())

			Expect(
				f.reloadBook(b.ID).EbookStatus,
			).To(Equal(book.EbookStatusAvailable))
		})

		It("tells the importer to replace when the caller asked for it", func() {
			b := f.addBook(1, "Elantris", "both")
			rec := f.client.DownloadRecord.Create().SetTitle("x").SetBookID(b.ID).
				SetBookKind(downloadrecord.BookKindEbook).SetSavePath("/dl").
				SetStatus(downloadrecord.StatusDownloading).SaveX(f.ctx)
			f.dl.EXPECT().GrabBook(anyCtx, epub, b.ID, mediafile.BookKindEbook).
				Return(rec, nil).Once()

			Expect(f.svc.GrabBookRelease(f.ctx, b.ID, GrabParams{
				Result: epub, ReplaceExisting: true,
			})).To(Succeed())

			Expect(f.client.DownloadRecord.GetX(f.ctx, rec.ID).ReplaceMode).
				To(Equal(downloadrecord.ReplaceModeAll))
		})

		It("tells the importer to replace after an edition change", func() {
			b := f.addBook(1, "Elantris", "both")
			f.client.Book.UpdateOneID(b.ID).
				SetEbookReplacingLanguage("en").
				ExecX(f.ctx)
			rec := f.client.DownloadRecord.Create().SetTitle("x").SetBookID(b.ID).
				SetBookKind(downloadrecord.BookKindEbook).SetSavePath("/dl").
				SetStatus(downloadrecord.StatusDownloading).SaveX(f.ctx)
			f.dl.EXPECT().GrabBook(anyCtx, epub, b.ID, mediafile.BookKindEbook).
				Return(rec, nil).Once()

			Expect(
				f.svc.GrabBookRelease(f.ctx, b.ID, GrabParams{Result: epub}),
			).To(Succeed())

			Expect(f.client.DownloadRecord.GetX(f.ctx, rec.ID).ReplaceMode).
				To(Equal(downloadrecord.ReplaceModeAll))
		})

		It(
			"passes the download manager's refusal through and leaves the slot as it was",
			func() {
				b := f.addBook(1, "Elantris", "both")
				f.dl.EXPECT().GrabBook(anyCtx, epub, b.ID, mediafile.BookKindEbook).
					Return(nil, download.ErrUntrustedSource).Once()

				err := f.svc.GrabBookRelease(f.ctx, b.ID, GrabParams{Result: epub})

				Expect(err).To(MatchError(download.ErrUntrustedSource))
				Expect(
					f.reloadBook(b.ID).EbookStatus,
				).To(Equal(book.EbookStatusWanted))
			},
		)
	})

	Describe("SearchMissing", func() {
		It("grabs the best accepted release of every wanted slot", func() {
			b := f.addBook(1, "Elantris", "both")
			f.idx.EXPECT().
				SearchBook(anyCtx, "Brandon Sanderson", "Elantris", uint16(2005), mediafile.BookKindEbook).
				Return([]indexer.SearchResult{pdf, epub}, nil).
				Once()
			f.idx.EXPECT().
				SearchBook(anyCtx, "Brandon Sanderson", "Elantris", uint16(2006), mediafile.BookKindAudiobook).
				Return([]indexer.SearchResult{m4b}, nil).
				Once()
			f.dl.EXPECT().GrabBook(anyCtx, epub, b.ID, mediafile.BookKindEbook).
				Return(&ent.DownloadRecord{}, nil).Once()
			f.dl.EXPECT().GrabBook(anyCtx, m4b, b.ID, mediafile.BookKindAudiobook).
				Return(&ent.DownloadRecord{}, nil).Once()

			Expect(f.svc.SearchMissing(f.ctx)).To(Succeed())

			got := f.reloadBook(b.ID)
			Expect(got.EbookStatus).To(Equal(book.EbookStatusDownloading))
			Expect(got.AudiobookStatus).To(Equal(book.AudiobookStatusDownloading))
			Expect(got.EbookLastSearchAt).NotTo(BeNil())
		})

		It(
			"skips a release in another language than the edition, and an untagged one is fine",
			func() {
				b := f.addBook(1, "Elantris", "ebook")
				french := indexer.SearchResult{
					Title:   "Brandon Sanderson - Elantris FRENCH EPUB",
					Seeders: 99,
				}
				f.idx.EXPECT().
					SearchBook(anyCtx, "Brandon Sanderson", "Elantris", uint16(2005), mediafile.BookKindEbook).
					Return([]indexer.SearchResult{french, epub}, nil).
					Once()
				f.dl.EXPECT().GrabBook(anyCtx, epub, b.ID, mediafile.BookKindEbook).
					Return(&ent.DownloadRecord{}, nil).Once()

				Expect(f.svc.SearchMissing(f.ctx)).To(Succeed())
			},
		)

		It(
			"stamps the search and counts no strike when nothing is accepted",
			func() {
				b := f.addBook(1, "Elantris", "ebook")
				f.idx.EXPECT().
					SearchBook(anyCtx, "Brandon Sanderson", "Elantris", uint16(2005), mediafile.BookKindEbook).
					Return([]indexer.SearchResult{pdf}, nil).
					Once()

				Expect(f.svc.SearchMissing(f.ctx)).To(Succeed())

				got := f.reloadBook(b.ID)
				Expect(got.EbookLastSearchAt).NotTo(BeNil())
				Expect(got.EbookGrabFailures).To(BeZero())
				Expect(got.EbookStatus).To(Equal(book.EbookStatusWanted))
			},
		)

		It(
			"counts a strike for a refused grab and none for a transport failure",
			func() {
				b := f.addBook(1, "Elantris", "ebook")
				f.idx.EXPECT().
					SearchBook(anyCtx, "Brandon Sanderson", "Elantris", uint16(2005), mediafile.BookKindEbook).
					Return([]indexer.SearchResult{epub}, nil).
					Twice()
				f.dl.EXPECT().GrabBook(anyCtx, epub, b.ID, mediafile.BookKindEbook).
					Return(nil, download.ErrNoWantedFiles).Once()
				f.dl.EXPECT().GrabBook(anyCtx, epub, b.ID, mediafile.BookKindEbook).
					Return(nil, download.ErrUnreachable).Once()

				Expect(f.svc.SearchMissing(manualContext())).To(Succeed())
				Expect(f.reloadBook(b.ID).EbookGrabFailures).To(Equal(uint8(1)))
				Expect(f.svc.SearchMissing(manualContext())).To(Succeed())
				Expect(f.reloadBook(b.ID).EbookGrabFailures).To(Equal(uint8(1)))
			},
		)

		It("does nothing without an enabled download client", func() {
			f.addBook(1, "Elantris", "ebook")
			cfg := bookConfig()
			cfg["download_clients"] = []map[string]any{}
			configtest.Setup(cfg)

			Expect(f.svc.SearchMissing(f.ctx)).To(Succeed())
		})

		It("never searches an unreleased book or a stub", func() {
			b := f.addBook(1, "Elantris", "both")
			f.client.Book.UpdateOneID(b.ID).
				SetReleaseDate(time.Now().Add(24 * time.Hour)).
				ExecX(f.ctx)
			f.client.Book.Create().SetHardcoverID(9).SetTitle("Stub").
				SetEbookMonitored(true).
				SetEbookStatus(book.EbookStatusWanted).SaveX(f.ctx)

			Expect(f.svc.SearchMissing(f.ctx)).To(Succeed())
		})

		It("leaves a book that is in cooldown alone", func() {
			b := f.addBook(1, "Elantris", "ebook")
			f.client.Book.UpdateOneID(b.ID).
				SetEbookLastSearchAt(time.Now()).
				ExecX(f.ctx)

			Expect(f.svc.SearchMissing(f.ctx)).To(Succeed())
		})
	})

	Describe("series volumes", func() {
		add := func(n int) *ent.BookSeries {
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
			s, err := f.svc.AddSeries(
				f.ctx,
				AddSeriesParams{HardcoverID: 50, QualityProfile: "comics"},
			)
			Expect(err).NotTo(HaveOccurred())
			return s
		}
		tome := func(n string) indexer.SearchResult {
			return indexer.SearchResult{
				Title:   "One Piece T" + n + " CBZ",
				Seeders: 5,
			}
		}

		It(
			"searches a whole series with one query and gives each volume its release",
			func() {
				s := add(3)
				f.idx.EXPECT().
					SearchBook(anyCtx, "Eiichiro Oda", "One Piece", uint16(0), mediafile.BookKindEbook).
					Return([]indexer.SearchResult{
						tome("03"), tome("01"), tome("02"),
						{Title: "One Piece Tomes 1-3 CBZ"},
						{Title: "Two Piece T01 CBZ"},
						{Title: "One Piece T01 EPUB"},
					}, nil).
					Once()
				for i, v := range s.Edges.Volumes {
					item := tome([]string{"01", "02", "03"}[i])
					f.dl.EXPECT().
						GrabBook(anyCtx, item, v.ID, mediafile.BookKindEbook).
						Return(&ent.DownloadRecord{}, nil).
						Once()
				}

				Expect(f.svc.SearchMissing(f.ctx)).To(Succeed())

				for _, v := range f.reloadSeries(s.ID).Edges.Volumes {
					Expect(v.EbookStatus).To(Equal(book.EbookStatusDownloading))
					Expect(v.EbookLastSearchAt).NotTo(BeNil())
				}
			},
		)

		It(
			"makes at most three fallback queries for the volumes the series query missed",
			func() {
				s := add(5)
				f.idx.EXPECT().
					SearchBook(anyCtx, "Eiichiro Oda", "One Piece", uint16(0), mediafile.BookKindEbook).
					Return(nil, nil).
					Once()
				for _, label := range []string{"1", "2", "3"} {
					f.idx.EXPECT().
						SearchBook(anyCtx, "Eiichiro Oda", "One Piece "+label, uint16(0), mediafile.BookKindEbook).
						Return(nil, nil).
						Once()
				}

				Expect(f.svc.SearchMissing(f.ctx)).To(Succeed())

				for _, v := range f.reloadSeries(s.ID).Edges.Volumes {
					// Every volume was covered by the series query, so all are stamped.
					Expect(v.EbookLastSearchAt).NotTo(BeNil())
				}
			},
		)

		It("takes a fallback query's hit for the volume it names", func() {
			s := add(1)
			f.idx.EXPECT().
				SearchBook(anyCtx, "Eiichiro Oda", "One Piece", uint16(0), mediafile.BookKindEbook).
				Return(nil, nil).
				Once()
			f.idx.EXPECT().
				SearchBook(anyCtx, "Eiichiro Oda", "One Piece 1", uint16(0), mediafile.BookKindEbook).
				Return([]indexer.SearchResult{tome("01")}, nil).
				Once()
			f.dl.EXPECT().
				GrabBook(anyCtx, tome("01"), s.Edges.Volumes[0].ID, mediafile.BookKindEbook).
				Return(&ent.DownloadRecord{}, nil).
				Once()

			Expect(f.svc.SearchMissing(f.ctx)).To(Succeed())

			Expect(
				f.reloadSeries(s.ID).Edges.Volumes[0].EbookStatus,
			).To(Equal(book.EbookStatusDownloading))
		})

		It("skips a release in another language than the volume's edition", func() {
			s := add(1)
			f.idx.EXPECT().
				SearchBook(anyCtx, "Eiichiro Oda", "One Piece", uint16(0), mediafile.BookKindEbook).
				Return([]indexer.SearchResult{{Title: "One Piece T01 FRENCH CBZ"}}, nil).
				Once()
			f.idx.EXPECT().
				SearchBook(anyCtx, "Eiichiro Oda", "One Piece 1", uint16(0), mediafile.BookKindEbook).
				Return(nil, nil).
				Once()

			Expect(f.svc.SearchMissing(f.ctx)).To(Succeed())

			Expect(
				f.reloadSeries(s.ID).Edges.Volumes[0].EbookStatus,
			).To(Equal(book.EbookStatusWanted))
		})

		It("never searches a volume still hydrating", func() {
			s := add(1)
			stub := f.client.Book.Create().
				SetHardcoverID(1002).
				SetTitle("One Piece #2").
				SetSeriesID(s.ID).
				SetSeriesPosition(2).
				SetEbookMonitored(true).
				SetEbookStatus(book.EbookStatusWanted).
				SaveX(f.ctx)
			f.idx.EXPECT().
				SearchBook(anyCtx, "Eiichiro Oda", "One Piece", uint16(0), mediafile.BookKindEbook).
				Return(nil, nil).
				Once()
			f.idx.EXPECT().
				SearchBook(anyCtx, "Eiichiro Oda", "One Piece 1", uint16(0), mediafile.BookKindEbook).
				Return(nil, nil).
				Once()

			Expect(f.svc.SearchMissing(f.ctx)).To(Succeed())

			Expect(f.reloadBook(stub.ID).EbookLastSearchAt).To(BeNil())
		})

		It("does not fail the pass when a series search errors", func() {
			s := add(1)
			f.idx.EXPECT().
				SearchBook(anyCtx, "Eiichiro Oda", "One Piece", uint16(0), mediafile.BookKindEbook).
				Return(nil, errors.New("indexer down")).
				Once()

			Expect(f.svc.SearchMissing(f.ctx)).To(Succeed())

			Expect(
				f.reloadSeries(s.ID).Edges.Volumes[0].EbookLastSearchAt,
			).To(BeNil())
		})
	})

	Describe("search now", func() {
		It(
			"reports the monitored wanted slots it will search and searches them",
			func() {
				b := f.addBook(1, "Elantris", "both")
				f.idx.EXPECT().
					SearchBook(anyCtx, "Brandon Sanderson", "Elantris", uint16(2005), mediafile.BookKindEbook).
					Return([]indexer.SearchResult{epub}, nil).
					Once()
				f.dl.EXPECT().GrabBook(anyCtx, epub, b.ID, mediafile.BookKindEbook).
					Return(&ent.DownloadRecord{}, nil).Once()

				queued, err := f.svc.SearchNowBook(f.ctx, b.ID, "ebook")

				Expect(err).NotTo(HaveOccurred())
				Expect(queued).To(Equal(uint32(1)))
				Eventually(func() book.EbookStatus {
					return f.reloadBook(b.ID).EbookStatus
				}).Should(Equal(book.EbookStatusDownloading))
			},
		)

		It(
			"counts both slots when no kind is given, none that are not wanted",
			func() {
				b := f.addBook(1, "Elantris", "ebook")
				f.client.Book.UpdateOneID(b.ID).
					SetEbookStatus(book.EbookStatusAvailable).
					ExecX(f.ctx)

				queued, err := f.svc.SearchNowBook(f.ctx, b.ID, "")

				Expect(err).NotTo(HaveOccurred())
				Expect(queued).To(BeZero())
			},
		)

		It("queues nothing for an unreleased book", func() {
			b := f.addBook(1, "Elantris", "both")
			f.client.Book.UpdateOneID(b.ID).
				SetReleaseDate(time.Now().Add(time.Hour)).
				ExecX(f.ctx)

			queued, err := f.svc.SearchNowBook(f.ctx, b.ID, "")

			Expect(err).NotTo(HaveOccurred())
			Expect(queued).To(BeZero())
		})

		It("refuses an unknown slot and an unknown book", func() {
			b := f.addBook(1, "Elantris", "both")
			_, err := f.svc.SearchNowBook(f.ctx, b.ID, "paper")
			Expect(err).To(MatchError(ErrInvalidSlotKind))
			_, err = f.svc.SearchNowBook(f.ctx, 404, "")
			Expect(err).To(MatchError(ErrBookNotFound))
			_, err = f.svc.SearchNowSeries(f.ctx, 404)
			Expect(err).To(MatchError(ErrSeriesNotFound))
		})

		It("counts the wanted volumes of a series", func() {
			f.meta.EXPECT().GetSeries(anyCtx, uint32(50)).
				Return(seriesRecord("One Piece", 2), nil).Once()
			f.meta.EXPECT().GetBooks(anyCtx, ids(1001, 1002)).
				Return([]*metadata.BookRecord{volumeRec(1001), volumeRec(1002)}, nil).
				Once()
			s, err := f.svc.AddSeries(
				f.ctx,
				AddSeriesParams{HardcoverID: 50, QualityProfile: "comics"},
			)
			Expect(err).NotTo(HaveOccurred())
			f.client.Book.UpdateOneID(s.Edges.Volumes[0].ID).
				SetEbookStatus(book.EbookStatusAvailable).
				ExecX(f.ctx)
			f.idx.EXPECT().
				SearchBook(anyCtx, "Eiichiro Oda", "One Piece", uint16(0), mediafile.BookKindEbook).
				Return(nil, nil).
				Once()
			f.idx.EXPECT().
				SearchBook(anyCtx, "Eiichiro Oda", "One Piece 2", uint16(0), mediafile.BookKindEbook).
				Return(nil, nil).
				Once()

			queued, err := f.svc.SearchNowSeries(f.ctx, s.ID)

			Expect(err).NotTo(HaveOccurred())
			Expect(queued).To(Equal(uint32(1)))
			Eventually(func() *time.Time {
				return f.reloadSeries(s.ID).Edges.Volumes[1].EbookLastSearchAt
			}).ShouldNot(BeNil())
		})
	})

	It("reads its profile from the configured default for the book's kind", func() {
		cfg := bookConfig()
		cfg["book_quality_default_profiles"] = map[string]any{
			"novel": "comics", "bd": "std", "comic": "std", "manga": "std",
		}
		configtest.Setup(cfg)
		b := f.addBook(1, "Elantris", "ebook")

		p, ok := ProfileFor(b)

		Expect(ok).To(BeTrue())
		Expect(p.Name).To(Equal("comics"))
	})
})
