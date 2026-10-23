package restapi

import (
	"context"
	"errors"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/stretchr/testify/mock"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/internal/indexer"
	"github.com/datahearth/streamline/internal/testutil/configtest"

	dbmocks "github.com/datahearth/streamline/internal/db/mocks"
)

var _ = Describe("toSearchResults", Label("unit", "server"), func() {
	const (
		hashA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
		hashB = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	)

	var (
		ctx   context.Context
		store *dbmocks.MockStore
		srv   *Server
	)

	BeforeEach(func() {
		ctx = context.Background()
		store = dbmocks.NewMockStore(GinkgoT())
		srv = &Server{store: store}
		configtest.Setup(map[string]any{
			"indexers": []map[string]any{
				{
					"name": "public-tz", "host": "h", "port": 9117,
					"protocol": "torznab", "api_key": "k",
				},
				{
					"name": "private-tz", "host": "h", "port": 9117,
					"protocol": "torznab", "api_key": "k", "private": true,
				},
				{
					"name": "prowlarr", "host": "h", "port": 9696,
					"protocol": "prowlarr", "api_key": "k", "private": true,
				},
			},
		})
	})

	byTitle := func(items []SearchResult) map[string]SearchResult {
		GinkgoHelper()
		out := make(map[string]SearchResult, len(items))
		for _, it := range items {
			out[it.Title] = it
		}
		return out
	}

	Describe("indexer_private", func() {
		It(
			"stamps the configured entry's setting, absent when unconfigured",
			func() {
				store.EXPECT().
					ListReleaseGrabs(mock.Anything, uint32(5), uint32(0),
						mock.Anything, mock.Anything).
					Return(nil, nil).Once()

				items := byTitle(srv.toSearchResults(ctx, []indexer.SearchResult{
					{
						Title:             "private",
						Indexer:           "private-tz",
						ConfiguredIndexer: "private-tz",
					},
					{
						Title:             "public",
						Indexer:           "public-tz",
						ConfiguredIndexer: "public-tz",
					},
					{
						Title:             "behind-prowlarr",
						Indexer:           "SomeTracker",
						ConfiguredIndexer: "prowlarr",
					},
					{Title: "removed", Indexer: "gone", ConfiguredIndexer: "gone"},
				}, 5, 0))

				Expect(items["private"].IndexerPrivate).To(HaveValue(BeTrue()))
				Expect(items["public"].IndexerPrivate).To(HaveValue(BeFalse()))
				Expect(
					items["behind-prowlarr"].IndexerPrivate,
				).To(HaveValue(BeTrue()))
				Expect(
					items["behind-prowlarr"].Indexer,
				).To(HaveValue(Equal("SomeTracker")))
				Expect(items["removed"].IndexerPrivate).To(BeNil())
			},
		)
	})

	Describe("previously_grabbed_at", func() {
		older := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
		newer := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)

		It(
			"asks for the scope's records by every title and every known hash",
			func() {
				store.EXPECT().
					ListReleaseGrabs(mock.Anything, uint32(0), uint32(3),
						[]string{hashA}, []string{"One", "Two"}).
					Return(nil, nil).Once()

				srv.toSearchResults(ctx, []indexer.SearchResult{
					{Title: "One", InfoHash: hashA},
					{Title: "Two"},
				}, 0, 3)
			},
		)

		It("matches a hashless release on its title, ignoring case", func() {
			store.EXPECT().
				ListReleaseGrabs(mock.Anything, uint32(5), uint32(0),
					mock.Anything, mock.Anything).
				Return([]*ent.DownloadRecord{
					{
						Title:       "fight.club.1999.1080p",
						TorrentHash: hashA,
						CreateTime:  older,
					},
					{
						Title:       "Fight.Club.1999.1080p",
						TorrentHash: hashB,
						CreateTime:  newer,
					},
				}, nil).Once()

			items := srv.toSearchResults(ctx, []indexer.SearchResult{
				{Title: "Fight.Club.1999.1080p"},
			}, 5, 0)

			Expect(items[0].PreviouslyGrabbedAt).To(HaveValue(Equal(newer)))
		})

		It(
			"matches a hashed release on its hash, whatever the record's title",
			func() {
				store.EXPECT().
					ListReleaseGrabs(mock.Anything, uint32(5), uint32(0),
						mock.Anything, mock.Anything).
					Return([]*ent.DownloadRecord{
						{
							Title:       "renamed by the tracker",
							TorrentHash: hashA,
							CreateTime:  older,
						},
					}, nil).Once()

				items := srv.toSearchResults(ctx, []indexer.SearchResult{
					{Title: "Fight.Club.1999.1080p", InfoHash: hashA},
				}, 5, 0)

				Expect(items[0].PreviouslyGrabbedAt).To(HaveValue(Equal(older)))
			},
		)

		It("does not match a same-titled record holding a different hash", func() {
			store.EXPECT().
				ListReleaseGrabs(mock.Anything, uint32(5), uint32(0),
					mock.Anything, mock.Anything).
				Return([]*ent.DownloadRecord{
					{
						Title:       "Fight.Club.1999.1080p",
						TorrentHash: hashB,
						CreateTime:  older,
					},
				}, nil).Once()

			items := srv.toSearchResults(ctx, []indexer.SearchResult{
				{Title: "Fight.Club.1999.1080p", InfoHash: hashA},
			}, 5, 0)

			Expect(items[0].PreviouslyGrabbedAt).To(BeNil())
		})

		It(
			"falls back to the title for a record that never learned its hash",
			func() {
				store.EXPECT().
					ListReleaseGrabs(mock.Anything, uint32(5), uint32(0),
						mock.Anything, mock.Anything).
					Return([]*ent.DownloadRecord{
						{Title: "Fight.Club.1999.1080p", CreateTime: older},
					}, nil).Once()

				items := srv.toSearchResults(ctx, []indexer.SearchResult{
					{Title: "Fight.Club.1999.1080p", InfoHash: hashA},
				}, 5, 0)

				Expect(items[0].PreviouslyGrabbedAt).To(HaveValue(Equal(older)))
			},
		)

		It("is absent for a release with no matching record", func() {
			store.EXPECT().
				ListReleaseGrabs(mock.Anything, uint32(5), uint32(0),
					mock.Anything, mock.Anything).
				Return([]*ent.DownloadRecord{
					{
						Title:       "Fight.Club.1999.2160p",
						TorrentHash: hashB,
						CreateTime:  older,
					},
				}, nil).Once()

			items := srv.toSearchResults(ctx, []indexer.SearchResult{
				{Title: "Fight.Club.1999.1080p", InfoHash: hashA},
				{Title: "Fight.Club.1999.720p"},
			}, 5, 0)

			Expect(items[0].PreviouslyGrabbedAt).To(BeNil())
			Expect(items[1].PreviouslyGrabbedAt).To(BeNil())
		})

		It("leaves every result unmarked when the history lookup fails", func() {
			store.EXPECT().
				ListReleaseGrabs(mock.Anything, uint32(5), uint32(0),
					mock.Anything, mock.Anything).
				Return(nil, errors.New("database is locked")).Once()

			items := srv.toSearchResults(ctx, []indexer.SearchResult{
				{Title: "Fight.Club.1999.1080p"},
			}, 5, 0)

			Expect(items).To(HaveLen(1))
			Expect(items[0].PreviouslyGrabbedAt).To(BeNil())
		})

		It("skips the lookup for an empty result set", func() {
			Expect(srv.toSearchResults(ctx, nil, 5, 0)).To(BeEmpty())
		})
	})
})
