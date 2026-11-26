package book

import (
	"context"
	"errors"
	"slices"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stretchr/testify/mock"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/mediafile"
	"github.com/datahearth/streamline/ent/metadatachange"
	"github.com/datahearth/streamline/internal/metadata"
)

var _ = Describe(
	"Following a book's change",
	Label("unit", "integration", "books"),
	func() {
		var (
			f    *fixture
			b    *ent.Book
			slot *ent.BookEdition
			r    *metadata.BookRecord
			c    *ent.MetadataChange
		)

		allExist := func(_ context.Context, ids []uint32) (map[uint32]bool, error) {
			all := map[uint32]bool{}
			for _, id := range ids {
				all[id] = true
			}
			return all, nil
		}

		// An ebook with a file whose edition Hardcover removed, and the pending
		// change a refresh recorded for it.
		BeforeEach(func() {
			f = newFixture()
			b = f.addBook(1, "Elantris", "ebook")
			f.client.MediaFile.Create().
				SetPath("/books/elantris.epub").
				SetSize(1).
				SetBookID(b.ID).
				SetBookKind(mediafile.BookKindEbook).
				ExecX(f.ctx)
			slot = f.reloadBook(b.ID).Edges.EbookEdition
			r = rec(1, "Elantris")
			r.Editions = slices.DeleteFunc(
				r.Editions,
				func(e metadata.EditionRecord) bool {
					return e.HardcoverID == slot.HardcoverEditionID
				},
			)
			f.meta.EXPECT().GetBooksFresh(anyCtx, ids(1)).
				Return([]*metadata.BookRecord{r}, nil).Once()
			f.meta.EXPECT().ExistingEditions(anyCtx, mock.Anything).
				Return(map[uint32]bool{}, nil).Once()
			_, err := f.svc.RefreshBook(f.ctx, b.ID)
			Expect(err).NotTo(HaveOccurred())
			c = f.client.MetadataChange.Query().WithBook().OnlyX(f.ctx)
			Expect(c.Kind).To(Equal(metadatachange.KindEditionRemoved))
		})

		DescribeTable(
			"keeps the change when the refresh fails, so it can be retried",
			func(fail func()) {
				fail()

				Expect(f.svc.FollowChange(f.ctx, c)).NotTo(Succeed())
				Expect(
					f.client.MetadataChange.Query().OnlyX(f.ctx).ID,
				).To(Equal(c.ID))

				f.meta.EXPECT().GetBooksFresh(anyCtx, ids(1)).
					Return([]*metadata.BookRecord{r}, nil).Once()
				f.meta.EXPECT().ExistingEditions(anyCtx, mock.Anything).
					RunAndReturn(allExist).Once()
				Expect(f.svc.FollowChange(f.ctx, c)).To(Succeed())

				after := f.reloadBook(b.ID)
				Expect(after.Edges.EbookEdition).NotTo(BeNil())
				Expect(after.Edges.EbookEdition.HardcoverEditionID).
					NotTo(Equal(slot.HardcoverEditionID))
				Expect(after.Edges.MediaFiles).To(HaveLen(1))
				Expect(f.client.MetadataChange.Query().CountX(f.ctx)).To(BeZero())
			},
			Entry("Hardcover rate limits the read", func() {
				f.meta.EXPECT().GetBooksFresh(anyCtx, ids(1)).
					Return(nil, &metadata.RateLimitedError{}).Once()
			}),
			Entry("Hardcover refuses the key", func() {
				f.meta.EXPECT().GetBooksFresh(anyCtx, ids(1)).
					Return(nil, metadata.ErrHardcoverUnauthorized).Once()
			}),
			Entry("the edition check fails after the slot was filled", func() {
				f.meta.EXPECT().GetBooksFresh(anyCtx, ids(1)).
					Return([]*metadata.BookRecord{r}, nil).Once()
				f.meta.EXPECT().ExistingEditions(anyCtx, mock.Anything).
					Return(nil, errors.New("boom")).Once()
			}),
		)
	},
)

