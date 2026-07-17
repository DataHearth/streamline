package bittorrent

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	antorrent "github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/metainfo"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/internal/db"
	"github.com/datahearth/streamline/internal/download"
	"github.com/datahearth/streamline/internal/otelx"
)

var _ download.Client = (*Engine)(nil)

// specFromSource builds a torrent spec plus the persistable source fields.
// Exactly one of Magnet/Bytes must be set (mirrors download.TorrentSource).
func specFromSource(
	src download.TorrentSource,
) (*antorrent.TorrentSpec, string, []byte, error) {
	switch {
	case src.Magnet != "":
		spec, err := antorrent.TorrentSpecFromMagnetUri(src.Magnet)
		if err != nil {
			return nil, "", nil, fmt.Errorf("parse magnet: %w", err)
		}
		return spec, src.Magnet, nil, nil
	case len(src.Bytes) > 0:
		mi, err := metainfo.Load(bytes.NewReader(src.Bytes))
		if err != nil {
			return nil, "", nil, fmt.Errorf("parse torrent file: %w", err)
		}
		spec, err := antorrent.TorrentSpecFromMetaInfoErr(mi)
		if err != nil {
			return nil, "", nil, fmt.Errorf("build torrent spec: %w", err)
		}
		return spec, "", src.Bytes, nil
	default:
		return nil, "", nil, errors.New(
			"torrent source has neither bytes nor magnet",
		)
	}
}

// AddTorrent persists the session row first (boot re-add is idempotent by
// infohash), then hands the spec to the engine.
func (e *Engine) AddTorrent(
	ctx context.Context,
	src download.TorrentSource,
) (string, error) {
	ctx, span := tracer.Start(ctx, "bittorrent.add_torrent")
	defer span.End()

	spec, sourceMagnet, sourceBytes, err := specFromSource(src)
	if err != nil {
		return "", otelx.RecordSpanError(span, err)
	}
	hash := spec.InfoHash.HexString()
	if _, err := e.store.CreateTorrentSession(ctx, db.CreateTorrentSessionParams{
		InfoHash:      hash,
		Name:          spec.DisplayName,
		SavePath:      e.downloadDir,
		SourceMagnet:  sourceMagnet,
		SourceTorrent: sourceBytes,
	}); err != nil && !ent.IsConstraintError(err) {
		return "", otelx.RecordSpanError(
			span, fmt.Errorf("persist torrent session: %w", err),
		)
	}
	t, _, err := e.client.AddTorrentSpec(spec)
	if err != nil {
		return "", otelx.RecordSpanError(span, fmt.Errorf("add torrent: %w", err))
	}
	e.setState(hash, func(st *torrentState) {
		if st.addedAt.IsZero() {
			st.addedAt = time.Now()
		}
	})
	e.startWhenReady(t, false)
	return hash, nil
}

func (e *Engine) GetTorrent(
	ctx context.Context,
	hash string,
) (*download.Torrent, error) {
	t, err := e.torrent(hash)
	if err != nil {
		return nil, err
	}
	v := e.view(t)
	return &v, nil
}

func (e *Engine) ListTorrents(ctx context.Context) ([]download.Torrent, error) {
	live := e.client.Torrents()
	out := make([]download.Torrent, 0, len(live))
	for _, t := range live {
		out = append(out, e.view(t))
	}
	return out, nil
}

func (e *Engine) RemoveTorrent(
	ctx context.Context,
	hash string,
	deleteFiles bool,
) error {
	ctx, span := tracer.Start(ctx, "bittorrent.remove_torrent")
	defer span.End()

	t, err := e.torrent(hash)
	if err != nil {
		return otelx.RecordSpanError(span, err)
	}
	var contentPath string
	if info := t.Info(); info != nil {
		// Guard against deleting the whole download dir when the torrent
		// has no usable name.
		if name := info.BestName(); name != "" && name != "." && name != ".." {
			contentPath = filepath.Join(e.downloadDir, name)
		}
	}
	t.Drop()
	if deleteFiles && contentPath != "" {
		// An incomplete single-file torrent stores its partial data at
		// "<name>.part" (anacrolix UsePartFiles), a sibling of contentPath that
		// os.RemoveAll(contentPath) would miss. Remove both; the .part path is a
		// harmless no-op for a completed or multi-file torrent.
		for _, p := range []string{contentPath, contentPath + ".part"} {
			if err := os.RemoveAll(p); err != nil {
				return otelx.RecordSpanError(
					span, fmt.Errorf("delete torrent data: %w", err),
				)
			}
		}
	}
	if err := e.store.DeleteTorrentSessionByHash(ctx, hash); err != nil {
		return otelx.RecordSpanError(
			span, fmt.Errorf("delete torrent session: %w", err),
		)
	}
	e.mu.Lock()
	delete(e.state, hash)
	delete(e.sample, hash)
	e.mu.Unlock()
	return nil
}

