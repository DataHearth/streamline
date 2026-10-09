package music

import (
	"context"
	"maps"
	"os"
	"path/filepath"
	"strconv"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/stretchr/testify/mock"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/album"
	"github.com/datahearth/streamline/internal/db"
	mockdownload "github.com/datahearth/streamline/internal/download/mocks"
	mockindexer "github.com/datahearth/streamline/internal/indexer/mocks"
	"github.com/datahearth/streamline/internal/metadata"
	mockmeta "github.com/datahearth/streamline/internal/metadata/mocks"
	mockposters "github.com/datahearth/streamline/internal/posters/mocks"
	"github.com/datahearth/streamline/internal/testutil/configtest"
)

// env is the shared harness of the service specs: a real in-memory database
// behind mocked providers, indexers, download manager and poster cache.
type env struct {
	ctx       context.Context
	client    *ent.Client
	store     *db.DB
	provider  *mockmeta.MockMusicProvider
	overviews *mockmeta.MockOverviewProvider
	photos    *mockmeta.MockArtistPhotoProvider
	posters   *mockposters.MockManager
	idx       *mockindexer.MockManager
	dl        *mockdownload.MockDownloader
	svc       *Service
	cacheArt  func(id uint32)
}

// newEnv builds the harness; withEnrichment wires the Wikipedia and Deezer
// photo steps, which stay off otherwise.
func newEnv(withEnrichment bool) *env {
	GinkgoHelper()
	e := &env{ctx: context.Background()}
	var err error
	e.client, err = db.Open(e.ctx, ":memory:")
	Expect(err).NotTo(HaveOccurred())
	DeferCleanup(func() { e.client.Close() })
	e.store = db.New(e.client)
	e.provider = mockmeta.NewMockMusicProvider(GinkgoT())
	e.posters = mockposters.NewMockManager(GinkgoT())
	e.idx = mockindexer.NewMockManager(GinkgoT())
	e.dl = mockdownload.NewMockDownloader(GinkgoT())
	stubCoverPaths(e.posters)
	var (
		ov metadata.OverviewProvider
		ph metadata.ArtistPhotoProvider
	)
	if withEnrichment {
		e.cacheArt = stubKindPaths(e.posters, "artists")
		e.overviews = mockmeta.NewMockOverviewProvider(GinkgoT())
		e.photos = mockmeta.NewMockArtistPhotoProvider(GinkgoT())
		ov, ph = e.overviews, e.photos
	}
	e.svc = NewService(e.store, e.provider, e.posters, nil, e.idx, e.dl, ov, ph)
	e.setConfig(map[string]any{})
	return e
}

func (e *env) setConfig(extra map[string]any) {
	GinkgoHelper()
	cfg := map[string]any{
		"library": map[string]any{
			"music_path":        GinkgoT().TempDir(),
			"no_match_cooldown": "6h",
			"max_grab_failures": 3,
		},
		"download_clients": []map[string]any{{
			"name":        "qbit",
			"client_type": "qbittorrent",
			"host":        "127.0.0.1",
			"port":        8080,
			"auth_method": "password",
			"enabled":     true,
		}},
		"music_quality_profiles": []map[string]any{
			{
				"name":      "lossless",
				"tiers":     []string{"hires", "lossless", "high"},
				"preferred": "lossless",
			},
			{
				"name":      "any",
				"tiers":     []string{"lossless", "high", "standard", "low"},
				"preferred": "lossless",
			},
		},
		"music_quality_default_profile": "lossless",
	}
	maps.Copy(cfg, extra)
	configtest.Setup(cfg)
}

// albumSeed is a stub release group for seedArtist.
type albumSeed struct {
	mbid, title string
	date        *time.Time
	monitored   bool
	// tracks, when set, hydrates the album with these titles.
	tracks []string
}

