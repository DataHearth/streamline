package book

import (
	"context"
	"errors"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stretchr/testify/mock"

	"github.com/datahearth/streamline/ent"
	entauthor "github.com/datahearth/streamline/ent/author"
	entbook "github.com/datahearth/streamline/ent/book"
	"github.com/datahearth/streamline/ent/mediafile"
	"github.com/datahearth/streamline/internal/db"
	"github.com/datahearth/streamline/internal/download"
	mockdownload "github.com/datahearth/streamline/internal/download/mocks"
	"github.com/datahearth/streamline/internal/indexer"
	mockindexer "github.com/datahearth/streamline/internal/indexer/mocks"
	"github.com/datahearth/streamline/internal/metadata"
	mockmeta "github.com/datahearth/streamline/internal/metadata/mocks"
	mockposters "github.com/datahearth/streamline/internal/posters/mocks"
	"github.com/datahearth/streamline/internal/scheduler"
	"github.com/datahearth/streamline/internal/testutil/configtest"
)

func manualContext() context.Context {
	GinkgoHelper()
	got := make(chan context.Context, 4)
	s := scheduler.New()
	s.Register("probe", time.Hour, func(ctx context.Context) error {
		got <- ctx
		return nil
	})
	root, cancel := context.WithCancel(context.Background())
	DeferCleanup(cancel)
	go s.Start(root)
	Eventually(got).Should(Receive())
	Eventually(func() bool {
		info, err := s.Get("probe")
		Expect(err).NotTo(HaveOccurred())
		return info.Running
	}).Should(BeFalse())
	Expect(s.RunNow("probe")).To(Succeed())
	var manual context.Context
	Eventually(got).Should(Receive(&manual))
	return manual
}

