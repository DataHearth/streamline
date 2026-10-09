package book

import (
	"context"
	"errors"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/book"
	"github.com/datahearth/streamline/internal/metadata"
	metamocks "github.com/datahearth/streamline/internal/metadata/mocks"
	"github.com/datahearth/streamline/internal/scheduler"
)

// manualContext is a context a manual scheduler run would carry.
func manualContext() context.Context {
	GinkgoHelper()
	got := make(chan context.Context, 4)
	s := scheduler.New()
	s.Register("probe", time.Hour, func(ctx context.Context) error {
		got <- ctx
		return nil
	})
	root, cancel := context.WithCancel(context.Background())
	DeferCleanup(cancel)
	go s.Start(root)
	Eventually(got).Should(Receive())
	Eventually(func() bool {
		info, err := s.Get("probe")
		Expect(err).NotTo(HaveOccurred())
		return info.Running
	}).Should(BeFalse())
	Expect(s.RunNow("probe")).To(Succeed())
	var manual context.Context
	Eventually(got).Should(Receive(&manual))
	return manual
}

var _ = Describe("Refreshing books", Label("unit", "integration", "books"), func() {
	var f *fixture

	BeforeEach(func() { f = newFixture() })

	Describe("RefreshBook", func() {
		It("writes fresh data and keeps what the user chose", func() {
			b := f.addBook(1, "Elantris", "ebook")
			f.client.Book.UpdateOneID(b.ID).
				SetKind(book.KindComic).
				SetQualityProfile("comics").
				ExecX(f.ctx)
			r := rec(1, "Elantris")
			r.Description = "Rewritten"
			r.Rating = 4.2
			r.Kind = metadata.BookKindNovel
			r.Editions = append(r.Editions, metadata.EditionRecord{
				HardcoverID: 19, Language: "de", Title: "Elantris (de)",
				Format: metadata.FormatEbook, Popularity: 1,
			})
			f.meta.EXPECT().GetBooksFresh(anyCtx, ids(1)).
				Return([]*metadata.BookRecord{r}, nil).Once()

			got, err := f.svc.RefreshBook(f.ctx, b.ID)

			Expect(err).NotTo(HaveOccurred())
			Expect(got.Overview).To(Equal("Rewritten"))
			Expect(*got.RatingTenths).To(Equal(uint8(42)))
			Expect(got.Edges.Editions).To(HaveLen(4))
			// Corrected kind, profile and monitoring are the user's.
			Expect(got.Kind).To(Equal(book.KindComic))
			Expect(got.QualityProfile).To(Equal("comics"))
			Expect(got.EbookMonitored).To(BeTrue())
			Expect(got.AudiobookMonitored).To(BeFalse())
			Expect(got.LastRefreshedAt.After(*b.LastRefreshedAt)).To(BeTrue())
		})

		It("answers a book Hardcover no longer knows", func() {
			b := f.addBook(1, "Elantris", "")
			f.meta.EXPECT().GetBooksFresh(anyCtx, ids(1)).
				Return([]*metadata.BookRecord{}, nil).Once()

			_, err := f.svc.RefreshBook(f.ctx, b.ID)

			Expect(err).To(MatchError(ErrHardcoverNotFound))
		})

		It("passes a rate limit through and changes nothing", func() {
			b := f.addBook(1, "Elantris", "")
			f.meta.EXPECT().GetBooksFresh(anyCtx, ids(1)).
				Return(nil, &metadata.RateLimitedError{}).Once()

			_, err := f.svc.RefreshBook(f.ctx, b.ID)

			Expect(err).To(MatchError(metadata.ErrRateLimited))
			Expect(f.reloadBook(b.ID).Overview).To(Equal("About Elantris"))
		})

		It("answers a book that does not exist", func() {
			_, err := f.svc.RefreshBook(f.ctx, 404)
			Expect(err).To(MatchError(ErrBookNotFound))
		})
	})

	Describe("RefreshSeries", func() {
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

		It(
			"adds a volume the skeleton gained, under the policy and the chosen edition",
			func() {
				s := addSeries(2)
				Expect(
					f.svc.PatchSeries(
						f.ctx,
						s.ID,
						PatchSeriesParams{QualityProfile: new("comics")},
					),
				).
					NotTo(BeNil())
				f.meta.EXPECT().GetSeries(anyCtx, uint32(50)).
					Return(seriesRecord("One Piece (new)", 3), nil).Once()
				recs := []*metadata.BookRecord{
					volumeRec(1001),
					volumeRec(1002),
					volumeRec(1003),
				}
				f.meta.EXPECT().
					GetBooksFresh(anyCtx, ids(1001, 1002, 1003)).
					Return(recs, nil).
					Once()

				got, err := f.svc.RefreshSeries(f.ctx, s.ID)

				Expect(err).NotTo(HaveOccurred())
				Expect(got.Title).To(Equal("One Piece (new)"))
				Expect(got.Edges.Volumes).To(HaveLen(3))
				fresh := got.Edges.Volumes[2]
				Expect(fresh.HardcoverID).To(Equal(uint32(1003)))
				Expect(fresh.EbookMonitored).To(BeTrue())
				Expect(fresh.QualityProfile).To(Equal("comics"))
				Expect(fresh.Edges.EbookEdition.Publisher).To(Equal("Tor"))
				Expect(*got.Since).To(Equal(uint16(2005)))
			},
		)

		It(
			"leaves a volume that left the skeleton, and the user's choices, alone",
			func() {
				s := addSeries(2)
				_, err := f.svc.PatchSeries(
					f.ctx,
					s.ID,
					PatchSeriesParams{Monitor: new("none")},
				)
				Expect(err).NotTo(HaveOccurred())
				f.meta.EXPECT().GetSeries(anyCtx, uint32(50)).
					Return(seriesRecord("One Piece", 1), nil).Once()
				f.meta.EXPECT().GetBooksFresh(anyCtx, ids(1001)).
					Return([]*metadata.BookRecord{volumeRec(1001)}, nil).Once()

				got, err := f.svc.RefreshSeries(f.ctx, s.ID)

				Expect(err).NotTo(HaveOccurred())
				Expect(got.Edges.Volumes).To(HaveLen(2))
				Expect(got.Monitor.String()).To(Equal("none"))
			},
		)

		It("hydrates a stub it meets", func() {
			s := addSeries(1)
			stub := f.client.Book.Create().
				SetHardcoverID(1002).
				SetTitle("One Piece #2").
				SetSeries(f.client.BookSeries.GetX(f.ctx, s.ID)).
				SetSeriesPosition(2).SetEbookMonitored(true).SaveX(f.ctx)
			f.meta.EXPECT().GetSeries(anyCtx, uint32(50)).
				Return(seriesRecord("One Piece", 2), nil).Once()
			f.meta.EXPECT().GetBooksFresh(anyCtx, ids(1001, 1002)).
				Return([]*metadata.BookRecord{volumeRec(1001), volumeRec(1002)}, nil).
				Once()

			got, err := f.svc.RefreshSeries(f.ctx, s.ID)

			Expect(err).NotTo(HaveOccurred())
			Expect(Hydrating(got)).To(BeFalse())
			Expect(f.reloadBook(stub.ID).Title).To(Equal("Volume 1002"))
		})

		It("does not add a compilation", func() {
			s := addSeries(1)
			c := volumeRec(1002)
			c.Compilation = true
			f.meta.EXPECT().GetSeries(anyCtx, uint32(50)).
				Return(seriesRecord("One Piece", 2), nil).Once()
			f.meta.EXPECT().GetBooksFresh(anyCtx, ids(1001, 1002)).
				Return([]*metadata.BookRecord{volumeRec(1001), c}, nil).Once()

			got, err := f.svc.RefreshSeries(f.ctx, s.ID)

			Expect(err).NotTo(HaveOccurred())
			Expect(got.Edges.Volumes).To(HaveLen(1))
		})

		It("answers a series Hardcover no longer knows", func() {
			s := addSeries(1)
			f.meta.EXPECT().GetSeries(anyCtx, uint32(50)).Return(nil, nil).Once()
			_, err := f.svc.RefreshSeries(f.ctx, s.ID)
			Expect(err).To(MatchError(ErrHardcoverNotFound))
		})
	})

	Describe("RefreshStale", func() {
		stale := func(b *ent.Book) {
			GinkgoHelper()
			f.client.Book.UpdateOneID(b.ID).
				SetLastRefreshedAt(time.Now().Add(-48 * time.Hour)).
				ExecX(f.ctx)
		}

		It("does nothing without a Hardcover key", func() {
			svc := NewService(f.store, nil, f.posters, f.idx, f.dl)
			Expect(svc.RefreshStale(f.ctx)).To(Succeed())
		})

		It(
			"refreshes stale standalone books in one batch and leaves fresh ones",
			func() {
				a, b := f.addBook(1, "Elantris", ""), f.addBook(2, "Warbreaker", "")
				f.addBook(3, "Fresh", "")
				stale(a)
				stale(b)
				f.meta.EXPECT().GetBooksFresh(anyCtx, ids(1, 2)).
					Return([]*metadata.BookRecord{rec(1, "Elantris"), rec(2, "Warbreaker")}, nil).
					Once()

				Expect(f.svc.RefreshStale(f.ctx)).To(Succeed())

				Expect(
					f.reloadBook(
						a.ID,
					).LastRefreshedAt.After(
						time.Now().Add(-time.Minute),
					),
				).To(BeTrue())
				Expect(
					f.reloadBook(
						b.ID,
					).LastRefreshedAt.After(
						time.Now().Add(-time.Minute),
					),
				).To(BeTrue())
			},
		)

		It(
			"stamps a book Hardcover no longer knows so it is not picked again today",
			func() {
				a := f.addBook(1, "Elantris", "")
				stale(a)
				f.meta.EXPECT().GetBooksFresh(anyCtx, ids(1)).
					Return([]*metadata.BookRecord{}, nil).Once()

				Expect(f.svc.RefreshStale(f.ctx)).To(Succeed())

				Expect(
					f.reloadBook(
						a.ID,
					).LastRefreshedAt.After(
						time.Now().Add(-time.Minute),
					),
				).To(BeTrue())
				Expect(f.reloadBook(a.ID).Overview).To(Equal("About Elantris"))
			},
		)

		It("hydrates stubs before refreshing anything", func() {
			a := f.addBook(1, "Elantris", "")
			stale(a)
			stub := f.client.Book.Create().
				SetHardcoverID(7).SetTitle("Stub").SaveX(f.ctx)
			f.meta.EXPECT().GetBooksFresh(anyCtx, ids(7)).
				Return([]*metadata.BookRecord{rec(7, "Hydrated")}, nil).Once()
			f.meta.EXPECT().GetBooksFresh(anyCtx, ids(1)).
				Return([]*metadata.BookRecord{rec(1, "Elantris")}, nil).Once()

			Expect(f.svc.RefreshStale(f.ctx)).To(Succeed())

			Expect(f.reloadBook(stub.ID).LastRefreshedAt).NotTo(BeNil())
			Expect(f.reloadBook(stub.ID).Title).To(Equal("Hydrated"))
		})

		It("refreshes the stale series after the books", func() {
			s := func() *ent.BookSeries {
				f.meta.EXPECT().GetSeries(anyCtx, uint32(50)).
					Return(seriesRecord("One Piece", 1), nil).Once()
				f.meta.EXPECT().GetBooks(anyCtx, ids(1001)).
					Return([]*metadata.BookRecord{volumeRec(1001)}, nil).Once()
				got, err := f.svc.AddSeries(f.ctx, AddSeriesParams{HardcoverID: 50})
				Expect(err).NotTo(HaveOccurred())
				return got
			}()
			f.client.BookSeries.UpdateOneID(s.ID).
				SetLastRefreshedAt(time.Now().Add(-48 * time.Hour)).
				ExecX(f.ctx)
			f.meta.EXPECT().GetSeries(anyCtx, uint32(50)).
				Return(seriesRecord("One Piece", 1), nil).Once()
			f.meta.EXPECT().GetBooksFresh(anyCtx, ids(1001)).
				Return([]*metadata.BookRecord{volumeRec(1001)}, nil).Once()

			Expect(f.svc.RefreshStale(f.ctx)).To(Succeed())

			Expect(
				f.reloadSeries(
					s.ID,
				).LastRefreshedAt.After(
					time.Now().Add(-time.Minute),
				),
			).To(BeTrue())
		})

		It("stops at the first rate limit and returns nil", func() {
			a, b := f.addBook(1, "Elantris", ""), f.addBook(2, "Warbreaker", "")
			stale(a)
			stale(b)
			stub := f.client.Book.Create().
				SetHardcoverID(7).
				SetTitle("Stub").
				SaveX(f.ctx)
			f.meta.EXPECT().GetBooksFresh(anyCtx, ids(7)).
				Return(nil, &metadata.RateLimitedError{RetryAfter: time.Minute}).
				Once()

			Expect(f.svc.RefreshStale(f.ctx)).To(Succeed())

			Expect(f.reloadBook(stub.ID).LastRefreshedAt).To(BeNil())
			Expect(f.reloadBook(a.ID).Overview).To(Equal("About Elantris"))
		})

		It("goes on past a failed step", func() {
			a, b := f.addBook(1, "Elantris", ""), f.addBook(2, "Warbreaker", "")
			stale(a)
			stale(b)
			stub := f.client.Book.Create().
				SetHardcoverID(7).
				SetTitle("Stub").
				SaveX(f.ctx)
			f.meta.EXPECT().GetBooksFresh(anyCtx, ids(7)).
				Return(nil, errors.New("boom")).Once()
			f.meta.EXPECT().GetBooksFresh(anyCtx, ids(1, 2)).
				Return([]*metadata.BookRecord{rec(1, "Elantris"), rec(2, "Warbreaker")}, nil).
				Once()

			Expect(f.svc.RefreshStale(f.ctx)).To(Succeed())

			Expect(f.reloadBook(stub.ID).LastRefreshedAt).To(BeNil())
			Expect(
				f.reloadBook(
					a.ID,
				).LastRefreshedAt.After(
					time.Now().Add(-time.Minute),
				),
			).To(BeTrue())
		})

		It("stops when only the scan reserve is left", func() {
			a := f.addBook(1, "Elantris", "")
			stale(a)
			mock := metamocks.NewMockBookProvider(GinkgoT())
			svc := NewService(
				f.store,
				&budgetedProvider{
					MockBookProvider: mock,
					remaining:        400,
					reserve:          500,
				},
				f.posters,
				f.idx,
				f.dl,
			)

			Expect(svc.RefreshStale(f.ctx)).To(Succeed())

			Expect(f.reloadBook(a.ID).Overview).To(Equal("About Elantris"))
		})

		It("waives the interval for a manual run", func() {
			a := f.addBook(1, "Elantris", "")
			f.meta.EXPECT().GetBooksFresh(anyCtx, ids(1)).
				Return([]*metadata.BookRecord{rec(1, "Elantris")}, nil).Once()

			Expect(f.svc.RefreshStale(manualContext())).To(Succeed())

			Expect(
				f.reloadBook(a.ID).LastRefreshedAt.After(*a.LastRefreshedAt),
			).To(BeTrue())
		})

		It("leaves a fresh library alone on a scheduled run", func() {
			f.addBook(1, "Elantris", "")
			Expect(f.svc.RefreshStale(f.ctx)).To(Succeed())
		})
	})

	Describe("hydration", func() {
		series := func() *ent.BookSeries {
			s, _, err := f.store.CreateSeries(f.ctx, dbSeriesWithStubs())
			Expect(err).NotTo(HaveOccurred())
			return s
		}

		It("fills stubs under the series policy and its chosen edition", func() {
			s := series()
			past, future := time.Now().
				Add(-48*time.Hour),
				time.Now().
					Add(48*time.Hour)
			a, b := volumeRec(1001), volumeRec(1002)
			a.ReleaseDate, b.ReleaseDate = &past, &future
			f.meta.EXPECT().GetBooksFresh(anyCtx, ids(1001, 1002)).
				Return([]*metadata.BookRecord{a, b}, nil).Once()

			Expect(f.svc.RefreshStale(f.ctx)).To(Succeed())

			got := f.reloadSeries(s.ID)
			Expect(Hydrating(got)).To(BeFalse())
			// future: measured from the series' creation, so only the later one.
			Expect(got.Edges.Volumes[0].EbookMonitored).To(BeFalse())
			Expect(got.Edges.Volumes[1].EbookMonitored).To(BeTrue())
			Expect(
				got.Edges.Volumes[1].Edges.EbookEdition.Publisher,
			).To(Equal("Mnemos"))
			Expect(got.Edges.Volumes[1].Kind).To(Equal(book.KindManga))
			Expect(*got.Since).To(Equal(uint16(2005)))
			Eventually(f.posters.fetches).Should(ContainElement(
				fetch{"books", got.Edges.Volumes[0].ID, "https://img/b1001.jpg"},
			))
		})

		It(
			"deletes a stub that is a compilation or that Hardcover no longer knows",
			func() {
				s := series()
				c := volumeRec(1001)
				c.Compilation = true
				f.meta.EXPECT().GetBooksFresh(anyCtx, ids(1001, 1002)).
					Return([]*metadata.BookRecord{c}, nil).Once()

				Expect(f.svc.RefreshStale(f.ctx)).To(Succeed())

				Expect(f.reloadSeries(s.ID).Edges.Volumes).To(BeEmpty())
			},
		)

		It("starts one worker however often it is asked", func() {
			series()
			release := make(chan struct{})
			f.meta.EXPECT().GetBooksFresh(anyCtx, ids(1001, 1002)).
				RunAndReturn(func(context.Context, []uint32) ([]*metadata.BookRecord, error) {
					<-release
					return []*metadata.BookRecord{
						volumeRec(1001),
						volumeRec(1002),
					}, nil
				}).
				Once()

			f.svc.HydrateSeriesInBackground(f.ctx)
			f.svc.HydrateSeriesInBackground(f.ctx)
			f.svc.HydrateSeriesInBackground(f.ctx)
			close(release)

			Eventually(f.svc.hydrating.Load).Should(BeFalse())
			stubs, err := f.store.ListHydrationStubs(f.ctx, 10)
			Expect(err).NotTo(HaveOccurred())
			Expect(stubs).To(BeEmpty())
		})

		It("stops on a rate limit and leaves the stubs for the next tick", func() {
			s := series()
			f.meta.EXPECT().GetBooksFresh(anyCtx, ids(1001, 1002)).
				Return(nil, &metadata.RateLimitedError{}).Once()

			f.svc.HydrateSeriesInBackground(f.ctx)

			Eventually(f.svc.hydrating.Load).Should(BeFalse())
			Expect(Hydrating(f.reloadSeries(s.ID))).To(BeTrue())
		})

		It("does not start without a Hardcover key", func() {
			svc := NewService(f.store, nil, f.posters, f.idx, f.dl)
			svc.HydrateSeriesInBackground(f.ctx)
			Expect(svc.hydrating.Load()).To(BeFalse())
		})
	})
})
