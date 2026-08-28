package bittorrent

import antorrent "github.com/anacrolix/torrent"

// anacrolix's defaults are sized for a desktop client with memory to spare.
// Streamline's target is a 256 MB single-core host, where the peer pool is
// the single largest thing the process can be talked into allocating: each
// established connection costs a 128 KB bufio read buffer, up to 32 KB of
// write buffer, two goroutines and two timers. Ten seeding torrents at the
// stock 50 conns each is ~500 connections — roughly 80 MB of buffers and a
// thousand goroutines, for a workload that is not throughput-bound.
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
	// (default 64 MiB — a quarter of the whole target machine, and the one
	// default here that can be exceeded in a single burst).
	maxUnverifiedBytes = 16 << 20
)

// applyMemoryBounds caps the anacrolix client's per-peer and buffer growth.
func applyMemoryBounds(cc *antorrent.ClientConfig) {
	cc.EstablishedConnsPerTorrent = maxPeersPerTorrent
	cc.TotalHalfOpenConns = maxHalfOpenConns
	cc.TorrentPeersHighWater = maxKnownPeersPerTorrent
	cc.MaxUnverifiedBytes = maxUnverifiedBytes
}