// seedArtist adds an artist with its albums through the store, hydrating the
// albums that name tracks, and returns the albums in seed order.
func (e *env) seedArtist(
	name string,
	seeds ...albumSeed,
) (*ent.Artist, []*ent.Album) {
	GinkgoHelper()
	albums := make([]db.AlbumSeed, len(seeds))
	for i, s := range seeds {
		albums[i] = db.AlbumSeed{
			MBID: s.mbid, Title: s.title, Type: "album",
			ReleaseDate: s.date, Monitored: s.monitored,
		}
	}
	a, err := e.store.CreateArtist(e.ctx, db.CreateArtistParams{
		MBID: "mbid-" + name, Name: name, SortName: name,
		QualityProfile: "lossless", Albums: albums,
	})
	Expect(err).NotTo(HaveOccurred())
	out := make([]*ent.Album, len(seeds))
	for i, s := range seeds {
		out[i] = e.client.Album.Query().Where(album.MbidEQ(s.mbid)).OnlyX(e.ctx)
		if s.tracks == nil {
			continue
		}
		tracks := make([]db.TrackSeed, len(s.tracks))
		for j, t := range s.tracks {
			tracks[j] = db.TrackSeed{
				MBID: s.mbid + "-t" + t, Title: t, Disc: 1, Position: uint16(j + 1),
			}
		}
		Expect(e.store.SetAlbumHydration(
			e.ctx, out[i].ID, db.HydrationParams{Tracks: tracks}, time.Now(),
		)).To(Succeed())
		out[i] = e.client.Album.GetX(e.ctx, out[i].ID)
	}
	return a, out
}

// addFile gives the track a library file of the given quality tier.
func (e *env) addFile(t *ent.Track, path, quality string) *ent.MediaFile {
	GinkgoHelper()
	return e.client.MediaFile.Create().
		SetPath(path).SetSize(1000).SetQuality(quality).SetTrackID(t.ID).
		SaveX(e.ctx)
}

// hydrated is an album's release call as MusicBrainz would answer it.
func hydrated(rg string, titles ...string) *metadata.ReleaseGroupDetails {
	d := &metadata.ReleaseGroupDetails{
		MBID:            rg,
		ReleaseMBID:     "rel-" + rg,
		Barcode:         "0720642442524",
		Label:           "DGC",
		CreditsComplete: true,
	}
	for i, t := range titles {
		d.Tracks = append(d.Tracks, metadata.TrackInfo{
			MBID: "rec-" + t, Title: t, Disc: 1, Position: uint16(i + 1),
		})
	}
	return d
}

// idle reports that the hydration worker has nothing queued or running.
func (e *env) idle() bool {
	e.svc.hydrate.mu.Lock()
	defer e.svc.hydrate.mu.Unlock()
	return !e.svc.hydrate.running && len(e.svc.hydrate.queued) == 0
}

// settle waits for the add's background work: the hydration worker and the
// overview and photo step, which stamps the artist when it ends.
func (e *env) settle(artistID uint32) {
	GinkgoHelper()
	Eventually(e.idle).Should(BeTrue())
	Eventually(func() bool {
		return e.client.Artist.GetX(e.ctx, artistID).DetailsFetchedAt != nil
	}).Should(BeTrue())
}

// stubKindPaths points the mock's Path for a poster kind at a per-spec
// directory and returns a function that marks an id's poster as cached.
func stubKindPaths(p *mockposters.MockManager, kind string) func(id uint32) {
	GinkgoHelper()
	dir := GinkgoT().TempDir()
	path := func(id uint32) string {
		return filepath.Join(dir, strconv.FormatUint(uint64(id), 10))
	}
	p.EXPECT().Path(kind, mock.Anything).
		RunAndReturn(func(_ string, id uint32) string { return path(id) }).
		Maybe()
	return func(id uint32) {
		GinkgoHelper()
		Expect(os.WriteFile(path(id), []byte("cached"), 0o600)).To(Succeed())
	}
}

// newArtistParams is a bare Nirvana linked to its Wikidata item.
func newArtistParams() db.CreateArtistParams {
	return db.CreateArtistParams{
		MBID: "mbid-n", Name: "Nirvana", SortName: "Nirvana", WikidataID: "Q11649",
	}
}
