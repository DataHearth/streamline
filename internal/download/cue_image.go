package download

import (
	"context"
	"log/slog"
	"path"
	"slices"
	"strings"

	"go.opentelemetry.io/otel/attribute"

	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/library"
	"github.com/datahearth/streamline/internal/otelx"
)

// cueImageFor reports a release whose files are disc images of the album:
// what the album importer refuses. An album that cannot be read is given the
// benefit of the doubt, since the importer still checks it.
func (d *download) cueImageFor(
	ctx context.Context,
	albumID uint32,
	paths []string,
) bool {
	if !slices.ContainsFunc(paths, func(p string) bool {
		return strings.EqualFold(path.Ext(p), ".cue")
	}) {
		return false
	}
	alb, err := d.db.FindAlbumByID(ctx, albumID)
	if err != nil {
		slog.WarnContext(ctx, "cue image check: album lookup failed",
			"album.id", albumID, "error", err)
		return false
	}
	return library.CueImage(paths, len(alb.Edges.Tracks))
}

// RejectCueImages drops every album download whose files, once its client
// knows them, are disc images split by a CUE sheet: the torrent and its data are
// removed, the record is failed as rejected so the automatic searches skip
// that release, and the album goes back to wanted with no strike. A .torrent
// grab is checked before it is added; this is the magnet's check, which can
// only run once the client has fetched the metadata.
//
// A record is listed once its files are known, then remembered until it stops
// downloading, so a long download costs one listing rather than one a tick.
func (d *download) RejectCueImages(ctx context.Context) error {
	ctx, span := tracer.Start(ctx, "download.reject_cue_images")
	defer span.End()

	records, err := d.db.ListDownloadingAlbumRecords(ctx)
	if err != nil {
		return otelx.RecordSpanError(span, err)
	}

	d.cueMu.Lock()
	defer d.cueMu.Unlock()
	checked := make(map[uint32]struct{}, len(records))
	rejected := 0
	for _, rec := range records {
		if _, ok := d.cueChecked[rec.ID]; ok {
			checked[rec.ID] = struct{}{}
			continue
		}
		dc, ok := config.FindDownloadClient(rec.DownloadClientName)
		if !ok {
			continue
		}
		client, err := d.buildClient(dc)
		if err != nil {
			continue
		}
		files, err := client.ListFiles(ctx, rec.TorrentHash)
		if err != nil || len(files) == 0 {
			continue
		}
		checked[rec.ID] = struct{}{}
		paths := make([]string, len(files))
		for i, f := range files {
			paths[i] = f.Path
		}
		if !d.cueImageFor(ctx, rec.Edges.Album.ID, paths) {
			continue
		}
		slog.InfoContext(ctx, "rejecting a CUE disc image download",
			"record.id", rec.ID, "album.id", rec.Edges.Album.ID,
			"torrent", rec.Title, "hash", rec.TorrentHash)
		if err := client.RemoveTorrent(ctx, rec.TorrentHash, true); err != nil {
			slog.WarnContext(ctx, "cue image: remove torrent failed",
				"record.id", rec.ID, "hash", rec.TorrentHash, "error", err)
			continue
		}
		if err := d.db.RejectDownloadingRecord(
			ctx, rec.ID, "release is a "+library.CueImageReason,
		); err != nil {
			slog.WarnContext(ctx, "cue image: reject record failed",
				"record.id", rec.ID, "error", err)
			continue
		}
		rejected++
	}
	d.cueChecked = checked
	span.SetAttributes(attribute.Int("cue_images.rejected", rejected))
	return nil
}
