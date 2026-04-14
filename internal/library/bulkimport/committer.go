package bulkimport

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/datahearth/streamline/ent"
	entimportscan "github.com/datahearth/streamline/ent/importscan"
	entimportscanfile "github.com/datahearth/streamline/ent/importscanfile"
	entmediafile "github.com/datahearth/streamline/ent/mediafile"
	entmovie "github.com/datahearth/streamline/ent/movie"
	"github.com/datahearth/streamline/internal/db"
	"github.com/datahearth/streamline/internal/otelx"
)

const commitConcurrency = 4

func (s *Service) Commit(ctx context.Context, id uint32) error {
	ctx, span := tracer.Start(ctx, "bulkimport.commit",
		trace.WithAttributes(attribute.Int64("scan.id", int64(id))))
	defer span.End()

	scan, err := s.store.FindImportScan(ctx, id)
	if err != nil {
		if ent.IsNotFound(err) {
			return otelx.RecordSpanError(span, ErrScanNotFound)
		}
		return otelx.RecordSpanError(span, err)
	}
	if scan.Status != entimportscan.StatusAwaitingReview {
		return otelx.RecordSpanError(span, ErrScanNotReviewable)
	}
	if err := s.store.UpdateImportScanStatus(
		ctx,
		id,
		entimportscan.StatusCommitting,
		db.UpdateScanStatusOpts{},
	); err != nil {
		return otelx.RecordSpanError(span, fmt.Errorf("flip status: %w", err))
	}

	bg := context.WithoutCancel(ctx)
	if scan.Kind == entimportscan.KindSeries {
		go s.runCommitSeries(bg, scan)
	} else {
		go s.runCommit(bg, scan)
	}
	return nil
}

