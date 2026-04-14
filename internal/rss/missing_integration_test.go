package rss

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/stretchr/testify/mock"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/movie"
	"github.com/datahearth/streamline/internal/db"
	"github.com/datahearth/streamline/internal/indexer"
	"github.com/datahearth/streamline/internal/rss/mocks"
	"github.com/datahearth/streamline/internal/testutil/configtest"
)

var _ = Describe("MissingSearcher.Run", Label("integration", "rss"), func() {
	var (
		ctx      context.Context
		dbClient *ent.Client
		indexerM *mocks.MockIndexerSearcher
		dlM      *mocks.MockDownloader
		syncer   *MissingSearcher
	)

	BeforeEach(func() {
		ctx = context.Background()

		var err error
		dbClient, err = db.Open(ctx, ":memory:")
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { dbClient.Close() })

		indexerM = mocks.NewMockIndexerSearcher(GinkgoT())
		dlM = mocks.NewMockDownloader(GinkgoT())

		syncer = newTestSearcher(dbClient, indexerM, dlM)
	})

	Context("with one wanted movie and one matching result", func() {
		var m *ent.Movie

		BeforeEach(func() {
			var err error
			m, err = dbClient.Movie.Create().
				SetTitle("Fight Club").
				SetOriginalTitle("Fight Club").
				SetYear(1999).
				SetTmdbID(550).
				SetStatus(movie.StatusWanted).
				Save(ctx)
			Expect(err).NotTo(HaveOccurred())

			indexerM.EXPECT().
				SearchMovie(mock.Anything, []string{"Fight Club", "Fight Club"}, uint32(550)).
				Return([]indexer.SearchResult{
					{
						Title:    "Fight.Club.1999.1080p.BluRay.x264-GROUP",
						Download: "magnet:test",
						Size:     1000,
						Seeders:  50,
					},
				}, nil)

			dlM.EXPECT().
				Grab(mock.Anything, mock.Anything, m.ID).
				Return(&ent.DownloadRecord{}, nil)
		})

		It("grabs the release and updates last_search_at", func() {
			Expect(syncer.Run(ctx)).To(Succeed())

			refreshed, err := dbClient.Movie.Get(ctx, m.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(refreshed.LastSearchAt).NotTo(BeNil())
			Expect(refreshed.GrabFailures).To(Equal(uint8(0)))
		})
	})

	Context("when indexer returns only low-res results", func() {
		var m *ent.Movie

		BeforeEach(func() {
			var err error
			m, err = dbClient.Movie.Create().
				SetTitle("Fight Club").
				SetOriginalTitle("Fight Club").SetYear(1999).SetTmdbID(550).
				SetStatus(movie.StatusWanted).Save(ctx)
			Expect(err).NotTo(HaveOccurred())

			// Raise the min-resolution bar so the only test result gets filtered out.
			overlay := defaultRSSConfig()
			overlay["library"].(map[string]any)["default_quality"].(map[string]any)["min_resolution"] = "1080p"
			configtest.Setup(overlay)
			syncer = newTestSearcher(dbClient, indexerM, dlM)

			indexerM.EXPECT().
				SearchMovie(mock.Anything, []string{"Fight Club", "Fight Club"}, uint32(550)).
				Return([]indexer.SearchResult{
					{Title: "Fight.Club.1999.720p.BluRay.x264-GROUP", Seeders: 100},
				}, nil)
		})

		It("does not grab but sets last_search_at", func() {
			Expect(syncer.Run(ctx)).To(Succeed())

			refreshed, err := dbClient.Movie.Get(ctx, m.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(refreshed.LastSearchAt).NotTo(BeNil())
			Expect(refreshed.GrabFailures).To(Equal(uint8(0)))
		})
	})

	Context("when movie is inside cooldown window", func() {
		BeforeEach(func() {
			_, err := dbClient.Movie.Create().
				SetTitle("Fight Club").
				SetOriginalTitle("Fight Club").SetYear(1999).SetTmdbID(550).
				SetStatus(movie.StatusWanted).
				SetLastSearchAt(time.Now().Add(-1 * time.Hour)).
				Save(ctx)
			Expect(err).NotTo(HaveOccurred())
		})

		It("is excluded from the query", func() {
			Expect(syncer.Run(ctx)).To(Succeed())
			// mockery verifies no unexpected calls via GinkgoT()
		})
	})

	Context("when movie has grab_failures >= max", func() {
		BeforeEach(func() {
			_, err := dbClient.Movie.Create().
				SetTitle("Fight Club").
				SetOriginalTitle("Fight Club").SetYear(1999).SetTmdbID(550).
				SetStatus(movie.StatusWanted).
				SetGrabFailures(3).
				Save(ctx)
			Expect(err).NotTo(HaveOccurred())
		})

		It("is excluded from the query", func() {
			Expect(syncer.Run(ctx)).To(Succeed())
		})
	})

	Context("when download.Grab errors", func() {
		var m *ent.Movie

		BeforeEach(func() {
			var err error
			m, err = dbClient.Movie.Create().
				SetTitle("Fight Club").
				SetOriginalTitle("Fight Club").SetYear(1999).SetTmdbID(550).
				SetStatus(movie.StatusWanted).
				SetGrabFailures(1).
				Save(ctx)
			Expect(err).NotTo(HaveOccurred())

			indexerM.EXPECT().
				SearchMovie(mock.Anything, []string{"Fight Club", "Fight Club"}, uint32(550)).
				Return([]indexer.SearchResult{
					{Title: "Fight.Club.1999.1080p.BluRay.x264-GROUP", Seeders: 50},
				}, nil)

			dlM.EXPECT().
				Grab(mock.Anything, mock.Anything, m.ID).
				Return(nil, errors.New("qbit down"))
		})

		It("increments grab_failures and sets last_search_at", func() {
			Expect(syncer.Run(ctx)).To(Succeed())
			refreshed, err := dbClient.Movie.Get(ctx, m.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(refreshed.LastSearchAt).NotTo(BeNil())
			Expect(refreshed.GrabFailures).To(Equal(uint8(2)))
		})
	})

	Context("with 12 wanted movies and the hard-coded worker cap", func() {
		It(
			"caps concurrent SearchMovie calls at defaultRSSWorkers",
			func() {
				for i := range 12 {
					title := fmt.Sprintf("Movie %d", i)
					_, err := dbClient.Movie.Create().
						SetTitle(title).
						SetOriginalTitle(title).
						SetYear(2020).SetTmdbID(uint32(1000 + i)).
						SetStatus(movie.StatusWanted).Save(ctx)
					Expect(err).NotTo(HaveOccurred())
				}

				var (
					mu      sync.Mutex
					active  int
					maxSeen int
				)

				indexerM.EXPECT().
					SearchMovie(mock.Anything, mock.Anything, mock.Anything).
					Run(func(ctx context.Context, titles []string, tmdbID uint32) {
						mu.Lock()
						active++
						if active > maxSeen {
							maxSeen = active
						}
						mu.Unlock()

						time.Sleep(20 * time.Millisecond)

						mu.Lock()
						active--
						mu.Unlock()
					}).
					Return([]indexer.SearchResult{}, nil).
					Times(12)

				Expect(syncer.Run(ctx)).To(Succeed())
				Expect(
					maxSeen,
				).To(BeNumerically(">", 1), "expected real parallelism")
				Expect(maxSeen).To(BeNumerically("<=", defaultRSSWorkers))
			},
		)
	})
})
