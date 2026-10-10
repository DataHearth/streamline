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

var _ = Describe("Book shelf", Label("integration", "db"), func() {
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

	standalone := func(hc uint32, title, authorName, kind string, year uint16) *ent.Book {
		GinkgoHelper()
		b, err := store.CreateBook(ctx, BookSeed{
			HardcoverID: hc, Title: title, AuthorName: authorName, Kind: kind,
			PreferredLanguage: "en", ReleaseYear: &year,
			EbookMonitored: true, AudiobookMonitored: true,
			Editions: []EditionSeed{
				{
					HardcoverID: hc*10 + 1,
					Language:    "en",
					Title:       title,
					Format:      "ebook",
					Popularity:  1,
				},
				{
					HardcoverID: hc*10 + 2,
					Language:    "en",
					Title:       title,
					Format:      "audiobook",
					Popularity:  1,
				},
			},
			Credits: []CreditSeed{{
				AuthorHardcoverID: 1000 + hc, Name: authorName, Role: "author",
			}},
			RefreshedAt: &now,
		})
		Expect(err).NotTo(HaveOccurred())
		return b
	}
	setSlots := func(b *ent.Book, ebook, audio string, ebookMon, audioMon bool) {
		GinkgoHelper()
		client.Book.UpdateOneID(b.ID).
			SetEbookStatus(book.EbookStatus(ebook)).
			SetAudiobookStatus(book.AudiobookStatus(audio)).
			SetEbookMonitored(ebookMon).
			SetAudiobookMonitored(audioMon).
			ExecX(ctx)
	}
	names := func(rows []ShelfRow) []string {
		out := make([]string, 0, len(rows))
		for _, r := range rows {
			out = append(out, r.Type+":"+r.Title)
		}
		return out
	}
	list := func(p ShelfParams) ([]ShelfRow, uint32) {
		GinkgoHelper()
		if p.Limit == 0 {
			p.Limit = 50
		}
		if p.Sort == "" {
			p.Sort = "title"
		}
		rows, total, err := store.ListShelf(ctx, p)
		Expect(err).NotTo(HaveOccurred())
		return rows, total
	}

	var elantris, warbreaker, mistborn, unreleased *ent.Book

	BeforeEach(func() {
		elantris = standalone(1, "Elantris", "Brandon Sanderson", "novel", 2005)
		setSlots(elantris, "available", "skipped", true, false)
		warbreaker = standalone(2, "Warbreaker", "Brandon Sanderson", "novel", 2009)
		setSlots(warbreaker, "wanted", "downloading", true, true)
		mistborn = standalone(3, "Mistborn", "Brandon Sanderson", "novel", 2006)
		setSlots(mistborn, "skipped", "skipped", false, false)
		unreleased = standalone(4, "Future Book", "Éric Zèbre", "novel", 2030)
		setSlots(unreleased, "wanted", "wanted", true, true)
		client.Book.UpdateOneID(unreleased.ID).
			SetReleaseDate(now.Add(48 * time.Hour)).ExecX(ctx)

		mk := func(hc uint32, title string, monitor string, vols ...BookSeed) *ent.BookSeries {
			s, _, err := store.CreateSeries(ctx, CreateSeriesParams{
				HardcoverID: hc, Title: title, AuthorName: "Mangaka " + title,
				Kind: "manga", Monitor: monitor, EditionLanguage: "en",
				RefreshedAt: now, Volumes: vols,
				Credits: []CreditSeed{
					{
						AuthorHardcoverID: 2000 + hc,
						Name:              "Mangaka " + title,
						Role:              "author",
					},
				},
			})
			Expect(err).NotTo(HaveOccurred())
			return s
		}
		mk(
			100,
			"One Piece",
			"all",
			volumeSeed(101, 1, now),
			volumeSeed(102, 2, now),
		)
		mk(200, "Naruto", "none", volumeSeed(201, 1, now))
		mk(300, "Berserk", "all", volumeSeed(301, 1, now), BookSeed{
			HardcoverID:       302,
			Title:             "Berserk #2",
			Kind:              "manga",
			PreferredLanguage: "en",
			SeriesPosition:    new(2.0),
			EbookMonitored:    true,
		})

		vol := func(hc uint32) *ent.Book {
			return client.Book.Query().Where(book.HardcoverIDEQ(hc)).OnlyX(ctx)
		}
		client.Book.UpdateOneID(vol(101).ID).
			SetEbookStatus(book.EbookStatusAvailable).ExecX(ctx)
		client.Book.UpdateOneID(vol(102).ID).
			SetEbookStatus(book.EbookStatusDownloading).ExecX(ctx)
		client.Book.UpdateOneID(vol(201).ID).
			SetEbookMonitored(false).
			SetEbookStatus(book.EbookStatusSkipped).ExecX(ctx)
	})

	Describe("ListShelf", func() {
		It("merges standalone books and series, volumes never on their own", func() {
			rows, total := list(ShelfParams{})

			Expect(total).To(Equal(uint32(7)))
			Expect(names(rows)).To(Equal([]string{
				"series:Berserk",
				"book:Elantris",
				"book:Future Book",
				"book:Mistborn",
				"series:Naruto",
				"series:One Piece",
				"book:Warbreaker",
			}))
		})

		It("derives a book's status and slot states", func() {
			rows, _ := list(ShelfParams{})
			byTitle := map[string]ShelfRow{}
			for _, r := range rows {
				byTitle[r.Title] = r
			}

			Expect(byTitle["Elantris"].Status).To(Equal("available"))
			Expect(byTitle["Elantris"].EbookState).To(Equal("available"))
			Expect(byTitle["Elantris"].AudiobookState).To(Equal("unmonitored"))
			Expect(byTitle["Warbreaker"].Status).To(Equal("downloading"))
			Expect(byTitle["Warbreaker"].EbookState).To(Equal("wanted"))
			Expect(byTitle["Mistborn"].Status).To(Equal("available"))
			Expect(byTitle["Mistborn"].EbookState).To(Equal("unmonitored"))
			// A book not yet released ignores its slots.
			Expect(byTitle["Future Book"].Status).To(Equal("available"))
			Expect(byTitle["Warbreaker"].Year).To(Equal(uint16(2009)))
		})

		It("derives a series' status from its released, hydrated volumes", func() {
			rows, _ := list(ShelfParams{})
			byTitle := map[string]ShelfRow{}
			for _, r := range rows {
				byTitle[r.Title] = r
			}

			Expect(byTitle["One Piece"].Status).To(Equal("downloading"))
			Expect(byTitle["One Piece"].VolumesHave).To(Equal(uint32(1)))
			Expect(byTitle["One Piece"].VolumesOut).To(Equal(uint32(2)))
			// Naruto is not monitored: its wanted-looking volume does not count.
			Expect(byTitle["Naruto"].Status).To(Equal("available"))
			// A stub of Berserk is ignored while it hydrates.
			Expect(byTitle["Berserk"].Status).To(Equal("wanted"))
			Expect(byTitle["Berserk"].VolumesOut).To(Equal(uint32(2)))
			Expect(byTitle["One Piece"].CoverID).To(Equal(
				client.Book.Query().Where(book.HardcoverIDEQ(101)).OnlyX(ctx).ID,
			))
		})

		It(
			"leaves out an upcoming volume from the status and the out count",
			func() {
				future := now.Add(24 * time.Hour)
				v := client.Book.Query().Where(book.HardcoverIDEQ(102)).OnlyX(ctx)
				client.Book.UpdateOneID(v.ID).SetReleaseDate(future).ExecX(ctx)

				rows, _ := list(ShelfParams{Query: "one piece"})

				Expect(rows).To(HaveLen(1))
				Expect(rows[0].Status).To(Equal("available"))
				Expect(rows[0].VolumesOut).To(Equal(uint32(1)))
			},
		)

		It("pages after the union, ordered across both populations", func() {
			rows, total := list(ShelfParams{Limit: 3, Offset: 3})

			Expect(total).To(Equal(uint32(7)))
			Expect(names(rows)).To(Equal([]string{
				"book:Mistborn", "series:Naruto", "series:One Piece",
			}))
		})

		DescribeTable(
			"filters",
			func(p ShelfParams, want ...string) {
				rows, total := list(p)
				Expect(names(rows)).To(ConsistOf(want))
				Expect(total).To(Equal(uint32(len(want))))
			},
			Entry("by status", ShelfParams{Status: "wanted"}, "series:Berserk"),
			Entry("by downloading", ShelfParams{Status: "downloading"},
				"series:One Piece", "book:Warbreaker"),
			Entry("by a kind", ShelfParams{Kinds: []string{"manga"}},
				"series:Berserk", "series:Naruto", "series:One Piece"),
			Entry("by a list of kinds", ShelfParams{Kinds: []string{"manga", "bd"}},
				"series:Berserk", "series:Naruto", "series:One Piece"),
			Entry("by ebook format, a series always qualifies",
				ShelfParams{Format: "ebook"},
				"book:Elantris", "book:Warbreaker", "book:Future Book",
				"series:Berserk", "series:Naruto", "series:One Piece"),
			Entry("by audiobook format, a series never does",
				ShelfParams{Format: "audiobook"},
				"book:Warbreaker", "book:Future Book"),
			Entry("by an author, accent and case folded",
				ShelfParams{Author: "brandon sanderson"},
				"book:Elantris", "book:Warbreaker", "book:Mistborn"),
			Entry("by an author with accents", ShelfParams{Author: "ERIC ZEBRE"},
				"book:Future Book"),
			Entry("by a series' author", ShelfParams{Author: "Mangaka Naruto"},
				"series:Naruto"),
			Entry(
				"by title, folded",
				ShelfParams{Query: "ELANTRIS"},
				"book:Elantris",
			),
			Entry(
				"by creator name",
				ShelfParams{Query: "zebre"},
				"book:Future Book",
			),
			Entry("by an edition title", ShelfParams{Query: "en volume"},
				"series:Berserk", "series:Naruto", "series:One Piece"),
		)

		It("combines filters", func() {
			rows, total := list(ShelfParams{
				Status: "downloading", Author: "Brandon Sanderson", Format: "ebook",
			})

			Expect(names(rows)).To(Equal([]string{"book:Warbreaker"}))
			Expect(total).To(Equal(uint32(1)))
		})

		It("sorts by year and by arrival", func() {
			rows, _ := list(ShelfParams{Sort: "year", Kinds: []string{"novel"}})
			Expect(names(rows)).To(Equal([]string{
				"book:Elantris",
				"book:Mistborn",
				"book:Warbreaker",
				"book:Future Book",
			}))

			rows, _ = list(ShelfParams{Sort: "added", Desc: true, Limit: 1})
			Expect(names(rows)).To(Equal([]string{"series:Berserk"}))
			Expect(rows[0].AddedAt).NotTo(BeZero())

			rows, _ = list(ShelfParams{Sort: "title", Desc: true, Limit: 1})
			Expect(names(rows)).To(Equal([]string{"book:Warbreaker"}))
		})

		It("answers an empty page for a filter nothing matches", func() {
			rows, total := list(ShelfParams{Query: "zzzz"})
			Expect(rows).To(BeEmpty())
			Expect(total).To(BeZero())
		})
	})

	Describe("ShelfCountsFor", func() {
		It("counts the whole library whatever the filters are", func() {
			c, err := store.ShelfCountsFor(
				ctx,
				ShelfParams{Status: "wanted", Query: "zzz"},
			)
			Expect(err).NotTo(HaveOccurred())
			Expect(c.Total).To(Equal(uint32(7)))
		})

		It("counts each facet with the others applied and its own left out", func() {
			c, err := store.ShelfCountsFor(ctx, ShelfParams{
				Status: "downloading",
				Author: "Brandon Sanderson",
				Format: "audiobook",
			})
			Expect(err).NotTo(HaveOccurred())

			// Status ignores its own filter: Sanderson's audiobook-capable items.
			Expect(c.StatusTotal).To(Equal(uint32(1)))
			Expect(c.Downloading).To(Equal(uint32(1)))
			Expect(c.Wanted).To(BeZero())
			// Format ignores its own filter: Sanderson's downloading items.
			Expect(c.FormatTotal).To(Equal(uint32(1)))
			Expect(c.Ebook).To(Equal(uint32(1)))
			Expect(c.Audiobook).To(Equal(uint32(1)))
			// Author ignores its own filter: downloading audiobook-capable items.
			Expect(c.AuthorTotal).To(Equal(uint32(1)))
			Expect(
				c.Authors,
			).To(Equal([]ShelfAuthorCount{{Name: "Brandon Sanderson", Count: 1}}))
		})

		It("lists one entry per creator, sorted, counting items not roles", func() {
			c, err := store.ShelfCountsFor(ctx, ShelfParams{})
			Expect(err).NotTo(HaveOccurred())

			Expect(c.StatusTotal).To(Equal(uint32(7)))
			Expect(c.Available).To(Equal(uint32(4)))
			Expect(c.Wanted).To(Equal(uint32(1)))
			Expect(c.Downloading).To(Equal(uint32(2)))
			Expect(c.Authors).To(Equal([]ShelfAuthorCount{
				{Name: "Brandon Sanderson", Count: 3},
				{Name: "Éric Zèbre", Count: 1},
				{Name: "Mangaka Berserk", Count: 1},
				{Name: "Mangaka Naruto", Count: 1},
				{Name: "Mangaka One Piece", Count: 1},
			}))
			Expect(c.FormatTotal).To(Equal(uint32(7)))
			// Mistborn has neither slot monitored nor a file.
			Expect(c.Ebook).To(Equal(uint32(6)))
		})

		It("counts a slot holding a file even when it is not monitored", func() {
			client.Book.UpdateOneID(mistborn.ID).
				SetAudiobookMonitored(false).
				ExecX(ctx)
			client.MediaFile.Create().
				SetPath("/b/m.m4b").SetSize(1).SetQuality("M4B").SetFormat("m4b").
				SetBookID(mistborn.ID).
				SetBookKind(mediafile.BookKindAudiobook).SaveX(ctx)

			c, err := store.ShelfCountsFor(ctx, ShelfParams{Format: "audiobook"})
			Expect(err).NotTo(HaveOccurred())
			Expect(c.FormatTotal).To(Equal(uint32(7)))
			Expect(c.Audiobook).To(Equal(uint32(3)))
		})
	})
})
