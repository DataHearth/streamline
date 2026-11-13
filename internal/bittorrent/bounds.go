package bittorrent

import (
	antorrent "github.com/anacrolix/torrent"

	"github.com/datahearth/streamline/internal/config"
)

// anacrolix's defaults are sized for a desktop client with memory to spare.
// Streamline's target is a 256 MB single-core host, where the peer pool is
// the single largest thing the process can be talked into allocating: each
// established connection costs a 128 KB bufio read buffer, up to 32 KB of
// write buffer, two goroutines and two timers, and a connection we upload
// on also buffers the piece data its peer requested until it is sent (up to
// maxPeerRequestDataPerConn). Ten seeding torrents at the stock 50 conns
// each is ~500 connections — roughly 80 MB of buffers and a thousand
// goroutines before any upload data, and up to ~500 MB more with it at the
// stock 1 MiB per connection, for a workload that is not throughput-bound.
//
// The default* values below are what an unset (or 0) builtin entry key
// resolves to (resolveBounds), so an install that sets nothing keeps the
// 256 MB envelope. A host with memory to spare can raise them through
// max_peers_per_torrent, max_unverified_mb and max_torrents; the config's
// validate tags cap each, so a typo cannot ask for thousands of connections.
// Raising the peer count is the one that costs process memory — about
// 160 KB per established connection, up to ~420 KB while seeding.
const (
	// defaultPeersPerTorrent is the established-connection ceiling per
	// torrent (anacrolix default 50). A swarm saturates a home connection long
	// before this; the tail of extra peers buys resilience, not speed.
	defaultPeersPerTorrent = 15
	// minHalfOpenConns bounds connections being dialled across all torrents
	// (default 100). Each is a pending socket and a goroutine. It is a floor:
	// the bound scales with the peer count (resolveBounds), since dialling
	// 20 at a time for torrents allowed 100 peers each only slows the ramp-up.
	minHalfOpenConns = 20
	// minKnownPeersPerTorrent caps the *known* peer list per torrent (default
	// 500) — addresses held in memory whether or not they are dialled, which
	// trackers and DHT will happily keep supplying. A floor like
	// minHalfOpenConns, and the reason TorrentPeersLowWater is never touched:
	// at 100 or more it stays above anacrolix's low water (50), so the
	// pruner, which drops known peers down to high water, never fights the
	// announcer, which keeps asking for more while the count is at or under
	// low water.
	minKnownPeersPerTorrent = 100
	// defaultUnverifiedMB bounds piece data received but not yet hashed, in
	// MiB (anacrolix default 64), per torrent: anacrolix pools the count
	// across torrents only for storage that reports a capacity, and file
	// storage reports none, so every downloading torrent has its own. It is
	// not heap: anacrolix writes each block to storage as it arrives
	// (Torrent.writeChunk), so what it bounds is page cache, which the memory
	// cgroup charges — dirty until writeback, reclaimable after. It is
	// counted in whole pieces, the first always in full, so it also bounds
	// throughput: a release with 16 MiB pieces fills it alone, and that
	// torrent then downloads one piece at a time. That is what
	// max_unverified_mb is for.
	defaultUnverifiedMB = 16
	// maxPeerRequestDataPerConn bounds the piece data one connection buffers
	// for its peer's requests before sending it (default 1 MiB). A peer
	// pipelines many requests at once, and an upload rate limit or a slow
	// peer holds them back, so on a seeding connection this fills: at the default it was the
	// largest per-connection cost, unbounded by anything above — 150 seeding
	// connections could hold 150 MB. anacrolix refuses a request longer than
	// this outright, so it must stay well above the 16 KiB block size peers
	// request; 256 KiB leaves room for clients that ask for larger blocks.
	// Not configurable: raising it buys nothing a peer count does not.
	maxPeerRequestDataPerConn = 256 << 10
	// defaultMaxTorrents caps how many torrents the engine holds at once.
	// Every grab is open to members and adds one, and nothing else ever
	// counts them: each costs resident state, a goroutine until its metadata
	// resolves, and a re-add on every boot, and only an admin can remove one.
	// A re-add of a torrent already held is never refused, and neither is a
	// boot restore: lowering the cap below what the engine holds drops
	// nothing, it only refuses new hashes until the count is back under.
	defaultMaxTorrents = 500
)

// peerBounds is the engine's resolved peer-pool and torrent limits.
type peerBounds struct {
	peersPerTorrent int
	halfOpenConns   int
	knownPeers      int
	unverifiedBytes int64
	torrents        int
}

// resolveBounds reads the builtin entry's limits, each falling back to its
// default when unset or 0. Half-open and known peers are not keys of their
// own: each only matters relative to the peer count, so both scale with it at
// the default's ratios (20:15 and 100:15), never below their floors.
func resolveBounds(entry config.DownloadClientEntry) peerBounds {
	peers := orDefault(entry.MaxPeersPerTorrent, defaultPeersPerTorrent)
	return peerBounds{
		peersPerTorrent: peers,
		halfOpenConns:   max(minHalfOpenConns, peers*4/3),
		knownPeers:      max(minKnownPeersPerTorrent, peers*20/3),
		unverifiedBytes: int64(
			orDefault(entry.MaxUnverifiedMB, defaultUnverifiedMB),
		) << 20,
		torrents: orDefault(entry.MaxTorrents, defaultMaxTorrents),
	}
}

func orDefault(v, def int) int {
	if v > 0 {
		return v
	}
	return def
}

// applyMemoryBounds caps the anacrolix client's per-peer and buffer growth.
func applyMemoryBounds(cc *antorrent.ClientConfig, b peerBounds) {
	cc.EstablishedConnsPerTorrent = b.peersPerTorrent
	cc.TotalHalfOpenConns = b.halfOpenConns
	cc.TorrentPeersHighWater = b.knownPeers
	cc.MaxUnverifiedBytes = b.unverifiedBytes
	cc.MaxAllocPeerRequestDataPerConn = maxPeerRequestDataPerConn
}
