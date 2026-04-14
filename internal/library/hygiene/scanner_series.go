package hygiene

import (
	"context"
	"errors"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"

	entimportscan "github.com/datahearth/streamline/ent/importscan"
	"github.com/datahearth/streamline/internal/db"
	"github.com/datahearth/streamline/internal/library"
	"github.com/datahearth/streamline/internal/otelx"
)

// RunSeriesOrphanScan walks series_path's top-level folders, classifies each
// untracked show against TVDB, and appends it to the directory's open review
// scan — one series import entry per directory, consolidated across runs. Shows
// whose files are all already tracked, or that are already queued for review,
// are skipped. Adoption happens on commit, not here (review-only).
func (s *Service) RunSeriesOrphanScan(ctx context.Context) error {
	ctx, span := tracer.Start(ctx, "hygiene.series_orphan_scan")
	defer span.End()

	if _, err := os.Stat(s.cfg.SeriesPath); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			slog.InfoContext(ctx, "series path missing; skipping series orphan scan",
				"path", s.cfg.SeriesPath)
			return nil
		}
		return otelx.RecordSpanError(span, err)
	}

	tracked, err := s.trackedEpisodePathSet(ctx)
	if err != nil {
		return otelx.RecordSpanError(span, err)
	}
	pending, err := s.pendingShowFolderSet(ctx)
	if err != nil {
		return otelx.RecordSpanError(span, err)
	}
	trackedByTVDB, err := s.trackedShowsByTVDB(ctx)
	if err != nil {
		return otelx.RecordSpanError(span, err)
	}

	entries, err := os.ReadDir(s.cfg.SeriesPath)
	if err != nil {
		return otelx.RecordSpanError(span, err)
	}

	var queue []db.CreateImportScanShowParams
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		folder := filepath.Join(s.cfg.SeriesPath, e.Name())
		if _, ok := pending[folder]; ok {
			continue
		}
		files := gatherVideoFiles(ctx, folder)
		if len(files) == 0 {
			continue
		}
		if allTracked(files, tracked) {
			continue
		}

		p := library.Parse(e.Name())
		hits, herr := s.tvmeta.SearchSeries(ctx, p.Title)
		if herr != nil {
			slog.WarnContext(ctx, "series tvdb lookup failed",
				"folder", e.Name(), "error", herr)
		}
		c := classifyShow(p.Title, p.Year, hits, trackedByTVDB)
		queue = append(
			queue,
			buildShowParams(folder, p, c, uint16(len(files))),
		) //nolint:gosec // file count is bounded
	}

	if len(queue) == 0 {
		return nil
	}
	scanID, err := s.openReviewScanID(
		ctx,
		s.cfg.SeriesPath,
		entimportscan.KindSeries,
	)
	if err != nil {
		return otelx.RecordSpanError(span, err)
	}
	if err := s.store.BulkCreateImportScanShows(ctx, scanID, queue); err != nil {
		return otelx.RecordSpanError(span, err)
	}
	slog.InfoContext(ctx, "series orphan scan queued shows for review",
		"count", len(queue), "path", s.cfg.SeriesPath)
	return nil
}

func (s *Service) trackedEpisodePathSet(
	ctx context.Context,
) (map[string]struct{}, error) {
	paths, err := s.store.ListAllEpisodeMediaFilePaths(ctx)
	if err != nil {
		return nil, err
	}
	out := make(map[string]struct{}, len(paths))
	for _, p := range paths {
		out[p] = struct{}{}
	}
	return out, nil
}

func (s *Service) pendingShowFolderSet(
	ctx context.Context,
) (map[string]struct{}, error) {
	folders, err := s.store.ListPendingImportScanShowFolders(ctx)
	if err != nil {
		return nil, err
	}
	out := make(map[string]struct{}, len(folders))
	for _, f := range folders {
		out[f] = struct{}{}
	}
	return out, nil
}

// trackedShowsByTVDB maps tvdb_id → tracked tvshow id, so the classifier can
// flag a scanned folder as "existing" rather than proposing a duplicate show.
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

// gatherVideoFiles recursively collects importable media files under root. Thin
// wrapper over library.ListVideoFilesRecursive that logs an unreadable root
// rather than surfacing it — a single bad folder shouldn't fail the whole scan.
func gatherVideoFiles(ctx context.Context, root string) []string {
	files, err := library.ListVideoFilesRecursive(root)
	if err != nil {
		slog.WarnContext(ctx, "series scan: folder walk failed",
			"folder", root, "error", err)
	}
	return files
}

func allTracked(files []string, tracked map[string]struct{}) bool {
	for _, f := range files {
		if _, ok := tracked[f]; !ok {
			return false
		}
	}
	return true
}

func buildShowParams(
	folder string, p library.ParseResult, c showClassification, fileCount uint16,
) db.CreateImportScanShowParams {
	params := db.CreateImportScanShowParams{
		FolderPath:     folder,
		ParsedTitle:    p.Title,
		Classification: c.Kind,
		Candidates:     c.Candidates,
		FileCount:      fileCount,
	}
	if p.Year != 0 {
		year := p.Year
		params.ParsedYear = &year
	}
	if c.TVDBID != 0 {
		id := c.TVDBID
		params.TVDBID = &id
	}
	if c.ExistingTvshowID != 0 {
		id := c.ExistingTvshowID
		params.ExistingTvshowID = &id
	}
	return params
}
