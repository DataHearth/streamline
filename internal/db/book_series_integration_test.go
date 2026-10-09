package db

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/book"
	"github.com/datahearth/streamline/ent/bookseries"
	"github.com/datahearth/streamline/ent/mediafile"
)

// volumeSeed is a hydrated volume with an English and a French ebook edition.
func volumeSeed(hc uint32, position float64, refreshed time.Time) BookSeed {
	return BookSeed{
		HardcoverID:       hc,
		Title:             "Volume",
		AuthorName:        "Oda",
		Kind:              "manga",
		PreferredLanguage: "en",
		SeriesPosition:    &position,
		EbookMonitored:    true,
		Editions: []EditionSeed{
			{
				HardcoverID: hc*10 + 1, Language: "en", Title: "En Volume",
				Publisher: "Viz", Format: "ebook", Popularity: 5, Original: false,
			},
			{
				HardcoverID: hc*10 + 2, Language: "fr", Title: "Tome",
				Publisher: "Glenat", Format: "ebook", Popularity: 9,
			},
			{
				HardcoverID: hc*10 + 3, Language: "fr", Title: "Tome (Kana)",
				Publisher: "Kana", Format: "ebook", Popularity: 2,
			},
		},
		Credits: []CreditSeed{
			{AuthorHardcoverID: 300, Name: "Oda", Role: "writer"},
		},
		RefreshedAt: &refreshed,
	}
}

