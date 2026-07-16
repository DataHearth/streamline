package bittorrent

import (
	"context"
	"errors"
	"fmt"

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
}

var _ Manager = (*Engine)(nil)

// TorrentView is one torrent with live transfer stats.
type TorrentView struct {
	Hash          string
	Name          string
	Status        download.TorrentStatus
	Progress      float64 // 0..1
	Size          int64
	DownloadSpeed int64 // bytes/sec
	Uploaded      int64
	Ratio         float64
	PeerCount     int
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
	live := e.client.Torrents()
	out := make([]TorrentView, 0, len(live))
	for _, t := range live {
		base := e.view(t)
		stats := t.Stats()
		uploaded := stats.BytesWrittenData.Int64()
		out = append(out, TorrentView{
			Hash:          base.Hash,
			Name:          base.Name,
			Status:        base.Status,
			Progress:      base.Progress,
			Size:          base.Size,
			DownloadSpeed: base.DownloadSpeed,
			Uploaded:      uploaded,
			Ratio:         ratio(uploaded, base.Size),
			PeerCount:     stats.ActivePeers,
		})
	}
	return out
}

func (e *Engine) Details(
	ctx context.Context,
	hash string,
) (TorrentDetails, error) {
	t, err := e.torrent(hash)
	if err != nil {
		return TorrentDetails{}, err
	}
	base := e.view(t)
	stats := t.Stats()
	uploaded := stats.BytesWrittenData.Int64()
	d := TorrentDetails{
		TorrentView: TorrentView{
			Hash:          base.Hash,
			Name:          base.Name,
			Status:        base.Status,
			Progress:      base.Progress,
			Size:          base.Size,
			DownloadSpeed: base.DownloadSpeed,
			Uploaded:      uploaded,
			Ratio:         ratio(uploaded, base.Size),
			PeerCount:     stats.ActivePeers,
		},
		Files:    []FileView{},
		Trackers: []string{},
		Peers:    []PeerView{},
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
		d.Peers = append(d.Peers, PeerView{
			Addr:         fmt.Sprint(pc.RemoteAddr),
			Client:       name,
			DownloadRate: pc.Stats().DownloadRate,
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
