package rss

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/stretchr/testify/mock"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/downloadrecord"
	"github.com/datahearth/streamline/ent/movie"
	"github.com/datahearth/streamline/ent/schema"
	"github.com/datahearth/streamline/internal/db"
	"github.com/datahearth/streamline/internal/indexer"
	"github.com/datahearth/streamline/internal/rss/mocks"
)

// The held-download dialog's "Search for a replacement" promises the rejected
// release is not grabbed again. These run the real resolve path
// (FailHeldDownloadRecord) and then the next automatic search against it.
var _ = Describe("rejected releases", Label("integration", "rss"), func() {
	const (
		bad  = "Fight.Club.1999.1080p.BluRay.x264-BAD"
		good = "Fight.Club.1999.1080p.WEB-DL.x264-GOOD"
	)

	var (
		ctx      context.Context
		dbClient *ent.Client
		store    *db.DB
	)

	BeforeEach(func() {
		ctx = context.Background()
		var err error
		dbClient, err = db.Open(ctx, ":memory:")
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { dbClient.Close() })
		store = db.New(dbClient)
	})

	reject := func(p db.CreateDownloadRecordParams) {
		GinkgoHelper()
		p.Status = downloadrecord.StatusImporting
		rec, err := store.CreateDownloadRecord(ctx, p)
		Expect(err).NotTo(HaveOccurred())
		Expect(store.HoldDownloadRecord(ctx, rec.ID, []schema.HoldReason{{
			File: "/dl/f.mkv", Check: "codec", Expected: "hevc", Actual: "h264",
		}})).To(Succeed())
		Expect(store.FailHeldDownloadRecord(
			ctx, rec.ID, "rejected by user (re-grab)", true,
		)).To(Succeed())
	}

	// The rejected release is the one a search would pick: it outscores the
	// other on seeders alone.
	results := func(badHash string) []indexer.SearchResult {
		return []indexer.SearchResult{
			{
				Title:    bad,
				InfoHash: badHash,
				Download: "magnet:bad",
				Size:     1000,
				Seeders:  500,
			},
			{
				Title:    good,
				InfoHash: "good",
				Download: "magnet:good",
				Size:     1000,
				Seeders:  10,
			},
		}
	}
	titled := func(title string) any {
		return mock.MatchedBy(func(r indexer.SearchResult) bool {
			return r.Title == title
		})
	}

	Describe("the movie missing search", func() {
		var (
			m        *ent.Movie
			indexerM *mocks.MockIndexerSearcher
			dlM      *mocks.MockDownloader
		)

		BeforeEach(func() {
			var err error
			m, err = dbClient.Movie.Create().
				SetTitle("Fight Club").SetOriginalTitle("Fight Club").
				SetYear(1999).SetTmdbID(550).
				SetStatus(movie.StatusDownloading).
				Save(ctx)
			Expect(err).NotTo(HaveOccurred())
			reject(db.CreateDownloadRecordParams{
				Title: bad, Size: 1000, TorrentHash: "bad", MovieID: m.ID,
				DownloadClientName: "qbit",
			})
			indexerM = mocks.NewMockIndexerSearcher(GinkgoT())
			dlM = mocks.NewMockDownloader(GinkgoT())
		})

		It("grabs the next-best release instead of the rejected one", func() {
			indexerM.EXPECT().
				SearchMovie(mock.Anything, mock.Anything, mock.Anything, uint32(550)).
				Return(results("BAD"), nil)
			dlM.EXPECT().Grab(mock.Anything, titled(good), m.ID).
				Return(&ent.DownloadRecord{}, nil)

			Expect(newTestSearcher(dbClient, indexerM, dlM).Run(ctx)).To(Succeed())
		})

		It("matches a result that carries no hash by its title", func() {
			indexerM.EXPECT().
				SearchMovie(mock.Anything, mock.Anything, mock.Anything, uint32(550)).
				Return(results(""), nil)
			dlM.EXPECT().Grab(mock.Anything, titled(good), m.ID).
				Return(&ent.DownloadRecord{}, nil)

			Expect(newTestSearcher(dbClient, indexerM, dlM).Run(ctx)).To(Succeed())
		})

		It("grabs nothing when the rejected release is the only one", func() {
			indexerM.EXPECT().
				SearchMovie(mock.Anything, mock.Anything, mock.Anything, uint32(550)).
				Return(results("bad")[:1], nil)

			err := newTestSearcher(dbClient, indexerM, dlM).
				SearchOne(ctx, m)
			Expect(err).To(MatchError(ErrNoEligibleRelease))
		})
	})

	Describe("the episode missing search", func() {
		const (
			badEp  = "Show.S01E01.1080p.WEB-DL.x264-BAD"
			goodEp = "Show.S01E01.1080p.WEB-DL.x264-GOOD"
		)

		It("grabs the next-best release instead of the rejected one", func() {
			aired := time.Now().AddDate(0, -1, 0)
			show, err := store.CreateTVShow(ctx, db.CreateTVShowParams{
				Title: "Show", Year: 2024, TvdbID: 4242,
				Seasons: []db.SeasonSeed{{
					Number: 1,
					Episodes: []db.EpisodeSeed{{
						Number: 1, Title: "Pilot", AirDate: &aired,
					}},
				}},
			})
			Expect(err).NotTo(HaveOccurred())
			epID := show.Edges.Seasons[0].Edges.Episodes[0].ID
			reject(db.CreateDownloadRecordParams{
				Title: badEp, Size: 1000, TorrentHash: "bad", EpisodeID: epID,
				DownloadClientName: "qbit",
			})

			indexerM := mocks.NewMockTVIndexerSearcher(GinkgoT())
			dlM := mocks.NewMockEpisodeGrabber(GinkgoT())
			indexerM.EXPECT().
				SearchEpisode(mock.Anything, mock.Anything, mock.Anything,
					uint32(4242), uint16(1), uint16(1)).
				Return([]indexer.SearchResult{
					{
						Title:    badEp,
						InfoHash: "bad",
						Download: "magnet:bad",
						Size:     1000,
						Seeders:  500,
					},
					{
						Title:    goodEp,
						InfoHash: "good",
						Download: "magnet:good",
						Size:     1000,
						Seeders:  10,
					},
				}, 0, nil)
			dlM.EXPECT().
				GrabEpisode(mock.Anything, titled(goodEp), epID, []uint32{epID}).
				Return(&ent.DownloadRecord{}, nil)

			Expect(NewEpisodeMissingSearcher(store, indexerM, dlM).Run(ctx)).
				To(Succeed())
		})
	})
})
