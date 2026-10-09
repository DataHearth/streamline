package book

import (
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/book"
	"github.com/datahearth/streamline/ent/mediafile"
	"github.com/datahearth/streamline/internal/metadata"
)

var _ = Describe("Patching books", Label("unit", "integration", "books"), func() {
	var f *fixture

	BeforeEach(func() { f = newFixture() })

	editionOf := func(b *ent.Book, language, format string) *ent.BookEdition {
		GinkgoHelper()
		for _, e := range b.Edges.Editions {
			if e.Language == language && string(e.Format) == format {
				return e
			}
		}
		Fail("no such edition")
		return nil
	}
	addFile := func(b *ent.Book, kind mediafile.BookKind) {
		GinkgoHelper()
		f.client.MediaFile.Create().
			SetPath("/b/" + string(kind)).
			SetSize(1).SetQuality("EPUB").SetFormat("epub").
			SetBookID(b.ID).SetBookKind(kind).SaveX(f.ctx)
		status := f.client.Book.UpdateOneID(b.ID)
		if kind == mediafile.BookKindEbook {
			status.SetEbookStatus(book.EbookStatusAvailable)
		} else {
			status.SetAudiobookStatus(book.AudiobookStatusAvailable)
		}
		status.ExecX(f.ctx)
	}

	Describe("PatchBook", func() {
		DescribeTable("monitor",
			func(monitor string, ebook, audiobook bool) {
				b := f.addBook(1, "Elantris", "none")

				got, err := f.svc.PatchBook(
					f.ctx,
					b.ID,
					PatchBookParams{Monitor: &monitor},
				)

				Expect(err).NotTo(HaveOccurred())
				Expect(got.EbookMonitored).To(Equal(ebook))
				Expect(got.AudiobookMonitored).To(Equal(audiobook))
				if ebook {
					Expect(got.EbookStatus).To(Equal(book.EbookStatusWanted))
				} else {
					Expect(got.EbookStatus).To(Equal(book.EbookStatusSkipped))
				}
			},
			Entry("both", "both", true, true),
			Entry("ebook", "ebook", true, false),
			Entry("audiobook", "audiobook", false, true),
			Entry("none", "none", false, false),
		)

		It(
			"unmonitoring moves wanted to skipped and keeps a slot in flight",
			func() {
				b := f.addBook(1, "Elantris", "both")
				f.client.Book.UpdateOneID(b.ID).
					SetAudiobookStatus(book.AudiobookStatusDownloading).
					ExecX(f.ctx)

				got, err := f.svc.PatchBook(
					f.ctx,
					b.ID,
					PatchBookParams{Monitor: new("none")},
				)

				Expect(err).NotTo(HaveOccurred())
				Expect(got.EbookStatus).To(Equal(book.EbookStatusSkipped))
				Expect(
					got.AudiobookStatus,
				).To(Equal(book.AudiobookStatusDownloading))
			},
		)

		It("picks an edition when monitoring a slot that has none", func() {
			b := f.addBook(1, "Elantris", "none")
			f.client.Book.UpdateOneID(b.ID).ClearEbookEdition().ExecX(f.ctx)

			got, err := f.svc.PatchBook(
				f.ctx,
				b.ID,
				PatchBookParams{Monitor: new("ebook")},
			)

			Expect(err).NotTo(HaveOccurred())
			Expect(got.Edges.EbookEdition.Language).To(Equal("en"))
		})

		It(
			"silently leaves a slot unmonitored when there is no edition of that format",
			func() {
				r := rec(1, "Elantris")
				r.Editions = r.Editions[:2]
				f.meta.EXPECT().
					GetBooks(anyCtx, ids(1)).
					Return([]*metadata.BookRecord{r}, nil).
					Once()
				b, err := f.svc.AddBook(
					f.ctx,
					AddBookParams{HardcoverID: 1, Monitor: "none"},
				)
				Expect(err).NotTo(HaveOccurred())

				got, err := f.svc.PatchBook(
					f.ctx,
					b.ID,
					PatchBookParams{Monitor: new("both")},
				)

				Expect(err).NotTo(HaveOccurred())
				Expect(got.EbookMonitored).To(BeTrue())
				Expect(got.AudiobookMonitored).To(BeFalse())
			},
		)

		It(
			"changing the preferred language retitles and re-picks slots without a file",
			func() {
				b := f.addBook(1, "Elantris", "both")
				addFile(b, mediafile.BookKindAudiobook)

				got, err := f.svc.PatchBook(
					f.ctx,
					b.ID,
					PatchBookParams{PreferredLanguage: new("fr")},
				)

				Expect(err).NotTo(HaveOccurred())
				Expect(got.PreferredLanguage).To(Equal("fr"))
				Expect(got.Title).To(Equal("Elantris (fr)"))
				Expect(got.OriginalTitle).To(Equal("Elantris"))
				Expect(got.Edges.EbookEdition.Language).To(Equal("fr"))
				// The audiobook has a file: its edition is not touched.
				Expect(got.Edges.AudiobookEdition.Language).To(Equal("en"))
			},
		)

		It("does not re-pick a slot that holds a file", func() {
			b := f.addBook(1, "Elantris", "both")
			addFile(b, mediafile.BookKindEbook)

			got, err := f.svc.PatchBook(
				f.ctx,
				b.ID,
				PatchBookParams{PreferredLanguage: new("fr")},
			)

			Expect(err).NotTo(HaveOccurred())
			Expect(got.Edges.EbookEdition.Language).To(Equal("en"))
		})

		It("moves a slot's edition freely while it has no file", func() {
			b := f.addBook(1, "Elantris", "both")
			fr := editionOf(b, "fr", "ebook")

			got, err := f.svc.PatchBook(f.ctx, b.ID, PatchBookParams{
				Format: new("ebook"), EditionID: &fr.ID,
			})

			Expect(err).NotTo(HaveOccurred())
			Expect(got.Edges.EbookEdition.ID).To(Equal(fr.ID))
			Expect(got.EbookReplacingLanguage).To(BeEmpty())
			Expect(got.EbookStatus).To(Equal(book.EbookStatusWanted))
		})

		It(
			"queues a replacement when the slot has a file in another edition",
			func() {
				b := f.addBook(1, "Elantris", "both")
				addFile(b, mediafile.BookKindEbook)
				fr := editionOf(b, "fr", "ebook")

				got, err := f.svc.PatchBook(f.ctx, b.ID, PatchBookParams{
					Format: new("ebook"), EditionID: &fr.ID,
				})

				Expect(err).NotTo(HaveOccurred())
				Expect(got.Edges.EbookEdition.ID).To(Equal(fr.ID))
				Expect(got.EbookReplacingLanguage).To(Equal("en"))
				Expect(got.EbookStatus).To(Equal(book.EbookStatusWanted))
				Expect(got.Edges.MediaFiles).To(HaveLen(1))
			},
		)

		It("queues nothing for the edition the file is already in", func() {
			b := f.addBook(1, "Elantris", "both")
			addFile(b, mediafile.BookKindEbook)
			current := b.Edges.EbookEdition

			got, err := f.svc.PatchBook(f.ctx, b.ID, PatchBookParams{
				Format: new("ebook"), EditionID: &current.ID,
			})

			Expect(err).NotTo(HaveOccurred())
			Expect(got.EbookReplacingLanguage).To(BeEmpty())
			Expect(got.EbookStatus).To(Equal(book.EbookStatusAvailable))
		})

		It(
			"writes the profile, the kind, and clears the profile with an empty name",
			func() {
				b := f.addBook(1, "Elantris", "both")

				got, err := f.svc.PatchBook(f.ctx, b.ID, PatchBookParams{
					QualityProfile: new("comics"), Kind: new("manga"),
				})
				Expect(err).NotTo(HaveOccurred())
				Expect(got.QualityProfile).To(Equal("comics"))
				Expect(got.Kind).To(Equal(book.KindManga))

				got, err = f.svc.PatchBook(
					f.ctx,
					b.ID,
					PatchBookParams{QualityProfile: new("")},
				)
				Expect(err).NotTo(HaveOccurred())
				Expect(got.QualityProfile).To(BeEmpty())
			},
		)

		It("refuses what it cannot apply, changing nothing", func() {
			b := f.addBook(1, "Elantris", "both")
			audio := editionOf(b, "en", "audiobook")
			other := f.addBook(2, "Warbreaker", "both")
			foreign := editionOf(other, "fr", "ebook")

			for _, tc := range []struct {
				name string
				p    PatchBookParams
				want error
			}{
				{"monitor", PatchBookParams{Monitor: new("some")}, ErrInvalidMonitor},
				{"language", PatchBookParams{PreferredLanguage: new("english")}, ErrInvalidLanguage},
				{"profile", PatchBookParams{QualityProfile: new("ghost")}, ErrUnknownProfile},
				{"kind", PatchBookParams{Kind: new("zine")}, ErrInvalidKind},
				{"format alone", PatchBookParams{Format: new("ebook")}, ErrEditionMismatch},
				{"edition alone", PatchBookParams{EditionID: &audio.ID}, ErrEditionMismatch},
				{"slot", PatchBookParams{Format: new("paper"), EditionID: &audio.ID}, ErrInvalidSlotKind},
				{"edition of another format", PatchBookParams{Format: new("ebook"), EditionID: &audio.ID}, ErrUnknownEdition},
				{"edition of another book", PatchBookParams{Format: new("ebook"), EditionID: &foreign.ID}, ErrUnknownEdition},
			} {
				_, err := f.svc.PatchBook(f.ctx, b.ID, tc.p)
				Expect(err).To(MatchError(tc.want), tc.name)
			}
			Expect(f.reloadBook(b.ID).Title).To(Equal("Elantris"))
		})

		It("answers a book that does not exist", func() {
			_, err := f.svc.PatchBook(f.ctx, 404, PatchBookParams{})
			Expect(err).To(MatchError(ErrBookNotFound))
		})
	})

	Describe("PatchSeries", func() {
		add := func(n int, policy string) *ent.BookSeries {
			GinkgoHelper()
			f.meta.EXPECT().GetSeries(anyCtx, uint32(50)).
				Return(seriesRecord("One Piece", n), nil).Once()
			want := []uint32{}
			recs := []*metadata.BookRecord{}
			for i := 1; i <= n; i++ {
				want = append(want, 1000+uint32(i))
				r := volumeRec(1000 + uint32(i))
				// Every volume has a French Kana edition besides the English and
				// French ones.
				r.Editions = append(r.Editions, metadata.EditionRecord{
					HardcoverID: r.HardcoverID*10 + 7,
					Language:    "fr",
					Title:       r.Title + " (Kana)",
					Publisher:   "Kana",
					Format:      metadata.FormatEbook,
					Popularity:  1,
				})
				recs = append(recs, r)
			}
			f.meta.EXPECT().GetBooks(anyCtx, ids(want...)).Return(recs, nil).Once()
			s, err := f.svc.AddSeries(
				f.ctx,
				AddSeriesParams{HardcoverID: 50, Monitor: policy},
			)
			Expect(err).NotTo(HaveOccurred())
			return s
		}

		It("applies the monitor policy to the volumes' ebook slots", func() {
			s := add(2, "all")

			got, err := f.svc.PatchSeries(
				f.ctx,
				s.ID,
				PatchSeriesParams{Monitor: new("none")},
			)

			Expect(err).NotTo(HaveOccurred())
			Expect(got.Monitor.String()).To(Equal("none"))
			for _, v := range got.Edges.Volumes {
				Expect(v.EbookMonitored).To(BeFalse())
				Expect(v.EbookStatus).To(Equal(book.EbookStatusSkipped))
			}
		})

		It("measures the future policy from the moment of the patch", func() {
			s := add(2, "all")
			past, future := time.Now().Add(-time.Hour), time.Now().Add(time.Hour)
			f.client.Book.UpdateOneID(s.Edges.Volumes[0].ID).
				SetReleaseDate(past).
				ExecX(f.ctx)
			f.client.Book.UpdateOneID(s.Edges.Volumes[1].ID).
				SetReleaseDate(future).
				ExecX(f.ctx)

			got, err := f.svc.PatchSeries(
				f.ctx,
				s.ID,
				PatchSeriesParams{Monitor: new("future")},
			)

			Expect(err).NotTo(HaveOccurred())
			Expect(got.Edges.Volumes[0].EbookMonitored).To(BeFalse())
			Expect(got.Edges.Volumes[1].EbookMonitored).To(BeTrue())
		})

		It("writes the profile through to every volume", func() {
			s := add(2, "all")

			got, err := f.svc.PatchSeries(
				f.ctx,
				s.ID,
				PatchSeriesParams{QualityProfile: new("comics")},
			)

			Expect(err).NotTo(HaveOccurred())
			Expect(got.QualityProfile).To(Equal("comics"))
			for _, v := range got.Edges.Volumes {
				Expect(v.QualityProfile).To(Equal("comics"))
			}
		})

		It("lets one volume override the series' profile afterwards", func() {
			s := add(2, "all")
			_, err := f.svc.PatchSeries(
				f.ctx,
				s.ID,
				PatchSeriesParams{QualityProfile: new("comics")},
			)
			Expect(err).NotTo(HaveOccurred())

			got, err := f.svc.PatchBook(
				f.ctx,
				s.Edges.Volumes[0].ID,
				PatchBookParams{QualityProfile: new("std")},
			)

			Expect(err).NotTo(HaveOccurred())
			Expect(got.QualityProfile).To(Equal("std"))
			Expect(
				f.reloadBook(s.Edges.Volumes[1].ID).QualityProfile,
			).To(Equal("comics"))
		})

		It("switches the edition of volumes without an ebook file", func() {
			s := add(2, "all")
			f.client.MediaFile.Create().
				SetPath("/b/1.cbz").SetSize(1).SetQuality("CBZ").SetFormat("cbz").
				SetBookID(s.Edges.Volumes[1].ID).
				SetBookKind(mediafile.BookKindEbook).SaveX(f.ctx)
			Expect(SeriesEditions(f.reloadSeries(s.ID))).To(HaveLen(3))

			got, err := f.svc.PatchSeries(
				f.ctx,
				s.ID,
				PatchSeriesParams{Edition: new("Français · Kana")},
			)

			Expect(err).NotTo(HaveOccurred())
			Expect(got.EditionLanguage).To(Equal("fr"))
			Expect(got.EditionPublisher).To(Equal("Kana"))
			Expect(SeriesEditionLabel(got)).To(Equal("Français · Kana"))
			one, two := got.Edges.Volumes[0], got.Edges.Volumes[1]
			Expect(one.Edges.EbookEdition.Publisher).To(Equal("Kana"))
			Expect(one.PreferredLanguage).To(Equal("fr"))
			// The volume holding a file keeps its edition and its language.
			Expect(two.Edges.EbookEdition.Publisher).To(Equal("Tor"))
			Expect(two.PreferredLanguage).To(Equal("en"))
		})

		It("lists the choices by the number of volumes they cover", func() {
			s := add(2, "all")
			// Only the first volume has the Kana edition.
			f.client.BookEdition.Delete().Where(
				bookEditionOf(s.Edges.Volumes[1].ID, "Kana"),
			).ExecX(f.ctx)

			opts := SeriesEditions(f.reloadSeries(s.ID))

			Expect(opts).To(HaveLen(3))
			Expect(opts[0].Volumes).To(Equal(2))
			Expect(opts[len(opts)-1].Label).To(Equal("Français · Kana"))
			Expect(opts[len(opts)-1].Volumes).To(Equal(1))
		})

		It("refuses an edition that is not one of the choices", func() {
			s := add(1, "all")

			_, err := f.svc.PatchSeries(
				f.ctx,
				s.ID,
				PatchSeriesParams{Edition: new("Klingon · Nobody")},
			)
			Expect(err).To(MatchError(ErrUnknownEdition))
			_, err = f.svc.PatchSeries(
				f.ctx,
				s.ID,
				PatchSeriesParams{Monitor: new("always")},
			)
			Expect(err).To(MatchError(ErrInvalidMonitor))
			_, err = f.svc.PatchSeries(
				f.ctx,
				s.ID,
				PatchSeriesParams{QualityProfile: new("ghost")},
			)
			Expect(err).To(MatchError(ErrUnknownProfile))
		})

		It("answers a series that does not exist", func() {
			_, err := f.svc.PatchSeries(f.ctx, 404, PatchSeriesParams{})
			Expect(err).To(MatchError(ErrSeriesNotFound))
		})
	})
})