var _ = Describe(
	"Reviewing a series' volumes",
	Label("unit", "integration", "books"),
	func() {
		var (
			f   *fixture
			s   *ent.BookSeries
			vol *ent.Book
		)

		// Series 50 holds volumes 1001 and 1002; 1001 has an ebook file.
		BeforeEach(func() {
			f = newFixture()
			f.meta.EXPECT().GetSeries(anyCtx, uint32(50)).
				Return(seriesRecord("One Piece", 2), nil).Once()
			f.meta.EXPECT().GetBooks(anyCtx, ids(1001, 1002)).
				Return([]*metadata.BookRecord{volumeRec(1001), volumeRec(1002)}, nil).
				Once()
			var err error
			s, err = f.svc.AddSeries(f.ctx, AddSeriesParams{HardcoverID: 50})
			Expect(err).NotTo(HaveOccurred())
			vol, err = f.store.FindBookByHardcoverID(f.ctx, 1001)
			Expect(err).NotTo(HaveOccurred())
			f.client.MediaFile.Create().
				SetPath("/books/one-piece-1.epub").
				SetSize(1).
				SetBookID(vol.ID).
				SetBookKind(mediafile.BookKindEbook).
				ExecX(f.ctx)
			vol = f.reloadBook(vol.ID)
			Expect(vol.Edges.EbookEdition).NotTo(BeNil())
		})

		withoutSlot := func() *metadata.BookRecord {
			r := volumeRec(1001)
			r.Editions = slices.DeleteFunc(
				r.Editions,
				func(e metadata.EditionRecord) bool {
					return e.HardcoverID == vol.Edges.EbookEdition.HardcoverEditionID
				},
			)
			return r
		}
		changeOf := func(kind metadatachange.Kind) *ent.MetadataChange {
			GinkgoHelper()
			c := f.client.MetadataChange.Query().WithBook().OnlyX(f.ctx)
			Expect(c.Kind).To(Equal(kind))
			Expect(c.Edges.Book.ID).To(Equal(vol.ID))
			return c
		}

		It("asks about a volume's format whose edition Hardcover removed", func() {
			f.meta.EXPECT().GetSeries(anyCtx, uint32(50)).
				Return(seriesRecord("One Piece", 2), nil).Once()
			f.meta.EXPECT().GetBooksFresh(anyCtx, ids(1001, 1002)).
				Return([]*metadata.BookRecord{withoutSlot(), volumeRec(1002)}, nil).
				Once()
			f.meta.EXPECT().
				ExistingEditions(anyCtx, []uint32{vol.Edges.EbookEdition.HardcoverEditionID}).
				Return(map[uint32]bool{}, nil).Once()

			_, err := f.svc.RefreshSeries(f.ctx, s.ID)

			Expect(err).NotTo(HaveOccurred())
			c := changeOf(metadatachange.KindEditionRemoved)
			Expect(c.Payload).To(HaveKeyWithValue("slots", ConsistOf("ebook")))
		})

		It(
			"reports a volume the skeleton lists and Hardcover no longer returns",
			func() {
				f.meta.EXPECT().GetSeries(anyCtx, uint32(50)).
					Return(seriesRecord("One Piece", 2), nil).Once()
				f.meta.EXPECT().GetBooksFresh(anyCtx, ids(1001, 1002)).
					Return([]*metadata.BookRecord{volumeRec(1002)}, nil).Once()

				_, err := f.svc.RefreshSeries(f.ctx, s.ID)

				Expect(err).NotTo(HaveOccurred())
				changeOf(metadatachange.KindBookRemoved)
			},
		)

		It("reviews the volumes of a stale series on the scheduled refresh", func() {
			f.client.BookSeries.UpdateOneID(s.ID).
				SetLastRefreshedAt(time.Now().Add(-48 * time.Hour)).
				ExecX(f.ctx)
			f.meta.EXPECT().GetSeries(anyCtx, uint32(50)).
				Return(seriesRecord("One Piece", 2), nil).Once()
			f.meta.EXPECT().GetBooksFresh(anyCtx, ids(1001, 1002)).
				Return([]*metadata.BookRecord{withoutSlot(), volumeRec(1002)}, nil).
				Once()
			f.meta.EXPECT().ExistingEditions(anyCtx, mock.Anything).
				Return(map[uint32]bool{}, nil).Once()

			Expect(f.svc.RefreshStale(f.ctx)).To(Succeed())

			changeOf(metadatachange.KindEditionRemoved)
		})
	},
)
