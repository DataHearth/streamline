package db

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/book"
	"github.com/datahearth/streamline/ent/mediafile"
)

var _ = Describe("Book persistence", Label("integration", "db"), func() {
	var (
		ctx    context.Context
		client *ent.Client
		store  *DB
	)

	BeforeEach(func() {
		ctx = context.Background()
		var err error
		client, err = Open(ctx, ":memory:")
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { client.Close() })
		store = New(client)
	})

	seed := func() *ent.Author {
		GinkgoHelper()
		a, err := store.CreateAuthor(ctx, CreateAuthorParams{
			HardcoverID: 10, Name: "Sanderson", Monitored: true,
			MonitorPolicy: "all", WantKinds: "ebook",
			Books: []BookSeed{
				{HardcoverID: 1, Title: "Elantris", EbookMonitored: true},
				{HardcoverID: 2, Title: "Warbreaker", AudiobookMonitored: true},
				{HardcoverID: 3, Title: "Mistborn"},
			},
		})
		Expect(err).NotTo(HaveOccurred())
		return a
	}

	bookByHC := func(hc uint32) *ent.Book {
		GinkgoHelper()
		return client.Book.Query().Where(book.HardcoverIDEQ(hc)).OnlyX(ctx)
	}

	It("seeds slot statuses from the flags each seed carries", func() {
		a := seed()
		Expect(a.Edges.Books).To(HaveLen(3))
		b1, b2, b3 := bookByHC(1), bookByHC(2), bookByHC(3)
		Expect(b1.EbookStatus).To(Equal(book.EbookStatusWanted))
		Expect(b1.AudiobookStatus).To(Equal(book.AudiobookStatusSkipped))
		Expect(b2.EbookStatus).To(Equal(book.EbookStatusSkipped))
		Expect(b2.AudiobookStatus).To(Equal(book.AudiobookStatusWanted))
		Expect(b3.EbookStatus).To(Equal(book.EbookStatusSkipped))
		Expect(b3.AudiobookStatus).To(Equal(book.AudiobookStatusSkipped))
	})

	It("returns nil, nil for an unknown hardcover id", func() {
		row, err := store.FindAuthorByHardcoverID(ctx, 404)
		Expect(err).NotTo(HaveOccurred())
		Expect(row).To(BeNil())
	})

	It("flips an empty slot on monitor and unmonitor", func() {
		seed()
		b := bookByHC(3)
		Expect(store.SetBookSlot(ctx, b.ID, "ebook", true)).To(Succeed())
		got := client.Book.GetX(ctx, b.ID)
		Expect(got.EbookMonitored).To(BeTrue())
		Expect(got.EbookStatus).To(Equal(book.EbookStatusWanted))
		Expect(store.SetBookSlot(ctx, b.ID, "ebook", false)).To(Succeed())
		got = client.Book.GetX(ctx, b.ID)
		Expect(got.EbookMonitored).To(BeFalse())
		Expect(got.EbookStatus).To(Equal(book.EbookStatusSkipped))
	})

	It("keeps a slot with a file available", func() {
		seed()
		b := bookByHC(1)
		client.MediaFile.Create().SetPath("/x.epub").SetSize(1).SetBookID(b.ID).
			SetBookKind(mediafile.BookKindEbook).SaveX(ctx)
		client.Book.UpdateOneID(b.ID).
			SetEbookStatus(book.EbookStatusAvailable).ExecX(ctx)
		Expect(store.SetBookSlot(ctx, b.ID, "ebook", false)).To(Succeed())
		got := client.Book.GetX(ctx, b.ID)
		Expect(got.EbookMonitored).To(BeFalse())
		Expect(got.EbookStatus).To(Equal(book.EbookStatusAvailable))
	})

	It("leaves a downloading slot alone", func() {
		seed()
		b := bookByHC(1)
		client.Book.UpdateOneID(b.ID).
			SetEbookStatus(book.EbookStatusDownloading).ExecX(ctx)
		Expect(store.SetBookSlot(ctx, b.ID, "ebook", false)).To(Succeed())
		Expect(client.Book.GetX(ctx, b.ID).EbookStatus).
			To(Equal(book.EbookStatusDownloading))
	})

	It(
		"refreshes metadata only for known books, seeds new ones, persists last_refreshed_at",
		func() {
			a := seed()
			b1 := bookByHC(1)
			Expect(store.RefreshAuthor(ctx, a.ID, RefreshAuthorParams{
				Name: "Sanderson", RefreshedAt: time.Now(),
				Books: []BookSeed{
					{HardcoverID: 1, Title: "Elantris (10th)"},
					{
						HardcoverID:        4,
						Title:              "Emperor's Soul",
						AudiobookMonitored: true,
					},
				},
			})).To(Succeed())
			got := client.Book.GetX(ctx, b1.ID)
			Expect(got.Title).To(Equal("Elantris (10th)"))
			Expect(got.EbookMonitored).To(BeTrue())
			Expect(got.EbookStatus).To(Equal(book.EbookStatusWanted))
			nb := bookByHC(4)
			Expect(nb.AudiobookStatus).To(Equal(book.AudiobookStatusWanted))
			Expect(nb.EbookStatus).To(Equal(book.EbookStatusSkipped))
			Expect(client.Author.GetX(ctx, a.ID).LastRefreshedAt).NotTo(BeNil())
		},
	)

	It("cascades the delete to books", func() {
		a := seed()
		Expect(store.DeleteAuthor(ctx, a.ID)).To(Succeed())
		Expect(client.Book.Query().Count(ctx)).To(BeZero())
	})
})
