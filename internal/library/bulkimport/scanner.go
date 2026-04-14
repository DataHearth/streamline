package bulkimport

import (
	"context"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/datahearth/streamline/ent"
	entimportscan "github.com/datahearth/streamline/ent/importscan"
	"github.com/datahearth/streamline/internal/db"
	"github.com/datahearth/streamline/internal/library"
	"github.com/datahearth/streamline/internal/otelx"
)

var tracer = otel.Tracer(
	"github.com/datahearth/streamline/internal/library/bulkimport",
)

// StartScan validates params, creates the ImportScan row, and kicks off the scan
// goroutine. The goroutine uses context.WithoutCancel because the originating
// HTTP request ends within milliseconds; cancellation flows through DB state.
func (s *Service) StartScan(
	ctx context.Context,
	p StartScanParams,
) (*ent.ImportScan, error) {
	ctx, span := tracer.Start(ctx, "bulkimport.start_scan",
		trace.WithAttributes(attribute.String("scan.mode", string(p.Mode))),
	)
	defer span.End()

	resolved, err := s.validateScanParams(p)
	if err != nil {
		return nil, otelx.RecordSpanError(span, err)
	}
	// import_mode override only meaningful in rename mode; silently clear otherwise
	// so callers don't have to special-case it in the UI.
	if p.Mode != entimportscan.ModeRename {
		p.ImportMode = ""
	}

	n, err := s.store.CountActiveImportScans(ctx)
	if err != nil {
		return nil, otelx.RecordSpanError(
			span,
			fmt.Errorf("count active scans: %w", err),
		)
	}
	if n > 0 {
		return nil, otelx.RecordSpanError(span, ErrScanRunning)
	}

	scan, err := s.store.CreateImportScan(ctx, db.CreateImportScanParams{
		SourcePath: resolved,
		Mode:       p.Mode,
		ImportMode: p.ImportMode,
	})
	if err != nil {
		return nil, otelx.RecordSpanError(
			span,
			fmt.Errorf("create import scan: %w", err),
		)
	}
	span.SetAttributes(attribute.Int64("scan.id", int64(scan.ID)))

	bg := context.WithoutCancel(ctx)
	go s.runScan(bg, scan)
	slog.InfoContext(ctx, "bulk import scan started",
		"scan.id", scan.ID, "scan.mode", scan.Mode, "scan.source_path", resolved)

	return scan, nil
}

func (s *Service) validateScanParams(p StartScanParams) (string, error) {
	if !filepath.IsAbs(p.SourcePath) {
		return "", ErrInvalidPath
	}
	resolved, err := filepath.EvalSymlinks(p.SourcePath)
	if err != nil {
		return "", ErrInvalidPath
	}
	info, err := os.Stat(resolved)
	if err != nil || !info.IsDir() {
		return "", ErrInvalidPath
	}

	libAbs, err := filepath.EvalSymlinks(s.libraryDir)
	if err != nil {
		return "", ErrInvalidPath
	}
	inside := strings.HasPrefix(resolved, libAbs+string(filepath.Separator)) ||
		resolved == libAbs

	switch p.Mode {
	case entimportscan.ModeInPlace:
		if !inside {
			return "", ErrPathOutsideLibrary
		}
	case entimportscan.ModeRename:
		if inside {
			return "", ErrPathOutsideLibrary
		}
	}
	return resolved, nil
}

type scannedCandidate struct {
	path string
	size int64
}

func (s *Service) runScan(ctx context.Context, scan *ent.ImportScan) {
	ctx, span := tracer.Start(ctx, "bulkimport.scan",
		trace.WithAttributes(
			attribute.Int64("scan.id", int64(scan.ID)),
			attribute.String("scan.mode", string(scan.Mode)),
		))
	defer span.End()

	defer func() {
		if r := recover(); r != nil {
			reason := fmt.Sprintf("panic: %v", r)
			now := time.Now()
			if err := s.store.UpdateImportScanStatus(
				ctx,
				scan.ID,
				entimportscan.StatusFailed,
				db.UpdateScanStatusOpts{
					FailureReason: &reason,
					ScannedAt:     &now,
				},
			); err != nil {
				slog.ErrorContext(
					ctx,
					"bulk import: failed to mark scan failed after panic",
					"scan.id",
					scan.ID,
					"panic",
					r,
					"error",
					err,
				)
			}
		}
	}()

	candidates, err := walkSourceDir(ctx, scan.SourcePath)
	if err != nil {
		reason := err.Error()
		now := time.Now()
		if uerr := s.store.UpdateImportScanStatus(
			ctx,
			scan.ID,
			entimportscan.StatusFailed,
			db.UpdateScanStatusOpts{
				FailureReason: &reason,
				ScannedAt:     &now,
			},
		); uerr != nil {
			slog.ErrorContext(
				ctx,
				"bulk import: failed to mark scan failed after walk error",
				"scan.id",
				scan.ID,
				"walk.error",
				err,
				"update.error",
				uerr,
			)
		}
		return
	}
	totalU32 := uint32(len(candidates))
	if err := s.store.UpdateImportScanStatus(
		ctx,
		scan.ID,
		entimportscan.StatusRunning,
		db.UpdateScanStatusOpts{TotalCount: &totalU32},
	); err != nil {
		slog.WarnContext(ctx, "bulk import: failed to set total_count",
			"scan.id", scan.ID, "error", err)
	}

	alreadyAdded, err := s.buildExistingMap(ctx)
	if err != nil {
		slog.WarnContext(
			ctx,
			"bulk import: existing-map lookup failed",
			"scan.id",
			scan.ID,
			"error",
			err,
		)
		alreadyAdded = map[uint32]uint32{}
	}

	if !s.runMatchPhase(ctx, scan, candidates, alreadyAdded) {
		return
	}

	scannedAt := time.Now()
	if err := s.store.UpdateImportScanStatus(
		ctx,
		scan.ID,
		entimportscan.StatusAwaitingReview,
		db.UpdateScanStatusOpts{ScannedAt: &scannedAt},
	); err != nil {
		slog.ErrorContext(ctx, "bulk import: failed to flip scan to awaiting_review",
			"scan.id", scan.ID, "error", err)
	}
	slog.InfoContext(
		ctx,
		"bulk import scan finished",
		"scan.id",
		scan.ID,
		"scan.outcome",
		"awaiting_review",
		"scan.total_count",
		totalU32,
	)
}

