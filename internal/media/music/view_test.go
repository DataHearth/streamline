package music

import (
	"sort"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/album"
	"github.com/datahearth/streamline/ent/artist"
)

var _ = Describe("Views", Label("unit", "music"), func() {
	now := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	past := now.AddDate(-1, 0, 0)
	future := now.AddDate(0, 1, 0)

	al := func(status album.Status, monitored bool, date *time.Time) *ent.Album {
		return &ent.Album{Status: status, Monitored: monitored, ReleaseDate: date}
	}

	Describe("albumStatusAt", func() {
		DescribeTable(
			"derives upcoming from wanted and a future date",
			func(a *ent.Album, want string) {
				Expect(albumStatusAt(a, now)).To(Equal(want))
			},
			Entry(
				"wanted and dated ahead",
				al(album.StatusWanted, true, &future),
				"upcoming",
			),
			Entry(
				"wanted and released",
				al(album.StatusWanted, true, &past),
				"wanted",
			),
			Entry(
				"wanted and undated is never upcoming",
				al(album.StatusWanted, true, nil),
				"wanted",
			),
			Entry("downloading ahead of its date stays downloading",
				al(album.StatusDownloading, true, &future), "downloading"),
			Entry("paused", al(album.StatusPaused, true, nil), "paused"),
			Entry("available", al(album.StatusAvailable, true, &past), "available"),
			Entry("skipped", al(album.StatusSkipped, true, nil), "skipped"),
		)
	})

	Describe("artistStatusAt", func() {
		DescribeTable("rolls up the monitored, non-upcoming albums",
			func(want string, albums ...*ent.Album) {
				Expect(artistStatusAt(albums, now)).To(Equal(want))
			},
			Entry(
				"downloading beats wanted",
				"downloading",
				al(
					album.StatusWanted,
					true,
					&past,
				),
				al(album.StatusDownloading, true, &past),
			),
			Entry("paused counts as downloading", "downloading",
				al(album.StatusPaused, true, &past)),
			Entry(
				"wanted when any monitored album is wanted",
				"wanted",
				al(
					album.StatusAvailable,
					true,
					&past,
				),
				al(album.StatusWanted, true, &past),
			),
			Entry(
				"an upcoming album is not missing",
				"available",
				al(
					album.StatusAvailable,
					true,
					&past,
				),
				al(album.StatusWanted, true, &future),
			),
			Entry(
				"an unmonitored album does not count",
				"available",
				al(
					album.StatusWanted,
					false,
					&past,
				),
				al(album.StatusDownloading, false, &past),
			),
			Entry("nothing monitored is available", "available"),
		)
	})

	Describe("pickOverview", func() {
		a := &ent.Artist{
			Overview: "English", OverviewSource: "https://en",
			OverviewFr: "Francais", OverviewSourceFr: "https://fr",
		}

		It("serves the requested locale with its own source", func() {
			text, src := pickOverview(a, "fr")
			Expect(text).To(Equal("Francais"))
			Expect(src).To(Equal("https://fr"))
		})

		It("serves English by default", func() {
			text, src := pickOverview(a, "en")
			Expect([]string{text, src}).To(Equal([]string{"English", "https://en"}))
		})

		It(
			"falls back to English, source included, when the locale has no article",
			func() {
				text, src := pickOverview(
					&ent.Artist{Overview: "English", OverviewSource: "https://en"},
					"fr",
				)
				Expect(
					[]string{text, src},
				).To(Equal([]string{"English", "https://en"}))
			},
		)

		It("is empty when neither locale has one", func() {
			text, src := pickOverview(&ent.Artist{}, "fr")
			Expect([]string{text, src}).To(Equal([]string{"", ""}))
		})
	})

	Describe("byNewest", func() {
		It("orders newest first, undated last, then by title", func() {
			albums := []*ent.Album{
				{ID: 1, Title: "B", ReleaseDate: &past},
				{ID: 2, Title: "Undated"},
				{ID: 3, Title: "New", ReleaseDate: &future},
				{ID: 4, Title: "A", ReleaseDate: &past},
			}
			sort.SliceStable(
				albums,
				func(i, j int) bool { return byNewest(albums[i], albums[j]) < 0 },
			)
			ids := []uint32{albums[0].ID, albums[1].ID, albums[2].ID, albums[3].ID}
			Expect(ids).To(Equal([]uint32{3, 4, 1, 2}))
		})
	})

	Describe("worstTier and formatLabel", func() {
		f := func(quality, format, codec string, bitrate uint32) *ent.MediaFile {
			return &ent.MediaFile{
				Quality:    quality,
				Format:     format,
				AudioCodec: codec,
				Bitrate:    bitrate,
			}
		}

		It("states the weakest tier among the files that have one", func() {
			tier, ok := worstTier([]*ent.MediaFile{
				f(
					"hires",
					"flac",
					"",
					0,
				),
				f("high", "mp3", "", 0),
				f("", "ogg", "", 0),
			})
			Expect(ok).To(BeTrue())
			Expect(tier.String()).To(Equal("high"))
		})

		It("has no tier when no file states one", func() {
			_, ok := worstTier([]*ent.MediaFile{f("", "flac", "", 0)})
			Expect(ok).To(BeFalse())
		})

		It("labels a lossy file with its rate in kbps", func() {
			Expect(formatLabel([]*ent.MediaFile{f("high", "mp3", "mp3", 320000)})).
				To(Equal("MP3 320"))
		})

		It("labels a lossless file with the codec alone", func() {
			Expect(
				formatLabel([]*ent.MediaFile{f("hires", "flac", "flac", 1200000)}),
			).
				To(Equal("FLAC"))
		})

		It("falls back to the extension when the file was never probed", func() {
			Expect(
				formatLabel([]*ent.MediaFile{f("lossless", "flac", "", 0)}),
			).To(Equal("FLAC"))
			Expect(
				formatLabel([]*ent.MediaFile{f("high", "m4a", "", 0)}),
			).To(Equal("M4A"))
		})

		It("names the weakest file, the lowest bit rate on a tie", func() {
			Expect(formatLabel([]*ent.MediaFile{
				f("lossless", "flac", "flac", 0),
				f("high", "mp3", "mp3", 320000),
				f("high", "m4a", "aac", 256000),
			})).To(Equal("AAC 256"))
		})

		It("is empty without files", func() {
			Expect(formatLabel(nil)).To(BeEmpty())
		})
	})

	Describe("monitoredFor", func() {
		DescribeTable("by policy",
			func(policy artist.Monitor, date *time.Time, want bool) {
				Expect(monitoredFor(policy, date, now)).To(Equal(want))
			},
			Entry("all, past", artist.MonitorAll, &past, true),
			Entry("future, past", artist.MonitorFuture, &past, false),
			Entry("future, ahead", artist.MonitorFuture, &future, true),
			Entry("future, undated", artist.MonitorFuture, nil, true),
			Entry("manual", artist.MonitorManual, &future, false),
			Entry("none", artist.MonitorNone, &future, false),
		)
	})

	Describe("parseMonitor", func() {
		It("defaults to all and rejects anything unknown", func() {
			m, err := parseMonitor("")
			Expect(err).NotTo(HaveOccurred())
			Expect(m).To(Equal(artist.MonitorAll))
			_, err = parseMonitor("sometimes")
			Expect(err).To(MatchError(ErrInvalidMonitor))
		})
	})

	It("splits a comma-joined list, empty meaning none", func() {
		Expect(splitList("")).To(BeNil())
		Expect(splitList("guitar,vocals")).To(Equal([]string{"guitar", "vocals"}))
	})
})
