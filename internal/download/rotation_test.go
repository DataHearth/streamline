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
	"github.com/datahearth/streamline/internal/testutil/configtest"
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

// limitedClient is a fullClient that names its own torrent limit, as the
// builtin engine does.
type limitedClient struct {
	fullClient

	limit int
}

func (c *limitedClient) TorrentLimit() int { return c.limit }

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

	Context("with an engine that names its limit", func() {
		var limited *limitedClient

		BeforeEach(func() {
			limited = &limitedClient{fullClient: *client}
			mgr = New(store, limited).(*download)
		})

		It("evicts one torrent at the limit", func() {
			limited.limit = 3
			evictable()

			hash, err := mgr.addRotating(ctx, builtin, limited, TorrentSource{})
			Expect(err).NotTo(HaveOccurred())
			Expect(hash).To(Equal("new"))
			Expect(limited.removed).To(Equal([]string{"old"}))
		})

		It("removes nothing past the limit, where one slot is not enough", func() {
			limited.limit = 2

			_, err := mgr.addRotating(ctx, builtin, limited, TorrentSource{})
			Expect(err).To(MatchError(ErrClientFull))
			Expect(limited.removed).To(BeEmpty())
			Expect(limited.adds).To(Equal(1))
		})
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

var _ = Describe("rotationVictims", Label("unit", "downloads"), func() {
	day1 := time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC)
	day2 := day1.Add(24 * time.Hour)

	It("picks the torrent imported on the earliest day", func() {
		got := rotationVictims(
			[]Torrent{{Hash: "new", Ratio: 5}, {Hash: "old", Ratio: 0.1}},
			map[string]time.Time{"new": day2, "old": day1},
			1,
		)
		Expect(got).To(HaveLen(1))
		Expect(got[0].Hash).To(Equal("old"))
	})

	It("picks the higher ratio among same-day imports", func() {
		got := rotationVictims(
			[]Torrent{{Hash: "low", Ratio: 0.4}, {Hash: "high", Ratio: 2.1}},
			map[string]time.Time{"low": day1, "high": day1.Add(6 * time.Hour)},
			1,
		)
		Expect(got).To(HaveLen(1))
		Expect(got[0].Hash).To(Equal("high"))
	})

	It("goes by day across midnight whatever the ratios", func() {
		before := time.Date(2026, 3, 1, 23, 59, 0, 0, time.UTC)
		after := time.Date(2026, 3, 2, 0, 1, 0, 0, time.UTC)
		got := rotationVictims(
			[]Torrent{{Hash: "after", Ratio: 9}, {Hash: "before", Ratio: 0.1}},
			map[string]time.Time{"after": after, "before": before},
			1,
		)
		Expect(got).To(HaveLen(1))
		Expect(got[0].Hash).To(Equal("before"))
	})

	It("treats a zero import time as the oldest", func() {
		got := rotationVictims(
			[]Torrent{{Hash: "dated"}, {Hash: "undated"}},
			map[string]time.Time{"dated": day1, "undated": {}},
			1,
		)
		Expect(got).To(HaveLen(1))
		Expect(got[0].Hash).To(Equal("undated"))
	})

	It("ignores torrents with no import time entry", func() {
		Expect(rotationVictims(
			[]Torrent{{Hash: "downloading"}},
			map[string]time.Time{"elsewhere": day1},
			1,
		)).To(BeEmpty())
	})

	It("reports no victim for an empty listing", func() {
		Expect(rotationVictims(nil, map[string]time.Time{"a": day1}, 1)).
			To(BeEmpty())
	})

	It("lists several in eviction order", func() {
		got := rotationVictims(
			[]Torrent{
				{Hash: "day2"},
				{Hash: "day1-low", Ratio: 0.4},
				{Hash: "day1-high", Ratio: 2.1},
			},
			map[string]time.Time{
				"day2": day2, "day1-low": day1, "day1-high": day1,
			},
			2,
		)
		Expect(got).To(HaveExactElements(
			HaveField("Hash", "day1-high"), HaveField("Hash", "day1-low"),
		))
	})

	It("lists every candidate when asked for more than there are", func() {
		got := rotationVictims(
			[]Torrent{{Hash: "a"}, {Hash: "downloading"}},
			map[string]time.Time{"a": day1},
			5,
		)
		Expect(got).To(HaveExactElements(HaveField("Hash", "a")))
	})
})

var _ = Describe("TrimOverLimitTorrents", Label("unit", "downloads"), func() {
	var (
		ctx    context.Context
		store  *dbmocks.MockStore
		client *limitedClient
	)
	old := time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC)

	// trimmer wires an engine holding a, b, c and d, limited to limit, behind
	// the builtin entry.
	trimmer := func(limit int) SeedReaper {
		GinkgoHelper()
		client = &limitedClient{limit: limit}
		client.torrents = []Torrent{
			{Hash: "a"}, {Hash: "b"}, {Hash: "c"}, {Hash: "d"},
		}
		configtest.Setup(map[string]any{
			"download_clients": []map[string]any{{
				"name": "embedded", "client_type": "builtin",
				"download_dir": "/downloads", "enabled": true,
			}},
		})
		return New(store, client).(SeedReaper)
	}
	imported := func(times map[string]time.Time) {
		GinkgoHelper()
		store.EXPECT().
			ImportedTorrentTimes(mock.Anything, []string{"a", "b", "c", "d"}).
			Return(times, nil).
			Once()
	}

	BeforeEach(func() {
		ctx = context.Background()
		store = dbmocks.NewMockStore(GinkgoT())
	})

	It("removes the excess with its files, oldest import first", func() {
		mgr := trimmer(2)
		imported(map[string]time.Time{
			"a": old.Add(48 * time.Hour), "b": old, "c": old.Add(24 * time.Hour),
		})

		Expect(mgr.TrimOverLimitTorrents(ctx)).To(Succeed())
		Expect(client.removed).To(Equal([]string{"b", "c"}))
		Expect(client.deleted).To(Equal([]bool{true, true}))
	})

	It("touches nothing at the limit", func() {
		mgr := trimmer(4)

		Expect(mgr.TrimOverLimitTorrents(ctx)).To(Succeed())
		Expect(client.removed).To(BeEmpty())
	})

	It("touches nothing under the limit", func() {
		mgr := trimmer(10)

		Expect(mgr.TrimOverLimitTorrents(ctx)).To(Succeed())
		Expect(client.removed).To(BeEmpty())
	})

	It("removes only what is imported and stays over the limit", func() {
		mgr := trimmer(1)
		imported(map[string]time.Time{"d": old})

		Expect(mgr.TrimOverLimitTorrents(ctx)).To(Succeed())
		Expect(client.removed).To(Equal([]string{"d"}))
	})

	It("moves on past a removal that fails", func() {
		mgr := trimmer(2)
		client.failHash = "a"
		imported(map[string]time.Time{"a": old, "b": old.Add(24 * time.Hour)})

		Expect(mgr.TrimOverLimitTorrents(ctx)).To(Succeed())
		Expect(client.removed).To(Equal([]string{"b"}))
	})

	It("returns a failed listing", func() {
		mgr := trimmer(2)
		client.listErr = errors.New("engine boom")

		Expect(mgr.TrimOverLimitTorrents(ctx)).To(MatchError(
			ContainSubstring("engine boom"),
		))
	})

	It("returns a failed lookup and removes nothing", func() {
		mgr := trimmer(2)
		store.EXPECT().
			ImportedTorrentTimes(mock.Anything, mock.Anything).
			Return(nil, errors.New("db boom")).
			Once()

		Expect(mgr.TrimOverLimitTorrents(ctx)).To(MatchError(
			ContainSubstring("db boom"),
		))
		Expect(client.removed).To(BeEmpty())
	})

	It("leaves a client that names no limit alone", func() {
		plain := &fullClient{}
		plain.torrents = []Torrent{{Hash: "a"}, {Hash: "b"}}
		configtest.Setup(map[string]any{
			"download_clients": []map[string]any{{
				"name": "embedded", "client_type": "builtin",
				"download_dir": "/downloads", "enabled": true,
			}},
		})

		Expect(New(store, plain).(SeedReaper).TrimOverLimitTorrents(ctx)).
			To(Succeed())
		Expect(plain.listed).To(BeZero())
	})

	It("does nothing without a builtin client", func() {
		trimmer(1)
		configtest.Setup()

		Expect(New(store, client).(SeedReaper).TrimOverLimitTorrents(ctx)).
			To(Succeed())
		Expect(client.removed).To(BeEmpty())
	})
})