func (s *Service) runCommit(ctx context.Context, scan *ent.ImportScan) {
	ctx, span := tracer.Start(ctx, "bulkimport.run_commit",
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
					CommittedAt:   &now,
				},
			); err != nil {
				slog.ErrorContext(
					ctx,
					"bulk import commit: failed to mark scan failed after panic",
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

	files, err := s.store.ListImportScanFilesForCommit(ctx, scan.ID)
	if err != nil {
		reason := err.Error()
		now := time.Now()
		if uerr := s.store.UpdateImportScanStatus(
			ctx,
			scan.ID,
			entimportscan.StatusFailed,
			db.UpdateScanStatusOpts{
				FailureReason: &reason,
				CommittedAt:   &now,
			},
		); uerr != nil {
			slog.ErrorContext(
				ctx,
				"bulk import commit: failed to mark scan failed after list error",
				"scan.id",
				scan.ID,
				"list.error",
				err,
				"update.error",
				uerr,
			)
		}
		return
	}

	sem := make(chan struct{}, commitConcurrency)
	var success, failed atomic.Uint32
	var wg sync.WaitGroup
	for _, f := range files {
		sem <- struct{}{}
		wg.Add(1)
		go func(f *ent.ImportScanFile) {
			defer wg.Done()
			defer func() { <-sem }()
			outcome, msg, movieID := s.commitOne(ctx, scan, f)
			if err := s.store.UpdateImportScanFileOutcome(
				ctx,
				f.ID,
				outcome,
				db.UpdateScanFileOutcomeOpts{
					Message:        msg,
					CreatedMovieID: movieID,
				},
			); err != nil {
				slog.ErrorContext(
					ctx,
					"bulk import commit: failed to record file outcome",
					"scan.id",
					scan.ID,
					"file.id",
					f.ID,
					"outcome",
					outcome,
					"error",
					err,
				)
			}
			switch outcome {
			case entimportscanfile.OutcomeCreated, entimportscanfile.OutcomeAttached:
				success.Add(1)
			case entimportscanfile.OutcomeFailed:
				failed.Add(1)
			}
		}(f)
	}
	wg.Wait()

	committedAt := time.Now()
	successCount, failedCount := success.Load(), failed.Load()
	if err := s.store.UpdateImportScanStatus(
		ctx,
		scan.ID,
		entimportscan.StatusCompleted,
		db.UpdateScanStatusOpts{
			CommittedAt:        &committedAt,
			CommitSuccessCount: &successCount,
			CommitFailedCount:  &failedCount,
		},
	); err != nil {
		slog.ErrorContext(
			ctx,
			"bulk import commit: failed to flip scan to completed",
			"scan.id",
			scan.ID,
			"error",
			err,
		)
	}
	slog.InfoContext(
		ctx,
		"bulk import commit finished",
		"scan.id",
		scan.ID,
		"commit.success_count",
		successCount,
		"commit.failed_count",
		failedCount,
	)
}

func (s *Service) commitOne(
	ctx context.Context,
	scan *ent.ImportScan,
	f *ent.ImportScanFile,
) (entimportscanfile.Outcome, string, uint32) {
	ctx, span := tracer.Start(ctx, "bulkimport.commit_file",
		trace.WithAttributes(
			attribute.Int64("scan.id", int64(scan.ID)),
			attribute.Int64("file.id", int64(f.ID)),
			attribute.String("file.classification", string(f.Classification)),
		))
	defer span.End()

	switch f.Classification {
	case entimportscanfile.ClassificationExisting:
		return s.commitAttach(ctx, f)
	default:
		tmdbID := f.DecisionTmdbID
		if tmdbID == 0 {
			tmdbID = f.TmdbID
		}
		if scan.Mode == entimportscan.ModeInPlace {
			return s.commitAdoptInPlace(ctx, f, tmdbID)
		}
		return s.commitRename(ctx, scan, f, tmdbID)
	}
}

// commitFail wraps an error as a failed-outcome triple. movieID is 0 when the
// failure happens before a movie row is materialised.
func commitFail(
	label string,
	err error,
	movieID uint32,
) (entimportscanfile.Outcome, string, uint32) {
	return entimportscanfile.OutcomeFailed, fmt.Sprintf(
		"%s: %v",
		label,
		err,
	), movieID
}

func (s *Service) commitAttach(
	ctx context.Context,
	f *ent.ImportScanFile,
) (entimportscanfile.Outcome, string, uint32) {
	if _, err := s.store.CreateMediaFile(ctx, db.CreateMediaFileParams{
		MovieID:      f.ExistingMovieID,
		Path:         f.SourcePath,
		Size:         f.Size,
		Quality:      f.ParsedQuality,
		ReleaseGroup: f.ParsedReleaseGroup,
		Source:       entmediafile.SourceWizard,
	}); err != nil {
		return commitFail("create media file", err, 0)
	}
	if err := s.store.UpdateMovieStatus(
		ctx,
		f.ExistingMovieID,
		entmovie.StatusAvailable,
	); err != nil {
		return commitFail("update movie status", err, 0)
	}
	return entimportscanfile.OutcomeAttached, "", f.ExistingMovieID
}

func (s *Service) commitAdoptInPlace(
	ctx context.Context,
	f *ent.ImportScanFile,
	tmdbID uint32,
) (entimportscanfile.Outcome, string, uint32) {
	m, _, err := s.movieSvc.Add(ctx, tmdbID, "")
	if err != nil {
		return commitFail("add movie", err, 0)
	}
	if _, err := s.store.CreateMediaFile(ctx, db.CreateMediaFileParams{
		MovieID:      m.ID,
		Path:         f.SourcePath,
		Size:         f.Size,
		Quality:      f.ParsedQuality,
		ReleaseGroup: f.ParsedReleaseGroup,
		Source:       entmediafile.SourceWizard,
	}); err != nil {
		return commitFail("create media file", err, m.ID)
	}
	if err := s.store.UpdateMovieStatus(
		ctx,
		m.ID,
		entmovie.StatusAvailable,
	); err != nil {
		return commitFail("update movie status", err, m.ID)
	}
	return entimportscanfile.OutcomeCreated, "", m.ID
}

func (s *Service) commitRename(
	ctx context.Context,
	scan *ent.ImportScan,
	f *ent.ImportScanFile,
	tmdbID uint32,
) (entimportscanfile.Outcome, string, uint32) {
	m, _, err := s.movieSvc.Add(ctx, tmdbID, "")
	if err != nil {
		return commitFail("add movie", err, 0)
	}
	imported, err := s.importSvc.ImportMovieWithMode(
		ctx,
		filepath.Dir(f.SourcePath),
		m,
		"",
		string(scan.ImportMode),
	)
	if err != nil {
		return commitFail("import movie", err, m.ID)
	}
	if _, err := s.store.CreateMediaFile(ctx, db.CreateMediaFileParams{
		MovieID:      m.ID,
		Path:         imported.Path,
		Size:         imported.Size,
		Quality:      imported.Parsed.Resolution,
		ReleaseGroup: imported.Parsed.Group,
		Source:       entmediafile.SourceWizard,
	}); err != nil {
		return commitFail("create media file", err, m.ID)
	}
	if err := s.store.UpdateMovieStatus(
		ctx,
		m.ID,
		entmovie.StatusAvailable,
	); err != nil {
		return commitFail("update movie status", err, m.ID)
	}
	return entimportscanfile.OutcomeCreated, "", m.ID
}
