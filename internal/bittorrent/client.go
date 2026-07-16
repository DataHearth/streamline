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
	e.setState(hash, func(*torrentState) {})
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
		if err := os.RemoveAll(contentPath); err != nil {
			return otelx.RecordSpanError(
				span, fmt.Errorf("delete torrent data: %w", err),
			)
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
	if t.Info() != nil {
		t.DownloadAll()
	}
	e.setState(hash, func(st *torrentState) { st.paused = false })
	return e.store.SetTorrentSessionPaused(ctx, hash, false)
}

// TestConnection is a no-op: a constructed engine is by definition running.
func (e *Engine) TestConnection(ctx context.Context) error { return nil }

// view builds the download.Client-facing snapshot of one torrent.
func (e *Engine) view(t *antorrent.Torrent) download.Torrent {
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
	speed := e.speed(hash, completed)
	var eta int64
	if speed > 0 && size > completed {
		eta = (size - completed) / speed
	}
	return download.Torrent{
		Hash:          hash,
		Name:          t.Name(),
		Status:        e.status(t, hash),
		Progress:      progress,
		Size:          size,
		SavePath:      e.downloadDir,
		DownloadSpeed: speed,
		ETA:           eta,
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
		return download.StatusDownloading
	}
	if t.BytesMissing() == 0 {
		if st.seedStopped {
			return download.StatusCompleted
		}
		return download.StatusSeeding
	}
	return download.StatusDownloading
}

// speed derives bytes/sec from the delta since the previous observation of
// this torrent. First observation (or byte regression after a failed piece
// check) reports 0.
func (e *Engine) speed(hash string, completed int64) int64 {
	e.mu.Lock()
	defer e.mu.Unlock()
	prev, ok := e.sample[hash]
	now := time.Now()
	e.sample[hash] = speedSample{bytes: completed, at: now}
	elapsed := now.Sub(prev.at).Seconds()
	if !ok || elapsed <= 0 || completed < prev.bytes {
		return 0
	}
	return int64(float64(completed-prev.bytes) / elapsed)
}
