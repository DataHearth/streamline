package download

import (
	"context"
	"errors"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/indexer"
	"github.com/datahearth/streamline/internal/otelx"
)

// ErrFileListUnavailable is returned by ReleaseFiles for a magnet whose file
// list could not be had: it lives in the metainfo only peers can hand over,
// and the download client either cannot ask them without adding the torrent
// or got no answer.
var ErrFileListUnavailable = errors.New(
	"a magnet link carries no file list until a client fetches it from peers",
)

// magnetProber is a client that can fetch a magnet's metainfo from its swarm
// for a person's preview, at a cost a grab should not pay: the builtin
// engine starts a client of its own for it.
type magnetProber interface {
	ProbeMagnet(ctx context.Context, magnet string) ([]byte, error)
}

// magnetMetainfo asks the download client a grab would use for the magnet's
// metainfo, without adding the torrent to it.
func (d *download) magnetMetainfo(
	ctx context.Context,
	magnet string,
) ([]byte, error) {
	dc, ok := config.PickDownloadClient()
	if !ok {
		return nil, ErrFileListUnavailable
	}
	client, err := d.buildClient(dc)
	if err != nil {
		return nil, err
	}
	switch c := client.(type) {
	case MagnetMetadataFetcher:
		return c.FetchMagnetMetadata(ctx, magnet)
	case magnetProber:
		return c.ProbeMagnet(ctx, magnet)
	}
	return nil, ErrFileListUnavailable
}

// ReleaseFile is one file of a release's torrent, as its metainfo lists it.
type ReleaseFile struct {
	Path string
	Size int64
}

// ReleaseFiles fetches the release's .torrent through the same guarded path a
// grab takes and lists its files, without adding anything to a client. It is
// for a person deciding whether to grab, so it is never called on a search's
// whole result set: every call is one request to the indexer.
func (d *download) ReleaseFiles(
	ctx context.Context,
	result indexer.SearchResult,
) ([]ReleaseFile, error) {
	ctx, span := tracer.Start(ctx, "download.release_files",
		trace.WithAttributes(
			attribute.String("release.title", result.Title),
			attribute.String("indexer.name", result.Indexer),
		))
	defer span.End()

	if err := checkReleaseSource(result.Download); err != nil {
		return nil, otelx.RecordSpanError(span, err)
	}
	src, err := resolveTorrentSource(ctx, d.db, result.Download)
	if err != nil {
		return nil, otelx.RecordSpanError(span, err)
	}
	if src.Magnet != "" {
		if src.Bytes, err = d.magnetMetainfo(ctx, src.Magnet); err != nil {
			return nil, otelx.RecordSpanError(span, err)
		}
	}
	files, err := decodeTorrentFiles(src.Bytes)
	if err != nil {
		return nil, otelx.RecordSpanError(span, err)
	}
	out := make([]ReleaseFile, 0, len(files))
	for _, f := range files {
		out = append(out, ReleaseFile{Path: f.Path, Size: f.Size})
	}
	span.SetAttributes(attribute.Int("files", len(out)))
	return out, nil
}
