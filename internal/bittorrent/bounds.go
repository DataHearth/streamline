package bittorrent

import antorrent "github.com/anacrolix/torrent"

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
// These are constants rather than config keys deliberately. They exist to
// keep the engine inside a memory envelope, and an operator who raises them
// past what the host has does not get faster downloads, they get the OOM
// killer. A machine with room to spare is better served by an external
// client, which owns its own limits.
const (
	// maxPeersPerTorrent is the established-connection ceiling per torrent
	// (anacrolix default 50). A swarm saturates a home connection long before
	// this; the tail of extra peers buys resilience, not speed.
	maxPeersPerTorrent = 15
	// maxHalfOpenConns bounds connections being dialled across all torrents
	// (default 100). Each is a pending socket and a goroutine.
	maxHalfOpenConns = 20
	// maxKnownPeersPerTorrent caps the *known* peer list per torrent
	// (default 500) — addresses held in memory whether or not they are
	// dialled, which trackers and DHT will happily keep supplying.
	maxKnownPeersPerTorrent = 100
	// maxUnverifiedBytes bounds piece data received but not yet hashed
	// (default 64 MiB), per torrent: anacrolix pools the count across
	// torrents only for storage that reports a capacity, and file storage
	// reports none, so every downloading torrent has its own. It is not
	// heap: anacrolix writes each block to storage as it arrives
	// (Torrent.writeChunk), so what it bounds is page cache, which the memory
	// cgroup charges — dirty until writeback, reclaimable after. It is
	// counted in whole pieces, the first always in full, so it also bounds
	// throughput: a release with 16 MiB pieces fills it alone, and that
	// torrent then downloads one piece at a time.
	maxUnverifiedBytes = 16 << 20
	// maxPeerRequestDataPerConn bounds the piece data one connection buffers
	// for its peer's requests before sending it (default 1 MiB). A peer
	// pipelines many requests at once, and an upload rate limit or a slow
	// peer holds them back, so on a seeding connection this fills: at the default it was the
	// largest per-connection cost, unbounded by anything above — 150 seeding
	// connections could hold 150 MB. anacrolix refuses a request longer than
	// this outright, so it must stay well above the 16 KiB block size peers
	// request; 256 KiB leaves room for clients that ask for larger blocks.
	maxPeerRequestDataPerConn = 256 << 10
	// maxTorrents caps how many torrents the engine holds at once. Every grab
	// is open to members and adds one, and nothing else ever counts them:
	// each costs resident state, a goroutine until its metadata resolves, and
	// a re-add on every boot, and only an admin can remove one. A re-add of a
	// torrent already held is never refused.
	maxTorrents = 500
)

// applyMemoryBounds caps the anacrolix client's per-peer and buffer growth.
func applyMemoryBounds(cc *antorrent.ClientConfig) {
	cc.EstablishedConnsPerTorrent = maxPeersPerTorrent
	cc.TotalHalfOpenConns = maxHalfOpenConns
	cc.TorrentPeersHighWater = maxKnownPeersPerTorrent
	cc.MaxUnverifiedBytes = maxUnverifiedBytes
	cc.MaxAllocPeerRequestDataPerConn = maxPeerRequestDataPerConn
}
