package restapi

import (
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/album"
	"github.com/datahearth/streamline/ent/book"
)

var _ = Describe("calendar converters", Label("unit", "server", "restapi"), func() {
	Describe("toUpcomingAlbum", func() {
		It("carries type, stored status and a day-precision date", func() {
			rel := time.Date(2026, 10, 9, 23, 30, 0, 0, time.UTC)
			a := &ent.Album{
				ID:          3,
				Title:       "Debut",
				Type:        album.TypeEp,
				Status:      album.StatusPaused,
				ReleaseDate: &rel,
			}
			a.Edges.Artist = &ent.Artist{ID: 7, Name: "Band"}

			out := toUpcomingAlbum(a)
			Expect(out.Type).To(Equal(MusicAlbumTypeEp))
			Expect(out.Status).To(Equal(UpcomingAlbumStatusPaused))
			Expect(out.ReleaseDate.Format("2006-01-02")).To(Equal("2026-10-09"))
			Expect(out.ArtistId).To(Equal(uint32(7)))
			Expect(out.ArtistName).To(Equal("Band"))
		})
	})

	Describe("toUpcomingBook", func() {
		It("carries the series and its position", func() {
			pos := 1.5
			b := &ent.Book{
				ID:             1,
				Title:          "Vol",
				AuthorName:     "Writer",
				SeriesPosition: &pos,
			}
			b.Edges.Series = &ent.BookSeries{ID: 9, Title: "Saga"}

			out := toUpcomingBook(b)
			Expect(out.Author).To(Equal("Writer"))
			Expect(out.SeriesId).To(HaveValue(Equal(uint32(9))))
			Expect(out.SeriesTitle).To(HaveValue(Equal("Saga")))
			Expect(out.Position).To(HaveValue(BeNumerically("==", 1.5)))
		})

		It("omits the series on a standalone book", func() {
			out := toUpcomingBook(&ent.Book{ID: 1, Title: "Solo"})
			Expect(out.SeriesId).To(BeNil())
			Expect(out.SeriesTitle).To(BeNil())
			Expect(out.Position).To(BeNil())
		})
	})

	DescribeTable(
		"upcomingBookStatus is total over the slot statuses",
		func(ebookMon bool, ebook book.EbookStatus, audioMon bool, audio book.AudiobookStatus, want UpcomingBookStatus) {
			b := &ent.Book{
				EbookMonitored:     ebookMon,
				EbookStatus:        ebook,
				AudiobookMonitored: audioMon,
				AudiobookStatus:    audio,
			}
			Expect(upcomingBookStatus(b)).To(Equal(want))
		},
		Entry(
			"downloading beats wanted",
			true,
			book.EbookStatusWanted,
			true,
			book.AudiobookStatusDownloading,
			UpcomingBookStatusDownloading,
		),
		Entry(
			"wanted beats paused",
			true,
			book.EbookStatusPaused,
			true,
			book.AudiobookStatusWanted,
			UpcomingBookStatusWanted,
		),
		Entry(
			"paused beats available",
			true,
			book.EbookStatusAvailable,
			true,
			book.AudiobookStatusPaused,
			UpcomingBookStatusPaused,
		),
		Entry(
			"paused alone reads paused",
			true,
			book.EbookStatusPaused,
			false,
			book.AudiobookStatusSkipped,
			UpcomingBookStatusPaused,
		),
		Entry(
			"available alone",
			true,
			book.EbookStatusAvailable,
			false,
			book.AudiobookStatusSkipped,
			UpcomingBookStatusAvailable,
		),
		Entry(
			"a skipped slot is set aside",
			true,
			book.EbookStatusSkipped,
			true,
			book.AudiobookStatusAvailable,
			UpcomingBookStatusAvailable,
		),
		Entry(
			"only a skipped monitored slot reads skipped",
			true,
			book.EbookStatusSkipped,
			false,
			book.AudiobookStatusWanted,
			UpcomingBookStatusSkipped,
		),
		Entry(
			"an unmonitored slot is ignored",
			false,
			book.EbookStatusDownloading,
			true,
			book.AudiobookStatusAvailable,
			UpcomingBookStatusAvailable,
		),
	)
})
