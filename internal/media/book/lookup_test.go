package book

import (
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/internal/metadata"
)

var _ = Describe("Book lookup", Label("unit", "integration", "books"), func() {
	var f *fixture

	BeforeEach(func() { f = newFixture() })

	bookHit := func(id uint32, title string) metadata.BookLookupHit {
		return metadata.BookLookupHit{
			HardcoverID: id, Type: "book", Title: title, Author: "Someone",
			Year: 2005, Genres: []string{"Fantasy"}, ImageURL: "https://img/x.jpg",
		}
	}
	seriesHit := func(id uint32, name string) metadata.BookLookupHit {
		ongoing := true
		return metadata.BookLookupHit{
			HardcoverID: id, Type: "series", Title: name, Author: "Someone",
			Volumes: 12, Ongoing: &ongoing,
		}
	}

	Describe("Lookup", func() {
		It(
			"puts the series named like the query first, then books, then the other series",
			func() {
				f.meta.EXPECT().LookupBooks(anyCtx, "one piece").
					Return([]metadata.BookLookupHit{bookHit(1, "One Piece Guide"), bookHit(2, "Another")}, nil).
					Once()
				f.meta.EXPECT().LookupSeries(anyCtx, "one piece").
					Return([]metadata.BookLookupHit{
						seriesHit(10, "Piece of Cake"),
						seriesHit(11, "One Piece"),
						seriesHit(12, "One Piece: Color Walk"),
					}, nil).Once()

				hits, err := f.svc.Lookup(f.ctx, "one piece", lookupAll)

				Expect(err).NotTo(HaveOccurred())
				titles := make([]string, 0, len(hits))
				for _, h := range hits {
					titles = append(titles, h.Type+":"+h.Title)
				}
				Expect(titles).To(Equal([]string{
					"series:One Piece", "series:One Piece: Color Walk",
					"book:One Piece Guide", "book:Another", "series:Piece of Cake",
				}))
			},
		)

		It(
			"puts a hit credited to the author the query names ahead of a mis-credited one",
			func() {
				wrong := bookHit(1, "Ted Chiang - Exhalation Stories")
				wrong.Author = "F. Scott Fitzgerald"
				right := bookHit(2, "Exhalation")
				right.Author = "Ted Chiang"
				unrelated := bookHit(3, "Something Else")
				f.meta.EXPECT().LookupBooks(anyCtx, "Exhalation Ted Chiang").
					Return([]metadata.BookLookupHit{wrong, unrelated, right}, nil).
					Once()

				hits, err := f.svc.Lookup(f.ctx, "Exhalation Ted Chiang", lookupBook)

				Expect(err).NotTo(HaveOccurred())
				ids := make([]uint32, 0, len(hits))
				for _, h := range hits {
					ids = append(ids, h.HardcoverID)
				}
				Expect(ids).To(Equal([]uint32{2, 1, 3}))
			},
		)

		It("asks Hardcover for one kind only when told to", func() {
			f.meta.EXPECT().LookupBooks(anyCtx, "elantris").
				Return([]metadata.BookLookupHit{bookHit(1, "Elantris")}, nil).Once()

			hits, err := f.svc.Lookup(f.ctx, "elantris", lookupBook)

			Expect(err).NotTo(HaveOccurred())
			Expect(hits).To(HaveLen(1))
		})

		It("caps the answer at twenty", func() {
			many := make([]metadata.BookLookupHit, 0, 30)
			for i := range 30 {
				many = append(many, bookHit(uint32(i+1), fmt.Sprintf("Book %d", i)))
			}
			f.meta.EXPECT().LookupBooks(anyCtx, "book").Return(many, nil).Once()

			hits, err := f.svc.Lookup(f.ctx, "book", lookupBook)

			Expect(err).NotTo(HaveOccurred())
			Expect(hits).To(HaveLen(20))
		})

		It("classifies a book from the genres its search document carries", func() {
			manga := bookHit(1, "Berserk")
			manga.Genres = []string{"Manga"}
			f.meta.EXPECT().LookupBooks(anyCtx, "berserk").
				Return([]metadata.BookLookupHit{manga, bookHit(2, "Plain")}, nil).
				Once()

			hits, err := f.svc.Lookup(f.ctx, "berserk", lookupBook)

			Expect(err).NotTo(HaveOccurred())
			Expect(hits[0].Kind).To(Equal("manga"))
			Expect(hits[1].Kind).To(Equal("novel"))
		})

		It(
			"leaves a series' kind out when its search document says nothing",
			func() {
				f.meta.EXPECT().LookupSeries(anyCtx, "one").
					Return([]metadata.BookLookupHit{seriesHit(11, "One Piece")}, nil).
					Once()

				hits, err := f.svc.Lookup(f.ctx, "one", lookupSeries)

				Expect(err).NotTo(HaveOccurred())
				Expect(hits[0].Kind).To(BeEmpty())
				Expect(*hits[0].Ongoing).To(BeTrue())
				Expect(hits[0].Volumes).To(Equal(uint32(12)))
			},
		)

		It(
			"marks what the library already holds, with the cover and year of a series",
			func() {
				added := f.addBook(1, "Elantris", "")
				f.meta.EXPECT().GetSeries(anyCtx, uint32(50)).
					Return(seriesRecord("One Piece", 1), nil).Once()
				f.meta.EXPECT().GetBooks(anyCtx, ids(1001)).
					Return([]*metadata.BookRecord{volumeRec(1001)}, nil).Once()
				series, err := f.svc.AddSeries(
					f.ctx,
					AddSeriesParams{HardcoverID: 50},
				)
				Expect(err).NotTo(HaveOccurred())
				f.meta.EXPECT().LookupBooks(anyCtx, "x").
					Return([]metadata.BookLookupHit{bookHit(1, "Elantris"), bookHit(2, "Other")}, nil).
					Once()
				f.meta.EXPECT().LookupSeries(anyCtx, "x").
					Return([]metadata.BookLookupHit{seriesHit(50, "One Piece"), seriesHit(51, "New")}, nil).
					Once()

				hits, err := f.svc.Lookup(f.ctx, "x", lookupAll)

				Expect(err).NotTo(HaveOccurred())
				byTitle := map[string]LookupHit{}
				for _, h := range hits {
					byTitle[h.Title] = h
				}
				Expect(byTitle["Elantris"].AlreadyAdded).To(BeTrue())
				Expect(byTitle["Elantris"].LibraryID).To(Equal(added.ID))
				Expect(byTitle["Other"].AlreadyAdded).To(BeFalse())
				Expect(byTitle["Other"].LibraryID).To(BeZero())
				Expect(byTitle["One Piece"].AlreadyAdded).To(BeTrue())
				Expect(byTitle["One Piece"].LibraryID).To(Equal(series.ID))
				Expect(
					byTitle["One Piece"].CoverID,
				).To(Equal(series.Edges.Volumes[0].ID))
				Expect(byTitle["One Piece"].Year).To(Equal(uint16(2005)))
				Expect(byTitle["New"].AlreadyAdded).To(BeFalse())
			},
		)

		It("says so when there is no Hardcover key", func() {
			svc := NewService(f.store, nil, f.posters, f.idx, f.dl)
			_, err := svc.Lookup(f.ctx, "x", lookupAll)
			Expect(err).To(MatchError(ErrNotConfigured))
		})

		It("passes a rate limit through", func() {
			f.meta.EXPECT().LookupBooks(anyCtx, "x").
				Return(nil, &metadata.RateLimitedError{}).Once()
			_, err := f.svc.Lookup(f.ctx, "x", lookupBook)
			Expect(err).To(MatchError(metadata.ErrRateLimited))
		})
	})

	Describe("LookupDetail", func() {
		It(
			"describes a book with its editions, pages and genres, titled in the library language",
			func() {
				r := rec(1, "Elantris")
				r.Genres = []string{"Fantasy", "Epic", "Fiction", "Magic"}
				r.Editions[0].Pages = 492
				r.Editions[1].Pages = 600
				f.meta.EXPECT().GetBooks(anyCtx, ids(1)).
					Return([]*metadata.BookRecord{r}, nil).Once()

				d, err := f.svc.LookupDetail(f.ctx, lookupBook, 1)

				Expect(err).NotTo(HaveOccurred())
				Expect(d.Type).To(Equal("book"))
				Expect(d.Title).To(Equal("Elantris"))
				Expect(d.Author).To(Equal("Brandon Sanderson"))
				Expect(d.Kind).To(Equal("novel"))
				Expect(d.Year).To(Equal(uint16(2005)))
				Expect(d.Overview).To(Equal("About Elantris"))
				Expect(d.Genres).To(Equal([]string{"Fantasy", "Epic", "Fiction"}))
				// The original-language edition's page count, not the largest.
				Expect(d.Pages).To(Equal(uint16(492)))
				Expect(d.Editions).To(HaveLen(3))
				Expect(d.Editions[0]).To(Equal(LookupEdition{
					Language:  "en",
					Format:    "ebook",
					Publisher: "Tor",
					Year:      2005,
					Original:  true,
				}))
				Expect(d.AlreadyAdded).To(BeFalse())
			},
		)

		It("is memoised, with the library join read fresh", func() {
			f.meta.EXPECT().GetBooks(anyCtx, ids(1)).
				Return([]*metadata.BookRecord{rec(1, "Elantris")}, nil).Once()

			first, err := f.svc.LookupDetail(f.ctx, lookupBook, 1)
			Expect(err).NotTo(HaveOccurred())
			Expect(first.AlreadyAdded).To(BeFalse())

			// The add reads the same book from the provider's own memo; here it
			// is the mock, so it is asked once more.
			f.meta.EXPECT().GetBooks(anyCtx, ids(1)).
				Return([]*metadata.BookRecord{rec(1, "Elantris")}, nil).Once()
			added, err := f.svc.AddBook(f.ctx, AddBookParams{HardcoverID: 1})
			Expect(err).NotTo(HaveOccurred())

			second, err := f.svc.LookupDetail(f.ctx, lookupBook, 1)
			Expect(err).NotTo(HaveOccurred())
			Expect(second.AlreadyAdded).To(BeTrue())
			Expect(second.LibraryID).To(Equal(added.ID))
		})

		It("answers a book Hardcover does not know", func() {
			f.meta.EXPECT().GetBooks(anyCtx, ids(404)).
				Return([]*metadata.BookRecord{}, nil).Once()
			_, err := f.svc.LookupDetail(f.ctx, lookupBook, 404)
			Expect(err).To(MatchError(ErrHardcoverNotFound))
		})

		It(
			"describes a series from its skeleton and one batch of its first volumes",
			func() {
				f.meta.EXPECT().GetSeries(anyCtx, uint32(50)).
					Return(seriesRecord("One Piece", 10), nil).Once()
				recs := []*metadata.BookRecord{}
				for i := uint32(1); i <= 6; i++ {
					recs = append(recs, volumeRec(1000+i))
				}
				recs[0].Genres = []string{"Manga"}
				f.meta.EXPECT().
					GetBooks(anyCtx, ids(1001, 1002, 1003, 1004, 1005, 1006)).
					Return(recs, nil).
					Once()

				d, err := f.svc.LookupDetail(f.ctx, lookupSeries, 50)

				Expect(err).NotTo(HaveOccurred())
				Expect(d.Type).To(Equal("series"))
				Expect(d.Title).To(Equal("One Piece"))
				Expect(d.Author).To(Equal("Eiichiro Oda"))
				Expect(d.Volumes).To(Equal(uint32(10)))
				Expect(*d.Ongoing).To(BeTrue())
				Expect(d.Kind).To(Equal("manga"))
				Expect(
					d.VolumeBookIDs,
				).To(Equal([]uint32{1001, 1002, 1003, 1004, 1005, 1006}))
				// Distinct (language, publisher) of the first volume's ebooks.
				Expect(d.Editions).To(HaveLen(2))
				Expect(d.Editions[0].Format).To(Equal("ebook"))
			},
		)

		It("answers a series Hardcover does not know", func() {
			f.meta.EXPECT().GetSeries(anyCtx, uint32(404)).Return(nil, nil).Once()
			_, err := f.svc.LookupDetail(f.ctx, lookupSeries, 404)
			Expect(err).To(MatchError(ErrHardcoverNotFound))
		})
	})
})