var _ = Describe("Book schedulers", Label("unit", "integration", "book"), func() {
	var (
		ctx      context.Context
		client   *ent.Client
		provider *mockmeta.MockBookProvider
		posters  *mockposters.MockManager
		indexers *mockindexer.MockManager
		dl       *mockdownload.MockDownloader
		svc      *Service
	)

	const (
		ebook     = mediafile.BookKindEbook
		audiobook = mediafile.BookKindAudiobook
	)
	epub := indexer.SearchResult{
		Title:    "Brandon Sanderson - Elantris (2005) EPUB",
		Download: "magnet:?xt=urn:btih:abc",
		Seeders:  5,
	}
	m4b := indexer.SearchResult{
		Title:    "Brandon Sanderson - Elantris (2005) M4B",
		Download: "magnet:?xt=urn:btih:def",
		Seeders:  5,
	}

	setupConfig := func(withClient bool) {
		GinkgoHelper()
		clients := []map[string]any{}
		if withClient {
			clients = append(clients, map[string]any{
				"name":        "qbit",
				"client_type": "qbittorrent",
				"host":        "127.0.0.1",
				"port":        8080,
				"auth_method": "password",
				"enabled":     true,
			})
		}
		configtest.Setup(map[string]any{
			"ebook_quality_profiles": []map[string]any{
				{
					"name":    "std",
					"formats": []string{"epub", "azw3"},
					"cutoff":  "epub",
				},
			},
			"ebook_quality_default_profile": "std",
			"audiobook_quality_profiles": []map[string]any{
				{"name": "std", "formats": []string{"m4b", "mp3"}, "cutoff": "m4b"},
			},
			"audiobook_quality_default_profile": "std",
			"library": map[string]any{
				"ebook_path":        GinkgoT().TempDir(),
				"audiobook_path":    GinkgoT().TempDir(),
				"no_match_cooldown": "6h",
				"max_grab_failures": 3,
			},
			"download_clients": clients,
		})
	}

	BeforeEach(func() {
		ctx = context.Background()
		var err error
		client, err = db.Open(ctx, ":memory:")
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { client.Close() })
		provider = mockmeta.NewMockBookProvider(GinkgoT())
		posters = mockposters.NewMockManager(GinkgoT())
		indexers = mockindexer.NewMockManager(GinkgoT())
		dl = mockdownload.NewMockDownloader(GinkgoT())
		svc = NewService(db.New(client), provider, posters, indexers, dl)
		setupConfig(true)
	})

	seedBooks := func(seeds ...db.BookSeed) []*ent.Book {
		GinkgoHelper()
		_, err := db.New(client).CreateAuthor(ctx, db.CreateAuthorParams{
			HardcoverID: 219851, Name: "Brandon Sanderson", Monitored: true,
			MonitorPolicy: "all", WantKinds: "both", Books: seeds,
		})
		Expect(err).NotTo(HaveOccurred())
		return client.Book.Query().Order(ent.Asc(entbook.FieldID)).AllX(ctx)
	}
	elantris := func(ebookWanted, audioWanted bool) db.BookSeed {
		return db.BookSeed{
			HardcoverID: 1, Title: "Elantris",
			EbookMonitored: ebookWanted, AudiobookMonitored: audioWanted,
		}
	}
	expectSearch := func(title string, kind mediafile.BookKind, rs ...indexer.SearchResult) {
		GinkgoHelper()
		indexers.EXPECT().
			SearchBook(mock.Anything, "Brandon Sanderson", title, uint16(0), kind).
			Return(rs, nil).Once()
	}

	Describe("SearchMissing", func() {
		It("grabs the ebook slot and leaves the audiobook slot alone", func() {
			b := seedBooks(elantris(true, false))[0]
			client.Book.UpdateOneID(b.ID).SetEbookGrabFailures(2).ExecX(ctx)
			expectSearch("Elantris", ebook, epub)
			dl.EXPECT().GrabBook(mock.Anything, epub, b.ID, ebook).
				Return(&ent.DownloadRecord{}, nil).Once()

			Expect(svc.SearchMissing(ctx)).To(Succeed())

			got := client.Book.GetX(ctx, b.ID)
			Expect(got.EbookStatus).To(Equal(entbook.EbookStatusDownloading))
			Expect(got.EbookGrabFailures).To(BeZero())
			Expect(got.EbookLastSearchAt).NotTo(BeNil())
			Expect(got.AudiobookStatus).To(Equal(entbook.AudiobookStatusSkipped))
			Expect(got.AudiobookLastSearchAt).To(BeNil())
		})

		It("searches and grabs a book wanted in both slots once per kind", func() {
			b := seedBooks(elantris(true, true))[0]
			expectSearch("Elantris", ebook, epub)
			expectSearch("Elantris", audiobook, m4b)
			dl.EXPECT().GrabBook(mock.Anything, epub, b.ID, ebook).
				Return(&ent.DownloadRecord{}, nil).Once()
			dl.EXPECT().GrabBook(mock.Anything, m4b, b.ID, audiobook).
				Return(&ent.DownloadRecord{}, nil).Once()

			Expect(svc.SearchMissing(ctx)).To(Succeed())

			got := client.Book.GetX(ctx, b.ID)
			Expect(got.EbookStatus).To(Equal(entbook.EbookStatusDownloading))
			Expect(got.AudiobookStatus).To(Equal(entbook.AudiobookStatusDownloading))
		})

		It("never grabs a rejected release", func() {
			b := seedBooks(elantris(true, false))[0]
			pdf := indexer.SearchResult{
				Title: "Brandon Sanderson - Elantris (2005) PDF", Seeders: 99,
			}
			expectSearch("Elantris", ebook, pdf, epub)
			dl.EXPECT().GrabBook(mock.Anything, epub, b.ID, ebook).
				Return(&ent.DownloadRecord{}, nil).Once()

			Expect(svc.SearchMissing(ctx)).To(Succeed())
		})

		It("treats all-rejected as no result and stamps the slot", func() {
			b := seedBooks(elantris(true, false))[0]
			expectSearch("Elantris", ebook, indexer.SearchResult{
				Title: "Brandon Sanderson - Elantris (2005) PDF", Seeders: 99,
			})

			Expect(svc.SearchMissing(ctx)).To(Succeed())

			got := client.Book.GetX(ctx, b.ID)
			Expect(got.EbookStatus).To(Equal(entbook.EbookStatusWanted))
			Expect(got.EbookGrabFailures).To(BeZero())
			Expect(got.EbookLastSearchAt).NotTo(BeNil())
		})

		It("stamps the slot and counts no failure when there is no result", func() {
			b := seedBooks(elantris(false, true))[0]
			expectSearch("Elantris", audiobook)

			Expect(svc.SearchMissing(ctx)).To(Succeed())

			got := client.Book.GetX(ctx, b.ID)
			Expect(got.AudiobookLastSearchAt).NotTo(BeNil())
			Expect(got.AudiobookGrabFailures).To(BeZero())
			Expect(got.EbookLastSearchAt).To(BeNil())
		})

		It("counts a non-transport grab failure against that slot only", func() {
			b := seedBooks(elantris(true, false))[0]
			expectSearch("Elantris", ebook, epub)
			dl.EXPECT().GrabBook(mock.Anything, epub, b.ID, ebook).
				Return(nil, errors.New("bad torrent")).Once()

			Expect(svc.SearchMissing(ctx)).To(Succeed())

			got := client.Book.GetX(ctx, b.ID)
			Expect(got.EbookGrabFailures).To(Equal(uint8(1)))
			Expect(got.AudiobookGrabFailures).To(BeZero())
			Expect(got.EbookStatus).To(Equal(entbook.EbookStatusWanted))
		})

		It("does not count an unreachable download client", func() {
			b := seedBooks(elantris(true, false))[0]
			expectSearch("Elantris", ebook, epub)
			dl.EXPECT().GrabBook(mock.Anything, epub, b.ID, ebook).
				Return(nil, fmt.Errorf("add: %w", download.ErrUnreachable)).Once()

			Expect(svc.SearchMissing(ctx)).To(Succeed())

			Expect(client.Book.GetX(ctx, b.ID).EbookGrabFailures).To(BeZero())
		})

		It("skips a slot whose search errors and continues", func() {
			books := seedBooks(
				elantris(true, false),
				db.BookSeed{
					HardcoverID:    2,
					Title:          "Warbreaker",
					EbookMonitored: true,
				},
			)
			indexers.EXPECT().
				SearchBook(mock.Anything, "Brandon Sanderson", "Elantris", uint16(0), ebook).
				Return(nil, fmt.Errorf("q: %w", indexer.ErrUnreachable)).Once()
			warbreaker := indexer.SearchResult{
				Title: "Brandon Sanderson - Warbreaker (2009) EPUB", Seeders: 3,
			}
			expectSearch("Warbreaker", ebook, warbreaker)
			dl.EXPECT().GrabBook(mock.Anything, warbreaker, books[1].ID, ebook).
				Return(&ent.DownloadRecord{}, nil).Once()

			Expect(svc.SearchMissing(ctx)).To(Succeed())

			failed := client.Book.GetX(ctx, books[0].ID)
			Expect(failed.EbookGrabFailures).To(BeZero())
			Expect(failed.EbookLastSearchAt).To(BeNil())
		})

		It("gates each slot on its own cap and cooldown", func() {
			b := seedBooks(elantris(true, true))[0]
			client.Book.UpdateOneID(b.ID).
				SetEbookGrabFailures(3).
				SetAudiobookLastSearchAt(time.Now()).ExecX(ctx)

			Expect(svc.SearchMissing(ctx)).To(Succeed())
		})

		It("waives the cap and the cooldown on a manual run", func() {
			b := seedBooks(elantris(true, true))[0]
			client.Book.UpdateOneID(b.ID).
				SetEbookGrabFailures(3).
				SetAudiobookLastSearchAt(time.Now()).ExecX(ctx)
			expectSearch("Elantris", ebook)
			expectSearch("Elantris", audiobook)

			Expect(svc.SearchMissing(manualContext())).To(Succeed())
		})

		It("does nothing without an enabled download client", func() {
			setupConfig(false)
			seedBooks(elantris(true, true))

			Expect(svc.SearchMissing(ctx)).To(Succeed())
		})
	})

	Describe("RefreshStale", func() {
		makeAuthor := func(hc uint32, refreshed *time.Time) *ent.Author {
			GinkgoHelper()
			a, err := db.New(client).CreateAuthor(ctx, db.CreateAuthorParams{
				HardcoverID: hc, Name: fmt.Sprintf("author-%d", hc), Monitored: true,
				MonitorPolicy: "all", WantKinds: "ebook",
			})
			Expect(err).NotTo(HaveOccurred())
			if refreshed != nil {
				client.Author.UpdateOneID(a.ID).
					SetLastRefreshedAt(*refreshed).
					ExecX(ctx)
			}
			return a
		}
		details := func(hc uint32, books ...metadata.BookInfo) *metadata.AuthorDetails {
			return &metadata.AuthorDetails{
				HardcoverID: hc,
				Name:        fmt.Sprintf("author-%d", hc),
				Books:       books,
			}
		}

		It("refreshes only stale authors, at most refreshBatch per run", func() {
			recent := time.Now().Add(-time.Hour)
			for i := range uint32(refreshBatch + 2) {
				makeAuthor(100+i, nil)
			}
			fresh := makeAuthor(500, &recent)
			provider.EXPECT().GetAuthor(mock.Anything, mock.Anything).
				RunAndReturn(func(_ context.Context, hc uint32) (*metadata.AuthorDetails, error) {
					return details(hc), nil
				}).
				Times(refreshBatch)
			start := time.Now()

			Expect(svc.RefreshStale(ctx)).To(Succeed())

			Expect(client.Author.Query().
				Where(entauthor.LastRefreshedAtGTE(start)).Count(ctx)).
				To(Equal(refreshBatch))
			Expect(client.Author.GetX(ctx, fresh.ID).LastRefreshedAt).
				To(HaveValue(BeTemporally("~", recent, time.Second)))
		})

		It("applies the monitor policy to new books and persists them", func() {
			a, err := db.New(client).CreateAuthor(ctx, db.CreateAuthorParams{
				HardcoverID: 7, Name: "author-7", Monitored: true,
				MonitorPolicy: "future", WantKinds: "ebook",
			})
			Expect(err).NotTo(HaveOccurred())
			past := time.Date(2019, 1, 1, 0, 0, 0, 0, time.UTC)
			future := time.Now().AddDate(1, 0, 0)
			provider.EXPECT().GetAuthor(mock.Anything, uint32(7)).
				Return(details(7,
					metadata.BookInfo{
						HardcoverID: 5, Title: "Future", ReleaseDate: &future,
						CoverURL: "https://img/5.jpg",
					},
					metadata.BookInfo{
						HardcoverID: 6, Title: "Past", ReleaseDate: &past,
						CoverURL: "https://img/6.jpg",
					},
				), nil).Once()
			fetched := make(chan struct{})
			remaining := 2
			posters.EXPECT().
				Fetch(mock.Anything, "books", mock.Anything, mock.Anything).
				Run(func(context.Context, string, uint32, string) {
					remaining--
					if remaining == 0 {
						close(fetched)
					}
				}).
				Return(nil).Times(2)

			Expect(svc.RefreshStale(ctx)).To(Succeed())
			Eventually(fetched).Should(BeClosed())

			stored, err := db.New(client).FindAuthorByID(ctx, a.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(stored.Edges.Books).To(HaveLen(2))
			byHC := map[uint32]*ent.Book{}
			for _, b := range stored.Edges.Books {
				byHC[b.HardcoverID] = b
			}
			Expect(byHC[5].EbookStatus).To(Equal(entbook.EbookStatusWanted))
			Expect(byHC[5].EbookMonitored).To(BeTrue())
			Expect(byHC[6].EbookStatus).To(Equal(entbook.EbookStatusSkipped))
			Expect(stored.LastRefreshedAt).NotTo(BeNil())
		})

		It("skips an author whose metadata fails and refreshes the rest", func() {
			bad := makeAuthor(1, nil)
			good := makeAuthor(2, nil)
			provider.EXPECT().GetAuthor(mock.Anything, uint32(1)).
				Return(nil, errors.New("hardcover 503")).Once()
			provider.EXPECT().GetAuthor(mock.Anything, uint32(2)).
				Return(details(2), nil).Once()

			Expect(svc.RefreshStale(ctx)).To(Succeed())

			Expect(client.Author.GetX(ctx, bad.ID).LastRefreshedAt).To(BeNil())
			Expect(client.Author.GetX(ctx, good.ID).LastRefreshedAt).NotTo(BeNil())
		})

		It("stops the batch at the first rate limit and returns nil", func() {
			first := makeAuthor(1, nil)
			second := makeAuthor(2, nil)
			provider.EXPECT().GetAuthor(mock.Anything, mock.Anything).
				Return(nil, &metadata.RateLimitedError{RetryAfter: time.Minute}).
				Once()

			Expect(svc.RefreshStale(ctx)).To(Succeed())

			Expect(client.Author.GetX(ctx, first.ID).LastRefreshedAt).To(BeNil())
			Expect(client.Author.GetX(ctx, second.ID).LastRefreshedAt).To(BeNil())
		})

		It("surfaces a rate limit from Add and RefreshOne", func() {
			limited := &metadata.RateLimitedError{RetryAfter: time.Minute}
			provider.EXPECT().GetAuthor(mock.Anything, mock.Anything).
				Return(nil, limited)
			a := makeAuthor(3, nil)

			_, err := svc.Add(ctx, AddParams{HardcoverID: 9})
			Expect(err).To(MatchError(metadata.ErrRateLimited))
			_, err = svc.RefreshOne(ctx, a.ID)
			Expect(err).To(MatchError(metadata.ErrRateLimited))
		})

		It("does nothing when Hardcover is not configured", func() {
			makeAuthor(1, nil)
			unconfigured := NewService(db.New(client), nil, posters, indexers, dl)

			Expect(unconfigured.RefreshStale(ctx)).To(Succeed())
		})

		It("honours the 24h interval unless the run is manual", func() {
			recent := time.Now().Add(-time.Hour)
			a := makeAuthor(1, &recent)

			Expect(svc.RefreshStale(ctx)).To(Succeed())

			provider.EXPECT().GetAuthor(mock.Anything, uint32(1)).
				Return(details(1), nil).Once()
			Expect(svc.RefreshStale(manualContext())).To(Succeed())
			Expect(client.Author.GetX(ctx, a.ID).LastRefreshedAt).
				To(HaveValue(BeTemporally(">", recent)))
		})
	})
})