var _ = Describe("Book series persistence", Label("integration", "db"), func() {
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

	params := func(volumes ...BookSeed) CreateSeriesParams {
		return CreateSeriesParams{
			HardcoverID: 50, Title: "One Piece", AuthorName: "Oda", Kind: "manga",
			Monitor: "all", QualityProfile: "manga", EditionLanguage: "en",
			EditionPublisher: "Viz", RefreshedAt: now,
			Credits: []CreditSeed{
				{AuthorHardcoverID: 300, Name: "Oda", Role: "author"},
				{
					AuthorHardcoverID: 301,
					Name:              "Translator",
					Role:              "translator",
					Language:          "fr",
				},
			},
			Volumes: volumes,
		}
	}
	create := func(volumes ...BookSeed) *ent.BookSeries {
		GinkgoHelper()
		s, _, err := store.CreateSeries(ctx, params(volumes...))
		Expect(err).NotTo(HaveOccurred())
		return s
	}
	volume := func(s *ent.BookSeries, hc uint32) *ent.Book {
		GinkgoHelper()
		for _, v := range s.Edges.Volumes {
			if v.HardcoverID == hc {
				return v
			}
		}
		Fail("volume not in the series")
		return nil
	}
	fresh := func(id uint32) *ent.BookSeries {
		GinkgoHelper()
		s, err := store.FindSeriesByID(ctx, id)
		Expect(err).NotTo(HaveOccurred())
		return s
	}

	Describe("CreateSeries", func() {
		It("writes the series with hydrated volumes and stubs in order", func() {
			stub := BookSeed{
				HardcoverID:       3,
				Title:             "One Piece #3",
				Kind:              "manga",
				PreferredLanguage: "en",
				SeriesPosition:    new(3.0),
				EbookMonitored:    true,
			}
			s := create(volumeSeed(1, 1, now), volumeSeed(2, 2, now), stub)

			Expect(s.Edges.Volumes).To(HaveLen(3))
			Expect(*s.Edges.Volumes[0].SeriesPosition).To(Equal(1.0))
			Expect(s.Edges.Volumes[2].LastRefreshedAt).To(BeNil())
			Expect(s.Edges.Volumes[0].Edges.Editions).To(HaveLen(3))
			Expect(s.Edges.Contributions).To(HaveLen(2))
			Expect(s.QualityProfile).To(Equal("manga"))
			Expect(s.EditionPublisher).To(Equal("Viz"))
		})

		It("adopts a volume already in the library as a standalone book", func() {
			standalone, err := store.CreateBook(ctx, volumeSeed(1, 1, now))
			Expect(err).NotTo(HaveOccurred())
			Expect(
				client.Book.GetX(ctx, standalone.ID).QueryEditions().CountX(ctx),
			).To(Equal(3))

			seed := volumeSeed(1, 1, now)
			seed.QualityProfile = "x"
			s, counts, err := store.CreateSeries(
				ctx,
				params(seed, volumeSeed(2, 2, now)),
			)
			Expect(err).NotTo(HaveOccurred())

			Expect(counts).To(Equal(VolumeCounts{Created: 1, Adopted: 1}))
			adopted := volume(s, 1)
			Expect(adopted.ID).To(Equal(standalone.ID))
			// Its own data is kept; the series' profile is written through.
			Expect(adopted.Title).To(Equal("En Volume"))
			Expect(adopted.QualityProfile).To(Equal("manga"))
			Expect(client.Book.Query().CountX(ctx)).To(Equal(2))
		})

		It("skips a volume that belongs to another series", func() {
			create(volumeSeed(1, 1, now))
			other := params(volumeSeed(1, 1, now), volumeSeed(9, 2, now))
			other.HardcoverID = 51

			s, counts, err := store.CreateSeries(ctx, other)
			Expect(err).NotTo(HaveOccurred())

			Expect(counts.Skipped).To(Equal(1))
			Expect(s.Edges.Volumes).To(HaveLen(1))
			Expect(s.Edges.Volumes[0].HardcoverID).To(Equal(uint32(9)))
		})

		It("writes nothing when a volume fails", func() {
			bad := volumeSeed(1, 1, now)
			bad.Kind = "not-a-kind"
			_, _, err := store.CreateSeries(ctx, params(bad))

			Expect(err).To(HaveOccurred())
			Expect(client.BookSeries.Query().CountX(ctx)).To(BeZero())
			Expect(client.Book.Query().CountX(ctx)).To(BeZero())
			Expect(client.Author.Query().CountX(ctx)).To(BeZero())
		})
	})

	Describe("FindSeriesByHardcoverID and FirstVolumeID", func() {
		It(
			"returns nil for an unknown series and the lowest volume for a known one",
			func() {
				got, err := store.FindSeriesByHardcoverID(ctx, 404)
				Expect(err).NotTo(HaveOccurred())
				Expect(got).To(BeNil())

				s := create(volumeSeed(2, 2, now), volumeSeed(1, 1, now))
				id, err := store.FirstVolumeID(ctx, s.ID)
				Expect(err).NotTo(HaveOccurred())
				Expect(id).To(Equal(volume(s, 1).ID))

				empty := params()
				empty.HardcoverID = 77
				es, _, err := store.CreateSeries(ctx, empty)
				Expect(err).NotTo(HaveOccurred())
				id, err = store.FirstVolumeID(ctx, es.ID)
				Expect(err).NotTo(HaveOccurred())
				Expect(id).To(BeZero())
			},
		)
	})

	Describe("SetSeriesQualityProfile", func() {
		It("writes the series and every volume", func() {
			s := create(volumeSeed(1, 1, now), volumeSeed(2, 2, now))

			Expect(store.SetSeriesQualityProfile(ctx, s.ID, "bd")).To(Succeed())

			got := fresh(s.ID)
			Expect(got.QualityProfile).To(Equal("bd"))
			for _, v := range got.Edges.Volumes {
				Expect(v.QualityProfile).To(Equal("bd"))
			}
		})
	})

	Describe("SetSeriesMonitor", func() {
		It(
			"none unmonitors every volume without a file and keeps one with a file",
			func() {
				s := create(volumeSeed(1, 1, now), volumeSeed(2, 2, now))
				v1 := volume(s, 1)
				client.MediaFile.Create().
					SetPath("/b/1.cbz").
					SetSize(1).SetQuality("CBZ").SetFormat("cbz").
					SetBookID(v1.ID).SetBookKind(mediafile.BookKindEbook).SaveX(ctx)
				client.Book.UpdateOneID(v1.ID).
					SetEbookStatus(book.EbookStatusAvailable).
					ExecX(ctx)

				Expect(store.SetSeriesMonitor(ctx, s.ID, "none", now)).To(Succeed())

				got := fresh(s.ID)
				Expect(got.Monitor).To(Equal(bookseries.MonitorNone))
				Expect(
					volume(got, 1).EbookStatus,
				).To(Equal(book.EbookStatusAvailable))
				two := volume(got, 2)
				Expect(two.EbookMonitored).To(BeFalse())
				Expect(two.EbookStatus).To(Equal(book.EbookStatusSkipped))
			},
		)

		It(
			"future monitors the volumes releasing after the moment, no earlier ones",
			func() {
				past, future := now.Add(-24*time.Hour), now.Add(24*time.Hour)
				a, b := volumeSeed(1, 1, now), volumeSeed(2, 2, now)
				a.ReleaseDate, b.ReleaseDate = &past, &future
				s := create(a, b)

				Expect(
					store.SetSeriesMonitor(ctx, s.ID, "future", now),
				).To(Succeed())

				got := fresh(s.ID)
				Expect(volume(got, 1).EbookMonitored).To(BeFalse())
				Expect(volume(got, 2).EbookMonitored).To(BeTrue())
			},
		)

		It(
			"all monitors every volume and picks an edition for one that has none",
			func() {
				a := volumeSeed(1, 1, now)
				a.EbookMonitored = false
				s := create(a)
				client.Book.UpdateOneID(volume(s, 1).ID).
					ClearEbookEdition().
					ExecX(ctx)

				Expect(store.SetSeriesMonitor(ctx, s.ID, "all", now)).To(Succeed())

				v := volume(fresh(s.ID), 1)
				Expect(v.EbookMonitored).To(BeTrue())
				Expect(v.EbookStatus).To(Equal(book.EbookStatusWanted))
				Expect(v.Edges.EbookEdition).NotTo(BeNil())
			},
		)

		It(
			"flags a stub without editions, and leaves a volume with no ebook edition alone",
			func() {
				stub := BookSeed{
					HardcoverID: 4, Title: "One Piece #4", Kind: "manga",
					PreferredLanguage: "en", SeriesPosition: new(4.0),
				}
				audioOnly := volumeSeed(5, 5, now)
				audioOnly.EbookMonitored = false
				audioOnly.Editions = []EditionSeed{
					{
						HardcoverID: 51,
						Language:    "en",
						Title:       "Audio",
						Format:      "audiobook",
						Popularity:  1,
					},
				}
				s := create(stub, audioOnly)

				Expect(store.SetSeriesMonitor(ctx, s.ID, "all", now)).To(Succeed())

				got := fresh(s.ID)
				Expect(volume(got, 4).EbookMonitored).To(BeTrue())
				Expect(volume(got, 5).EbookMonitored).To(BeFalse())
			},
		)
	})

	Describe("SetSeriesEdition", func() {
		It(
			"re-picks volumes without a file by language and publisher and retitles them",
			func() {
				s := create(volumeSeed(1, 1, now), volumeSeed(2, 2, now))
				held := volume(s, 2)
				client.MediaFile.Create().
					SetPath("/b/2.cbz").
					SetSize(1).SetQuality("CBZ").SetFormat("cbz").
					SetBookID(held.ID).
					SetBookKind(mediafile.BookKindEbook).SaveX(ctx)
				before := client.Book.GetX(ctx, held.ID).
					QueryEbookEdition().
					OnlyX(ctx)

				Expect(store.SetSeriesEdition(ctx, s.ID, "fr", "Kana")).To(Succeed())

				got := fresh(s.ID)
				Expect(got.EditionLanguage).To(Equal("fr"))
				Expect(got.EditionPublisher).To(Equal("Kana"))
				one := volume(got, 1)
				Expect(one.Edges.EbookEdition.Publisher).To(Equal("Kana"))
				Expect(one.PreferredLanguage).To(Equal("fr"))
				Expect(one.Title).To(Equal("Tome"))
				// A volume with a file keeps its file and its edition.
				two := volume(got, 2)
				Expect(two.Edges.EbookEdition.ID).To(Equal(before.ID))
				Expect(two.PreferredLanguage).To(Equal("en"))
			},
		)

		It(
			"falls back to the language's pick when a volume lacks that publisher",
			func() {
				s := create(volumeSeed(1, 1, now))

				Expect(
					store.SetSeriesEdition(ctx, s.ID, "fr", "Nobody"),
				).To(Succeed())

				one := volume(fresh(s.ID), 1)
				Expect(one.Edges.EbookEdition.Language).To(Equal("fr"))
				Expect(one.Edges.EbookEdition.Publisher).To(Equal("Glenat"))
			},
		)
	})

	Describe("ApplySeriesMetadata and RefreshSeriesStats", func() {
		It("replaces the contributions and keeps the user's choices", func() {
			s := create(volumeSeed(1, 1, now))
			Expect(store.SetSeriesMonitor(ctx, s.ID, "none", now)).To(Succeed())

			Expect(store.ApplySeriesMetadata(ctx, s.ID, SeriesMetadata{
				Title: "One Piece!", Overview: "Pirates", AuthorName: "Eiichiro Oda",
				Ongoing: true, Credits: []CreditSeed{
					{AuthorHardcoverID: 300, Name: "Oda", Role: "author"},
				},
				RefreshedAt: now,
			})).To(Succeed())

			got := fresh(s.ID)
			Expect(got.Title).To(Equal("One Piece!"))
			Expect(got.Ongoing).To(BeTrue())
			Expect(got.Monitor).To(Equal(bookseries.MonitorNone))
			Expect(got.QualityProfile).To(Equal("manga"))
			Expect(got.Edges.Contributions).To(HaveLen(1))
			// The translator left with nothing was deleted.
			Expect(client.Author.Query().CountX(ctx)).To(Equal(1))
		})

		It("computes since and the mean rating over the volumes", func() {
			a, b := volumeSeed(1, 1, now), volumeSeed(2, 2, now)
			a.ReleaseYear, b.ReleaseYear = new(uint16(1999)), new(uint16(1997))
			a.RatingTenths, b.RatingTenths = new(uint8(40)), new(uint8(45))
			s := create(a, b, volumeSeed(3, 3, now))

			Expect(store.RefreshSeriesStats(ctx, s.ID)).To(Succeed())

			got := fresh(s.ID)
			Expect(*got.Since).To(Equal(uint16(1997)))
			Expect(*got.RatingTenths).To(Equal(uint8(43)))
		})
	})

	Describe("AddSeriesVolumes", func() {
		It("adds volumes a refresh found and reports what it did", func() {
			s := create(volumeSeed(1, 1, now))

			counts, err := store.AddSeriesVolumes(ctx, s.ID, "manga", []BookSeed{
				volumeSeed(1, 1, now), volumeSeed(2, 2, now),
			})

			Expect(err).NotTo(HaveOccurred())
			Expect(counts).To(Equal(VolumeCounts{Created: 1}))
			Expect(fresh(s.ID).Edges.Volumes).To(HaveLen(2))
		})
	})

	Describe("ListStaleSeries", func() {
		It(
			"returns a series never refreshed or refreshed before the cutoff",
			func() {
				create(volumeSeed(1, 1, now))
				other := params()
				other.HardcoverID = 60
				other.RefreshedAt = now.Add(-48 * time.Hour)
				_, _, err := store.CreateSeries(ctx, other)
				Expect(err).NotTo(HaveOccurred())

				got, err := store.ListStaleSeries(ctx, now.Add(-24*time.Hour), 10)
				Expect(err).NotTo(HaveOccurred())
				Expect(got).To(HaveLen(1))
				Expect(got[0].HardcoverID).To(Equal(uint32(60)))
			},
		)
	})

	Describe("DeleteSeries", func() {
		It(
			"removes the volumes, their files and the people left with nothing",
			func() {
				s := create(volumeSeed(1, 1, now))
				client.MediaFile.Create().
					SetPath("/b/1.cbz").
					SetSize(1).SetQuality("CBZ").SetFormat("cbz").
					SetBookID(volume(s, 1).ID).
					SetBookKind(mediafile.BookKindEbook).SaveX(ctx)

				orphans, err := store.DeleteSeries(ctx, s.ID)

				Expect(err).NotTo(HaveOccurred())
				Expect(orphans).To(HaveLen(2))
				Expect(client.Book.Query().CountX(ctx)).To(BeZero())
				Expect(client.MediaFile.Query().CountX(ctx)).To(BeZero())
				Expect(client.Author.Query().CountX(ctx)).To(BeZero())
				Expect(client.BookEdition.Query().CountX(ctx)).To(BeZero())
			},
		)
	})
})
