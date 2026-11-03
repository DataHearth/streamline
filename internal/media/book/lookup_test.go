package book

import (
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/stretchr/testify/mock"

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
				f.meta.EXPECT().GetSeriesLookup(anyCtx, ids(10, 11, 12)).
					Return([]metadata.SeriesLookupRecord{
						seriesRec(10, "Piece of Cake", 3),
						seriesRec(11, "One Piece", 3),
						seriesRec(12, "One Piece: Color Walk", 3),
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
			elantris := bookHit(1, "Elantris")
			elantris.SeriesIDs = []uint32{5}
			f.meta.EXPECT().LookupBooks(anyCtx, "elantris").
				Return([]metadata.BookLookupHit{elantris}, nil).Once()

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
				f.meta.EXPECT().LookupBooks(anyCtx, "one").
					Return([]metadata.BookLookupHit{bookHit(1, "Unrelated")}, nil).
					Once()
				f.meta.EXPECT().LookupSeries(anyCtx, "one").
					Return([]metadata.BookLookupHit{seriesHit(11, "One Piece")}, nil).
					Once()
				f.meta.EXPECT().GetSeriesLookup(anyCtx, ids(11)).
					Return([]metadata.SeriesLookupRecord{seriesRec(11, "One Piece", 12)}, nil).
					Once()

				hits, err := f.svc.Lookup(f.ctx, "one", lookupSeries)

				Expect(err).NotTo(HaveOccurred())
				Expect(hits).To(HaveLen(1))
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
				f.meta.EXPECT().GetSeriesLookup(anyCtx, mock.Anything).
					Return([]metadata.SeriesLookupRecord{
						seriesRec(50, "One Piece", 3), seriesRec(51, "New", 3),
					}, nil).Once()

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

		Describe("series reached through a book", func() {
			foundation := func() metadata.BookLookupHit {
				b := bookHit(188628, "Foundation")
				b.Author = "Isaac Asimov"
				b.AlternativeTitles = []string{"Fondation", "Fundação"}
				b.SeriesIDs = []uint32{5450, 10331, 60563}
				return b
			}
			hitIDs := func(hits []LookupHit) []string {
				out := make([]string, 0, len(hits))
				for _, h := range hits {
					out = append(out, h.Type+":"+itoa(h.HardcoverID))
				}
				return out
			}

			It(
				"lists every series of a book whose translated title is the query, without the empty duplicates",
				func() {
					f.meta.EXPECT().LookupBooks(anyCtx, "Fondation").
						Return([]metadata.BookLookupHit{foundation()}, nil).Once()
					f.meta.EXPECT().LookupSeries(anyCtx, "Fondation").
						Return([]metadata.BookLookupHit{seriesHit(39538, "Fondation")}, nil).
						Once()
					empty := seriesRec(39538, "Fondation", 0)
					done := seriesRec(5450, "Foundation", 7)
					done.Ongoing = new(false)
					running := seriesRec(
						10331,
						"Foundation (Chronological Order)",
						7,
					)
					running.Ongoing = new(true)
					f.meta.EXPECT().
						GetSeriesLookup(anyCtx, ids(5450, 10331, 39538, 60563)).
						Return([]metadata.SeriesLookupRecord{
							done,
							running,
							empty,
							seriesRec(60563, "Greater Foundation Universe", 15),
						}, nil).Once()

					hits, err := f.svc.Lookup(f.ctx, "Fondation", lookupAll)

					Expect(err).NotTo(HaveOccurred())
					Expect(hitIDs(hits)).To(Equal([]string{
						"series:5450", "series:10331", "series:60563", "book:188628",
					}))
					Expect(hits[0].Title).To(Equal("Foundation"))
					Expect(hits[0].Author).To(Equal("Isaac Asimov"))
					Expect(hits[0].Volumes).To(Equal(uint32(7)))
					Expect(hits[0].Slug).To(Equal("slug-5450"))
					Expect(hits[0].MatchedTitle).To(Equal("Fondation"))
					Expect(hits[0].Ongoing).To(Equal(new(false)))
					Expect(hits[1].Ongoing).To(Equal(new(true)))
					Expect(hits[2].Volumes).To(Equal(uint32(15)))
					Expect(hits[2].MatchedTitle).To(Equal("Fondation"))
					Expect(hits[3].MatchedTitle).To(BeEmpty())
				},
			)

			It("drops an empty series the batch did not return", func() {
				empty := seriesHit(39538, "Fondation")
				empty.PrimaryBooks = new(uint32(0))
				f.meta.EXPECT().LookupBooks(anyCtx, "Fondation").
					Return([]metadata.BookLookupHit{foundation()}, nil).Once()
				f.meta.EXPECT().LookupSeries(anyCtx, "Fondation").
					Return([]metadata.BookLookupHit{empty}, nil).Once()
				f.meta.EXPECT().
					GetSeriesLookup(anyCtx, ids(5450, 10331, 39538, 60563)).
					Return([]metadata.SeriesLookupRecord{
						seriesRec(5450, "Foundation", 7),
						seriesRec(10331, "Foundation (Chronological Order)", 7),
						seriesRec(60563, "Greater Foundation Universe", 15),
					}, nil).Once()

				hits, err := f.svc.Lookup(f.ctx, "Fondation", lookupAll)

				Expect(err).NotTo(HaveOccurred())
				Expect(hitIDs(hits)).NotTo(ContainElement("series:39538"))
			})

			It("lists a series only once, at its name-matched place", func() {
				f.meta.EXPECT().LookupBooks(anyCtx, "foundation").
					Return([]metadata.BookLookupHit{foundation()}, nil).Once()
				f.meta.EXPECT().LookupSeries(anyCtx, "foundation").
					Return([]metadata.BookLookupHit{
						seriesHit(60563, "Greater Foundation Universe"),
						seriesHit(5450, "Foundation"),
					}, nil).Once()
				f.meta.EXPECT().GetSeriesLookup(anyCtx, ids(5450, 10331, 60563)).
					Return([]metadata.SeriesLookupRecord{
						seriesRec(5450, "Foundation", 7),
						seriesRec(10331, "Foundation (Chronological Order)", 7),
						seriesRec(60563, "Greater Foundation Universe", 15),
					}, nil).Once()

				hits, err := f.svc.Lookup(f.ctx, "foundation", lookupAll)

				Expect(err).NotTo(HaveOccurred())
				Expect(hitIDs(hits)).To(Equal([]string{
					"series:5450", "series:10331", "series:60563", "book:188628",
				}))
				Expect(hits[0].MatchedTitle).To(BeEmpty())
				Expect(hits[1].MatchedTitle).To(Equal("Foundation"))
			})

			It("never follows a book whose title is not the query", func() {
				prelude := bookHit(7, "Prelude to Empire")
				prelude.SeriesIDs = []uint32{5450}
				f.meta.EXPECT().LookupBooks(anyCtx, "fondation").
					Return([]metadata.BookLookupHit{prelude}, nil).Once()
				f.meta.EXPECT().LookupSeries(anyCtx, "fondation").
					Return(nil, nil).Once()

				hits, err := f.svc.Lookup(f.ctx, "fondation", lookupAll)

				Expect(err).NotTo(HaveOccurred())
				Expect(hitIDs(hits)).To(Equal([]string{"book:7"}))
			})

			It(
				"lists a series reached only by the query's words after the books",
				func() {
					heir := bookHit(1, "Star Wars: Heir to the Empire")
					heir.SeriesIDs = []uint32{300}
					f.meta.EXPECT().LookupBooks(anyCtx, "star wars").
						Return([]metadata.BookLookupHit{heir, bookHit(2, "Star Wars")}, nil).
						Once()
					f.meta.EXPECT().LookupSeries(anyCtx, "star wars").
						Return(nil, nil).Once()
					f.meta.EXPECT().GetSeriesLookup(anyCtx, ids(300)).
						Return([]metadata.SeriesLookupRecord{
							seriesRec(300, "The Thrawn Trilogy", 3),
						}, nil).Once()

					hits, err := f.svc.Lookup(f.ctx, "star wars", lookupAll)

					Expect(err).NotTo(HaveOccurred())
					Expect(hitIDs(hits)).To(Equal([]string{
						"book:1", "book:2", "series:300",
					}))
					Expect(
						hits[2].MatchedTitle,
					).To(Equal("Star Wars: Heir to the Empire"))
				},
			)

			It("keeps the series ahead of the books when the cap cuts", func() {
				books := make([]metadata.BookLookupHit, 0, 31)
				books = append(books, foundation())
				for i := range 30 {
					books = append(
						books,
						bookHit(uint32(i+1), fmt.Sprintf("Book %d", i)),
					)
				}
				f.meta.EXPECT().LookupBooks(anyCtx, "fondation").
					Return(books, nil).Once()
				f.meta.EXPECT().LookupSeries(anyCtx, "fondation").
					Return([]metadata.BookLookupHit{seriesHit(77, "Fondation et Empire")}, nil).
					Once()
				f.meta.EXPECT().
					GetSeriesLookup(anyCtx, ids(77, 5450, 10331, 60563)).
					Return([]metadata.SeriesLookupRecord{
						seriesRec(77, "Fondation et Empire", 2),
						seriesRec(5450, "Foundation", 7),
						seriesRec(10331, "Foundation (Chronological Order)", 7),
						seriesRec(60563, "Greater Foundation Universe", 15),
					}, nil).Once()

				hits, err := f.svc.Lookup(f.ctx, "fondation", lookupAll)

				Expect(err).NotTo(HaveOccurred())
				Expect(hits).To(HaveLen(20))
				Expect(hitIDs(hits)[:5]).To(Equal([]string{
					"series:77", "series:5450", "series:10331", "series:60563",
					"book:188628",
				}))
			})

			It("never lets the cap drop a series the query reached", func() {
				many := foundation()
				many.SeriesIDs = nil
				recs := make([]metadata.SeriesLookupRecord, 0, 21)
				want := make([]uint32, 0, 21)
				for i := range uint32(21) {
					many.SeriesIDs = append(many.SeriesIDs, 100+i)
					want = append(want, 100+i)
					recs = append(
						recs,
						seriesRec(100+i, fmt.Sprintf("Universe %d", i), 2),
					)
				}
				f.meta.EXPECT().LookupBooks(anyCtx, "fondation").
					Return([]metadata.BookLookupHit{many}, nil).Once()
				f.meta.EXPECT().
					LookupSeries(anyCtx, "fondation").
					Return(nil, nil).
					Once()
				f.meta.EXPECT().GetSeriesLookup(anyCtx, ids(want...)).
					Return(recs, nil).Once()

				hits, err := f.svc.Lookup(f.ctx, "fondation", lookupAll)

				Expect(err).NotTo(HaveOccurred())
				Expect(hits).To(HaveLen(21))
				for _, h := range hits {
					Expect(h.Type).To(Equal("series"))
				}
			})

			It(
				"covers a series with the book adding it would pick at each of its first positions",
				func() {
					f.meta.EXPECT().
						LookupBooks(anyCtx, "foundation").
						Return(nil, nil).
						Once()
					f.meta.EXPECT().LookupSeries(anyCtx, "foundation").
						Return([]metadata.BookLookupHit{seriesHit(5450, "Foundation")}, nil).
						Once()
					r := seriesRec(5450, "Foundation", 0)
					r.PrimaryBooks = 7
					r.Volumes = []metadata.SeriesLookupVolume{
						{Position: 1, BookHardcoverID: 900, UsersCount: 5},
						{
							Position:        1,
							BookHardcoverID: 901,
							UsersCount:      50,
							Compilation:     true,
						},
						{Position: 1, BookHardcoverID: 902, UsersCount: 20},
						{Position: 2, BookHardcoverID: 911, UsersCount: 1},
						{Position: 2, BookHardcoverID: 910, UsersCount: 1},
						{Position: 3, BookHardcoverID: 920},
						{Position: 4, BookHardcoverID: 930, UsersCount: 99},
					}
					for i := range r.Volumes {
						r.Volumes[i].ImageURL = "https://img/v.jpg"
					}
					f.meta.EXPECT().GetSeriesLookup(anyCtx, ids(5450)).
						Return([]metadata.SeriesLookupRecord{r}, nil).Once()

					hits, err := f.svc.Lookup(f.ctx, "foundation", lookupSeries)

					Expect(err).NotTo(HaveOccurred())
					Expect(hits).To(HaveLen(1))
					Expect(hits[0].CoverBookIDs).To(Equal([]uint32{902, 910, 920}))
					Expect(hits[0].Volumes).To(Equal(uint32(7)))
				},
			)
		})

		Describe("cover fan", func() {
			lookupFan := func(r metadata.SeriesLookupRecord) []uint32 {
				GinkgoHelper()
				f.meta.EXPECT().LookupBooks(anyCtx, "calvin").Return(nil, nil).Once()
				f.meta.EXPECT().LookupSeries(anyCtx, "calvin").
					Return([]metadata.BookLookupHit{seriesHit(r.HardcoverID, r.Name)}, nil).
					Once()
				f.meta.EXPECT().GetSeriesLookup(anyCtx, ids(r.HardcoverID)).
					Return([]metadata.SeriesLookupRecord{r}, nil).Once()
				hits, err := f.svc.Lookup(f.ctx, "calvin", lookupSeries)
				Expect(err).NotTo(HaveOccurred())
				Expect(hits).To(HaveLen(1))
				return hits[0].CoverBookIDs
			}

			It("leaves out a volume Hardcover has no cover for", func() {
				r := seriesRec(1, "Calvin", 0)
				r.PrimaryBooks = 3
				r.Volumes = []metadata.SeriesLookupVolume{
					{
						Position:        1,
						BookHardcoverID: 11,
						ImageURL:        "https://img/11.jpg",
					},
					{Position: 2, BookHardcoverID: 12},
					{
						Position:        3,
						BookHardcoverID: 13,
						ImageURL:        "https://img/13.jpg",
					},
				}
				r.Loose = []metadata.SeriesLookupVolume{
					{BookHardcoverID: 99, ImageURL: "https://img/99.jpg"},
				}

				Expect(lookupFan(r)).To(Equal([]uint32{11, 13}))
			})

			It(
				"fans the most read unnumbered books of a series that numbers none",
				func() {
					r := seriesRec(1, "Calvin und Hobbes", 0)
					r.PrimaryBooks = 6
					r.Loose = []metadata.SeriesLookupVolume{
						{
							BookHardcoverID: 21,
							UsersCount:      117,
							ImageURL:        "https://img/21.jpg",
						},
						{BookHardcoverID: 22, UsersCount: 2},
						{
							BookHardcoverID: 23,
							UsersCount:      1,
							Compilation:     true,
							ImageURL:        "https://img/23.jpg",
						},
						{
							BookHardcoverID: 24,
							UsersCount:      1,
							ImageURL:        "https://img/24.jpg",
						},
						{
							BookHardcoverID: 25,
							UsersCount:      1,
							ImageURL:        "https://img/25.jpg",
						},
						{
							BookHardcoverID: 26,
							UsersCount:      1,
							ImageURL:        "https://img/26.jpg",
						},
					}

					Expect(lookupFan(r)).To(Equal([]uint32{21, 24, 25}))
				},
			)
		})

		It("drops a book hit with neither a maker nor a cover", func() {
			stray := bookHit(2, "Calvin and Hobbes")
			stray.Author, stray.ImageURL = "", ""
			noCover := bookHit(3, "Calvin and Hobbes Sunday Pages")
			noCover.ImageURL = ""
			f.meta.EXPECT().LookupBooks(anyCtx, "calvin and hobbes").
				Return([]metadata.BookLookupHit{bookHit(1, "Calvin and Hobbes"), stray, noCover}, nil).
				Once()

			hits, err := f.svc.Lookup(f.ctx, "calvin and hobbes", lookupBook)

			Expect(err).NotTo(HaveOccurred())
			ids := make([]uint32, 0, len(hits))
			for _, h := range hits {
				ids = append(ids, h.HardcoverID)
			}
			Expect(ids).To(Equal([]uint32{1, 3}))
		})

		It("says so when there is no Hardcover key", func() {
			svc := NewService(f.store, nil, f.posters, f.idx, f.dl, nil, nil)
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

		It(
			"previews the volume adding the series would pick at each position",
			func() {
				sk := seriesRecord("One Piece", 3)
				sk.Slug = "one-piece"
				sk.Volumes[0].UsersCount = 5
				sk.Volumes = append(
					sk.Volumes,
					metadata.SeriesVolumeRef{
						Position:        1,
						BookHardcoverID: 1101,
						UsersCount:      50,
					},
					metadata.SeriesVolumeRef{
						Position:        2,
						BookHardcoverID: 1102,
						UsersCount:      90,
						Compilation:     true,
					},
				)
				f.meta.EXPECT().GetSeries(anyCtx, uint32(50)).Return(sk, nil).Once()
				f.meta.EXPECT().GetBooks(anyCtx, ids(1101, 1002, 1003)).
					Return([]*metadata.BookRecord{
						volumeRec(1101), volumeRec(1002), volumeRec(1003),
					}, nil).Once()

				d, err := f.svc.LookupDetail(f.ctx, lookupSeries, 50)

				Expect(err).NotTo(HaveOccurred())
				Expect(d.VolumeBookIDs).To(Equal([]uint32{1101, 1002, 1003}))
				Expect(d.Slug).To(Equal("one-piece"))
			},
		)

		It("answers a series Hardcover does not know", func() {
			f.meta.EXPECT().GetSeries(anyCtx, uint32(404)).Return(nil, nil).Once()
			_, err := f.svc.LookupDetail(f.ctx, lookupSeries, 404)
			Expect(err).To(MatchError(ErrHardcoverNotFound))
		})
	})
})
