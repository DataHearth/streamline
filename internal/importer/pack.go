package importer

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/library"
	"github.com/datahearth/streamline/internal/mediaserver"
	"github.com/datahearth/streamline/internal/otelx"
)

// importPackRecord imports a completed discography pack: its audio files are
// grouped by folder, each folder is matched to one of the albums the record
// was linked to by title (and year), and each match goes through the ordinary
// album import. The record completes when every match is done; the linked
// albums no folder named go back to wanted, with no failure counted against
// them. A folder that matches nothing is ignored, and a pack none of whose
// folders match fails like an album record with no matching track.
func (w *Worker) importPackRecord(
	ctx context.Context,
	span trace.Span,
	rec *ent.DownloadRecord,
	libCfg config.LibraryConfig,
) error {
	artist := rec.Edges.Artist
	linked := rec.Edges.Albums
	span.SetAttributes(
		attribute.Int64("artist.id", int64(artist.ID)),
		attribute.Int("pack.albums", len(linked)),
	)
	files, err := listAudioFiles(rec.SavePath)
	if err != nil {
		return otelx.RecordSpanError(span, fmt.Errorf("list pack files: %w", err))
	}
	byDir := map[string][]string{}
	for _, f := range files {
		d := library.PackAlbumDir(f)
		byDir[d] = append(byDir[d], f)
	}
	dirs := make([]string, 0, len(byDir))
	for d := range byDir {
		dirs = append(dirs, d)
	}
	slices.Sort(dirs)

	matched := map[uint32]struct{}{}
	var errs []error
	for _, d := range dirs {
		alb := library.MatchPackDir(d, linked)
		if alb == nil {
			slog.InfoContext(ctx, "pack import: folder matched no linked album",
				"dir", d, "record.id", rec.ID)
			continue
		}
		if _, dup := matched[alb.ID]; dup {
			continue
		}
		matched[alb.ID] = struct{}{}
		held, err := w.importAlbumFiles(ctx, span, rec, alb, byDir[d], libCfg, true)
		switch {
		case held:
			return nil
		case errors.Is(err, library.ErrDestExists):
			// Every track of the folder already holds a file: a re-run after
			// a partial failure, or a pack that adds nothing.
		case err != nil:
			slog.WarnContext(ctx, "pack import: album import failed",
				"album.id", alb.ID, "dir", d, "error", err)
			errs = append(errs, fmt.Errorf("album %d: %w", alb.ID, err))
		}
	}
	if len(matched) == 0 {
		return otelx.RecordSpanError(span, ErrNoAlbumTracks)
	}
	if len(errs) > 0 {
		return otelx.RecordSpanError(span, errors.Join(errs...))
	}

	var unmatched []uint32
	for _, a := range linked {
		if _, ok := matched[a.ID]; !ok {
			unmatched = append(unmatched, a.ID)
		}
	}
	if err := w.db.CompletePackRecord(ctx, rec.ID, unmatched); err != nil {
		return otelx.RecordSpanError(
			span,
			fmt.Errorf("complete pack record: %w", err),
		)
	}
	slog.InfoContext(ctx, "imported discography pack",
		"artist.id", artist.ID, "albums", len(matched), "unmatched", len(unmatched))

	libCfg.ImportMode = albumImportMode(libCfg)
	w.cleanupTorrent(ctx, rec, libCfg)
	w.refreshMediaServers(ctx, mediaserver.KindMusic, libCfg.MusicPath)
	return nil
}
