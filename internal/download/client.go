package download

import "context"

type TorrentStatus string

const (
	StatusDownloading TorrentStatus = "downloading"
	StatusSeeding     TorrentStatus = "seeding"
	StatusPaused      TorrentStatus = "paused"
	StatusCompleted   TorrentStatus = "completed"
	// StatusFetching (magnet metadata not yet resolved) and StatusStalled
	// (downloading with no connected peers) are only emitted by the builtin
	// bittorrent engine; external clients map their equivalents to
	// StatusDownloading.
	StatusFetching TorrentStatus = "fetching"
	StatusStalled  TorrentStatus = "stalled"
	StatusError    TorrentStatus = "error"
)

type Torrent struct {
	Hash     string
	Name     string
	Status   TorrentStatus
	Progress float64
	Size     int64
	SavePath string
	// DownloadSpeed is bytes/sec (0 when idle/unknown). ETA is seconds to
	// completion; the qBittorrent ∞ sentinel (8640000) is normalized to 0.
	DownloadSpeed int64
	ETA           int64
}

// TorrentSource is what the manager hands to Client.AddTorrent.
// Exactly one of Bytes or Magnet must be set: the manager fetches http(s)
// .torrent URLs itself (so download clients in network-isolated containers
// don't need to reach the indexer) and passes magnet URIs straight through.
type TorrentSource struct {
	Bytes  []byte // raw .torrent file contents
	Magnet string // magnet:?xt=urn:btih:... URI
	// WantedFiles selects which files to download, by metainfo index. nil means
	// every file. Ignored for a magnet source.
	WantedFiles []int
	// Selective marks a grab carrying a wanted set even when WantedFiles is nil
	// (a magnet) — the signal not to start downloading everything (spec §4.3).
	Selective bool
}

// TorrentFile is one file inside a torrent. Index is the client's own
// ordering and is what SetWantedFiles addresses.
type TorrentFile struct {
	Index  int
	Path   string // path within the torrent, forward slashes
	Size   int64
	Wanted bool
}

// MagnetMetadataFetcher is an optional Client capability: resolve a magnet's
// metainfo without (or before) admitting the torrent, so a selective magnet
// grab can take the exact .torrent path instead of the pending fallback.
// Deluge implements it via core.prefetch_magnet_metadata (spec §4.2). The
// builtin engine and Transmission don't yet — this phase was ruled skipped
// pending evidence it's worth the cost — but either could adopt it later by
// implementing this same interface.
type MagnetMetadataFetcher interface {
	FetchMagnetMetadata(ctx context.Context, magnet string) ([]byte, error)
}

type Client interface {
	AddTorrent(ctx context.Context, src TorrentSource) (string, error)
	GetTorrent(ctx context.Context, hash string) (*Torrent, error)
	ListTorrents(ctx context.Context) ([]Torrent, error)
	RemoveTorrent(ctx context.Context, hash string, deleteFiles bool) error
	PauseTorrent(ctx context.Context, hash string) error
	ResumeTorrent(ctx context.Context, hash string) error
	TestConnection(ctx context.Context) error
	// ListFiles returns the torrent's files. An empty slice with a nil error
	// means metadata is not yet available — the caller retries.
	ListFiles(ctx context.Context, hash string) ([]TorrentFile, error)
	// SetWantedFiles makes exactly `wanted` (indexes) downloaded and skips
	// every other file. ErrNotSupported flips the record to unsupported.
	SetWantedFiles(ctx context.Context, hash string, wanted []int) error
}
