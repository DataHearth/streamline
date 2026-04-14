package hygiene

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/datahearth/streamline/ent"
	entimportscan "github.com/datahearth/streamline/ent/importscan"
	entimportscanfile "github.com/datahearth/streamline/ent/importscanfile"
	entmediafile "github.com/datahearth/streamline/ent/mediafile"
	entmovie "github.com/datahearth/streamline/ent/movie"
	"github.com/datahearth/streamline/internal/db"
	"github.com/datahearth/streamline/internal/library"
	"github.com/datahearth/streamline/internal/library/bulkimport"
	"github.com/datahearth/streamline/internal/otelx"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// RunOrphanScan walks library.movie_path, classifies untracked media files
// against TMDB, and either auto-imports high-confidence matches or enqueues
// the rest into a bulk-import scan for human review.
func (s *Service) RunOrphanScan(ctx context.Context) error {
	ctx, span := tracer.Start(ctx, "hygiene.orphan_scan")
	defer span.End()

	if _, err := os.Stat(s.cfg.MoviePath); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			slog.InfoContext(ctx, "library path missing; skipping orphan scan",
				"path", s.cfg.MoviePath)
			return nil
		}
		return otelx.RecordSpanError(span, fmt.Errorf("stat library root: %w", err))
	}

	tracked, err := s.trackedPathSet(ctx)
	if err != nil {
		return otelx.RecordSpanError(span, err)
	}
	pending, err := s.pendingPathSet(ctx)
	if err != nil {
		return otelx.RecordSpanError(span, err)
	}

	var queue []queuedOrphan
	walkErr := filepath.WalkDir(
		s.cfg.MoviePath,
		func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				slog.WarnContext(ctx, "walk error", "path", p, "error", err)
				orphanWalkErrors.Add(ctx, 1)
				return nil
			}
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if d.IsDir() {
				return nil
			}
			if !library.MediaExts[strings.ToLower(filepath.Ext(p))] {
				return nil
			}
			info, statErr := d.Info()
			if statErr != nil || info.Size() < library.MinMediaSize {
				return nil
			}
			if library.SampleRe.MatchString(filepath.Base(p)) {
				return nil
			}
			if _, ok := tracked[p]; ok {
				orphanSkipped.Add(
					ctx,
					1,
					metric.WithAttributes(attribute.String("reason", "tracked")),
				)
				return nil
			}
			if _, ok := pending[p]; ok {
				orphanSkipped.Add(
					ctx,
					1,
					metric.WithAttributes(
						attribute.String("reason", "pending_review"),
					),
				)
				return nil
			}
			cand, classified, classifyErr := s.classifyOrphan(ctx, p, info.Size())
			if classifyErr != nil {
				slog.WarnContext(
					ctx,
					"classify failed",
					"path",
					p,
					"error",
					classifyErr,
				)
				orphanMatchErrors.Add(ctx, 1)
				return nil
			}
			switch s.handleOrphan(ctx, cand, classified) {
			case outcomeImported, outcomeSkip:
			case outcomeQueue:
				queue = append(
					queue,
					queuedOrphan{cand: cand, classified: classified},
				)
			}
			return nil
		},
	)
	if walkErr != nil {
		return otelx.RecordSpanError(span, walkErr)
	}

	if len(queue) > 0 {
		if err := s.queueAsImportScan(ctx, queue); err != nil {
			return otelx.RecordSpanError(span, err)
		}
	}
	return nil
}

type orphanCandidate struct {
	Path   string
	Size   int64
	Parsed library.ParseResult
}

type queuedOrphan struct {
	cand       orphanCandidate
	classified bulkimport.Classification
}

func (s *Service) trackedPathSet(ctx context.Context) (map[string]struct{}, error) {
	rows, err := s.store.ListAllMediaFilesWithMovie(ctx)
	if err != nil {
		return nil, err
	}
	out := make(map[string]struct{}, len(rows))
	for _, r := range rows {
		out[r.Path] = struct{}{}
	}
	return out, nil
}

func (s *Service) pendingPathSet(ctx context.Context) (map[string]struct{}, error) {
	paths, err := s.store.ListPendingImportScanFilePaths(ctx)
	if err != nil {
		return nil, err
	}
	out := make(map[string]struct{}, len(paths))
	for _, p := range paths {
		out[p] = struct{}{}
	}
	return out, nil
}

func (s *Service) classifyOrphan(
	ctx context.Context, path string, size int64,
) (orphanCandidate, bulkimport.Classification, error) {
	parsed := library.Parse(filepath.Base(path))
	hits, err := s.metadata.SearchMovie(ctx, parsed.Title, parsed.Year)
	if err != nil {
		return orphanCandidate{}, bulkimport.Classification{}, err
	}
	// alreadyAdded is empty for the orphan path: MovieHasMediaFile covers the
	// "already tracked" gate explicitly inside handleOrphan.
	classified := bulkimport.Classify(parsed, hits, map[uint32]uint32{})
	return orphanCandidate{Path: path, Size: size, Parsed: parsed}, classified, nil
}

// orphanOutcome is what handleOrphan decided to do with one candidate.
type orphanOutcome int

const (
	outcomeQueue    orphanOutcome = iota // needs human review
	outcomeImported                      // auto-imported into an existing movie
	outcomeSkip                          // ignore (movie already satisfied)
)

