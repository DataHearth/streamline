package book

import (
	"context"
	"errors"
	"slices"

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
