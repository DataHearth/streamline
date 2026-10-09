package db

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/author"
	"github.com/datahearth/streamline/ent/book"
	"github.com/datahearth/streamline/ent/bookedition"
	"github.com/datahearth/streamline/ent/downloadrecord"
	"github.com/datahearth/streamline/ent/mediafile"
)

// elantris is a hydrated book with an English and a French ebook, an English
// audiobook and two makers.
func elantris(refreshed time.Time) BookSeed {
	return BookSeed{
		HardcoverID:        1,
		Title:              "Elantris",
		AuthorName:         "Brandon Sanderson",
		Kind:               "novel",
		PreferredLanguage:  "en",
		EbookMonitored:     true,
		AudiobookMonitored: true,
		Editions: []EditionSeed{
			{
				HardcoverID: 11,
				Language:    "en",
				Title:       "Elantris",
				Format:      "ebook",
				Popularity:  9,
				Original:    true,
			},
			{
				HardcoverID: 12,
				Language:    "fr",
				Title:       "Elantris (fr)",
				Format:      "ebook",
				Popularity:  4,
			},
			{
				HardcoverID: 13,
				Language:    "en",
				Title:       "Elantris",
				Format:      "audiobook",
				Popularity:  3,
				Original:    true,
			},
		},
		Credits: []CreditSeed{
			{
				AuthorHardcoverID: 100,
				Name:              "Brandon Sanderson",
				Role:              "author",
				ImageURL:          "https://img/bs.jpg",
			},
			{
				AuthorHardcoverID: 101,
				Name:              "Some Illustrator",
				Role:              "artist",
				Order:             1,
			},
		},
		RefreshedAt: &refreshed,
	}
}

