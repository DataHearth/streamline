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
}

type Client interface {
	AddTorrent(ctx context.Context, src TorrentSource) (string, error)
	GetTorrent(ctx context.Context, hash string) (*Torrent, error)
	ListTorrents(ctx context.Context) ([]Torrent, error)
	RemoveTorrent(ctx context.Context, hash string, deleteFiles bool) error
	PauseTorrent(ctx context.Context, hash string) error
	ResumeTorrent(ctx context.Context, hash string) error
	TestConnection(ctx context.Context) error
}
