package bulkimport

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"slices"
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
	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/db"
	"github.com/datahearth/streamline/internal/library/roothealth"
	"github.com/datahearth/streamline/internal/media/book"
	"github.com/datahearth/streamline/internal/media/movie"
	"github.com/datahearth/streamline/internal/mediaserver"
	"github.com/datahearth/streamline/internal/otelx"
)

const commitConcurrency = 4

// Commit commits a reviewed scan with the default options.
func (s *Service) Commit(ctx context.Context, id uint32) error {
	return s.CommitWith(ctx, id, CommitOptions{})
}

// scanRoots names the roots a commit adds files to. A book scan covers both book
// roots, so it goes by the rows being committed: a healthy root is never held
// back by the other one.
func (s *Service) scanRoots(
	ctx context.Context,
	scan *ent.ImportScan,
) ([]db.LibraryRoot, error) {
	switch scan.Kind {
	case entimportscan.KindSeries:
		return []db.LibraryRoot{db.RootSeries}, nil
	case entimportscan.KindMusic:
		return []db.LibraryRoot{db.RootMusic}, nil
	case entimportscan.KindBook:
		rows, err := s.store.ListImportScanBooksForCommit(ctx, scan.ID)
		if err != nil {
			return nil, err
		}
		held := map[db.LibraryRoot]bool{}
		for _, row := range rows {
			held[db.BookRoot(string(row.Slot))] = true
		}
		return slices.Collect(maps.Keys(held)), nil
	}
	return []db.LibraryRoot{db.RootMovies}, nil
}