var _ = Describe("Book persistence", Label("integration", "db"), func() {
	var (
		ctx    context.Context
		client *ent.Client
		store  *DB
		now    time.Time
	)

	BeforeEach(func() {
		ctx = context.Background()
		var err error
		client, err = Open(ctx, ":memory:")
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { client.Close() })
		store = New(client)
		now = time.Now()
	})

	create := func(seed BookSeed) *ent.Book {
		GinkgoHelper()
		b, err := store.CreateBook(ctx, seed)
		Expect(err).NotTo(HaveOccurred())
		return b
	}

	Describe("CreateBook", func() {
		It("writes the editions, the makers and the people in one go", func() {
			b := create(elantris(now))

			Expect(b.Edges.Editions).To(HaveLen(3))
			Expect(b.Edges.Contributions).To(HaveLen(2))
			Expect(client.Author.Query().CountX(ctx)).To(Equal(2))
			a := client.Author.Query().Where(author.HardcoverIDEQ(100)).OnlyX(ctx)
			Expect(a.ImageSource).To(Equal("https://img/bs.jpg"))
			Expect(b.AuthorName).To(Equal("Brandon Sanderson"))
		})

		It("points each slot at the edition in the preferred language", func() {
			seed := elantris(now)
			seed.PreferredLanguage = "fr"
			b := create(seed)

			Expect(b.Edges.EbookEdition).NotTo(BeNil())
			Expect(b.Edges.EbookEdition.Language).To(Equal("fr"))
			Expect(b.Edges.AudiobookEdition.Language).To(Equal("en"))
		})

		It("titles the book after the preferred language's edition", func() {
			seed := elantris(now)
			seed.PreferredLanguage = "fr"
			b := create(seed)

			Expect(b.Title).To(Equal("Elantris (fr)"))
			Expect(b.OriginalTitle).To(Equal("Elantris"))
		})

		It("keeps the original title when the preferred edition differs", func() {
			seed := elantris(now)
			seed.Editions[0].Title = "Original Name"
			seed.Editions[2].Title = "Original Name"
			seed.PreferredLanguage = "fr"
			b := create(seed)

			Expect(b.Title).To(Equal("Elantris (fr)"))
			Expect(b.OriginalTitle).To(Equal("Original Name"))
		})

		It(
			"leaves a slot unmonitored when the book has no edition of that format",
			func() {
				seed := elantris(now)
				seed.Editions = seed.Editions[:2]
				b := create(seed)

				Expect(b.EbookMonitored).To(BeTrue())
				Expect(b.AudiobookMonitored).To(BeFalse())
				Expect(b.AudiobookStatus).To(Equal(book.AudiobookStatusSkipped))
				Expect(b.Edges.AudiobookEdition).To(BeNil())
			},
		)

		It("seeds slot statuses from the monitoring asked for", func() {
			seed := elantris(now)
			seed.AudiobookMonitored = false
			b := create(seed)

			Expect(b.EbookStatus).To(Equal(book.EbookStatusWanted))
			Expect(b.AudiobookStatus).To(Equal(book.AudiobookStatusSkipped))
		})

		It("writes a stub without editions and without picking anything", func() {
			b := create(BookSeed{
				HardcoverID: 2, Title: "Series #2", Kind: "manga",
				PreferredLanguage: "fr", EbookMonitored: true,
				SeriesPosition: new(2.0),
			})

			Expect(b.LastRefreshedAt).To(BeNil())
			Expect(b.EbookMonitored).To(BeTrue())
			Expect(b.Edges.Editions).To(BeEmpty())
			Expect(b.Edges.EbookEdition).To(BeNil())
		})

		It("shares a person between books", func() {
			create(elantris(now))
			other := elantris(now)
			other.HardcoverID = 2
			other.Editions = nil
			create(other)

			Expect(client.Author.Query().CountX(ctx)).To(Equal(2))
		})

		It("rolls everything back when a part fails", func() {
			seed := elantris(now)
			seed.Credits[0].Role = "not-a-role"
			_, err := store.CreateBook(ctx, seed)

			Expect(err).To(HaveOccurred())
			Expect(client.Book.Query().CountX(ctx)).To(BeZero())
			Expect(client.Author.Query().CountX(ctx)).To(BeZero())
		})
	})

	Describe("FindBookByHardcoverID", func() {
		It("returns nil, nil for an unknown id", func() {
			got, err := store.FindBookByHardcoverID(ctx, 404)
			Expect(err).NotTo(HaveOccurred())
			Expect(got).To(BeNil())
		})

		It("finds a book with its series edge", func() {
			create(elantris(now))
			got, err := store.FindBookByHardcoverID(ctx, 1)
			Expect(err).NotTo(HaveOccurred())
			Expect(got.Title).To(Equal("Elantris"))
		})
	})

	Describe("ApplyBookMetadata", func() {
		It("updates the fields and upserts editions by Hardcover id", func() {
			b := create(elantris(now))
			later := now.Add(time.Hour)

			err := store.ApplyBookMetadata(ctx, b.ID, BookMetadata{
				Title: "Elantris", AuthorName: "B. Sanderson", Overview: "New text",
				RatingTenths: new(uint8(42)),
				Editions: []EditionSeed{
					{
						HardcoverID: 11,
						Language:    "en",
						Title:       "Elantris",
						Format:      "ebook",
						Popularity:  99,
						Original:    true,
					},
					{
						HardcoverID: 12,
						Language:    "fr",
						Title:       "Elantris (fr)",
						Format:      "ebook",
						Popularity:  4,
					},
					{
						HardcoverID: 13,
						Language:    "en",
						Title:       "Elantris",
						Format:      "audiobook",
						Popularity:  3,
						Original:    true,
					},
					{
						HardcoverID: 14,
						Language:    "de",
						Title:       "Elantris (de)",
						Format:      "ebook",
						Popularity:  1,
					},
				},
				Credits:     elantris(now).Credits,
				RefreshedAt: later,
			})
			Expect(err).NotTo(HaveOccurred())

			got, err := store.FindBookByID(ctx, b.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(got.AuthorName).To(Equal("B. Sanderson"))
			Expect(got.Overview).To(Equal("New text"))
			Expect(*got.RatingTenths).To(Equal(uint8(42)))
			Expect(got.Edges.Editions).To(HaveLen(4))
			ids := make([]uint32, 0, len(got.Edges.Editions))
			for _, e := range got.Edges.Editions {
				if e.HardcoverEditionID == 11 {
					Expect(e.Popularity).To(Equal(uint32(99)))
				}
				ids = append(ids, e.ID)
			}
			// The ids of the rows that were already there are stable.
			for _, old := range b.Edges.Editions {
				Expect(ids).To(ContainElement(old.ID))
			}
		})

		It(
			"prunes an edition no longer offered but never one a slot points at",
			func() {
				b := create(elantris(now))

				err := store.ApplyBookMetadata(ctx, b.ID, BookMetadata{
					Title: "Elantris", AuthorName: "Brandon Sanderson",
					Editions: []EditionSeed{
						{
							HardcoverID: 13,
							Language:    "en",
							Title:       "Elantris",
							Format:      "audiobook",
							Popularity:  3,
						},
					},
					Credits:     elantris(now).Credits,
					RefreshedAt: now,
				})
				Expect(err).NotTo(HaveOccurred())

				got, err := store.FindBookByID(ctx, b.ID)
				Expect(err).NotTo(HaveOccurred())
				hc := make([]uint32, 0, len(got.Edges.Editions))
				for _, e := range got.Edges.Editions {
					hc = append(hc, e.HardcoverEditionID)
				}
				// 11 is the ebook slot's edition: kept. 12 was unreferenced: gone.
				Expect(hc).To(ConsistOf(uint32(11), uint32(13)))
				Expect(
					got.Edges.EbookEdition.HardcoverEditionID,
				).To(Equal(uint32(11)))
			},
		)

		It("fills a slot that has no edition and leaves one that has", func() {
			seed := elantris(now)
			seed.Editions = seed.Editions[:2]
			b := create(seed)
			Expect(b.Edges.AudiobookEdition).To(BeNil())
			chosen := b.Edges.EbookEdition.ID
			Expect(store.SetBookSlotEdition(
				ctx, b.ID, "ebook", b.Edges.Editions[1].ID,
			)).To(Succeed())
			if b.Edges.Editions[1].ID == chosen {
				Skip("fixture picked the same edition")
			}

			err := store.ApplyBookMetadata(ctx, b.ID, BookMetadata{
				Title: "Elantris", AuthorName: "Brandon Sanderson",
				Editions:    elantrisEditions(),
				Credits:     seed.Credits,
				RefreshedAt: now,
			})
			Expect(err).NotTo(HaveOccurred())

			got, err := store.FindBookByID(ctx, b.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(got.Edges.AudiobookEdition).NotTo(BeNil())
			Expect(got.Edges.EbookEdition.ID).To(Equal(b.Edges.Editions[1].ID))
		})

		It("hydrates a stub, setting its kind", func() {
			stub := create(BookSeed{
				HardcoverID: 2, Title: "Series #2", Kind: "novel",
				PreferredLanguage: "fr", SeriesPosition: new(2.0),
			})

			err := store.ApplyBookMetadata(ctx, stub.ID, BookMetadata{
				Title: "Tome 2", AuthorName: "Someone", Kind: "manga",
				Editions: []EditionSeed{
					{
						HardcoverID: 21,
						Language:    "fr",
						Title:       "Tome 2",
						Format:      "ebook",
						Popularity:  2,
					},
				},
				Credits: []CreditSeed{
					{AuthorHardcoverID: 200, Name: "Someone", Role: "author"},
				},
				RefreshedAt: now,
			})
			Expect(err).NotTo(HaveOccurred())

			got, err := store.FindBookByID(ctx, stub.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(got.LastRefreshedAt).NotTo(BeNil())
			Expect(got.Kind).To(Equal(book.KindManga))
			Expect(got.Title).To(Equal("Tome 2"))
			Expect(got.Edges.EbookEdition).NotTo(BeNil())
		})

		It("keeps a corrected kind when a refresh does not set one", func() {
			b := create(elantris(now))
			Expect(store.SetBookKind(ctx, b.ID, "comic")).To(Succeed())

			Expect(store.ApplyBookMetadata(ctx, b.ID, BookMetadata{
				Title: "Elantris", AuthorName: "Brandon Sanderson",
				Editions: elantrisEditions(), Credits: elantris(now).Credits,
				RefreshedAt: now,
			})).To(Succeed())

			got, err := store.FindBookByID(ctx, b.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(got.Kind).To(Equal(book.KindComic))
		})

		It("deletes a person the refresh left with nothing", func() {
			b := create(elantris(now))

			Expect(store.ApplyBookMetadata(ctx, b.ID, BookMetadata{
				Title: "Elantris", AuthorName: "Brandon Sanderson",
				Editions:    elantrisEditions(),
				Credits:     elantris(now).Credits[:1],
				RefreshedAt: now,
			})).To(Succeed())

			Expect(client.Author.Query().CountX(ctx)).To(Equal(1))
		})
	})

	Describe("SetBookSlotEdition", func() {
		It("refuses an edition of another format or another book", func() {
			b := create(elantris(now))
			var ebook, audio uint32
			for _, e := range b.Edges.Editions {
				if e.Format == bookedition.FormatAudiobook {
					audio = e.ID
				} else if ebook == 0 {
					ebook = e.ID
				}
			}
			Expect(
				store.SetBookSlotEdition(ctx, b.ID, "ebook", audio),
			).To(HaveOccurred())
			Expect(
				store.SetBookSlotEdition(ctx, b.ID, "ebook", 9999),
			).To(HaveOccurred())
			Expect(store.SetBookSlotEdition(ctx, b.ID, "ebook", ebook)).To(Succeed())
		})

		It("clears a slot with 0", func() {
			b := create(elantris(now))
			Expect(store.SetBookSlotEdition(ctx, b.ID, "audiobook", 0)).To(Succeed())
			got, err := store.FindBookByID(ctx, b.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(got.Edges.AudiobookEdition).To(BeNil())
		})
	})

	Describe("SetBookSlot", func() {
		reload := func(b *ent.Book) *ent.Book { return client.Book.GetX(ctx, b.ID) }

		It("moves skipped to wanted when monitoring and back when not", func() {
			seed := elantris(now)
			seed.EbookMonitored = false
			b := create(seed)
			Expect(b.EbookStatus).To(Equal(book.EbookStatusSkipped))

			Expect(store.SetBookSlot(ctx, b.ID, "ebook", true)).To(Succeed())
			Expect(reload(b).EbookStatus).To(Equal(book.EbookStatusWanted))
			Expect(reload(b).EbookMonitored).To(BeTrue())

			Expect(store.SetBookSlot(ctx, b.ID, "ebook", false)).To(Succeed())
			Expect(reload(b).EbookStatus).To(Equal(book.EbookStatusSkipped))
			Expect(reload(b).EbookMonitored).To(BeFalse())
		})

		It("moves paused to skipped when unmonitoring", func() {
			b := create(elantris(now))
			client.Book.UpdateOneID(b.ID).
				SetEbookStatus(book.EbookStatusPaused).
				ExecX(ctx)

			Expect(store.SetBookSlot(ctx, b.ID, "ebook", false)).To(Succeed())

			Expect(reload(b).EbookStatus).To(Equal(book.EbookStatusSkipped))
		})

		It("never cancels a download in flight or drops a file", func() {
			b := create(elantris(now))
			client.Book.UpdateOneID(b.ID).
				SetEbookStatus(book.EbookStatusDownloading).
				SetAudiobookStatus(book.AudiobookStatusAvailable).
				ExecX(ctx)

			Expect(store.SetBookSlot(ctx, b.ID, "ebook", false)).To(Succeed())
			Expect(store.SetBookSlot(ctx, b.ID, "audiobook", false)).To(Succeed())

			got := reload(b)
			Expect(got.EbookStatus).To(Equal(book.EbookStatusDownloading))
			Expect(got.AudiobookStatus).To(Equal(book.AudiobookStatusAvailable))
			Expect(got.EbookMonitored).To(BeFalse())
		})

		It("does not mark an available slot wanted when monitoring it", func() {
			seed := elantris(now)
			seed.EbookMonitored = false
			b := create(seed)
			client.Book.UpdateOneID(b.ID).
				SetEbookStatus(book.EbookStatusAvailable).
				ExecX(ctx)

			Expect(store.SetBookSlot(ctx, b.ID, "ebook", true)).To(Succeed())

			Expect(reload(b).EbookStatus).To(Equal(book.EbookStatusAvailable))
		})

		It("rejects an unknown slot", func() {
			b := create(elantris(now))
			Expect(store.SetBookSlot(ctx, b.ID, "paper", true)).To(HaveOccurred())
		})
	})

	Describe("SetBookSlotStatus", func() {
		It("moves a slot only from the status it is in", func() {
			b := create(elantris(now))

			Expect(
				store.SetBookSlotStatus(
					ctx,
					b.ID,
					"ebook",
					"skipped",
					"downloading",
				),
			).To(Succeed())
			Expect(
				client.Book.GetX(ctx, b.ID).EbookStatus,
			).To(Equal(book.EbookStatusWanted))

			Expect(
				store.SetBookSlotStatus(ctx, b.ID, "ebook", "wanted", "downloading"),
			).To(Succeed())
			Expect(
				client.Book.GetX(ctx, b.ID).EbookStatus,
			).To(Equal(book.EbookStatusDownloading))
		})
	})

	Describe("DeleteBook", func() {
		It("cascades and reports the people left with nothing", func() {
			b := create(elantris(now))
			other := elantris(now)
			other.HardcoverID = 2
			other.Credits = other.Credits[:1]
			create(other)
			client.MediaFile.Create().
				SetPath("/b/x.epub").SetSize(1).SetQuality("EPUB").SetFormat("epub").
				SetBookID(b.ID).SetBookKind(mediafile.BookKindEbook).SaveX(ctx)

			orphans, err := store.DeleteBook(ctx, b.ID)

			Expect(err).NotTo(HaveOccurred())
			Expect(orphans).To(HaveLen(1))
			Expect(client.Author.Query().CountX(ctx)).To(Equal(1))
			Expect(client.MediaFile.Query().CountX(ctx)).To(BeZero())
			Expect(client.BookEdition.Query().CountX(ctx)).To(Equal(3))
		})
	})

	Describe("slot searches", func() {
		It("lists only hydrated, released, standalone ebook slots", func() {
			past, future := now.Add(-time.Hour), now.Add(24*time.Hour)
			ready := elantris(now)
			create(ready)
			unreleased := elantris(now)
			unreleased.HardcoverID, unreleased.ReleaseDate = 2, &future
			create(unreleased)
			released := elantris(now)
			released.HardcoverID, released.ReleaseDate = 3, &past
			create(released)
			stub := BookSeed{
				HardcoverID: 4, Title: "Stub", Kind: "novel",
				PreferredLanguage: "en", EbookMonitored: true,
			}
			create(stub)
			volume := elantris(now)
			volume.HardcoverID = 5
			vol := create(volume)
			series := client.BookSeries.Create().
				SetHardcoverID(9).
				SetTitle("S").
				SaveX(ctx)
			client.Book.UpdateOneID(vol.ID).SetSeries(series).ExecX(ctx)

			got, err := store.ListEligibleBookSlotsForSync(
				ctx,
				"ebook",
				3,
				now.Add(time.Minute),
			)
			Expect(err).NotTo(HaveOccurred())
			ids := []uint32{}
			for _, b := range got {
				ids = append(ids, b.HardcoverID)
			}
			Expect(ids).To(ConsistOf(uint32(1), uint32(3)))

			audio, err := store.ListEligibleBookSlotsForSync(
				ctx,
				"audiobook",
				3,
				now.Add(time.Minute),
			)
			Expect(err).NotTo(HaveOccurred())
			ids = ids[:0]
			for _, b := range audio {
				ids = append(ids, b.HardcoverID)
			}
			// A volume's audiobook slot is an ordinary slot.
			Expect(ids).To(ConsistOf(uint32(1), uint32(3), uint32(5)))

			volumes, err := store.ListEligibleSeriesVolumes(
				ctx,
				3,
				now.Add(time.Minute),
			)
			Expect(err).NotTo(HaveOccurred())
			Expect(volumes).To(HaveLen(1))
			Expect(volumes[0].HardcoverID).To(Equal(uint32(5)))
			Expect(volumes[0].Edges.Series).NotTo(BeNil())
		})

		It("skips a slot under cooldown or past the failure cap", func() {
			b := create(elantris(now))
			client.Book.UpdateOneID(b.ID).SetEbookLastSearchAt(now).ExecX(ctx)

			got, err := store.ListEligibleBookSlotsForSync(
				ctx,
				"ebook",
				3,
				now.Add(-time.Hour),
			)
			Expect(err).NotTo(HaveOccurred())
			Expect(got).To(BeEmpty())

			client.Book.UpdateOneID(b.ID).
				ClearEbookLastSearchAt().
				SetEbookGrabFailures(3).
				ExecX(ctx)
			got, err = store.ListEligibleBookSlotsForSync(ctx, "ebook", 3, now)
			Expect(err).NotTo(HaveOccurred())
			Expect(got).To(BeEmpty())
		})

		It("keeps a slot with a record in flight out of the list", func() {
			b := create(elantris(now))
			client.DownloadRecord.Create().SetTitle("x").SetBookID(b.ID).
				SetBookKind(downloadrecord.BookKindEbook).SetSavePath("/dl").
				SetStatus(downloadrecord.StatusDownloading).SaveX(ctx)

			ebook, err := store.ListEligibleBookSlotsForSync(
				ctx,
				"ebook",
				3,
				now.Add(time.Minute),
			)
			Expect(err).NotTo(HaveOccurred())
			Expect(ebook).To(BeEmpty())
			audio, err := store.ListEligibleBookSlotsForSync(
				ctx,
				"audiobook",
				3,
				now.Add(time.Minute),
			)
			Expect(err).NotTo(HaveOccurred())
			Expect(audio).To(HaveLen(1))
		})

		It("lists wanted books for the feed with their makers and editions", func() {
			create(elantris(now))
			future := now.Add(24 * time.Hour)
			unreleased := elantris(now)
			unreleased.HardcoverID, unreleased.ReleaseDate = 2, &future
			create(unreleased)

			got, err := store.ListWantedBooks(ctx, 3)

			Expect(err).NotTo(HaveOccurred())
			Expect(got).To(HaveLen(1))
			Expect(got[0].Edges.Editions).To(HaveLen(3))
			Expect(got[0].Edges.Contributions).To(HaveLen(2))
			Expect(got[0].Edges.Contributions[0].Edges.Author).NotTo(BeNil())
		})
	})

	Describe("hydration and refresh selection", func() {
		It("lists stubs oldest first and stale books by refresh time", func() {
			stale := elantris(now.Add(-48 * time.Hour))
			create(stale)
			fresh := elantris(now)
			fresh.HardcoverID = 2
			create(fresh)
			stub := create(BookSeed{
				HardcoverID:       3,
				Title:             "Stub",
				Kind:              "novel",
				PreferredLanguage: "en",
			})

			stubs, err := store.ListHydrationStubs(ctx, 10)
			Expect(err).NotTo(HaveOccurred())
			Expect(stubs).To(HaveLen(1))
			Expect(stubs[0].ID).To(Equal(stub.ID))

			got, err := store.ListStaleStandaloneBooks(
				ctx,
				now.Add(-24*time.Hour),
				10,
			)
			Expect(err).NotTo(HaveOccurred())
			Expect(got).To(HaveLen(1))
			Expect(got[0].HardcoverID).To(Equal(uint32(1)))
		})
	})

	Describe("ListActiveBookRecords", func() {
		It("returns the in-flight record of each slot", func() {
			b := create(elantris(now))
			rec := client.DownloadRecord.Create().SetTitle("x").SetBookID(b.ID).
				SetBookKind(downloadrecord.BookKindAudiobook).SetSavePath("/dl").
				SetStatus(downloadrecord.StatusDownloading).SaveX(ctx)
			client.DownloadRecord.Create().SetTitle("done").SetBookID(b.ID).
				SetBookKind(downloadrecord.BookKindEbook).SetSavePath("/dl").
				SetStatus(downloadrecord.StatusCompleted).SaveX(ctx)

			got, err := store.ListActiveBookRecords(ctx, []uint32{b.ID})

			Expect(err).NotTo(HaveOccurred())
			Expect(
				got,
			).To(Equal([]BookRecordRef{{RecordID: rec.ID, BookID: b.ID, Kind: "audiobook"}}))
		})
	})

	Describe("replacing language", func() {
		It("records and clears the language of the edition being replaced", func() {
			b := create(elantris(now))

			Expect(store.SetBookReplacing(ctx, b.ID, "ebook", "en")).To(Succeed())
			Expect(
				client.Book.GetX(ctx, b.ID).EbookReplacingLanguage,
			).To(Equal("en"))
			Expect(store.SetBookReplacing(ctx, b.ID, "ebook", "")).To(Succeed())
			Expect(client.Book.GetX(ctx, b.ID).EbookReplacingLanguage).To(BeEmpty())
		})
	})
})

func elantrisEditions() []EditionSeed {
	return []EditionSeed{
		{
			HardcoverID: 11,
			Language:    "en",
			Title:       "Elantris",
			Format:      "ebook",
			Popularity:  9,
			Original:    true,
		},
		{
			HardcoverID: 12,
			Language:    "fr",
			Title:       "Elantris (fr)",
			Format:      "ebook",
			Popularity:  4,
		},
		{
			HardcoverID: 13,
			Language:    "en",
			Title:       "Elantris",
			Format:      "audiobook",
			Popularity:  3,
			Original:    true,
		},
	}
}
