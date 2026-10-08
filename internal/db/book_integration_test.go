package db

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/book"
	"github.com/datahearth/streamline/ent/downloadrecord"
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

	Describe("ListWantedBooks", func() {
		ids := func(rows []*ent.Book) []uint32 {
			out := make([]uint32, len(rows))
			for i, r := range rows {
				out[i] = r.ID
			}
			return out
		}

		It("returns books with an eligible slot, author loaded", func() {
			seed()
			got, err := store.ListWantedBooks(ctx, 3)
			Expect(err).NotTo(HaveOccurred())
			Expect(ids(got)).To(ConsistOf(bookByHC(1).ID, bookByHC(2).ID))
			Expect(got[0].Edges.Author).NotTo(BeNil())
			Expect(got[0].Edges.Author.Name).To(Equal("Sanderson"))
		})

		It("drops a slot at the failure cap and one with a live record", func() {
			seed()
			client.Book.UpdateOne(bookByHC(1)).SetEbookGrabFailures(3).ExecX(ctx)
			client.DownloadRecord.Create().
				SetTitle("Warbreaker").
				SetStatus(downloadrecord.StatusDownloading).
				SetBookKind(downloadrecord.BookKindAudiobook).
				SetBookID(bookByHC(2).ID).ExecX(ctx)
			got, err := store.ListWantedBooks(ctx, 3)
			Expect(err).NotTo(HaveOccurred())
			Expect(got).To(BeEmpty())
		})

		It("drops the books of an unmonitored author", func() {
			a := seed()
			Expect(store.SetAuthorMonitored(ctx, a.ID, false)).To(Succeed())
			got, err := store.ListWantedBooks(ctx, 3)
			Expect(err).NotTo(HaveOccurred())
			Expect(got).To(BeEmpty())
		})

		It("bumps, resets and stamps one slot only", func() {
			seed()
			b := bookByHC(1)
			Expect(
				store.IncrementBookSlotGrabFailures(ctx, b.ID, "ebook"),
			).To(Succeed())
			Expect(
				store.IncrementBookSlotGrabFailures(ctx, b.ID, "ebook"),
			).To(Succeed())
			Expect(store.SetBookSlotLastSearchAt(
				ctx, b.ID, "audiobook", time.Now(),
			)).To(Succeed())
			got := bookByHC(1)
			Expect(got.EbookGrabFailures).To(BeEquivalentTo(2))
			Expect(got.AudiobookGrabFailures).To(BeZero())
			Expect(got.EbookLastSearchAt).To(BeNil())
			Expect(got.AudiobookLastSearchAt).NotTo(BeNil())

			Expect(store.ResetBookSlotGrabFailures(ctx, b.ID, "ebook")).To(Succeed())
			Expect(bookByHC(1).EbookGrabFailures).To(BeZero())
			Expect(store.IncrementBookSlotGrabFailures(ctx, b.ID, "x")).
				To(MatchError(ContainSubstring("unknown book slot kind")))
		})
	})

	Describe("SetBookSlotStatus", func() {
		It("moves the named slot only when it is in the from status", func() {
			seed()
			b := bookByHC(1)
			Expect(b.EbookStatus).To(Equal(book.EbookStatusWanted))

			Expect(store.SetBookSlotStatus(
				ctx, b.ID, "ebook", "paused", "downloading")).To(Succeed())
			Expect(client.Book.GetX(ctx, b.ID).EbookStatus).
				To(Equal(book.EbookStatusWanted))

			Expect(store.SetBookSlotStatus(
				ctx, b.ID, "ebook", "wanted", "downloading")).To(Succeed())
			got := client.Book.GetX(ctx, b.ID)
			Expect(got.EbookStatus).To(Equal(book.EbookStatusDownloading))
			Expect(got.AudiobookStatus).To(Equal(book.AudiobookStatusSkipped))
		})

		It("moves the audiobook slot and leaves the ebook slot untouched", func() {
			seed()
			b := bookByHC(2)
			Expect(store.SetBookSlotStatus(
				ctx, b.ID, "audiobook", "wanted", "downloading")).To(Succeed())
			got := client.Book.GetX(ctx, b.ID)
			Expect(got.AudiobookStatus).To(Equal(book.AudiobookStatusDownloading))
			Expect(got.EbookStatus).To(Equal(book.EbookStatusSkipped))
		})

		It("rejects an unknown kind", func() {
			seed()
			b := bookByHC(1)
			err := store.SetBookSlotStatus(
				ctx,
				b.ID,
				"comic",
				"wanted",
				"downloading",
			)
			Expect(err).To(MatchError(ContainSubstring("unknown book slot kind")))
			Expect(client.Book.GetX(ctx, b.ID).EbookStatus).
				To(Equal(book.EbookStatusWanted))
		})
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

	Describe("backlog search", func() {
		var cutoff time.Time

		BeforeEach(func() {
			cutoff = time.Now().Add(-time.Hour)
		})

		ids := func(rows []*ent.Book) []uint32 {
			out := make([]uint32, len(rows))
			for i, r := range rows {
				out[i] = r.ID
			}
			return out
		}

		It("lists a book for one kind and not the other", func() {
			seed()
			ebookOnly, audioOnly := bookByHC(1), bookByHC(2)

			ebooks, err := store.ListEligibleBookSlotsForSync(
				ctx,
				"ebook",
				3,
				cutoff,
			)
			Expect(err).NotTo(HaveOccurred())
			Expect(ids(ebooks)).To(Equal([]uint32{ebookOnly.ID}))
			Expect(ebooks[0].Edges.Author).NotTo(BeNil())

			audio, err := store.ListEligibleBookSlotsForSync(
				ctx, "audiobook", 3, cutoff)
			Expect(err).NotTo(HaveOccurred())
			Expect(ids(audio)).To(Equal([]uint32{audioOnly.ID}))
		})

		It("applies the cap, cooldown and monitored gates per slot", func() {
			seed()
			b1 := bookByHC(1)
			client.Book.UpdateOneID(b1.ID).SetEbookGrabFailures(3).ExecX(ctx)
			rows, err := store.ListEligibleBookSlotsForSync(ctx, "ebook", 3, cutoff)
			Expect(err).NotTo(HaveOccurred())
			Expect(rows).To(BeEmpty())

			client.Book.UpdateOneID(b1.ID).SetEbookGrabFailures(0).
				SetEbookLastSearchAt(time.Now()).ExecX(ctx)
			rows, err = store.ListEligibleBookSlotsForSync(ctx, "ebook", 3, cutoff)
			Expect(err).NotTo(HaveOccurred())
			Expect(rows).To(BeEmpty())

			client.Book.UpdateOneID(b1.ID).ClearEbookLastSearchAt().
				SetEbookMonitored(false).ExecX(ctx)
			rows, err = store.ListEligibleBookSlotsForSync(ctx, "ebook", 3, cutoff)
			Expect(err).NotTo(HaveOccurred())
			Expect(rows).To(BeEmpty())
		})

		It("scopes the in-flight exclusion to the record's book kind", func() {
			seed()
			both := bookByHC(3)
			client.Book.UpdateOneID(both.ID).
				SetEbookMonitored(true).SetEbookStatus(book.EbookStatusWanted).
				SetAudiobookMonitored(true).
				SetAudiobookStatus(book.AudiobookStatusWanted).ExecX(ctx)
			client.DownloadRecord.Create().SetTitle("x").SetStatus("downloading").
				SetBookID(both.ID).
				SetBookKind(downloadrecord.BookKindAudiobook).SaveX(ctx)

			ebooks, err := store.ListEligibleBookSlotsForSync(
				ctx,
				"ebook",
				3,
				cutoff,
			)
			Expect(err).NotTo(HaveOccurred())
			Expect(ids(ebooks)).To(ContainElement(both.ID))

			audio, err := store.ListEligibleBookSlotsForSync(
				ctx, "audiobook", 3, cutoff)
			Expect(err).NotTo(HaveOccurred())
			Expect(ids(audio)).NotTo(ContainElement(both.ID))
		})

		It("orders never-searched first, then oldest slot search", func() {
			seed()
			b1, b3 := bookByHC(1), bookByHC(3)
			client.Book.UpdateOneID(b3.ID).
				SetEbookMonitored(true).SetEbookStatus(book.EbookStatusWanted).
				SetEbookLastSearchAt(time.Now().Add(-5 * time.Hour)).ExecX(ctx)
			client.Book.UpdateOneID(b1.ID).
				SetEbookLastSearchAt(time.Now().Add(-3 * time.Hour)).ExecX(ctx)
			fresh, err := store.CreateAuthor(ctx, CreateAuthorParams{
				HardcoverID: 11, Name: "Other", Monitored: true,
				MonitorPolicy: "all", WantKinds: "ebook",
				Books: []BookSeed{
					{HardcoverID: 9, Title: "Never", EbookMonitored: true},
				},
			})
			Expect(err).NotTo(HaveOccurred())

			rows, err := store.ListEligibleBookSlotsForSync(ctx, "ebook", 3, cutoff)
			Expect(err).NotTo(HaveOccurred())
			Expect(ids(rows)).To(Equal(
				[]uint32{fresh.Edges.Books[0].ID, b3.ID, b1.ID}))
		})

		It("writes only the named slot's search columns", func() {
			seed()
			b := bookByHC(1)
			when := time.Now().Truncate(time.Second)

			Expect(
				store.SetBookSlotLastSearchAt(ctx, b.ID, "ebook", when),
			).To(Succeed())
			Expect(
				store.IncrementBookSlotGrabFailures(ctx, b.ID, "ebook"),
			).To(Succeed())
			Expect(
				store.IncrementBookSlotGrabFailures(ctx, b.ID, "ebook"),
			).To(Succeed())
			got := client.Book.GetX(ctx, b.ID)
			Expect(got.EbookLastSearchAt).
				To(HaveValue(BeTemporally("~", when, time.Second)))
			Expect(got.EbookGrabFailures).To(Equal(uint8(2)))
			Expect(got.AudiobookLastSearchAt).To(BeNil())
			Expect(got.AudiobookGrabFailures).To(BeZero())

			Expect(store.IncrementBookSlotGrabFailures(ctx, b.ID, "audiobook")).
				To(Succeed())
			Expect(store.ResetBookSlotGrabFailures(ctx, b.ID, "ebook")).To(Succeed())
			got = client.Book.GetX(ctx, b.ID)
			Expect(got.EbookGrabFailures).To(BeZero())
			Expect(got.AudiobookGrabFailures).To(Equal(uint8(1)))
		})

		It("rejects an unknown kind on every slot method", func() {
			seed()
			b := bookByHC(1)
			_, err := store.ListEligibleBookSlotsForSync(ctx, "comic", 3, cutoff)
			Expect(err).To(MatchError(ContainSubstring("unknown book slot kind")))
			Expect(store.SetBookSlotLastSearchAt(ctx, b.ID, "comic", time.Now())).
				To(MatchError(ContainSubstring("unknown book slot kind")))
			Expect(store.IncrementBookSlotGrabFailures(ctx, b.ID, "comic")).
				To(MatchError(ContainSubstring("unknown book slot kind")))
			Expect(store.ResetBookSlotGrabFailures(ctx, b.ID, "comic")).
				To(MatchError(ContainSubstring("unknown book slot kind")))
		})

		It("lists stale authors oldest first within the limit", func() {
			never := seed()
			fresh, err := store.CreateAuthor(ctx, CreateAuthorParams{
				HardcoverID:   11,
				Name:          "Fresh",
				MonitorPolicy: "all",
				WantKinds:     "ebook",
			})
			Expect(err).NotTo(HaveOccurred())
			client.Author.UpdateOneID(fresh.ID).
				SetLastRefreshedAt(time.Now()).ExecX(ctx)
			older, err := store.CreateAuthor(ctx, CreateAuthorParams{
				HardcoverID:   12,
				Name:          "Older",
				MonitorPolicy: "all",
				WantKinds:     "ebook",
			})
			Expect(err).NotTo(HaveOccurred())
			client.Author.UpdateOneID(older.ID).
				SetLastRefreshedAt(time.Now().Add(-48 * time.Hour)).ExecX(ctx)

			rows, err := store.ListAuthorsStaleSince(ctx, cutoff, 10)
			Expect(err).NotTo(HaveOccurred())
			Expect(rows).To(HaveLen(2))
			Expect(rows[0].ID).To(Equal(never.ID))
			Expect(rows[1].ID).To(Equal(older.ID))

			rows, err = store.ListAuthorsStaleSince(ctx, cutoff, 1)
			Expect(err).NotTo(HaveOccurred())
			Expect(rows).To(HaveLen(1))
		})
	})

	It("cascades the delete to books", func() {
		a := seed()
		Expect(store.DeleteAuthor(ctx, a.ID)).To(Succeed())
		Expect(client.Book.Query().Count(ctx)).To(BeZero())
	})
})