// CommitWith commits a reviewed scan in the background.
func (s *Service) CommitWith(
	ctx context.Context,
	id uint32,
	opts CommitOptions,
) error {
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
	switch scan.Kind {
	case "",
		entimportscan.KindMovie,
		entimportscan.KindSeries,
		entimportscan.KindMusic,
		entimportscan.KindBook:
	default:
		return otelx.RecordSpanError(
			span, fmt.Errorf("%w: %s", ErrUnsupportedKind, scan.Kind),
		)
	}
	if scan.Kind == entimportscan.KindBook && s.bookmeta == nil {
		return otelx.RecordSpanError(span, book.ErrNotConfigured)
	}
	roots, err := s.scanRoots(ctx, scan)
	if err != nil {
		return otelx.RecordSpanError(span, err)
	}
	if err := roothealth.Require(ctx, roots...); err != nil {
		return otelx.RecordSpanError(span, err)
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
	switch scan.Kind {
	case entimportscan.KindSeries:
		go s.runCommitSeries(bg, scan)
	case entimportscan.KindMusic:
		go s.runCommitMusic(bg, scan, opts.writeTags())
	case entimportscan.KindBook:
		go s.runCommitBooks(bg, scan)
	default:
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
			s.markScanFailed(ctx, scan.ID, fmt.Sprintf("panic: %v", r))
		}
	}()

	files, err := s.store.ListImportScanFilesForCommit(ctx, scan.ID)
	if err != nil {
		s.markScanFailed(ctx, scan.ID, err.Error())
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
	countCommit(ctx, "movie", "success", int64(successCount))
	countCommit(ctx, "movie", "failed", int64(failedCount))
	if successCount > 0 {
		mediaserver.RefreshInBackground(ctx, s.ms, "movie", s.moviePath)
	}
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
		return s.commitAttach(ctx, scan, f)
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

// addOrFindMovie adds the movie, or returns the existing row when another scan
// (or another file in this one) added it while this scan sat in review. The
// desired end state — the movie is in the library — is already true, so
// failing the file would strand a real file over a race.
func (s *Service) addOrFindMovie(
	ctx context.Context,
	tmdbID uint32,
) (*ent.Movie, error) {
	m, _, err := s.movieSvc.Add(ctx, tmdbID, "")
	if err == nil {
		return m, nil
	}
	if !errors.Is(err, movie.ErrMovieExists) {
		return nil, err
	}
	existing, ferr := s.store.FindMovieByTMDBID(ctx, tmdbID)
	if ferr != nil {
		return nil, err
	}
	return existing, nil
}

func (s *Service) markScanFailed(ctx context.Context, scanID uint32, reason string) {
	s.markScanFailedWithCode(ctx, scanID, reason, "")
}

// markScanFailedWithCode also records the machine-readable cause a client keys
// a remedy on; an empty code leaves the free-text reason as the whole story.
func (s *Service) markScanFailedWithCode(
	ctx context.Context,
	scanID uint32,
	reason, code string,
) {
	now := time.Now()
	opts := db.UpdateScanStatusOpts{
		FailureReason: &reason,
		CommittedAt:   &now,
	}
	if code != "" {
		opts.FailureCode = &code
	}
	if err := s.store.UpdateImportScanStatus(
		ctx,
		scanID,
		entimportscan.StatusFailed,
		opts,
	); err != nil {
		slog.ErrorContext(ctx, "bulk import commit: failed to mark scan failed",
			"scan.id", scanID, "error", err)
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

// linkAndMarkAvailable records the committed file against params.MovieID and
// flips that movie to available, returning the commit-outcome triple: success
// on the happy path, otherwise the commitFail triple.
//
// failMovieID is what the failure triple reports as the created-movie id, which
// is not always params.MovieID: attaching to an already-known movie creates no
// movie row, so a half-applied attach must report 0.
func (s *Service) linkAndMarkAvailable(
	ctx context.Context,
	params db.CreateMediaFileParams,
	success entimportscanfile.Outcome,
	failMovieID uint32,
) (entimportscanfile.Outcome, string, uint32) {
	if _, err := s.store.CreateMediaFile(ctx, params); err != nil {
		return commitFail("create media file", err, failMovieID)
	}
	if err := s.store.UpdateMovieStatus(
		ctx,
		params.MovieID,
		entmovie.StatusAvailable,
	); err != nil {
		return commitFail("update movie status", err, failMovieID)
	}
	return success, "", params.MovieID
}

func (s *Service) commitAttach(
	ctx context.Context,
	scan *ent.ImportScan,
	f *ent.ImportScanFile,
) (entimportscanfile.Outcome, string, uint32) {
	// A commit runs several files at once and two of them can be copies of
	// one movie. Each lists the movie's rows, places its file and deletes what
	// it listed; run together both replace the same row, and the loser's
	// failed transaction takes the winner's file back out of the library.
	// Under the lock the second lists the row the first one wrote. It is the
	// only lock this goroutine takes, and nothing below re-enters.
	defer s.movieFiles.lock(f.ExistingMovieID)()
	existing, err := s.store.ListMediaFilesByMovieID(ctx, f.ExistingMovieID)
	if err != nil {
		return commitFail("list existing files", err, 0)
	}
	// The scanned file is already the movie's, under this path or another
	// spelling of it: nothing to replace. One that is gone since the scan
	// replaces nothing either, and an in-place commit has no import to fail
	// on it before the tracked file is removed.
	for _, mf := range existing {
		same, err := sameFile(mf.Path, f.SourcePath)
		if err != nil {
			return commitFail("stat source", err, 0)
		}
		if same {
			return entimportscanfile.OutcomeAttached, "", f.ExistingMovieID
		}
	}
	m, err := s.store.FindMovieByID(ctx, f.ExistingMovieID)
	if err != nil {
		return commitFail("find movie", err, 0)
	}
	// A rename scan relocates everything it accepts, existing rows included —
	// attaching this one where it lies would leave the library split across the
	// source root and the configured one, which only an inode sweep can find.
	params := db.CreateMediaFileParams{
		MovieID:      f.ExistingMovieID,
		Path:         f.SourcePath,
		Size:         f.Size,
		Quality:      f.ParsedQuality,
		ReleaseGroup: f.ParsedReleaseGroup,
		Source:       entmediafile.SourceWizard,
		QueueTranscode: config.TranscodeEligible(
			config.MediaMovie,
			m.QualityProfile,
		),
	}
	// A movie holds at most one media file: accepting a scanned file for an
	// already-known movie replaces whatever file it currently has. The old
	// files and rows go only once the new file is placed and its row written,
	// so a replacement that fails anywhere leaves the movie as it was.
	//
	// aside holds the old file at asidePath while its replacement is placed
	// at that very path (a rename re-commit renders there when the template
	// keeps no release tag); placed is a file this commit put in the library,
	// which a failed row write takes back out before the old one is restored.
	var aside, asidePath string
	placed := false
	src := f.SourcePath
	if scan.Mode == entimportscan.ModeRename {
		dir := filepath.Dir(f.SourcePath)
		if len(existing) > 0 {
			// A source the importer cannot resolve fails in the import itself.
			if resolved, dest, derr := s.importSvc.MovieDestination(
				dir,
				m,
			); derr == nil {
				src = resolved
				for _, mf := range existing {
					if !samePath(dest, mf.Path) {
						continue
					}
					if aside, err = setAside(src, mf.Path); err != nil {
						return commitFail("set aside replaced file", err, 0)
					}
					asidePath = mf.Path
					break
				}
			}
		}
		imported, err := s.importSvc.ImportMovieWithMode(
			ctx,
			dir,
			m,
			string(scan.ImportMode),
		)
		if err != nil {
			restoreAside(ctx, aside, asidePath)
			return commitFail("import movie", err, 0)
		}
		params.Path = imported.Path
		params.Size = imported.Size
		params.Quality = imported.Parsed.Resolution
		params.ReleaseGroup = imported.Parsed.Group
		params.Parsed = &imported.Parsed
		// A destination already holding this very inode (a hardlink commit
		// run again) is an old file itself, not a new one.
		placed = aside != "" ||
			!slices.ContainsFunc(existing, func(mf *ent.MediaFile) bool {
				return mf.Path == imported.Path
			})
	}
	if len(existing) == 0 {
		return s.linkAndMarkAvailable(
			ctx, params, entimportscanfile.OutcomeAttached, 0,
		)
	}
	oldIDs := make([]uint32, len(existing))
	for i, mf := range existing {
		oldIDs[i] = mf.ID
	}
	if _, err := s.store.ReplaceMediaFile(ctx, oldIDs, params); err != nil {
		if placed {
			undoPlacement(ctx, src, params.Path, string(scan.ImportMode))
		}
		restoreAside(ctx, aside, asidePath)
		return commitFail("replace media file", err, 0)
	}
	for _, mf := range existing {
		gone := mf.Path
		switch {
		case aside != "" && mf.Path == asidePath:
			gone = aside
		case mf.Path == params.Path:
			continue
		}
		if err := os.Remove(gone); err != nil && !os.IsNotExist(err) {
			slog.WarnContext(ctx, "attach replace: remove old file failed",
				"path", gone, "error", err)
			continue
		}
		// The deletion is what explains a file disappearing mid-commit.
		slog.InfoContext(ctx, "attach replace: replaced an existing file",
			"movie.id", f.ExistingMovieID,
			"old_path", mf.Path,
			"new_path", params.Path)
	}
	if err := s.store.UpdateMovieStatus(
		ctx,
		params.MovieID,
		entmovie.StatusAvailable,
	); err != nil {
		return commitFail("update movie status", err, 0)
	}
	return entimportscanfile.OutcomeAttached, "", params.MovieID
}

func (s *Service) commitAdoptInPlace(
	ctx context.Context,
	f *ent.ImportScanFile,
	tmdbID uint32,
) (entimportscanfile.Outcome, string, uint32) {
	m, err := s.addOrFindMovie(ctx, tmdbID)
	if err != nil {
		return commitFail("add movie", err, 0)
	}
	return s.linkAndMarkAvailable(ctx, db.CreateMediaFileParams{
		MovieID:      m.ID,
		Path:         f.SourcePath,
		Size:         f.Size,
		Quality:      f.ParsedQuality,
		ReleaseGroup: f.ParsedReleaseGroup,
		Source:       entmediafile.SourceWizard,
		QueueTranscode: config.TranscodeEligible(
			config.MediaMovie,
			m.QualityProfile,
		),
	}, entimportscanfile.OutcomeCreated, m.ID)
}

func (s *Service) commitRename(
	ctx context.Context,
	scan *ent.ImportScan,
	f *ent.ImportScanFile,
	tmdbID uint32,
) (entimportscanfile.Outcome, string, uint32) {
	m, err := s.addOrFindMovie(ctx, tmdbID)
	if err != nil {
		return commitFail("add movie", err, 0)
	}
	imported, err := s.importSvc.ImportMovieWithMode(
		ctx,
		filepath.Dir(f.SourcePath),
		m,
		string(scan.ImportMode),
	)
	if err != nil {
		return commitFail("import movie", err, m.ID)
	}
	return s.linkAndMarkAvailable(ctx, db.CreateMediaFileParams{
		MovieID:      m.ID,
		Path:         imported.Path,
		Size:         imported.Size,
		Quality:      imported.Parsed.Resolution,
		ReleaseGroup: imported.Parsed.Group,
		Parsed:       &imported.Parsed,
		Source:       entmediafile.SourceWizard,
		QueueTranscode: config.TranscodeEligible(
			config.MediaMovie,
			m.QualityProfile,
		),
	}, entimportscanfile.OutcomeCreated, m.ID)
}
