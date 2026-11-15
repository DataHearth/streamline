package bittorrent

import (
	"context"
	"log/slog"
	"time"

	antorrent "github.com/anacrolix/torrent"

	"github.com/datahearth/streamline/internal/observability"
)

// seederSweepInterval is how often the engine closes connections to seeders
// it has no use for. A var so the specs can stretch it past their own length
// and drive the sweep by hand.
var seederSweepInterval = 10 * time.Second

// dropRedundantSeeders runs dropSeedersOnce every seederSweepInterval.
//
// anacrolix drops a mutually complete peer by itself
// (ClientConfig.DropMutuallyCompletePeers), but only on a torrent holding
// every piece. One with skipped files never does — under
// download.selective_files that is most season packs — and it announces as a
// leecher for as long as it lives, so trackers keep handing it seeders and
// handing it to them. Each such connection can move no byte either way and
// holds one of the EstablishedConnsPerTorrent places a leecher could take.
// This is libtorrent's close_redundant_connections for a finished torrent.
func (e *Engine) dropRedundantSeeders() {
	tick := time.NewTicker(seederSweepInterval)
	defer tick.Stop()
	for {
		select {
		case <-e.stop:
			return
		case <-tick.C:
		}
		func() {
			ctx := context.Background()
			defer observability.RecoverPanic(ctx, "drop redundant seeders", nil)
			e.dropSeedersOnce(ctx)
		}()
	}
}

// dropSeedersOnce closes every connection to a peer holding all pieces, on
// each torrent that holds every piece of its wanted files, and returns how
// many it closed. Leechers stay: seeding goes on.
//
// It only closes connections and keeps no memory of them. A torrent that
// wants data again — a file re-enabled, a selection widened — fails
// holdsWantedData from the next pass on, and anacrolix dials and accepts
// seeders for it as it does for any download.
func (e *Engine) dropSeedersOnce(ctx context.Context) int {
	var dropped int
	for _, t := range e.client.Torrents() {
		if !holdsWantedData(t) {
			continue
		}
		n := t.NumPieces()
		if n <= 0 {
			continue
		}
		pieces := uint64(n)
		for _, pc := range t.PeerConns() {
			if pc.PeerPieces().GetCardinality() < pieces {
				continue
			}
			if err := pc.Close(); err != nil {
				slog.DebugContext(ctx, "closing a redundant seeder failed",
					"info_hash", t.InfoHash().HexString(), "error", err)
				continue
			}
			dropped++
		}
	}
	if dropped > 0 {
		seederDrops.Add(ctx, int64(dropped))
	}
	return dropped
}

// holdsWantedData reports whether the torrent has every piece of its wanted
// files hash-verified, so a seeder has nothing left to give it. A torrent with
// no wanted file at all — a selective grab still waiting for its keep-set —
// is about to want data, not done with it.
func holdsWantedData(t *antorrent.Torrent) bool {
	if t.Info() == nil {
		return false
	}
	return wantedBytes(t) > 0 && wantedVerified(t)
}
