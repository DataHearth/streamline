package bittorrent

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"time"

	antorrent "github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/storage"

	"github.com/datahearth/streamline/internal/download"
	"github.com/datahearth/streamline/internal/otelx"
)

// magnetProbeTimeout bounds how long a probe waits on a swarm for a magnet's
// metadata. A person is waiting on the answer.
const magnetProbeTimeout = 30 * time.Second

// encodeMetainfo is t's .torrent: its info and the trackers it knows.
func encodeMetainfo(t *antorrent.Torrent) ([]byte, error) {
	mi := t.Metainfo()
	var buf bytes.Buffer
	if err := mi.Write(&buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// ProbeMagnet fetches a magnet's metainfo from its swarm without adding the
// torrent to the engine. It runs on a client of its own, built like the
// engine's and closed before it returns, so the probe is never in the torrent
// list, never claims a download folder and cannot be mistaken for a grab of
// the same magnet. A torrent the engine already holds answers from there.
func (e *Engine) ProbeMagnet(ctx context.Context, magnet string) ([]byte, error) {
	ctx, span := tracer.Start(ctx, "bittorrent.probe_magnet")
	defer span.End()

	spec, _, _, err := specFromSource(download.TorrentSource{Magnet: magnet})
	if err != nil {
		return nil, otelx.RecordSpanError(span, err)
	}
	if t, ok := e.client.Torrent(spec.InfoHash); ok && t.Info() != nil {
		raw, err := encodeMetainfo(t)
		return raw, otelx.RecordSpanError(span, err)
	}
	spec.Trackers, _ = dropUnusableTrackers(spec.Trackers)

	e.probing.Lock()
	defer e.probing.Unlock()
	ctx, cancel := context.WithTimeout(ctx, magnetProbeTimeout)
	defer cancel()

	// Under the session dir: the one place the engine is sure it can write.
	dir, err := os.MkdirTemp(sessionDir(e.downloadDir), "probe-")
	if err != nil {
		return nil, otelx.RecordSpanError(span, fmt.Errorf("probe dir: %w", err))
	}
	defer func() {
		if err := os.RemoveAll(dir); err != nil {
			slog.WarnContext(ctx, "removing the magnet probe dir failed",
				"error", err)
		}
	}()
	st := storage.NewFileWithCompletion(dir, storage.NewMapPieceCompletion())
	defer closeAndWarn(ctx, "probe storage", st)

	peers, err := newPeerClient(ctx, e.entry, e.bindIP, st, 0)
	if err != nil {
		return nil, otelx.RecordSpanError(span, err)
	}
	defer func() {
		if err := errors.Join(peers.client.Close()...); err != nil {
			slog.WarnContext(ctx, "closing the magnet probe client failed",
				"error", err)
		}
		closeAndWarn(ctx, "probe listener", peers.listener)
		closeAndWarn(ctx, "probe packet conn", peers.packetConn)
	}()

	t, _, err := peers.client.AddTorrentSpec(spec)
	if err != nil {
		return nil, otelx.RecordSpanError(span, fmt.Errorf("probe magnet: %w", err))
	}
	select {
	case <-t.GotInfo():
	case <-ctx.Done():
		return nil, otelx.RecordSpanError(span, fmt.Errorf(
			"%w: no peer handed over the metadata", download.ErrFileListUnavailable,
		))
	}
	raw, err := encodeMetainfo(t)
	return raw, otelx.RecordSpanError(span, err)
}
