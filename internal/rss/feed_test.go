package rss

import (
	"context"
	"errors"
	"maps"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/stretchr/testify/mock"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/downloadrecord"
	dbmocks "github.com/datahearth/streamline/internal/db/mocks"
	"github.com/datahearth/streamline/internal/indexer"
	"github.com/datahearth/streamline/internal/rss/mocks"
	"github.com/datahearth/streamline/internal/testutil/configtest"
)

func indexerConfig(names ...string) map[string]any {
	entries := make([]map[string]any, 0, len(names))
	for _, n := range names {
		entries = append(entries, map[string]any{
			"name": n, "host": "idx", "port": 9117, "api_key": "k",
			"protocol": "torznab", "enabled": true,
		})
	}
	// The scanner skips its pass without an enabled download client, so the
	// baseline config carries one; specs that want the skip path override it.
	cfg := defaultRSSConfig()
	cfg["indexers"] = entries
	return cfg
}

const (
	upgradableProfile = "upgradable"
	cappedProfile     = "capped"
	lockedProfile     = "locked"
	hdOnlyProfile     = "hdonly"
)

// upgradeConfig swaps the profile set for four: three differing only in
// upgrade policy — permissive, capped at 100, upgrades off — plus one whose
// band is 1080p..1080p, so a spec can pin exactly which rule stopped a grab.
// hdr is scored alongside remux so a release can outscore a remux file
// without leaving the band.
func upgradeConfig(names ...string) map[string]any {
	profile := func(name string, extra map[string]any) map[string]any {
		p := map[string]any{
			"name":                 name,
			"preferred_resolution": "2160p",
			"min_resolution":       "1080p",
			"upgrade_allowed":      true,
			"formats": []map[string]any{
				{"name": "remux", "score": 200},
				{"name": "hdr", "score": 50},
			},
		}
		maps.Copy(p, extra)
		return p
	}
	cfg := indexerConfig(names...)
	cfg["quality_default_profile"] = upgradableProfile
	cfg["quality_profiles"] = []map[string]any{
		profile(upgradableProfile, nil),
		profile(cappedProfile, map[string]any{"upgrade_until_score": 100}),
		profile(lockedProfile, map[string]any{"upgrade_allowed": false}),
		profile(hdOnlyProfile, map[string]any{"preferred_resolution": "1080p"}),
	}
	return cfg
}

const (
	plainFile = "Dune.2021.1080p.BluRay.x264-GROUP.mkv"
	remuxFile = "Dune.2021.1080p.BluRay.REMUX.x264-GROUP.mkv"
	sdFile    = "Dune.2021.720p.WEB-DL.x264-GROUP.mkv"
	uhdFile   = "Dune.2021.2160p.BluRay.REMUX.HDR.x265-GROUP.mkv"
	// No resolution token and no probe width — nothing says what this is.
	unknownFile = "Dune.2021.BluRay.x264-GROUP.mkv"

	remuxHDRRelease = "Dune.2021.2160p.BluRay.REMUX.HDR.x265-GROUP"
	remuxHDRelease  = "Dune.2021.1080p.BluRay.REMUX.x264-GROUP"
	plainUHDRelease = "Dune.2021.2160p.BluRay.x265-GROUP"
)

func movieWithFile(profile, basename string) *ent.Movie {
	return movieWithFileAt(profile, basename, 1920)
}

func movieWithFileAt(profile, basename string, width uint16) *ent.Movie {
	m := &ent.Movie{
		ID: 9, TmdbID: 42, Title: "Dune", Year: 2021,
		QualityProfile: profile,
	}
	m.Edges.MediaFiles = []*ent.MediaFile{{
		Path:       "/movies/Dune (2021)/" + basename,
		Size:       8_000_000_000,
		Width:      width,
		VideoCodec: "h264",
	}}
	return m
}

