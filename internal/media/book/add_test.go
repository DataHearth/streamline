package book

import (
	"errors"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	entbook "github.com/datahearth/streamline/ent/book"
	"github.com/datahearth/streamline/internal/metadata"
	"github.com/datahearth/streamline/internal/testutil/configtest"
)

var _ = Describe("Adding books", Label("unit", "integration", "books"), func() {
	var f *fixture

	BeforeEach(func() { f = newFixture() })

	Describe("AddBook", func() {
		It(
			"adds a book with its editions, makers and slot editions in one request",
			func() {
				b := f.addBook(1, "Elantris", "")

				Expect(b.Title).To(Equal("Elantris"))
				Expect(b.AuthorName).To(Equal("Brandon Sanderson"))
				Expect(b.Kind).To(Equal(entbook.KindNovel))
				Expect(b.PreferredLanguage).To(Equal("en"))
				Expect(b.Edges.Editions).To(HaveLen(3))
				Expect(b.Edges.EbookEdition.Publisher).To(Equal("Tor"))
				Expect(b.Edges.AudiobookEdition.Publisher).To(Equal("Audible"))
				Expect(b.EbookMonitored).To(BeTrue())
				Expect(b.AudiobookMonitored).To(BeTrue())
				Expect(b.LastRefreshedAt).NotTo(BeNil())
				Expect(*b.ReleaseYear).To(Equal(uint16(2005)))
			},
		)

		It("fetches the cover and the creators' photos after the commit", func() {
			b := f.addBook(1, "Elantris", "")

			author := b.Edges.Contributions[0].Edges.Author
			Eventually(f.posters.fetches).Should(ConsistOf(
				fetch{"books", b.ID, "https://img/b1.jpg"},
				fetch{"authors", author.ID, "https://img/a10.jpg"},
			))
		})

		DescribeTable("monitor",
			func(monitor string, ebook, audiobook bool) {
				b := f.addBook(1, "Elantris", monitor)
				Expect(b.EbookMonitored).To(Equal(ebook))
				Expect(b.AudiobookMonitored).To(Equal(audiobook))
				Expect(Monitor(b)).To(Equal(map[[2]bool]string{
					{true, true}: "both", {true, false}: "ebook",
					{false, true}: "audiobook", {false, false}: "none",
				}[[2]bool{ebook, audiobook}]))
			},
			Entry("both by default", "", true, true),
			Entry("ebook", "ebook", true, false),
			Entry("audiobook", "audiobook", false, true),
			Entry("none", "none", false, false),
		)

		It(
			"leaves a slot unmonitored when the book has no edition of that format",
			func() {
				r := rec(1, "Elantris")
				r.Editions = r.Editions[:2]
				f.meta.EXPECT().GetBooks(anyCtx, ids(1)).
					Return([]*metadata.BookRecord{r}, nil).Once()

				b, err := f.svc.AddBook(f.ctx, AddBookParams{HardcoverID: 1})

				Expect(err).NotTo(HaveOccurred())
				Expect(b.EbookMonitored).To(BeTrue())
				Expect(b.AudiobookMonitored).To(BeFalse())
				Expect(Monitor(b)).To(Equal("ebook"))
			},
		)

		It("seeds the preferred language from the library setting", func() {
			cfg := bookConfig()
			cfg["library"] = map[string]any{"book_language": "fr"}
			configtest.Setup(cfg)

			b := f.addBook(1, "Elantris", "")

			Expect(b.PreferredLanguage).To(Equal("fr"))
			Expect(b.Title).To(Equal("Elantris (fr)"))
			Expect(b.OriginalTitle).To(Equal("Elantris"))
			Expect(b.Edges.EbookEdition.Language).To(Equal("fr"))
		})

		It("writes the quality profile asked for", func() {
			f.meta.EXPECT().GetBooks(anyCtx, ids(1)).
				Return([]*metadata.BookRecord{rec(1, "Elantris")}, nil).Once()

			b, err := f.svc.AddBook(
				f.ctx,
				AddBookParams{HardcoverID: 1, QualityProfile: "comics"},
			)

			Expect(err).NotTo(HaveOccurred())
			Expect(b.QualityProfile).To(Equal("comics"))
		})

		It("refuses an unknown profile or monitor before asking Hardcover", func() {
			_, err := f.svc.AddBook(
				f.ctx,
				AddBookParams{HardcoverID: 1, QualityProfile: "ghost"},
			)
			Expect(err).To(MatchError(ErrUnknownProfile))
			_, err = f.svc.AddBook(
				f.ctx,
				AddBookParams{HardcoverID: 1, Monitor: "all"},
			)
			Expect(err).To(MatchError(ErrInvalidMonitor))
		})

		It("refuses a book already in the library", func() {
			f.addBook(1, "Elantris", "")

			_, err := f.svc.AddBook(f.ctx, AddBookParams{HardcoverID: 1})

			Expect(err).To(MatchError(ErrBookExists))
		})

		It("refuses a book Hardcover does not know", func() {
			f.meta.EXPECT().GetBooks(anyCtx, ids(404)).
				Return([]*metadata.BookRecord{}, nil).Once()

			_, err := f.svc.AddBook(f.ctx, AddBookParams{HardcoverID: 404})

			Expect(err).To(MatchError(ErrHardcoverNotFound))
			Expect(f.client.Book.Query().CountX(f.ctx)).To(BeZero())
		})

		It("passes a rate limit through", func() {
			f.meta.EXPECT().GetBooks(anyCtx, ids(1)).
				Return(nil, &metadata.RateLimitedError{RetryAfter: time.Minute}).
				Once()

			_, err := f.svc.AddBook(f.ctx, AddBookParams{HardcoverID: 1})

			Expect(err).To(MatchError(metadata.ErrRateLimited))
		})

		It("says so when there is no Hardcover key", func() {
			svc := NewService(f.store, nil, f.posters, f.idx, f.dl, nil)
			_, err := svc.AddBook(f.ctx, AddBookParams{HardcoverID: 1})
			Expect(err).To(MatchError(ErrNotConfigured))
		})
	})

	Describe("AddSeries", func() {
		skeleton := func(n int) {
			GinkgoHelper()
			f.meta.EXPECT().GetSeries(anyCtx, uint32(50)).
				Return(seriesRecord("One Piece", n), nil).Once()
		}
		batch := func(from, to uint32) {
			GinkgoHelper()
			var want []uint32
			var recs []*metadata.BookRecord
			for i := from; i <= to; i++ {
				want = append(want, 1000+i)
				recs = append(recs, volumeRec(1000+i))
			}
			f.meta.EXPECT().GetBooks(anyCtx, ids(want...)).Return(recs, nil).Once()
		}

		It("is two requests on the HTTP path for a short series", func() {
			skeleton(3)
			batch(1, 3)

			s, err := f.svc.AddSeries(f.ctx, AddSeriesParams{HardcoverID: 50})

			Expect(err).NotTo(HaveOccurred())
			Expect(s.Title).To(Equal("One Piece"))
			Expect(s.AuthorName).To(Equal("Eiichiro Oda"))
			Expect(s.Kind.String()).To(Equal("manga"))
			Expect(s.Monitor.String()).To(Equal("all"))
			Expect(s.Ongoing).To(BeTrue())
			Expect(s.Edges.Volumes).To(HaveLen(3))
			Expect(Hydrating(s)).To(BeFalse())
			Expect(*s.Edges.Volumes[0].SeriesPosition).To(Equal(1.0))
			Expect(s.Edges.Volumes[0].EbookMonitored).To(BeTrue())
			Expect(s.Edges.Volumes[0].Edges.Editions).To(HaveLen(3))
		})

		It(
			"reads the first twenty volumes and leaves a stub for each other position",
			func() {
				skeleton(25)
				batch(1, 20)
				// The worker the add starts reads the stubs.
				var rest []uint32
				var recs []*metadata.BookRecord
				for i := uint32(21); i <= 25; i++ {
					rest = append(rest, 1000+i)
					recs = append(recs, volumeRec(1000+i))
				}
				f.meta.EXPECT().
					GetBooksFresh(anyCtx, ids(rest...)).
					Return(recs, nil).
					Once()

				s, err := f.svc.AddSeries(f.ctx, AddSeriesParams{HardcoverID: 50})

				Expect(err).NotTo(HaveOccurred())
				Expect(s.Edges.Volumes).To(HaveLen(25))
				Expect(Hydrating(s)).To(BeTrue())
				stub := s.Edges.Volumes[24]
				Expect(stub.Title).To(Equal("One Piece #25"))
				Expect(stub.LastRefreshedAt).To(BeNil())
				Expect(stub.Edges.Editions).To(BeEmpty())
				Expect(stub.EbookMonitored).To(BeTrue())

				Eventually(func() bool {
					return Hydrating(f.reloadSeries(s.ID))
				}).Should(BeFalse())
				got := f.reloadSeries(s.ID)
				Expect(got.Edges.Volumes[24].Title).To(Equal("Volume 1025"))
				Expect(got.Edges.Volumes[24].Edges.EbookEdition).NotTo(BeNil())
			},
		)

		It("writes the profile to every volume, stubs included", func() {
			skeleton(22)
			batch(1, 20)
			f.meta.EXPECT().GetBooksFresh(anyCtx, ids(1021, 1022)).
				Return([]*metadata.BookRecord{volumeRec(1021), volumeRec(1022)}, nil).
				Once()

			s, err := f.svc.AddSeries(f.ctx, AddSeriesParams{
				HardcoverID: 50, QualityProfile: "comics",
			})

			Expect(err).NotTo(HaveOccurred())
			Expect(s.QualityProfile).To(Equal("comics"))
			for _, v := range s.Edges.Volumes {
				Expect(v.QualityProfile).To(Equal("comics"))
			}
			Eventually(
				func() bool { return Hydrating(f.reloadSeries(s.ID)) },
			).Should(BeFalse())
		})

		It(
			"credits the series' author, the first volume's makers and its translators",
			func() {
				skeleton(1)
				r := volumeRec(1001)
				r.Editions[1].Translator, r.Editions[1].TranslatorID = "Une Traductrice", 60
				f.meta.EXPECT().GetBooks(anyCtx, ids(1001)).
					Return([]*metadata.BookRecord{r}, nil).Once()

				s, err := f.svc.AddSeries(f.ctx, AddSeriesParams{HardcoverID: 50})

				Expect(err).NotTo(HaveOccurred())
				credits := make([]string, 0, len(s.Edges.Contributions))
				for _, c := range s.Edges.Contributions {
					credits = append(
						credits,
						c.Edges.Author.Name+"/"+string(c.Role)+"/"+c.Language,
					)
				}
				Expect(credits).To(ConsistOf(
					"Eiichiro Oda/author/",
					"Eiichiro Oda/writer/",
					"Une Traductrice/translator/fr",
				))
				// The author is credited once per role, not once for the series
				// and again for the volume's writer credit.
				Expect(s.Edges.Contributions).To(HaveLen(3))
			},
		)

		It(
			"drops a compilation and keeps the more popular book at a shared position",
			func() {
				sk := seriesRecord("One Piece", 3)
				sk.Volumes = append(sk.Volumes,
					metadata.SeriesVolumeRef{Position: 3, BookHardcoverID: 1004},
					metadata.SeriesVolumeRef{Position: 2, BookHardcoverID: 1005},
				)
				f.meta.EXPECT().GetSeries(anyCtx, uint32(50)).Return(sk, nil).Once()
				c := volumeRec(1002)
				c.Compilation = true
				lesser, better := volumeRec(1003), volumeRec(1004)
				lesser.UsersCount, better.UsersCount = 1, 50
				f.meta.EXPECT().GetBooks(anyCtx, ids(1001, 1002, 1005, 1003, 1004)).
					Return([]*metadata.BookRecord{
						volumeRec(1001), c, volumeRec(1005), lesser, better,
					}, nil).
					Once()

				s, err := f.svc.AddSeries(f.ctx, AddSeriesParams{HardcoverID: 50})

				Expect(err).NotTo(HaveOccurred())
				hc := make([]uint32, 0, len(s.Edges.Volumes))
				for _, v := range s.Edges.Volumes {
					hc = append(hc, v.HardcoverID)
				}
				Expect(hc).To(ConsistOf(uint32(1001), uint32(1005), uint32(1004)))
			},
		)

		It("keeps one listing of a volume listed twice at a position", func() {
			sk := seriesRecord("One Piece", 2)
			sk.Volumes = append(sk.Volumes, sk.Volumes[1])
			f.meta.EXPECT().GetSeries(anyCtx, uint32(50)).Return(sk, nil).Once()
			f.meta.EXPECT().GetBooks(anyCtx, ids(1001, 1002)).
				Return([]*metadata.BookRecord{volumeRec(1001), volumeRec(1002)}, nil).
				Once()

			s, err := f.svc.AddSeries(f.ctx, AddSeriesParams{HardcoverID: 50})

			Expect(err).NotTo(HaveOccurred())
			Expect(s.Edges.Volumes).To(HaveLen(2))
		})

		It(
			"monitors under the future policy only the volumes released after the add",
			func() {
				skeleton(2)
				past, future := time.Now().
					Add(-24*time.Hour),
					time.Now().
						Add(24*time.Hour)
				a, b := volumeRec(1001), volumeRec(1002)
				a.ReleaseDate, b.ReleaseDate = &past, &future
				f.meta.EXPECT().GetBooks(anyCtx, ids(1001, 1002)).
					Return([]*metadata.BookRecord{a, b}, nil).Once()

				s, err := f.svc.AddSeries(
					f.ctx,
					AddSeriesParams{HardcoverID: 50, Monitor: "future"},
				)

				Expect(err).NotTo(HaveOccurred())
				Expect(s.Edges.Volumes[0].EbookMonitored).To(BeFalse())
				Expect(s.Edges.Volumes[1].EbookMonitored).To(BeTrue())
			},
		)

		It(
			"records the edition of the first volume in the library language",
			func() {
				skeleton(1)
				batch(1, 1)

				s, err := f.svc.AddSeries(f.ctx, AddSeriesParams{HardcoverID: 50})

				Expect(err).NotTo(HaveOccurred())
				Expect(s.EditionLanguage).To(Equal("en"))
				Expect(s.EditionPublisher).To(Equal("Tor"))
				Expect(SeriesEditionLabel(s)).To(Equal("English · Tor"))
			},
		)

		It("adopts a volume already in the library as a standalone book", func() {
			standalone := f.addBook(1001, "Volume 1001", "")
			skeleton(2)
			batch(1, 2)

			s, err := f.svc.AddSeries(f.ctx, AddSeriesParams{HardcoverID: 50})

			Expect(err).NotTo(HaveOccurred())
			Expect(s.Edges.Volumes[0].ID).To(Equal(standalone.ID))
			Expect(f.client.Book.Query().CountX(f.ctx)).To(Equal(2))
		})

		It(
			"refuses a series already in the library or unknown to Hardcover",
			func() {
				skeleton(1)
				batch(1, 1)
				_, err := f.svc.AddSeries(f.ctx, AddSeriesParams{HardcoverID: 50})
				Expect(err).NotTo(HaveOccurred())

				_, err = f.svc.AddSeries(f.ctx, AddSeriesParams{HardcoverID: 50})
				Expect(err).To(MatchError(ErrSeriesExists))

				f.meta.EXPECT().
					GetSeries(anyCtx, uint32(404)).
					Return(nil, nil).
					Once()
				_, err = f.svc.AddSeries(f.ctx, AddSeriesParams{HardcoverID: 404})
				Expect(err).To(MatchError(ErrHardcoverNotFound))
			},
		)

		It("refuses an unknown policy or profile", func() {
			_, err := f.svc.AddSeries(
				f.ctx,
				AddSeriesParams{HardcoverID: 50, Monitor: "everything"},
			)
			Expect(err).To(MatchError(ErrInvalidMonitor))
			_, err = f.svc.AddSeries(
				f.ctx,
				AddSeriesParams{HardcoverID: 50, QualityProfile: "ghost"},
			)
			Expect(err).To(MatchError(ErrUnknownProfile))
		})

		It("writes nothing when the first batch fails", func() {
			skeleton(2)
			f.meta.EXPECT().GetBooks(anyCtx, ids(1001, 1002)).
				Return(nil, errors.New("hardcover down")).Once()

			_, err := f.svc.AddSeries(f.ctx, AddSeriesParams{HardcoverID: 50})

			Expect(err).To(MatchError(ContainSubstring("hardcover down")))
			Expect(f.client.BookSeries.Query().CountX(f.ctx)).To(BeZero())
			Expect(f.client.Book.Query().CountX(f.ctx)).To(BeZero())
		})

		It(
			fmt.Sprintf(
				"costs two requests however long the series is, until the %d-volume batch",
				hydrateBatch,
			),
			func() {
				skeleton(hydrateBatch)
				batch(1, hydrateBatch)

				s, err := f.svc.AddSeries(f.ctx, AddSeriesParams{HardcoverID: 50})

				Expect(err).NotTo(HaveOccurred())
				Expect(s.Edges.Volumes).To(HaveLen(hydrateBatch))
				Expect(Hydrating(s)).To(BeFalse())
			},
		)
	})
})