func (e *Engine) PauseTorrent(ctx context.Context, hash string) error {
	t, err := e.torrent(hash)
	if err != nil {
		return err
	}
	t.DisallowDataDownload()
	t.DisallowDataUpload()
	e.setState(hash, func(st *torrentState) { st.paused = true })
	return e.store.SetTorrentSessionPaused(ctx, hash, true)
}

func (e *Engine) ResumeTorrent(ctx context.Context, hash string) error {
	t, err := e.torrent(hash)
	if err != nil {
		return err
	}
	st := e.getState(hash)
	t.AllowDataDownload()
	if !st.seedStopped {
		t.AllowDataUpload()
	}
	// No priority work here: file priorities (the single demand source, set
	// by startWhenReady's default or the user) survive a pause untouched.
	e.setState(hash, func(st *torrentState) { st.paused = false })
	return e.store.SetTorrentSessionPaused(ctx, hash, false)
}

// TestConnection is a no-op: a constructed engine is by definition running.
func (e *Engine) TestConnection(ctx context.Context) error { return nil }

// liveStats is one consistent snapshot of a torrent's transfer state,
// shared by the download.Client view and the /torrents management views so
// the rate sampler is hit exactly once per observation.
type liveStats struct {
	hash           string
	name           string
	status         download.TorrentStatus
	progress       float64
	size           int64
	downloadSpeed  int64
	uploadSpeed    int64
	uploaded       int64
	eta            int64
	seeds          int
	activePeers    int
	addedAt        time.Time
	seedingStopped bool
}

func (e *Engine) live(t *antorrent.Torrent) liveStats {
	hash := t.InfoHash().HexString()
	var size, completed int64
	if t.Info() != nil {
		size = t.Length()
		completed = t.BytesCompleted()
	}
	var progress float64
	if size > 0 {
		progress = float64(completed) / float64(size)
	}
	stats := t.Stats()
	uploaded := stats.BytesWrittenData.Int64()
	down, up := e.rates(hash, completed, uploaded)
	var eta int64
	if down > 0 && size > completed {
		eta = (size - completed) / down
	}
	st := e.getState(hash)
	return liveStats{
		hash:           hash,
		name:           t.Name(),
		status:         e.status(t, hash),
		progress:       progress,
		size:           size,
		downloadSpeed:  down,
		uploadSpeed:    up,
		uploaded:       uploaded,
		eta:            eta,
		seeds:          stats.ConnectedSeeders,
		activePeers:    stats.ActivePeers,
		addedAt:        st.addedAt,
		seedingStopped: st.seedStopped,
	}
}

// view builds the download.Client-facing snapshot of one torrent.
func (e *Engine) view(t *antorrent.Torrent) download.Torrent {
	l := e.live(t)
	return download.Torrent{
		Hash:          l.hash,
		Name:          l.name,
		Status:        l.status,
		Progress:      l.progress,
		Size:          l.size,
		SavePath:      e.downloadDir,
		DownloadSpeed: l.downloadSpeed,
		ETA:           l.eta,
	}
}

func (e *Engine) status(
	t *antorrent.Torrent,
	hash string,
) download.TorrentStatus {
	st := e.getState(hash)
	if st.paused {
		return download.StatusPaused
	}
	if t.Info() == nil {
		return download.StatusFetching
	}
	if t.BytesMissing() == 0 {
		if st.seedStopped {
			return download.StatusCompleted
		}
		return download.StatusSeeding
	}
	// Downloading but with nothing to download from: no data can move, so
	// surface it as stalled rather than a healthy-looking "downloading".
	if t.Stats().ActivePeers == 0 {
		return download.StatusStalled
	}
	return download.StatusDownloading
}

// rates derives download/upload bytes/sec from the deltas since the
// previous observation of this torrent. First observation (or byte
// regression after a failed piece check) reports 0.
func (e *Engine) rates(hash string, completed, uploaded int64) (int64, int64) {
	e.mu.Lock()
	defer e.mu.Unlock()
	prev, ok := e.sample[hash]
	now := time.Now()
	e.sample[hash] = speedSample{down: completed, up: uploaded, at: now}
	elapsed := now.Sub(prev.at).Seconds()
	if !ok || elapsed <= 0 {
		return 0, 0
	}
	var down, up int64
	if completed >= prev.down {
		down = int64(float64(completed-prev.down) / elapsed)
	}
	if uploaded >= prev.up {
		up = int64(float64(uploaded-prev.up) / elapsed)
	}
	return down, up
}
