package music

import (
	"context"
	"errors"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stretchr/testify/mock"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/album"
	"github.com/datahearth/streamline/ent/artist"
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

var _ = Describe("Music schedulers", Label("unit", "integration", "music"), func() {
	var (
		ctx      context.Context
		client   *ent.Client
		provider *mockmeta.MockMusicProvider
		posters  *mockposters.MockManager
		idx      *mockindexer.MockManager
		dl       *mockdownload.MockDownloader
		svc      *Service
	)

	flac := indexer.SearchResult{
		Title:    "Nirvana - Nevermind (1991) [FLAC]",
		Download: "magnet:?xt=urn:btih:abc",
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
			"library": map[string]any{
				"music_path":        GinkgoT().TempDir(),
				"no_match_cooldown": "6h",
				"max_grab_failures": 3,
			},
			"download_clients": clients,
			"music_quality_profiles": []map[string]any{
				{
					"name":      "lossless",
					"tiers":     []string{"hires", "lossless", "high"},
					"preferred": "lossless",
				},
			},
			"music_quality_default_profile": "lossless",
		})
	}

	BeforeEach(func() {
		ctx = context.Background()
		var err error
		client, err = db.Open(ctx, ":memory:")
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { client.Close() })
		provider = mockmeta.NewMockMusicProvider(GinkgoT())
		posters = mockposters.NewMockManager(GinkgoT())
		idx = mockindexer.NewMockManager(GinkgoT())
		dl = mockdownload.NewMockDownloader(GinkgoT())
		stubCoverPaths(posters)
		svc = NewService(db.New(client), provider, posters, nil, idx, dl)
		setupConfig(true)
	})

	seedAlbums := func(titles ...string) []*ent.Album {
		GinkgoHelper()
		seeds := make([]db.AlbumSeed, len(titles))
		for i, t := range titles {
			seeds[i] = db.AlbumSeed{MBID: "rg-" + t, Title: t, Type: "album"}
		}
		a, err := db.New(client).CreateArtist(ctx, db.CreateArtistParams{
			MBID: "mbid-1", Name: "Nirvana", Monitored: true,
			QualityProfile: "lossless", Albums: seeds,
		})
		Expect(err).NotTo(HaveOccurred())
		return client.Album.Query().
			Where(album.HasArtistWith(artist.IDEQ(a.ID))).
			Order(ent.Asc(album.FieldID)).AllX(ctx)
	}

	Describe("SearchMissing", func() {
		It("grabs the best release and resets the failure counter", func() {
			al := seedAlbums("Nevermind")[0]
			client.Album.UpdateOneID(al.ID).SetGrabFailures(2).ExecX(ctx)
			idx.EXPECT().
				SearchAlbum(mock.Anything, "Nirvana", "Nevermind", uint16(0)).
				Return([]indexer.SearchResult{
					{Title: "Nirvana - Nevermind (1991) [MP3 320]", Seeders: 99},
					flac,
				}, nil).Once()
			dl.EXPECT().GrabAlbum(mock.Anything, flac, al.ID).
				Return(&ent.DownloadRecord{}, nil).Once()

			Expect(svc.SearchMissing(ctx)).To(Succeed())

			got := client.Album.GetX(ctx, al.ID)
			Expect(got.Status).To(Equal(album.StatusDownloading))
			Expect(got.GrabFailures).To(BeZero())
			Expect(got.LastSearchAt).NotTo(BeNil())
		})

		It(
			"stamps the search and counts no failure when nothing is accepted",
			func() {
				al := seedAlbums("Nevermind")[0]
				idx.EXPECT().
					SearchAlbum(mock.Anything, "Nirvana", "Nevermind", uint16(0)).
					Return([]indexer.SearchResult{
						{Title: "Nirvana - Nevermind (1991) [MP3 192]"},
					}, nil).Once()

				Expect(svc.SearchMissing(ctx)).To(Succeed())

				got := client.Album.GetX(ctx, al.ID)
				Expect(got.Status).To(Equal(album.StatusWanted))
				Expect(got.GrabFailures).To(BeZero())
				Expect(got.LastSearchAt).NotTo(BeNil())
			},
		)

		It("counts a non-transport grab failure", func() {
			al := seedAlbums("Nevermind")[0]
			idx.EXPECT().
				SearchAlbum(mock.Anything, "Nirvana", "Nevermind", uint16(0)).
				Return([]indexer.SearchResult{flac}, nil).
				Once()
			dl.EXPECT().GrabAlbum(mock.Anything, flac, al.ID).
				Return(nil, errors.New("bad torrent")).Once()

			Expect(svc.SearchMissing(ctx)).To(Succeed())

			got := client.Album.GetX(ctx, al.ID)
			Expect(got.GrabFailures).To(Equal(uint8(1)))
			Expect(got.Status).To(Equal(album.StatusWanted))
		})

		It("does not count an unreachable download client", func() {
			al := seedAlbums("Nevermind")[0]
			idx.EXPECT().
				SearchAlbum(mock.Anything, "Nirvana", "Nevermind", uint16(0)).
				Return([]indexer.SearchResult{flac}, nil).
				Once()
			dl.EXPECT().GrabAlbum(mock.Anything, flac, al.ID).
				Return(nil, fmt.Errorf("add: %w", download.ErrUnreachable)).Once()

			Expect(svc.SearchMissing(ctx)).To(Succeed())

			Expect(client.Album.GetX(ctx, al.ID).GrabFailures).To(BeZero())
		})

		It("skips an album whose search errors and continues with the next", func() {
			albums := seedAlbums("Nevermind", "In Utero")
			idx.EXPECT().
				SearchAlbum(mock.Anything, "Nirvana", "Nevermind", uint16(0)).
				Return(nil, fmt.Errorf("q: %w", indexer.ErrUnreachable)).
				Once()
			idx.EXPECT().
				SearchAlbum(mock.Anything, "Nirvana", "In Utero", uint16(0)).
				Return([]indexer.SearchResult{flac}, nil).
				Once()
			dl.EXPECT().GrabAlbum(mock.Anything, flac, albums[1].ID).
				Return(&ent.DownloadRecord{}, nil).Once()

			Expect(svc.SearchMissing(ctx)).To(Succeed())

			failed := client.Album.GetX(ctx, albums[0].ID)
			Expect(failed.GrabFailures).To(BeZero())
			Expect(failed.LastSearchAt).To(BeNil())
			Expect(client.Album.GetX(ctx, albums[1].ID).Status).
				To(Equal(album.StatusDownloading))
		})

		It("leaves albums over the cap or inside the cooldown alone", func() {
			albums := seedAlbums("Capped", "Cooling")
			client.Album.UpdateOneID(albums[0].ID).SetGrabFailures(3).ExecX(ctx)
			client.Album.UpdateOneID(albums[1].ID).
				SetLastSearchAt(time.Now()).
				ExecX(ctx)

			Expect(svc.SearchMissing(ctx)).To(Succeed())
		})

		It("waives the cap and the cooldown on a manual run", func() {
			albums := seedAlbums("Capped", "Cooling")
			client.Album.UpdateOneID(albums[0].ID).SetGrabFailures(3).ExecX(ctx)
			client.Album.UpdateOneID(albums[1].ID).
				SetLastSearchAt(time.Now()).
				ExecX(ctx)
			idx.EXPECT().SearchAlbum(mock.Anything, "Nirvana", "Capped", uint16(0)).
				Return(nil, nil).Once()
			idx.EXPECT().SearchAlbum(mock.Anything, "Nirvana", "Cooling", uint16(0)).
				Return(nil, nil).Once()

			Expect(svc.SearchMissing(manualContext())).To(Succeed())
		})

		It("does nothing without an enabled download client", func() {
			setupConfig(false)
			seedAlbums("Nevermind")

			Expect(svc.SearchMissing(ctx)).To(Succeed())
		})
	})

	Describe("RefreshStale", func() {
		makeArtist := func(
			mbid string,
			refreshed *time.Time,
		) *ent.Artist {
			GinkgoHelper()
			c := client.Artist.Create().
				SetMbid(mbid).
				SetName(mbid).
				SetMonitored(true)
			if refreshed != nil {
				c = c.SetLastRefreshedAt(*refreshed)
			}
			return c.SaveX(ctx)
		}

		details := func(mbid string) *metadata.ArtistDetails {
			return &metadata.ArtistDetails{MBID: mbid, Name: mbid}
		}

		It("refreshes only stale artists, at most refreshBatch per run", func() {
			recent := time.Now().Add(-time.Hour)
			for i := range refreshBatch + 2 {
				makeArtist(fmt.Sprintf("stale-%02d", i), nil)
			}
			fresh := makeArtist("fresh", &recent)
			provider.EXPECT().GetArtist(mock.Anything, mock.Anything).
				RunAndReturn(func(_ context.Context, mbid string) (*metadata.ArtistDetails, error) {
					return details(mbid), nil
				}).
				Times(refreshBatch)
			start := time.Now()

			Expect(svc.RefreshStale(ctx)).To(Succeed())

			Expect(client.Artist.Query().
				Where(artist.LastRefreshedAtGTE(start)).Count(ctx)).
				To(Equal(refreshBatch))
			Expect(client.Artist.GetX(ctx, fresh.ID).LastRefreshedAt).
				To(HaveValue(BeTemporally("~", recent, time.Second)))
		})

		It(
			"persists a release-group new to the artist as an album inheriting monitored",
			func() {
				a := makeArtist("mbid-1", nil)
				provider.EXPECT().GetArtist(mock.Anything, "mbid-1").
					Return(&metadata.ArtistDetails{
						MBID: "mbid-1", Name: "mbid-1",
						ReleaseGroups: []metadata.ReleaseGroupInfo{
							{
								MBID:  "rg-new",
								Title: "New",
								Type:  metadata.AlbumTypeAlbum,
							},
						},
					}, nil).Once()
				provider.EXPECT().GetReleaseGroup(mock.Anything, "rg-new").
					Return(&metadata.ReleaseGroupDetails{
						MBID: "rg-new", Title: "New", ReleaseMBID: "rel-1",
					}, nil).Once()
				fetched := make(chan struct{})
				posters.EXPECT().
					Fetch(mock.Anything, "albums", mock.Anything, metadata.CoverArtURL("rg-new")).
					Run(func(context.Context, string, uint32, string) { close(fetched) }).
					Return(nil).Once()

				Expect(svc.RefreshStale(ctx)).To(Succeed())
				Eventually(fetched).Should(BeClosed())

				stored, err := db.New(client).FindArtistByID(ctx, a.ID)
				Expect(err).NotTo(HaveOccurred())
				Expect(stored.Edges.Albums).To(HaveLen(1))
				Expect(stored.Edges.Albums[0].Mbid).To(Equal("rg-new"))
				Expect(stored.Edges.Albums[0].Monitored).To(BeTrue())
				Expect(stored.LastRefreshedAt).NotTo(BeNil())
			},
		)

		It("skips an artist whose metadata fails and refreshes the rest", func() {
			bad := makeArtist("bad", nil)
			good := makeArtist("good", nil)
			provider.EXPECT().GetArtist(mock.Anything, "bad").
				Return(nil, errors.New("musicbrainz 503")).Once()
			provider.EXPECT().GetArtist(mock.Anything, "good").
				Return(details("good"), nil).Once()

			Expect(svc.RefreshStale(ctx)).To(Succeed())

			Expect(client.Artist.GetX(ctx, bad.ID).LastRefreshedAt).To(BeNil())
			Expect(client.Artist.GetX(ctx, good.ID).LastRefreshedAt).NotTo(BeNil())
		})

		It("honours the 24h interval unless the run is manual", func() {
			recent := time.Now().Add(-time.Hour)
			a := makeArtist("recent", &recent)

			Expect(svc.RefreshStale(ctx)).To(Succeed())

			provider.EXPECT().GetArtist(mock.Anything, "recent").
				Return(details("recent"), nil).Once()
			Expect(svc.RefreshStale(manualContext())).To(Succeed())
			Expect(client.Artist.GetX(ctx, a.ID).LastRefreshedAt).
				To(HaveValue(BeTemporally(">", recent)))
		})
	})
})