func walkSourceDir(ctx context.Context, root string) ([]scannedCandidate, error) {
	var out []scannedCandidate
	err := filepath.WalkDir(
		root,
		func(path string, d fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				slog.WarnContext(
					ctx,
					"bulk import scan walk error skipped",
					"path",
					path,
					"error",
					walkErr,
				)
				return nil
			}
			if d.IsDir() {
				return nil
			}
			if !library.MediaExts[strings.ToLower(filepath.Ext(path))] {
				return nil
			}
			info, err := d.Info()
			if err != nil || info.Size() < library.MinMediaSize {
				return nil
			}
			if library.SampleRe.MatchString(filepath.Base(path)) {
				return nil
			}
			out = append(out, scannedCandidate{path: path, size: info.Size()})
			return nil
		},
	)
	if err != nil {
		return nil, fmt.Errorf("walk source dir: %w", err)
	}
	return out, nil
}

func (s *Service) buildExistingMap(ctx context.Context) (map[uint32]uint32, error) {
	movies, err := s.store.ListMovies(ctx, 0, 1<<31-1)
	if err != nil {
		return nil, err
	}
	m := make(map[uint32]uint32, len(movies))
	for _, mv := range movies {
		m[mv.TmdbID] = mv.ID
	}
	return m, nil
}

func (s *Service) runMatchPhase(
	ctx context.Context,
	scan *ent.ImportScan,
	candidates []scannedCandidate,
	alreadyAdded map[uint32]uint32,
) bool {
	sem := make(chan struct{}, scanConcurrency)
	var batchMu sync.Mutex
	batch := make([]db.CreateImportScanFileParams, 0, bulkInsertBatchSize)
	var wg sync.WaitGroup
	lastPoll := time.Now()
	stillActive := true

	flush := func() {
		if len(batch) == 0 {
			return
		}
		toFlush := batch
		batch = make([]db.CreateImportScanFileParams, 0, bulkInsertBatchSize)
		batchMu.Unlock()
		if err := s.store.BulkCreateImportScanFiles(
			ctx,
			scan.ID,
			toFlush,
		); err != nil {
			slog.ErrorContext(ctx, "bulk import: failed to persist file batch",
				"scan.id", scan.ID, "batch.size", len(toFlush), "error", err)
		}
		if err := s.store.IncrementImportScanProgress(
			ctx,
			scan.ID,
			uint32(len(toFlush)),
		); err != nil {
			slog.WarnContext(ctx, "bulk import: failed to increment progress",
				"scan.id", scan.ID, "delta", len(toFlush), "error", err)
		}
		batchMu.Lock()
	}

	for _, c := range candidates {
		if time.Since(lastPoll) > cancellationPollEvery {
			lastPoll = time.Now()
			cur, err := s.store.FindImportScan(ctx, scan.ID)
			if err == nil && cur.Status != entimportscan.StatusRunning {
				stillActive = false
				break
			}
		}
		sem <- struct{}{}
		wg.Add(1)
		go func(c scannedCandidate) {
			defer wg.Done()
			defer func() { <-sem }()
			row := s.classifyOne(ctx, c, alreadyAdded)
			batchMu.Lock()
			batch = append(batch, row)
			if len(batch) >= bulkInsertBatchSize {
				flush()
			}
			batchMu.Unlock()
		}(c)
	}
	wg.Wait()
	batchMu.Lock()
	flush()
	batchMu.Unlock()
	return stillActive
}

func (s *Service) classifyOne(
	ctx context.Context,
	c scannedCandidate,
	alreadyAdded map[uint32]uint32,
) db.CreateImportScanFileParams {
	parsed := library.Parse(filepath.Base(c.path))
	hits, err := s.metadata.SearchMovie(ctx, parsed.Title, parsed.Year)
	if err != nil {
		slog.WarnContext(ctx, "bulk import tmdb lookup failed",
			"file.basename", filepath.Base(c.path), "error", err)
	}
	cls := Classify(parsed, hits, alreadyAdded)

	row := db.CreateImportScanFileParams{
		SourcePath:         c.path,
		Size:               c.size,
		ParsedTitle:        parsed.Title,
		ParsedQuality:      parsed.Resolution,
		ParsedReleaseGroup: parsed.Group,
		Classification:     cls.Kind,
		Candidates:         cls.Candidates,
		TMDBID:             cls.TMDBID,
		ExistingMovieID:    cls.ExistingMovieID,
	}
	if parsed.Year != 0 {
		y := parsed.Year
		row.ParsedYear = &y
	}
	return row
}
