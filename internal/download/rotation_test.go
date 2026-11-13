package download

import (
	"context"
	"errors"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stretchr/testify/mock"

	"github.com/datahearth/streamline/internal/config"
	dbmocks "github.com/datahearth/streamline/internal/db/mocks"
)

// fullClient answers AddTorrent from addErrs in order, then with "new".
type fullClient struct {
	seedReapClient

	addErrs []error
	adds    int
	listed  int
}

func (c *fullClient) AddTorrent(context.Context, TorrentSource) (string, error) {
	c.adds++
	if c.adds <= len(c.addErrs) {
		return "", c.addErrs[c.adds-1]
	}
	return "new", nil
}

func (c *fullClient) ListTorrents(ctx context.Context) ([]Torrent, error) {
	c.listed++
	return c.seedReapClient.ListTorrents(ctx)
}

var _ = Describe("addRotating", Label("unit", "downloads"), func() {
	var (
		ctx    context.Context
		store  *dbmocks.MockStore
		client *fullClient
		mgr    *download
	)
	builtin := config.DownloadClientEntry{Name: "embedded", ClientType: "builtin"}
	old := time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC)

	BeforeEach(func() {
		ctx = context.Background()
		store = dbmocks.NewMockStore(GinkgoT())
		client = &fullClient{addErrs: []error{ErrClientFull}}
		client.torrents = []Torrent{
			{Hash: "old"}, {Hash: "recent"}, {Hash: "downloading"},
		}
		mgr = New(store, client).(*download)
	})

	evictable := func() {
		GinkgoHelper()
		store.EXPECT().
			ImportedTorrentTimes(
				mock.Anything, []string{"old", "recent", "downloading"},
			).
			Return(map[string]time.Time{
				"old": old, "recent": old.Add(72 * time.Hour),
			}, nil).
			Once()
	}

	It("evicts the victim with its files and adds the torrent", func() {
		evictable()

		hash, err := mgr.addRotating(ctx, builtin, client, TorrentSource{})
		Expect(err).NotTo(HaveOccurred())
		Expect(hash).To(Equal("new"))
		Expect(client.removed).To(Equal([]string{"old"}))
		Expect(client.deleted).To(Equal([]bool{true}))
	})

	It("returns ErrClientFull when nothing is evictable", func() {
		store.EXPECT().
			ImportedTorrentTimes(mock.Anything, mock.Anything).
			Return(map[string]time.Time{}, nil).
			Once()

		_, err := mgr.addRotating(ctx, builtin, client, TorrentSource{})
		Expect(err).To(MatchError(ErrClientFull))
		Expect(client.removed).To(BeEmpty())
		Expect(client.adds).To(Equal(1))
	})

	It("returns ErrClientFull when the lookup fails", func() {
		store.EXPECT().
			ImportedTorrentTimes(mock.Anything, mock.Anything).
			Return(nil, errors.New("db boom")).
			Once()

		_, err := mgr.addRotating(ctx, builtin, client, TorrentSource{})
		Expect(err).To(MatchError(ErrClientFull))
		Expect(client.adds).To(Equal(1))
	})

	It("returns ErrClientFull when the removal fails", func() {
		evictable()
		client.failHash = "old"

		_, err := mgr.addRotating(ctx, builtin, client, TorrentSource{})
		Expect(err).To(MatchError(ErrClientFull))
		Expect(client.adds).To(Equal(1))
	})

	It("returns a second refusal after one eviction", func() {
		evictable()
		client.addErrs = []error{ErrClientFull, ErrClientFull}

		_, err := mgr.addRotating(ctx, builtin, client, TorrentSource{})
		Expect(err).To(MatchError(ErrClientFull))
		Expect(client.removed).To(HaveLen(1))
		Expect(client.adds).To(Equal(2))
	})

	It("does not rotate a client that is not the builtin one", func() {
		qbit := config.DownloadClientEntry{Name: "qb", ClientType: "qbittorrent"}

		_, err := mgr.addRotating(ctx, qbit, client, TorrentSource{})
		Expect(err).To(MatchError(ErrClientFull))
		Expect(client.listed).To(BeZero())
	})

	It("passes any other add error through untouched", func() {
		client.addErrs = []error{ErrTorrentAlreadyExists}

		_, err := mgr.addRotating(ctx, builtin, client, TorrentSource{})
		Expect(err).To(MatchError(ErrTorrentAlreadyExists))
		Expect(client.listed).To(BeZero())
	})
})

var _ = Describe("rotationVictim", Label("unit", "downloads"), func() {
	day1 := time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC)
	day2 := day1.Add(24 * time.Hour)

	It("picks the torrent imported on the earliest day", func() {
		got, ok := rotationVictim(
			[]Torrent{{Hash: "new", Ratio: 5}, {Hash: "old", Ratio: 0.1}},
			map[string]time.Time{"new": day2, "old": day1},
		)
		Expect(ok).To(BeTrue())
		Expect(got.Hash).To(Equal("old"))
	})

	It("picks the higher ratio among same-day imports", func() {
		got, ok := rotationVictim(
			[]Torrent{{Hash: "low", Ratio: 0.4}, {Hash: "high", Ratio: 2.1}},
			map[string]time.Time{"low": day1, "high": day1.Add(6 * time.Hour)},
		)
		Expect(ok).To(BeTrue())
		Expect(got.Hash).To(Equal("high"))
	})

	It("goes by day across midnight whatever the ratios", func() {
		before := time.Date(2026, 3, 1, 23, 59, 0, 0, time.UTC)
		after := time.Date(2026, 3, 2, 0, 1, 0, 0, time.UTC)
		got, ok := rotationVictim(
			[]Torrent{{Hash: "after", Ratio: 9}, {Hash: "before", Ratio: 0.1}},
			map[string]time.Time{"after": after, "before": before},
		)
		Expect(ok).To(BeTrue())
		Expect(got.Hash).To(Equal("before"))
	})

	It("treats a zero import time as the oldest", func() {
		got, ok := rotationVictim(
			[]Torrent{{Hash: "dated"}, {Hash: "undated"}},
			map[string]time.Time{"dated": day1, "undated": {}},
		)
		Expect(ok).To(BeTrue())
		Expect(got.Hash).To(Equal("undated"))
	})

	It("ignores torrents with no import time entry", func() {
		_, ok := rotationVictim(
			[]Torrent{{Hash: "downloading"}},
			map[string]time.Time{"elsewhere": day1},
		)
		Expect(ok).To(BeFalse())
	})

	It("reports no victim for an empty listing", func() {
		_, ok := rotationVictim(nil, map[string]time.Time{"a": day1})
		Expect(ok).To(BeFalse())
	})
})
