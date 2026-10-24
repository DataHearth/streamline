package db

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/ent"
)

var _ = Describe("Title search", Label("integration", "db"), func() {
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

	day := func(y, m, d int) *time.Time {
		t := time.Date(y, time.Month(m), d, 0, 0, 0, 0, time.UTC)
		return &t
	}

	Describe("SearchAlbums", func() {
		var lennon *ent.Artist

		BeforeEach(func() {
			lennon = client.Artist.Create().
				SetMbid("a-lennon").SetName("John Lennon").SaveX(ctx)
		})

		seedAlbum := func(title string, released *time.Time, monitored bool) *ent.Album {
			GinkgoHelper()
			return client.Album.Create().
				SetMbid("rg-" + title).
				SetTitle(title).
				SetArtist(lennon).
				SetNillableReleaseDate(released).
				SetMonitored(monitored).
				SaveX(ctx)
		}

		hold := func(a *ent.Album) {
			GinkgoHelper()
			tr := client.Track.Create().
				SetTitle("t").SetPosition(1).SetAlbum(a).SaveX(ctx)
			client.MediaFile.Create().
				SetPath("/m/" + a.Title + ".flac").SetSize(10).
				SetTrackID(tr.ID).ExecX(ctx)
		}

		It("matches the title folded, with its artist loaded", func() {
			seedAlbum("Électrique Nuit", day(1971, 1, 1), true)
			seedAlbum("Imagine", day(1971, 9, 9), true)

			rows, total, err := store.SearchAlbums(ctx, TitleSearchParams{
				Query: "electrique-nuit", Limit: 5,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(total).To(Equal(uint32(1)))
			Expect(rows).To(HaveLen(1))
			Expect(rows[0].Title).To(Equal("Électrique Nuit"))
			Expect(rows[0].Edges.Artist).NotTo(BeNil())
			Expect(rows[0].Edges.Artist.Name).To(Equal("John Lennon"))
		})

		It("puts an album holding a file before newer and monitored ones", func() {
			// A followed artist's whole discography is album rows: the one a
			// search is for is usually the one on disk.
			newest := seedAlbum("Power to the People (Live)", day(2025, 1, 1), true)
			held := seedAlbum("Power to the People", day(1971, 3, 12), false)
			undated := seedAlbum("Power to the People (Demo)", nil, true)
			hold(held)

			rows, total, err := store.SearchAlbums(ctx, TitleSearchParams{
				Query: "power", Limit: 5,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(total).To(Equal(uint32(3)))
			ids := make([]uint32, 0, len(rows))
			for _, r := range rows {
				ids = append(ids, r.ID)
			}
			Expect(ids).To(Equal([]uint32{held.ID, newest.ID, undated.ID}))
		})

		It("pages with limit and offset while total counts every match", func() {
			for _, t := range []string{"Love A", "Love B", "Love C"} {
				seedAlbum(t, nil, true)
			}

			rows, total, err := store.SearchAlbums(ctx, TitleSearchParams{
				Query: "love", Limit: 2, Offset: 2,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(total).To(Equal(uint32(3)))
			Expect(rows).To(HaveLen(1))
		})

		It("answers an empty page when nothing matches", func() {
			seedAlbum("Imagine", nil, true)

			rows, total, err := store.SearchAlbums(ctx, TitleSearchParams{
				Query: "metallica", Limit: 5,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(total).To(BeZero())
			Expect(rows).To(BeEmpty())
		})
	})

	Describe("SearchEpisodes", func() {
		var season *ent.Season

		BeforeEach(func() {
			show := client.TVShow.Create().
				SetTitle("Dune: Prophecy").SetYear(2024).SetTvdbID(1).SaveX(ctx)
			season = client.Season.Create().
				SetNumber(1).SetTvShow(show).SaveX(ctx)
		})

		seedEpisode := func(n uint16, title string, aired *time.Time) *ent.Episode {
			GinkgoHelper()
			c := client.Episode.Create().
				SetNumber(n).SetTitle(title).SetSeason(season)
			if aired != nil {
				c.SetAirDate(*aired)
			}
			return c.SaveX(ctx)
		}

		It("matches the title folded, with its season and show loaded", func() {
			seedEpisode(1, "The Hidden Hand", day(2024, 11, 17))
			seedEpisode(2, "Two Wolves", day(2024, 11, 24))

			rows, total, err := store.SearchEpisodes(ctx, TitleSearchParams{
				Query: "HIDDEN hand", Limit: 5,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(total).To(Equal(uint32(1)))
			Expect(rows).To(HaveLen(1))
			Expect(rows[0].Number).To(Equal(uint16(1)))
			Expect(rows[0].Edges.Season).NotTo(BeNil())
			Expect(rows[0].Edges.Season.Number).To(Equal(uint16(1)))
			Expect(rows[0].Edges.Season.Edges.TvShow).NotTo(BeNil())
			Expect(rows[0].Edges.Season.Edges.TvShow.Title).
				To(Equal("Dune: Prophecy"))
		})

		It("puts held episodes first, then the latest aired, undated last", func() {
			undated := seedEpisode(6, "Sisterhood Above All", nil)
			older := seedEpisode(1, "Sisterhood Rising", day(2024, 11, 17))
			newer := seedEpisode(5, "The Sisterhood's Fall", day(2024, 12, 15))
			held := seedEpisode(3, "Sisterhood Sworn", day(2024, 12, 1))
			client.MediaFile.Create().
				SetPath("/tv/s01e03.mkv").SetSize(10).
				SetEpisodeID(held.ID).ExecX(ctx)

			rows, _, err := store.SearchEpisodes(ctx, TitleSearchParams{
				Query: "sisterhood", Limit: 10,
			})
			Expect(err).NotTo(HaveOccurred())
			ids := make([]uint32, 0, len(rows))
			for _, r := range rows {
				ids = append(ids, r.ID)
			}
			Expect(ids).To(Equal([]uint32{held.ID, newer.ID, older.ID, undated.ID}))
		})
	})
})
