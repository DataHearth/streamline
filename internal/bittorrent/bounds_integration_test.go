package bittorrent

import (
	"context"
	"errors"
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/internal/db"
	"github.com/datahearth/streamline/internal/download"
	"github.com/datahearth/streamline/internal/testutil/configtest"
	"github.com/datahearth/streamline/internal/testutil/dbtest"
)

var _ = Describe("Engine max_torrents", Label("integration", "bittorrent"), func() {
	const (
		firstHash  = "1111111111111111111111111111111111111111"
		secondHash = "2222222222222222222222222222222222222222"
		thirdHash  = "3333333333333333333333333333333333333333"
	)
	var (
		ctx   context.Context
		store db.Store
		dlDir string
	)

	BeforeEach(func() {
		ctx = context.Background()
		dlDir = filepath.Join(GinkgoT().TempDir(), "dl")
		Expect(os.MkdirAll(dlDir, 0o755)).To(Succeed())
		entClient := dbtest.SetupTestDB(ctx)
		DeferCleanup(entClient.Close)
		store = db.New(entClient)
	})

	// startCapped starts an engine whose builtin entry sets max_torrents.
	startCapped := func(limit int) *Engine {
		GinkgoHelper()
		configtest.Setup(map[string]any{
			"download_clients": []map[string]any{{
				"name": "embedded", "client_type": "builtin",
				"download_dir": dlDir, "listen_port": int(reserveListenPort()),
				"bind_interface": engineBindIP, "disable_dht": true,
				"enabled": true, "max_torrents": limit,
			}},
		})
		e, err := New(ctx, store)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { Expect(e.Close()).To(Succeed()) })
		return e
	}
	magnet := func(hash string) download.TorrentSource {
		return download.TorrentSource{Magnet: "magnet:?xt=urn:btih:" + hash}
	}

	It("refuses a new hash past the entry's cap, never a re-add", func() {
		e := startCapped(1)

		_, err := e.AddTorrent(ctx, magnet(firstHash))
		Expect(err).NotTo(HaveOccurred())

		_, err = e.AddTorrent(ctx, magnet(secondHash))
		Expect(err).To(MatchError(download.ErrClientFull))

		// Whatever a duplicate add answers, it is not the cap's refusal.
		_, err = e.AddTorrent(ctx, magnet(firstHash))
		Expect(errors.Is(err, download.ErrClientFull)).To(BeFalse())
	})

	It("restores every held torrent past a lowered cap, refusing new ones", func() {
		for _, h := range []string{firstHash, secondHash} {
			params := db.CreateTorrentSessionParams{
				InfoHash: h, Name: h, SavePath: dlDir,
				SourceMagnet: "magnet:?xt=urn:btih:" + h,
			}
			_, err := store.CreateTorrentSession(ctx, params)
			Expect(err).NotTo(HaveOccurred())
		}

		e := startCapped(1)

		Expect(e.client.Torrents()).To(HaveLen(2))
		_, err := e.AddTorrent(ctx, magnet(thirdHash))
		Expect(err).To(MatchError(download.ErrClientFull))
	})
})
