package bulkimport

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/datahearth/streamline/ent"
	entimportscan "github.com/datahearth/streamline/ent/importscan"
	"github.com/datahearth/streamline/internal/db"
	"github.com/datahearth/streamline/internal/library"
)

// runScanSeries is the series counterpart of runScan. A series scan reviews one
// entry per top-level folder rather than per file, because a show is adopted as
// a unit — the committer re-walks the folder and matches episodes itself.
func (s *Service) runScanSeries(ctx context.Context, scan *ent.ImportScan) {
	ctx, span := tracer.Start(ctx, "bulkimport.scan_series",
		trace.WithAttributes(
			attribute.Int64("scan.id", int64(scan.ID)),
			attribute.String("scan.mode", string(scan.Mode)),
		))
	defer span.End()

	defer func() {
		if r := recover(); r != nil {
			s.markScanFailed(ctx, scan.ID, fmt.Sprintf("panic: %v", r))
		}
	}()

	entries, err := os.ReadDir(scan.SourcePath)
	if err != nil {
		s.markScanFailed(ctx, scan.ID, err.Error())
		return
	}

	// Upper bound only: entries that are not directories, or hold no usable
	// video file, are skipped below. total_count is corrected to the real
	// queue length once the walk is done.
	total := uint32(len(entries))
	if err := s.store.UpdateImportScanStatus(
		ctx,
		scan.ID,
		entimportscan.StatusRunning,
		db.UpdateScanStatusOpts{TotalCount: &total},
	); err != nil {
		slog.WarnContext(ctx, "series scan: failed to set total_count",
			"scan.id", scan.ID, "error", err)
	}

	trackedByTVDB, err := s.trackedShowsByTVDB(ctx)
	if err != nil {
		slog.WarnContext(ctx, "series scan: tracked-show lookup failed",
			"scan.id", scan.ID, "error", err)
		trackedByTVDB = map[uint32]uint32{}
	}

	queue := make([]db.CreateImportScanShowParams, 0, len(entries))
	lastPoll := time.Now()
	for _, e := range entries {
		if time.Since(lastPoll) > cancellationPollEvery {
			lastPoll = time.Now()
			cur, ferr := s.store.FindImportScan(ctx, scan.ID)
			if ferr == nil && cur.Status != entimportscan.StatusRunning {
				return
			}
		}
		if !e.IsDir() {
			continue
		}
		folder := filepath.Join(scan.SourcePath, e.Name())
		files, lerr := library.ListVideoFilesRecursive(folder)
		if lerr != nil {
			slog.WarnContext(ctx, "series scan: folder walk failed",
				"folder", folder, "error", lerr)
		}
		if len(files) == 0 {
			continue
		}

		p := library.Parse(e.Name())
		hits, herr := s.tvmeta.SearchSeries(ctx, p.Title)
		if herr != nil {
			slog.WarnContext(ctx, "series scan: tvdb lookup failed",
				"folder", e.Name(), "error", herr)
		}
		c := ClassifyShow(p.Title, p.Year, hits, trackedByTVDB)
		//nolint:gosec // a folder holds far fewer than 65k episode files
		queue = append(queue, BuildShowParams(folder, p, c, uint16(len(files))))

		if err := s.store.IncrementImportScanProgress(ctx, scan.ID, 1); err != nil {
			slog.WarnContext(ctx, "series scan: failed to increment progress",
				"scan.id", scan.ID, "error", err)
		}
	}

	if len(queue) > 0 {
		if err := s.store.BulkCreateImportScanShows(
			ctx,
			scan.ID,
			queue,
		); err != nil {
			s.markScanFailed(ctx, scan.ID, err.Error())
			return
		}
	}

	scannedAt := time.Now()
	queued := uint32(len(queue))
	if err := s.store.UpdateImportScanStatus(
		ctx,
		scan.ID,
		entimportscan.StatusAwaitingReview,
		db.UpdateScanStatusOpts{ScannedAt: &scannedAt, TotalCount: &queued},
	); err != nil {
		slog.ErrorContext(ctx, "series scan: failed to flip scan to awaiting_review",
			"scan.id", scan.ID, "error", err)
	}
	slog.InfoContext(ctx, "series scan finished",
		"scan.id", scan.ID, "shows.queued", len(queue))
}

// trackedShowsByTVDB maps tvdb_id → tracked tvshow id so the classifier can flag
// a scanned folder as "existing" rather than proposing a duplicate show.
func (s *Service) trackedShowsByTVDB(
	ctx context.Context,
) (map[uint32]uint32, error) {
	shows, err := s.store.ListTvShowsForAdoption(ctx)
	if err != nil {
		return nil, err
	}
	out := make(map[uint32]uint32, len(shows))
	for _, sh := range shows {
		if sh.TvdbID != 0 {
			out[sh.TvdbID] = sh.ID
		}
	}
	return out, nil
}