// handleOrphan decides what to do with one orphan candidate. A confirmed match
// whose movie already has its file is skipped, not queued: import_mode hardlink
// (the default) and copy both leave the source in place, so re-queueing it would
// file a fresh review line on every scan.
func (s *Service) handleOrphan(
	ctx context.Context,
	cand orphanCandidate,
	classified bulkimport.Classification,
) orphanOutcome {
	if classified.Kind != entimportscanfile.ClassificationConfirmed {
		return outcomeQueue
	}
	if cand.Parsed.Year == 0 {
		return outcomeQueue
	}
	has, err := s.store.MovieHasMediaFile(ctx, classified.TMDBID)
	if err != nil {
		slog.WarnContext(ctx, "MovieHasMediaFile failed",
			"tmdb_id", classified.TMDBID, "error", err)
		return outcomeQueue
	}
	if has {
		orphanSkipped.Add(ctx, 1, metric.WithAttributes(
			attribute.String("reason", "already_imported")))
		return outcomeSkip
	}

	movie, err := s.store.FindMovieByTMDBID(ctx, classified.TMDBID)
	if err != nil && !ent.IsNotFound(err) {
		slog.WarnContext(ctx, "FindMovieByTMDBID failed",
			"tmdb_id", classified.TMDBID, "error", err)
		return outcomeQueue
	}
	if movie == nil {
		return outcomeQueue
	}

	imported, err := s.importer.ImportMovieWithMode(
		ctx, filepath.Dir(cand.Path), movie, "", "",
	)
	if err != nil {
		orphanImportFailed.Add(ctx, 1)
		slog.ErrorContext(ctx, "auto-import failed",
			"path", cand.Path, "error", err)
		return outcomeQueue
	}
	if _, err := s.store.CreateMediaFile(ctx, db.CreateMediaFileParams{
		MovieID:      movie.ID,
		Path:         imported.Path,
		Size:         imported.Size,
		Quality:      cand.Parsed.Resolution,
		ReleaseGroup: cand.Parsed.Group,
		Source:       entmediafile.SourceAuto,
	}); err != nil {
		orphanImportFailed.Add(ctx, 1)
		slog.ErrorContext(ctx, "auto-import: CreateMediaFile failed",
			"movie_id", movie.ID, "path", imported.Path, "error", err)
		return outcomeQueue
	}
	if err := s.store.UpdateMovieStatus(
		ctx,
		movie.ID,
		entmovie.StatusAvailable,
	); err != nil {
		slog.WarnContext(ctx, "auto-import: failed to flip movie status",
			"movie_id", movie.ID, "error", err)
	}
	orphanAutoImported.Add(ctx, 1)
	slog.InfoContext(ctx, "orphan auto-imported",
		"movie.id", movie.ID,
		"movie.tmdb_id", movie.TmdbID,
		"media_file.path", imported.Path)
	return outcomeImported
}

func (s *Service) queueAsImportScan(
	ctx context.Context,
	queue []queuedOrphan,
) error {
	fileParams := make([]db.CreateImportScanFileParams, 0, len(queue))
	for _, q := range queue {
		year := q.cand.Parsed.Year
		var yearPtr *uint16
		if year != 0 {
			yearPtr = &year
		}
		fileParams = append(fileParams, db.CreateImportScanFileParams{
			SourcePath:         q.cand.Path,
			Size:               q.cand.Size,
			ParsedTitle:        q.cand.Parsed.Title,
			ParsedYear:         yearPtr,
			ParsedQuality:      q.cand.Parsed.Resolution,
			ParsedReleaseGroup: q.cand.Parsed.Group,
			Classification:     q.classified.Kind,
			Candidates:         q.classified.Candidates,
			TMDBID:             q.classified.TMDBID,
			ExistingMovieID:    q.classified.ExistingMovieID,
		})
	}

	// Fold new orphans into the directory's existing review queue instead of
	// minting a fresh scan each run. Orphans trickle in across runs (a large
	// or migrated library classifies over several passes), and one directory
	// should surface as one review entry, not a new one on every restart.
	scanID, err := s.openReviewScanID(ctx, s.cfg.MoviePath, entimportscan.KindMovie)
	if err != nil {
		return err
	}
	if err := s.store.BulkCreateImportScanFiles(
		ctx,
		scanID,
		fileParams,
	); err != nil {
		return fmt.Errorf("bulk-create import_scan_files: %w", err)
	}
	orphanQueued.Add(ctx, int64(len(queue)))
	return nil
}

// openReviewScanID returns the id of the directory's open (awaiting_review)
// orphan scan for sourcePath, creating and flipping a fresh one of the given
// kind when none exists yet. source_path disambiguates movie (movie_path) from
// series (series_path) scans, so FindOpenImportScanForSource stays kind-agnostic.
func (s *Service) openReviewScanID(
	ctx context.Context, sourcePath string, kind entimportscan.Kind,
) (uint32, error) {
	existing, err := s.store.FindOpenImportScanForSource(ctx, sourcePath)
	if err == nil {
		return existing.ID, nil
	}
	if !ent.IsNotFound(err) {
		return 0, fmt.Errorf("find open import_scan: %w", err)
	}

	scan, err := s.store.CreateImportScan(ctx, db.CreateImportScanParams{
		SourcePath: sourcePath,
		Kind:       kind,
		Mode:       entimportscan.ModeInPlace,
	})
	if err != nil {
		return 0, fmt.Errorf("create hygiene import_scan: %w", err)
	}
	if err := s.store.UpdateImportScanStatus(
		ctx, scan.ID, entimportscan.StatusAwaitingReview, db.UpdateScanStatusOpts{},
	); err != nil {
		return 0, fmt.Errorf("flip scan to awaiting_review: %w", err)
	}
	return scan.ID, nil
}
