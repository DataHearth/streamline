package rss

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/stretchr/testify/mock"

	"github.com/datahearth/streamline/ent"
	dbmocks "github.com/datahearth/streamline/internal/db/mocks"
	"github.com/datahearth/streamline/internal/download"
	"github.com/datahearth/streamline/internal/indexer"
	"github.com/datahearth/streamline/internal/rss/mocks"
	"github.com/datahearth/streamline/internal/testutil/configtest"
)

func musicConfig(names ...string) map[string]any {
	cfg := indexerConfig(names...)
	cfg["music_quality_default_profile"] = "lossless"
	cfg["music_quality_profiles"] = []map[string]any{{
		"name":    "lossless",
		"formats": []string{"flac-24", "flac"},
		"cutoff":  "flac",
	}}
	return cfg
}

func wantedAlbum() *ent.Album {
	a := &ent.Album{ID: 1, Title: "Album Title", Monitored: true}
	a.Edges.Artist = &ent.Artist{
		ID: 101, Name: "Artist Name", QualityProfile: "lossless",
	}
	return a
}

var _ = Describe("FeedScanner music pass", Label("unit", "rss"), func() {
	var (
		ctx     context.Context
		store   *dbmocks.MockStore
		feeder  *mocks.MockIndexerFeeder
		grabber *mocks.MockDownloader
		albums  *mocks.MockAlbumGrabber
		scanner *FeedScanner
	)

	run := func(wanted []*ent.Album, items ...indexer.SearchResult) {
		GinkgoHelper()
		store.EXPECT().ListWantedMovies(mock.Anything).Return(nil, nil).Once()
		store.EXPECT().ListUpgradeCandidateMovies(mock.Anything).
			Return(nil, nil).Once()
		store.EXPECT().ListWantedAlbums(mock.Anything, uint8(3)).
			Return(wanted, nil).Once()
		feeder.EXPECT().Feed(mock.Anything, "idx").Return(items, nil).Once()
		Expect(scanner.Run(ctx)).To(Succeed())
	}

	expectBookkeeping := func(id uint32) {
		store.EXPECT().ResetAlbumGrabFailures(mock.Anything, id).Return(nil).Once()
		store.EXPECT().SetAlbumLastSearchAt(
			mock.Anything, id, mock.AnythingOfType("time.Time"),
		).Return(nil).Once()
	}

	BeforeEach(func() {
		ctx = context.Background()
		store = dbmocks.NewMockStore(GinkgoT())
		feeder = mocks.NewMockIndexerFeeder(GinkgoT())
		grabber = mocks.NewMockDownloader(GinkgoT())
		albums = mocks.NewMockAlbumGrabber(GinkgoT())
		configtest.Setup(musicConfig("idx"))
		scanner = NewFeedScanner(store, feeder, grabber, albums)
	})

	flacItem := indexer.SearchResult{
		Title:    "Artist Name - Album Title (2020) [FLAC]",
		Category: "3040",
	}

	It("grabs a matching release once and records the success", func() {
		item := flacItem
		albums.EXPECT().GrabAlbumRelease(mock.Anything, uint32(1), item).
			Return(nil).Once()
		expectBookkeeping(1)
		// Same release again (second indexer, repeated item): no second grab.
		run([]*ent.Album{wantedAlbum()}, item, item)
	})

	It("matches case- and punctuation-insensitively", func() {
		albums.EXPECT().GrabAlbumRelease(mock.Anything, uint32(1), mock.Anything).
			Return(nil).Once()
		expectBookkeeping(1)
		run(
			[]*ent.Album{wantedAlbum()},
			indexer.SearchResult{
				Title:    "artist.name - ALBUM.TITLE.2020.FLAC",
				Category: "3040",
			},
		)
	})

	It("skips a format outside the profile", func() {
		run(
			[]*ent.Album{wantedAlbum()},
			indexer.SearchResult{
				Title:    "Artist Name - Album Title (2020) [MP3 320]",
				Category: "3010",
			},
			indexer.SearchResult{
				Title:    "Artist Name - Album Title (2020)",
				Category: "3040",
			},
		)
	})

	It("passes the failure cap to the candidate query", func() {
		run(nil, flacItem)
	})

	It("does not let a recent last_search_at block a feed grab", func() {
		a := wantedAlbum()
		recent := time.Now()
		a.LastSearchAt = &recent
		albums.EXPECT().GrabAlbumRelease(mock.Anything, uint32(1), mock.Anything).
			Return(nil).Once()
		expectBookkeeping(1)
		run([]*ent.Album{a}, flacItem)
	})

	It("skips discographies", func() {
		run(
			[]*ent.Album{wantedAlbum()},
			indexer.SearchResult{
				Title:    "Artist Name - Album Title Discography [FLAC]",
				Category: "3040",
			},
		)
	})

	DescribeTable("never matches items outside the music categories",
		func(category string) {
			run(
				[]*ent.Album{wantedAlbum()},
				indexer.SearchResult{Title: flacItem.Title, Category: category},
			)
		},
		Entry("empty", ""),
		Entry("movies", "2040"),
		Entry("audiobooks", "3030"),
		Entry("ebooks", "7020"),
		Entry("unparsable", "music"),
	)

	It("bumps grab_failures on a release failure", func() {
		albums.EXPECT().GrabAlbumRelease(mock.Anything, uint32(1), mock.Anything).
			Return(fmt.Errorf("add torrent: %w", download.ErrNoWantedFiles)).Once()
		store.EXPECT().IncrementAlbumGrabFailures(mock.Anything, uint32(1)).
			Return(nil).Once()
		run([]*ent.Album{wantedAlbum()}, flacItem)
	})

	It("does not bump grab_failures on a transport failure", func() {
		albums.EXPECT().GrabAlbumRelease(mock.Anything, uint32(1), mock.Anything).
			Return(fmt.Errorf("grab: %w", download.ErrUnreachable)).Once()
		run([]*ent.Album{wantedAlbum()}, flacItem)
	})
})

var _ = DescribeTable("splitCreatorTitle", Label("unit", "rss"),
	func(name, creator, title string, ok bool) {
		c, t, got := splitCreatorTitle(name)
		Expect(got).To(Equal(ok))
		Expect(c).To(Equal(creator))
		Expect(t).To(Equal(title))
	},
	Entry("bracketed", "A - B (2020) [FLAC]", "A", "B", true),
	Entry("dotted", "A.B - C.D.2020.FLAC", "A B", "C D", true),
	Entry("year first token kept", "A - 1989 [FLAC]", "A", "1989", true),
	Entry("no separator", "A.B.C.2020.FLAC", "", "", false),
)
