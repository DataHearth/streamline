package db

import (
	"context"
	"fmt"
	"time"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/album"
	"github.com/datahearth/streamline/ent/book"
	"github.com/datahearth/streamline/ent/mediafile"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("RecentArrivals", Label("unit", "db"), func() {
	var (
		store  Store
		client *ent.Client
		ctx    context.Context
		now    time.Time
	)

	BeforeEach(func() {
		ctx = context.Background()
		var err error
		client, err = Open(ctx, ":memory:")
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { Expect(client.Close()).To(Succeed()) })
		store = New(client)
		now = time.Now().UTC().Truncate(time.Second)
	})

	file := func(at time.Time, path string, size int64) *ent.MediaFileCreate {
		return client.MediaFile.Create().
			SetPath(path).
			SetSize(size).
			SetCreateTime(at)
	}

	show := func(title string, tvdbID uint32, episodes int) []uint32 {
		GinkgoHelper()
		seeds := make([]EpisodeSeed, episodes)
		for i := range seeds {
			seeds[i] = EpisodeSeed{
				Number: uint16(i + 1),
				Title:  fmt.Sprintf("Episode %d", i+1),
			}
		}
		s, err := store.CreateTVShow(ctx, CreateTVShowParams{
			Title: title, Year: 2021, TvdbID: tvdbID,
			SeriesStatus: "continuing", Type: "standard",
			Seasons: []SeasonSeed{{Number: 1, Episodes: seeds}},
		})
		Expect(err).NotTo(HaveOccurred())
		ids := make([]uint32, 0, episodes)
		for _, e := range s.Edges.Seasons[0].Edges.Episodes {
			ids = append(ids, e.ID)
		}
		return ids
	}

	It(
		"folds each title's files into one import per hour, newest first, across the four media",
		func() {
			m := client.Movie.Create().
				SetTitle("Northern Halo").
				SetOriginalTitle("Northern Halo").SetYear(2024).SetTmdbID(1).
				SaveX(ctx)
			file(
				now.Add(-5*time.Minute),
				"/m/Northern.Halo.2024.720p.WEB.x264-GRP.mkv",
				10,
			).SetMovieID(m.ID).
				SaveX(ctx)
			file(
				now.Add(-5*time.Minute),
				"/m/Northern.Halo.2024.2160p.BluRay.x265-GRP.mkv",
				90,
			).SetMovieID(m.ID).
				SaveX(ctx)

			eps := show("The Long Field", 1, 3)
			file(
				now.Add(-10*time.Minute),
				"/tv/e2.mkv",
				1,
			).SetEpisodeID(eps[1]).
				SaveX(ctx)
			file(
				now.Add(-40*time.Minute),
				"/tv/e3.mkv",
				1,
			).SetEpisodeID(eps[2]).
				SaveX(ctx)
			file(
				now.Add(-48*time.Hour),
				"/tv/e1.mkv",
				1,
			).SetEpisodeID(eps[0]).
				SaveX(ctx)

			ar := client.Artist.Create().
				SetMbid("artist-1").
				SetName("Northbound Static").
				SaveX(ctx)
			al := client.Album.Create().
				SetMbid("rg-1").
				SetTitle("The Long Quiet").
				SetType(album.TypeEp).
				SetReleaseDate(time.Date(2020, 3, 1, 0, 0, 0, 0, time.UTC)).
				SetArtist(ar).
				SaveX(ctx)
			for i := range 2 {
				t := client.Track.Create().
					SetTitle(fmt.Sprintf("T%d", i)).
					SetPosition(uint16(i + 1)).
					SetAlbum(al).
					SaveX(ctx)
				file(
					now.Add(-20*time.Minute),
					fmt.Sprintf("/music/%d.flac", i),
					1,
				).SetTrackID(t.ID).
					SaveX(ctx)
			}

			series := client.BookSeries.Create().
				SetHardcoverID(7).
				SetTitle("Les Archives").
				SaveX(ctx)
			vol := 2.5
			b := client.Book.Create().
				SetHardcoverID(70).
				SetTitle("Les Archives du sel").
				SetAuthorName("Iris Calder").
				SetKind(book.KindBd).
				SetReleaseYear(2019).
				SetSeries(series).
				SetSeriesPosition(vol).
				SaveX(ctx)
			file(now.Add(-30*time.Minute), "/books/sel.epub", 1).
				SetBookID(b.ID).SetBookKind(mediafile.BookKindEbook).SaveX(ctx)
			file(now.Add(-31*time.Minute), "/books/sel/01.mp3", 1).
				SetBookID(b.ID).SetBookKind(mediafile.BookKindAudiobook).SaveX(ctx)

			got, err := store.RecentArrivals(ctx, 10)
			Expect(err).NotTo(HaveOccurred())
			kinds := make([]string, 0, len(got))
			for _, a := range got {
				kinds = append(kinds, fmt.Sprintf("%s:%s", a.Kind, a.Format))
			}
			Expect(kinds).To(Equal([]string{
				"movie:",
				"series:",
				"album:",
				"book:ebook",
				"book:audiobook",
				"series:",
			}))

			Expect(got[0].ID).To(Equal(m.ID))
			Expect(got[0].Title).To(Equal("Northern Halo"))
			Expect(got[0].Year).To(Equal(uint16(2024)))
			Expect(
				got[0].Path,
			).To(ContainSubstring("2160p"), "the largest file names the import")

			Expect(got[1].At).To(BeTemporally("==", now.Add(-10*time.Minute)))
			Expect(got[1].Addition.Episodes).To(Equal([]uint16{2, 3}))
			Expect(got[1].Addition.Count).To(Equal(uint32(2)))
			Expect(got[5].Addition.Episodes).To(Equal([]uint16{1}))
			Expect(got[5].Addition.EpisodeTitle).To(Equal("Episode 1"))

			Expect(got[2].ID).To(Equal(al.ID))
			Expect(got[2].ArtistID).To(Equal(ar.ID))
			Expect(got[2].ArtistName).To(Equal("Northbound Static"))
			Expect(got[2].ReleaseType).To(Equal(album.TypeEp))
			Expect(got[2].Year).To(Equal(uint16(2020)))

			Expect(got[3].ID).To(Equal(b.ID))
			Expect(got[3].Author).To(Equal("Iris Calder"))
			Expect(got[3].BookKind).To(Equal(book.KindBd))
			Expect(got[3].SeriesID).To(Equal(series.ID))
			Expect(got[3].Volume).To(HaveValue(Equal(vol)))
		},
	)

	It(
		"keeps gathering the last import's files past the limit and across scan pages",
		func() {
			eps := show("Pack", 2, arrivalScanPage+2)
			bulk := make([]*ent.MediaFileCreate, 0, len(eps))
			for i, id := range eps {
				bulk = append(
					bulk,
					file(
						now.Add(-time.Duration(i)*time.Second),
						fmt.Sprintf("/tv/p%d.mkv", i),
						1,
					).
						SetEpisodeID(id),
				)
			}
			client.MediaFile.CreateBulk(bulk...).SaveX(ctx)
			m := client.Movie.Create().
				SetTitle("Older").
				SetOriginalTitle("Older").SetYear(2000).SetTmdbID(2).
				SaveX(ctx)
			file(
				now.Add(-3*time.Hour),
				"/m/older.mkv",
				1,
			).SetMovieID(m.ID).
				SaveX(ctx)

			got, err := store.RecentArrivals(ctx, 1)
			Expect(err).NotTo(HaveOccurred())
			Expect(got).To(HaveLen(1))
			Expect(got[0].Kind).To(Equal(ArrivalSeries))
			Expect(got[0].Addition.Count).To(Equal(uint32(len(eps))))
			Expect(got[0].Addition.WholeSeason).To(BeTrue())
		},
	)

	It("answers an empty library with no arrivals", func() {
		got, err := store.RecentArrivals(ctx, 24)
		Expect(err).NotTo(HaveOccurred())
		Expect(got).To(BeEmpty())
	})
})
