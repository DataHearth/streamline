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
	"github.com/datahearth/streamline/internal/download"
	"github.com/datahearth/streamline/internal/indexer"
	"github.com/datahearth/streamline/internal/metadata"
	"github.com/datahearth/streamline/internal/scheduler"
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
	var e *env

	flac := indexer.SearchResult{
		Title:    "Nirvana - Nevermind (1991) [FLAC]",
		Download: "magnet:?xt=urn:btih:abc",
		Seeders:  5,
	}

	BeforeEach(func() {
		e = newEnv(false)
	})

	// seedAlbums adds Nirvana with a hydrated, monitored, released album per title.
	seedAlbums := func(titles ...string) []*ent.Album {
		GinkgoHelper()
		seeds := make([]albumSeed, len(titles))
		for i, t := range titles {
			seeds[i] = albumSeed{
				mbid: "rg-" + t, title: t, monitored: true, tracks: []string{"A"},
			}
		}
		_, albums := e.seedArtist("Nirvana", seeds...)
		return albums
	}

	Describe("SearchMissing", func() {
		It("grabs the best release and resets the failure counter", func() {
			al := seedAlbums("Nevermind")[0]
			e.client.Album.UpdateOneID(al.ID).SetGrabFailures(2).ExecX(e.ctx)
			e.idx.EXPECT().
				SearchAlbum(mock.Anything, "Nirvana", "Nevermind", uint16(0)).
				Return([]indexer.SearchResult{
					{Title: "Nirvana - Nevermind (1991) [MP3 192]", Seeders: 99},
					flac,
				}, nil).Once()
			e.dl.EXPECT().GrabAlbum(mock.Anything, flac, al.ID).
				Return(&ent.DownloadRecord{}, nil).Once()

			Expect(e.svc.SearchMissing(e.ctx)).To(Succeed())

			got := e.client.Album.GetX(e.ctx, al.ID)
			Expect(got.Status).To(Equal(album.StatusDownloading))
			Expect(got.GrabFailures).To(BeZero())
			Expect(got.LastSearchAt).NotTo(BeNil())
		})

		It(
			"stamps the search and counts no failure when nothing is accepted",
			func() {
				al := seedAlbums("Nevermind")[0]
				e.idx.EXPECT().
					SearchAlbum(mock.Anything, "Nirvana", "Nevermind", uint16(0)).
					Return([]indexer.SearchResult{
						{Title: "Nirvana - Nevermind (1991) [MP3 128]"},
					}, nil).Once()

				Expect(e.svc.SearchMissing(e.ctx)).To(Succeed())

				got := e.client.Album.GetX(e.ctx, al.ID)
				Expect(got.Status).To(Equal(album.StatusWanted))
				Expect(got.GrabFailures).To(BeZero())
				Expect(got.LastSearchAt).NotTo(BeNil())
			},
		)

		It(
			"never grabs a release the profile rejects, even when it is the only one",
			func() {
				al := seedAlbums("Nevermind")[0]
				e.idx.EXPECT().
					SearchAlbum(mock.Anything, "Nirvana", "Nevermind", uint16(0)).
					Return([]indexer.SearchResult{
						{Title: "Nirvana - Nevermind (1991) [MP3 128]", Seeders: 50},
						{
							Title:   "Nirvana - Discography (1989-1994) [FLAC]",
							Seeders: 50,
						},
					}, nil).Once()

				Expect(e.svc.SearchMissing(e.ctx)).To(Succeed())
				Expect(
					e.client.Album.GetX(e.ctx, al.ID).Status,
				).To(Equal(album.StatusWanted))
			},
		)

		It("counts a non-transport grab failure", func() {
			al := seedAlbums("Nevermind")[0]
			e.idx.EXPECT().
				SearchAlbum(mock.Anything, "Nirvana", "Nevermind", uint16(0)).
				Return([]indexer.SearchResult{flac}, nil).Once()
			e.dl.EXPECT().GrabAlbum(mock.Anything, flac, al.ID).
				Return(nil, errors.New("bad torrent")).Once()

			Expect(e.svc.SearchMissing(e.ctx)).To(Succeed())

			got := e.client.Album.GetX(e.ctx, al.ID)
			Expect(got.GrabFailures).To(Equal(uint8(1)))
			Expect(got.Status).To(Equal(album.StatusWanted))
		})

		It("does not count an unreachable download client", func() {
			al := seedAlbums("Nevermind")[0]
			e.idx.EXPECT().
				SearchAlbum(mock.Anything, "Nirvana", "Nevermind", uint16(0)).
				Return([]indexer.SearchResult{flac}, nil).Once()
			e.dl.EXPECT().GrabAlbum(mock.Anything, flac, al.ID).
				Return(nil, fmt.Errorf("add: %w", download.ErrUnreachable)).Once()

			Expect(e.svc.SearchMissing(e.ctx)).To(Succeed())
			Expect(e.client.Album.GetX(e.ctx, al.ID).GrabFailures).To(BeZero())
		})

		It("skips an album whose search errors and continues with the next", func() {
			albums := seedAlbums("Nevermind", "In Utero")
			inUtero := indexer.SearchResult{
				Title:    "Nirvana - In Utero (1993) [FLAC]",
				Download: "magnet:?xt=urn:btih:def",
				Seeders:  5,
			}
			e.idx.EXPECT().
				SearchAlbum(mock.Anything, "Nirvana", "Nevermind", uint16(0)).
				Return(nil, fmt.Errorf("q: %w", indexer.ErrUnreachable)).Once()
			e.idx.EXPECT().
				SearchAlbum(mock.Anything, "Nirvana", "In Utero", uint16(0)).
				Return([]indexer.SearchResult{inUtero}, nil).Once()
			e.dl.EXPECT().GrabAlbum(mock.Anything, inUtero, albums[1].ID).
				Return(&ent.DownloadRecord{}, nil).Once()

			Expect(e.svc.SearchMissing(e.ctx)).To(Succeed())

			failed := e.client.Album.GetX(e.ctx, albums[0].ID)
			Expect(failed.GrabFailures).To(BeZero())
			Expect(failed.LastSearchAt).To(BeNil())
			Expect(e.client.Album.GetX(e.ctx, albums[1].ID).Status).
				To(Equal(album.StatusDownloading))
		})

		It("never grabs a release that names another album", func() {
			al := seedAlbums("In Utero")[0]
			e.idx.EXPECT().
				SearchAlbum(mock.Anything, "Nirvana", "In Utero", uint16(0)).
				Return([]indexer.SearchResult{flac}, nil).Once()

			Expect(e.svc.SearchMissing(e.ctx)).To(Succeed())

			got := e.client.Album.GetX(e.ctx, al.ID)
			Expect(got.Status).To(Equal(album.StatusWanted))
			Expect(got.LastSearchAt).NotTo(BeNil())
		})

		It("leaves albums over the cap or inside the cooldown alone", func() {
			albums := seedAlbums("Capped", "Cooling")
			e.client.Album.UpdateOneID(albums[0].ID).SetGrabFailures(3).ExecX(e.ctx)
			e.client.Album.UpdateOneID(albums[1].ID).
				SetLastSearchAt(time.Now()).
				ExecX(e.ctx)

			Expect(e.svc.SearchMissing(e.ctx)).To(Succeed())
		})

		It("leaves an unreleased or unhydrated album alone", func() {
			_, albums := e.seedArtist("Nirvana",
				albumSeed{
					mbid:      "future",
					title:     "Future",
					monitored: true,
					date: new(
						time.Now().Add(30 * 24 * time.Hour),
					),
					tracks: []string{"A"},
				},
				albumSeed{mbid: "stub", title: "Stub", monitored: true},
			)
			Expect(albums).To(HaveLen(2))

			Expect(e.svc.SearchMissing(e.ctx)).To(Succeed())
		})

		It("waives the cap and the cooldown on a manual run", func() {
			albums := seedAlbums("Capped", "Cooling")
			e.client.Album.UpdateOneID(albums[0].ID).SetGrabFailures(3).ExecX(e.ctx)
			e.client.Album.UpdateOneID(albums[1].ID).
				SetLastSearchAt(time.Now()).
				ExecX(e.ctx)
			e.idx.EXPECT().
				SearchAlbum(mock.Anything, "Nirvana", "Capped", uint16(0)).
				Return(nil, nil).
				Once()
			e.idx.EXPECT().
				SearchAlbum(mock.Anything, "Nirvana", "Cooling", uint16(0)).
				Return(nil, nil).
				Once()

			Expect(e.svc.SearchMissing(manualContext())).To(Succeed())
		})

		It("does nothing without an enabled download client", func() {
			e.setConfig(map[string]any{"download_clients": []map[string]any{}})
			seedAlbums("Nevermind")

			Expect(e.svc.SearchMissing(e.ctx)).To(Succeed())
		})
	})

	Describe("RefreshStale", func() {
		makeArtist := func(mbid string, refreshed *time.Time) *ent.Artist {
			GinkgoHelper()
			c := e.client.Artist.Create().SetMbid(mbid).SetName(mbid)
			if refreshed != nil {
				c = c.SetLastRefreshedAt(*refreshed)
			}
			return c.SaveX(e.ctx)
		}

		details := func(mbid string) *metadata.ArtistDetails {
			return &metadata.ArtistDetails{
				MBID: mbid, Name: mbid,
			}
		}

		It("refreshes only stale artists, at most refreshBatch per run", func() {
			recent := time.Now().Add(-time.Hour)
			for i := range refreshBatch + 2 {
				makeArtist(fmt.Sprintf("stale-%02d", i), nil)
			}
			fresh := makeArtist("fresh", &recent)
			e.provider.EXPECT().GetArtist(mock.Anything, mock.Anything).
				RunAndReturn(func(_ context.Context, mbid string) (*metadata.ArtistDetails, error) {
					return details(mbid), nil
				}).
				Times(refreshBatch)
			start := time.Now()

			Expect(e.svc.RefreshStale(e.ctx)).To(Succeed())

			Expect(e.client.Artist.Query().
				Where(artist.LastRefreshedAtGTE(start)).Count(e.ctx)).
				To(Equal(refreshBatch))
			Expect(e.client.Artist.GetX(e.ctx, fresh.ID).LastRefreshedAt).
				To(HaveValue(BeTemporally("~", recent, time.Second)))
		})

		It("persists a release group new to the artist and hydrates it", func() {
			a := makeArtist("mbid-1", nil)
			e.provider.EXPECT().GetArtist(mock.Anything, "mbid-1").
				Return(&metadata.ArtistDetails{
					MBID: "mbid-1",
					Name: "mbid-1",
					ReleaseGroups: []metadata.ReleaseGroupInfo{
						{
							MBID:  "rg-new",
							Title: "New",
							Type:  metadata.AlbumTypeAlbum,
						},
					},
				}, nil).Once()
			e.provider.EXPECT().GetReleaseGroup(mock.Anything, "rg-new").
				Return(hydrated("rg-new", "A"), nil).Once()
			e.posters.EXPECT().
				Fetch(mock.Anything, "albums", mock.Anything, metadata.CoverArtURL("rg-new")).
				Return(nil).Once()

			Expect(e.svc.RefreshStale(e.ctx)).To(Succeed())
			e.settle(a.ID)

			stored, err := e.store.FindArtistByID(e.ctx, a.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(stored.Edges.Albums).To(HaveLen(1))
			Expect(stored.Edges.Albums[0].Mbid).To(Equal("rg-new"))
			Expect(stored.Edges.Albums[0].Monitored).To(BeTrue())
			Expect(stored.Edges.Albums[0].MetadataFetchedAt).NotTo(BeNil())
			Expect(stored.LastRefreshedAt).NotTo(BeNil())
		})

		It("queues the albums a previous run left unfinished", func() {
			a, albums := e.seedArtist("Nirvana",
				albumSeed{mbid: "rg-1", title: "Nevermind", monitored: true})
			e.provider.EXPECT().GetReleaseGroup(mock.Anything, "rg-1").
				Return(hydrated("rg-1", "A"), nil).Once()
			e.posters.EXPECT().
				Fetch(mock.Anything, "albums", mock.Anything, mock.Anything).
				Return(nil).
				Maybe()
			recent := time.Now().Add(-time.Hour)
			e.client.Artist.UpdateOneID(a.ID).SetLastRefreshedAt(recent).ExecX(e.ctx)

			Expect(e.svc.RefreshStale(e.ctx)).To(Succeed())
			Eventually(e.idle).Should(BeTrue())
			Expect(
				e.client.Album.GetX(e.ctx, albums[0].ID).MetadataFetchedAt,
			).NotTo(BeNil())
		})

		It("skips an artist whose metadata fails and refreshes the rest", func() {
			bad := makeArtist("bad", nil)
			good := makeArtist("good", nil)
			e.provider.EXPECT().GetArtist(mock.Anything, "bad").
				Return(nil, errors.New("musicbrainz 503")).Once()
			e.provider.EXPECT().GetArtist(mock.Anything, "good").
				Return(details("good"), nil).Once()

			Expect(e.svc.RefreshStale(e.ctx)).To(Succeed())

			Expect(e.client.Artist.GetX(e.ctx, bad.ID).LastRefreshedAt).To(BeNil())
			Expect(
				e.client.Artist.GetX(e.ctx, good.ID).LastRefreshedAt,
			).NotTo(BeNil())
		})

		It("honours the 24h interval unless the run is manual", func() {
			recent := time.Now().Add(-time.Hour)
			a := makeArtist("recent", &recent)

			Expect(e.svc.RefreshStale(e.ctx)).To(Succeed())

			e.provider.EXPECT().GetArtist(mock.Anything, "recent").
				Return(details("recent"), nil).Once()
			Expect(e.svc.RefreshStale(manualContext())).To(Succeed())
			Expect(e.client.Artist.GetX(e.ctx, a.ID).LastRefreshedAt).
				To(HaveValue(BeTemporally(">", recent)))
		})
	})
})
