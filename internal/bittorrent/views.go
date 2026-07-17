package bittorrent

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	antorrent "github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/types"

	"github.com/datahearth/streamline/internal/download"
)

// Manager is the surface the /api/v1/torrents handlers consume. Implemented
// by *Engine; mocked for handler tests.
type Manager interface {
	AddTorrent(ctx context.Context, src download.TorrentSource) (string, error)
	PauseTorrent(ctx context.Context, hash string) error
	ResumeTorrent(ctx context.Context, hash string) error
	RemoveTorrent(ctx context.Context, hash string, deleteFiles bool) error
	ListViews(ctx context.Context) []TorrentView
	Details(ctx context.Context, hash string) (TorrentDetails, error)
	SetFilePriorities(ctx context.Context, hash string, prios []FilePriority) error
	Runtime() RuntimeStatus
}

var _ Manager = (*Engine)(nil)

// RuntimeStatus reports the live engine's bind state for the builtin
// download-client read view: the actually bound listen port and interface.
type RuntimeStatus struct {
	Running        bool
	PortBound      uint16
	InterfaceBound string
}

func (e *Engine) Runtime() RuntimeStatus {
	return RuntimeStatus{
		Running:        true,
		PortBound:      uint16(e.client.LocalPort()),
		InterfaceBound: e.bindAddr,
	}
}

// TorrentView is one torrent with live transfer stats.
type TorrentView struct {
	Hash          string
	Name          string
	Status        download.TorrentStatus
	Progress      float64 // 0..1
	Size          int64
	DownloadSpeed int64 // bytes/sec
	UploadSpeed   int64 // bytes/sec
	Uploaded      int64
	Ratio         float64
	ETA           int64 // seconds; 0 = unknown
	Seeds         int
	PeerCount     int
	SavePath      string
	AddedAt       time.Time
	// SeedingStopped is set once the ratio/time limit stopped seeding.
	SeedingStopped bool
	// Tracked is false for arbitrary adds with no download_record.
	Tracked bool
}

type FileView struct {
	Index      int
	Path       string
	Size       int64
	Downloaded int64
	Priority   string
}

type PeerView struct {
	Addr         string
	Client       string
	DownloadRate float64
	UploadRate   float64
}

type TorrentDetails struct {
	TorrentView
	Files    []FileView
	Trackers []string
	Peers    []PeerView
}

// FilePriority is one file-priority assignment (index into Details.Files).
type FilePriority struct {
	Index    int
	Priority string
}

func (e *Engine) ListViews(ctx context.Context) []TorrentView {
	tracked := e.trackedHashes(ctx)
	live := e.client.Torrents()
	out := make([]TorrentView, 0, len(live))
	for _, t := range live {
		out = append(out, e.torrentView(t, tracked))
	}
	return out
}

// torrentView maps one live snapshot onto the management view.
func (e *Engine) torrentView(
	t *antorrent.Torrent,
	tracked map[string]struct{},
) TorrentView {
	l := e.live(t)
	_, isTracked := tracked[l.hash]
	return TorrentView{
		Hash:           l.hash,
		Name:           l.name,
		Status:         l.status,
		Progress:       l.progress,
		Size:           l.size,
		DownloadSpeed:  l.downloadSpeed,
		UploadSpeed:    l.uploadSpeed,
		Uploaded:       l.uploaded,
		Ratio:          ratio(l.uploaded, l.size),
		ETA:            l.eta,
		Seeds:          l.seeds,
		PeerCount:      l.activePeers,
		SavePath:       e.downloadDir,
		AddedAt:        l.addedAt,
		SeedingStopped: l.seedingStopped,
		Tracked:        isTracked,
	}
}

// trackedHashes is the set of torrent hashes some download_record owns.
// A lookup failure degrades to "nothing tracked" (badge-only data, and the
// list refreshes every poll) rather than failing the view.
func (e *Engine) trackedHashes(ctx context.Context) map[string]struct{} {
	set, err := e.store.AllDownloadRecordHashes(ctx)
	if err != nil {
		slog.WarnContext(ctx, "listing tracked torrent hashes failed", "error", err)
		return map[string]struct{}{}
	}
	return set
}

func (e *Engine) Details(
	ctx context.Context,
	hash string,
) (TorrentDetails, error) {
	t, err := e.torrent(hash)
	if err != nil {
		return TorrentDetails{}, err
	}
	d := TorrentDetails{
		TorrentView: e.torrentView(t, e.trackedHashes(ctx)),
		Files:       []FileView{},
		Trackers:    []string{},
		Peers:       []PeerView{},
	}
	if t.Info() != nil {
		for i, f := range t.Files() {
			d.Files = append(d.Files, FileView{
				Index:      i,
				Path:       f.DisplayPath(),
				Size:       f.Length(),
				Downloaded: f.BytesCompleted(),
				Priority:   priorityName(f.Priority()),
			})
		}
	}
	mi := t.Metainfo()
	for _, tier := range mi.UpvertedAnnounceList() {
		d.Trackers = append(d.Trackers, tier...)
	}
	for _, pc := range t.PeerConns() {
		name, _ := pc.PeerClientName.Load().(string)
		stats := pc.Stats()
		d.Peers = append(d.Peers, PeerView{
			Addr:         fmt.Sprint(pc.RemoteAddr),
			Client:       name,
			DownloadRate: stats.DownloadRate,
			UploadRate:   stats.LastWriteUploadRate,
		})
	}
	return d, nil
}

func (e *Engine) SetFilePriorities(
	ctx context.Context,
	hash string,
	prios []FilePriority,
) error {
	t, err := e.torrent(hash)
	if err != nil {
		return err
	}
	if t.Info() == nil {
		return errors.New("torrent metadata not yet available")
	}
	files := t.Files()
	for _, p := range prios {
		if p.Index < 0 || p.Index >= len(files) {
			return fmt.Errorf("file index %d out of range", p.Index)
		}
		prio, err := parsePriority(p.Priority)
		if err != nil {
			return err
		}
		files[p.Index].SetPriority(prio)
	}
	return nil
}

func parsePriority(name string) (types.PiecePriority, error) {
	switch name {
	case "skip":
		return types.PiecePriorityNone, nil
	case "normal":
		return types.PiecePriorityNormal, nil
	case "high":
		return types.PiecePriorityHigh, nil
	default:
		return 0, fmt.Errorf("unknown file priority %q", name)
	}
}

func priorityName(p types.PiecePriority) string {
	switch p {
	case types.PiecePriorityNone:
		return "skip"
	case types.PiecePriorityHigh:
		return "high"
	default:
		return "normal"
	}
}