var _ = Describe("FeedScanner.Run", Label("unit", "rss"), func() {
	var (
		ctx     context.Context
		store   *dbmocks.MockStore
		feeder  *mocks.MockIndexerFeeder
		grabber *mocks.MockDownloader
		scanner *FeedScanner
	)

	newScanner := func() {
		scanner = NewFeedScanner(store, feeder, grabber)
	}

	noUpgrades := func() {
		GinkgoHelper()
		store.EXPECT().ListUpgradeCandidateMovies(mock.Anything).
			Return(nil, nil).Once()
	}

	upgradeCandidates := func(movies ...*ent.Movie) {
		GinkgoHelper()
		store.EXPECT().ListWantedMovies(mock.Anything).Return(nil, nil).Once()
		store.EXPECT().ListUpgradeCandidateMovies(mock.Anything).
			Return(movies, nil).Once()
	}

	BeforeEach(func() {
		ctx = context.Background()
		store = dbmocks.NewMockStore(GinkgoT())
		feeder = mocks.NewMockIndexerFeeder(GinkgoT())
		grabber = mocks.NewMockDownloader(GinkgoT())
	})

	It("noops when no indexers are configured", func() {
		configtest.Setup()
		newScanner()
		Expect(scanner.Run(ctx)).To(Succeed())
	})

	It("skips the pass when no download client is enabled", func() {
		cfg := indexerConfig("a")
		cfg["download_clients"] = []map[string]any{}
		configtest.Setup(cfg)
		newScanner()
		store.EXPECT().ListWantedMovies(mock.Anything).
			Return([]*ent.Movie{{ID: 7, TmdbID: 42, Title: "Dune", Year: 2021}}, nil).
			Once()
		// No Feed call: the whole point is not to pull feeds we can't act on.
		// No upgrade-candidate call either — that query only pays off for a
		// pass that can still grab.
		Expect(scanner.Run(ctx)).To(Succeed())
	})

	It("continues to the next indexer on per-indexer error", func() {
		configtest.Setup(indexerConfig("a", "b"))
		newScanner()
		store.EXPECT().ListWantedMovies(mock.Anything).Return(nil, nil).Once()
		noUpgrades()
		feeder.EXPECT().Feed(mock.Anything, "a").
			Return(nil, errors.New("boom")).Once()
		feeder.EXPECT().Feed(mock.Anything, "b").Return(nil, nil).Once()
		Expect(scanner.Run(ctx)).To(Succeed())
	})

	It("grabs on title+year match passing the quality filter", func() {
		configtest.Setup(indexerConfig("a"))
		newScanner()
		wanted := &ent.Movie{ID: 7, TmdbID: 42, Title: "Dune", Year: 2021}
		store.EXPECT().ListWantedMovies(mock.Anything).
			Return([]*ent.Movie{wanted}, nil).Once()
		noUpgrades()
		feeder.EXPECT().Feed(mock.Anything, "a").
			Return([]indexer.SearchResult{
				{Title: "Dune.2021.1080p.BluRay.x264-GROUP"},
			}, nil).Once()
		grabber.EXPECT().Grab(mock.Anything, mock.Anything, uint32(7)).
			Return(&ent.DownloadRecord{}, nil).Once()
		store.EXPECT().ResetMovieGrabFailures(mock.Anything, uint32(7)).
			Return(nil).Once()
		store.EXPECT().SetMovieLastSearchAt(
			mock.Anything, uint32(7), mock.AnythingOfType("time.Time"),
		).Return(nil).Once()
		Expect(scanner.Run(ctx)).To(Succeed())
	})

	It("rejects items that fail the quality filter without grabbing", func() {
		configtest.Setup(indexerConfig("a"))
		newScanner()
		wanted := &ent.Movie{ID: 7, TmdbID: 42, Title: "Dune", Year: 2021}
		store.EXPECT().ListWantedMovies(mock.Anything).
			Return([]*ent.Movie{wanted}, nil).Once()
		noUpgrades()
		feeder.EXPECT().Feed(mock.Anything, "a").
			Return([]indexer.SearchResult{
				{Title: "Dune.2021.480p.CAM.XViD-LOL"},
			}, nil).Once()
		// No grabber/store EXPECTs — quality reject means no further calls.
		Expect(scanner.Run(ctx)).To(Succeed())
	})

	It("filters against the movie's own profile, not the default", func() {
		configtest.Setup(defaultRSSConfig(), indexerConfig("a"))
		newScanner()
		wanted := &ent.Movie{
			ID: 7, TmdbID: 42, Title: "Dune", Year: 2021,
			QualityProfile: uhdProfile,
		}
		store.EXPECT().ListWantedMovies(mock.Anything).
			Return([]*ent.Movie{wanted}, nil).Once()
		noUpgrades()
		// 1080p clears the default profile but not the movie's 2160p floor.
		feeder.EXPECT().Feed(mock.Anything, "a").
			Return([]indexer.SearchResult{
				{Title: "Dune.2021.1080p.BluRay.x264-GROUP"},
			}, nil).Once()
		Expect(scanner.Run(ctx)).To(Succeed())
	})

	It("bumps grab_failures when grab errors", func() {
		configtest.Setup(indexerConfig("a"))
		newScanner()
		wanted := &ent.Movie{ID: 7, TmdbID: 42, Title: "Dune", Year: 2021}
		store.EXPECT().ListWantedMovies(mock.Anything).
			Return([]*ent.Movie{wanted}, nil).Once()
		noUpgrades()
		feeder.EXPECT().Feed(mock.Anything, "a").
			Return([]indexer.SearchResult{
				{Title: "Dune.2021.1080p.BluRay.x264-GROUP"},
			}, nil).Once()
		grabber.EXPECT().Grab(mock.Anything, mock.Anything, uint32(7)).
			Return(nil, errors.New("client offline")).Once()
		store.EXPECT().IncrementMovieGrabFailures(mock.Anything, uint32(7)).
			Return(nil).Once()
		Expect(scanner.Run(ctx)).To(Succeed())
	})

	It("skips items whose titles have no parseable year", func() {
		configtest.Setup(indexerConfig("a"))
		newScanner()
		wanted := &ent.Movie{ID: 7, TmdbID: 42, Title: "Dune", Year: 2021}
		store.EXPECT().ListWantedMovies(mock.Anything).
			Return([]*ent.Movie{wanted}, nil).Once()
		noUpgrades()
		feeder.EXPECT().Feed(mock.Anything, "a").
			Return([]indexer.SearchResult{
				{Title: "Random Pack Without Year"},
			}, nil).Once()
		Expect(scanner.Run(ctx)).To(Succeed())
	})

	It("only grabs the first match when two indexers return the same movie", func() {
		configtest.Setup(indexerConfig("a", "b"))
		newScanner()
		wanted := &ent.Movie{ID: 7, TmdbID: 42, Title: "Dune", Year: 2021}
		store.EXPECT().ListWantedMovies(mock.Anything).
			Return([]*ent.Movie{wanted}, nil).Once()
		noUpgrades()
		feeder.EXPECT().Feed(mock.Anything, "a").
			Return([]indexer.SearchResult{
				{Title: "Dune.2021.1080p.BluRay.x264-GROUP"},
			}, nil).Once()
		feeder.EXPECT().Feed(mock.Anything, "b").
			Return([]indexer.SearchResult{
				{Title: "Dune.2021.1080p.WEB-DL.x265-OTHER"},
			}, nil).Once()
		grabber.EXPECT().Grab(mock.Anything, mock.Anything, uint32(7)).
			Return(&ent.DownloadRecord{}, nil).Once()
		store.EXPECT().ResetMovieGrabFailures(mock.Anything, uint32(7)).
			Return(nil).Once()
		store.EXPECT().SetMovieLastSearchAt(
			mock.Anything, uint32(7), mock.AnythingOfType("time.Time"),
		).Return(nil).Once()
		Expect(scanner.Run(ctx)).To(Succeed())
	})

	It("grabs an upgrade and flags the record replace_existing", func() {
		configtest.Setup(upgradeConfig("a"))
		newScanner()
		upgradeCandidates(movieWithFile(upgradableProfile, plainFile))
		feeder.EXPECT().Feed(mock.Anything, "a").
			Return([]indexer.SearchResult{{Title: remuxHDRRelease}}, nil).Once()
		grabber.EXPECT().Grab(mock.Anything, mock.Anything, uint32(9)).
			Return(&ent.DownloadRecord{ID: 55}, nil).Once()
		store.EXPECT().
			SetDownloadRecordReplaceMode(
				mock.Anything, uint32(55), downloadrecord.ReplaceModeAll,
			).
			Return(nil).Once()
		store.EXPECT().ResetMovieGrabFailures(mock.Anything, uint32(9)).
			Return(nil).Once()
		store.EXPECT().SetMovieLastSearchAt(
			mock.Anything, uint32(9), mock.AnythingOfType("time.Time"),
		).Return(nil).Once()
		Expect(scanner.Run(ctx)).To(Succeed())
	})

	It("leaves the file alone when the release scores no higher", func() {
		configtest.Setup(upgradeConfig("a"))
		newScanner()
		upgradeCandidates(movieWithFile(upgradableProfile, remuxFile))
		// Inside the band and accepted, but a plain 2160p scores 0 against the
		// file's remux 200.
		feeder.EXPECT().Feed(mock.Anything, "a").
			Return([]indexer.SearchResult{{Title: plainUHDRelease}}, nil).Once()
		Expect(scanner.Run(ctx)).To(Succeed())
	})

	It("stops upgrading once the file is at the profile's cap", func() {
		configtest.Setup(upgradeConfig("a"))
		newScanner()
		upgradeCandidates(movieWithFile(cappedProfile, remuxFile))
		// The release does outscore the file (250 > 200); only
		// upgrade_until_score=100 against the file's 200 stops the grab.
		feeder.EXPECT().Feed(mock.Anything, "a").
			Return([]indexer.SearchResult{{Title: remuxHDRRelease}}, nil).Once()
		Expect(scanner.Run(ctx)).To(Succeed())
	})

	It("never upgrades under a profile with upgrade_allowed false", func() {
		configtest.Setup(upgradeConfig("a"))
		newScanner()
		upgradeCandidates(movieWithFile(lockedProfile, plainFile))
		feeder.EXPECT().Feed(mock.Anything, "a").
			Return([]indexer.SearchResult{{Title: remuxHDRRelease}}, nil).Once()
		Expect(scanner.Run(ctx)).To(Succeed())
	})

	It("only grabs one upgrade when two indexers carry the release", func() {
		configtest.Setup(upgradeConfig("a", "b"))
		newScanner()
		upgradeCandidates(movieWithFile(upgradableProfile, plainFile))
		feeder.EXPECT().Feed(mock.Anything, "a").
			Return([]indexer.SearchResult{{Title: remuxHDRRelease}}, nil).Once()
		feeder.EXPECT().Feed(mock.Anything, "b").
			Return([]indexer.SearchResult{{Title: remuxHDRRelease}}, nil).Once()
		grabber.EXPECT().Grab(mock.Anything, mock.Anything, uint32(9)).
			Return(&ent.DownloadRecord{ID: 55}, nil).Once()
		store.EXPECT().
			SetDownloadRecordReplaceMode(
				mock.Anything, uint32(55), downloadrecord.ReplaceModeAll,
			).
			Return(nil).Once()
		store.EXPECT().ResetMovieGrabFailures(mock.Anything, uint32(9)).
			Return(nil).Once()
		store.EXPECT().SetMovieLastSearchAt(
			mock.Anything, uint32(9), mock.AnythingOfType("time.Time"),
		).Return(nil).Once()
		Expect(scanner.Run(ctx)).To(Succeed())
	})

	It("grabs a wanted movie once when it is also an upgrade candidate", func() {
		configtest.Setup(upgradeConfig("a"))
		newScanner()
		m := movieWithFile(upgradableProfile, plainFile)
		store.EXPECT().ListWantedMovies(mock.Anything).
			Return([]*ent.Movie{m}, nil).Once()
		store.EXPECT().ListUpgradeCandidateMovies(mock.Anything).
			Return([]*ent.Movie{m}, nil).Once()
		feeder.EXPECT().Feed(mock.Anything, "a").
			Return([]indexer.SearchResult{{Title: remuxHDRRelease}}, nil).Once()
		// One grab, and no replace_existing: the wanted path owns the movie
		// this tick and the shared grabbed map keeps the upgrade path off it.
		grabber.EXPECT().Grab(mock.Anything, mock.Anything, uint32(9)).
			Return(&ent.DownloadRecord{ID: 55}, nil).Once()
		store.EXPECT().ResetMovieGrabFailures(mock.Anything, uint32(9)).
			Return(nil).Once()
		store.EXPECT().SetMovieLastSearchAt(
			mock.Anything, uint32(9), mock.AnythingOfType("time.Time"),
		).Return(nil).Once()
		Expect(scanner.Run(ctx)).To(Succeed())
	})

	It("refuses to replace a file above the profile band", func() {
		configtest.Setup(upgradeConfig("a"))
		newScanner()
		upgradeCandidates(movieWithFileAt(hdOnlyProfile, uhdFile, 3840))
		// The release is in-band and scores 200 against the 2160p file's 0 —
		// but that 0 is the band rejecting the file, not a verdict on it.
		feeder.EXPECT().Feed(mock.Anything, "a").
			Return([]indexer.SearchResult{{Title: remuxHDRelease}}, nil).Once()
		Expect(scanner.Run(ctx)).To(Succeed())
	})

	It("refuses to replace a file whose resolution is unknown", func() {
		configtest.Setup(upgradeConfig("a"))
		newScanner()
		upgradeCandidates(movieWithFileAt(upgradableProfile, unknownFile, 0))
		feeder.EXPECT().Feed(mock.Anything, "a").
			Return([]indexer.SearchResult{{Title: remuxHDRRelease}}, nil).Once()
		Expect(scanner.Run(ctx)).To(Succeed())
	})

	It("upgrades a file that sits below the profile band", func() {
		configtest.Setup(upgradeConfig("a"))
		newScanner()
		upgradeCandidates(movieWithFileAt(upgradableProfile, sdFile, 1280))
		feeder.EXPECT().Feed(mock.Anything, "a").
			Return([]indexer.SearchResult{{Title: remuxHDRRelease}}, nil).Once()
		grabber.EXPECT().Grab(mock.Anything, mock.Anything, uint32(9)).
			Return(&ent.DownloadRecord{ID: 55}, nil).Once()
		store.EXPECT().
			SetDownloadRecordReplaceMode(
				mock.Anything, uint32(55), downloadrecord.ReplaceModeAll,
			).
			Return(nil).Once()
		store.EXPECT().ResetMovieGrabFailures(mock.Anything, uint32(9)).
			Return(nil).Once()
		store.EXPECT().SetMovieLastSearchAt(
			mock.Anything, uint32(9), mock.AnythingOfType("time.Time"),
		).Return(nil).Once()
		Expect(scanner.Run(ctx)).To(Succeed())
	})
})
