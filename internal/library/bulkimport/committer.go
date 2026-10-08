package bulkimport

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
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
	"github.com/datahearth/streamline/internal/media/book"
	"github.com/datahearth/streamline/internal/media/movie"
	"github.com/datahearth/streamline/internal/mediaserver"
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
		go s.runCommitMusic(bg, scan)
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
	countCommit(ctx, "movie", scan.Source, "success", int64(successCount))
	countCommit(ctx, "movie", scan.Source, "failed", int64(failedCount))
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

	if isMigration(scan) {
		return s.commitMigrated(ctx, scan, f)
	}
	switch f.Classification {
	case entimportscanfile.ClassificationExisting:
		return s.commitAttach(ctx, scan, f)
	default:
		return s.commitNew(ctx, scan, f, "")
	}
}

func (s *Service) commitNew(
	ctx context.Context,
	scan *ent.ImportScan,
	f *ent.ImportScanFile,
	profile string,
) (entimportscanfile.Outcome, string, uint32) {
	tmdbID := f.DecisionTmdbID
	if tmdbID == 0 {
		tmdbID = f.TmdbID
	}
	if scan.Mode == entimportscan.ModeInPlace {
		return s.commitAdoptInPlace(ctx, f, tmdbID, profile)
	}
	return s.commitRename(ctx, scan, f, tmdbID, profile)
}

func isMigration(scan *ent.ImportScan) bool {
	return scan.Source != "" && scan.Source != entimportscan.SourceFilesystem
}

// commitMigrated commits one Radarr row. The file arms are the filesystem
// scan's, wrapped: the source's profile goes into Add, and its monitored flag
// and profile are applied afterwards, since an existing movie keeps its own
// profile through Add and monitored is not an Add parameter at all.
func (s *Service) commitMigrated(
	ctx context.Context,
	scan *ent.ImportScan,
	f *ent.ImportScanFile,
) (entimportscanfile.Outcome, string, uint32) {
	profile, note := migratedProfile(f.QualityProfile)

	var (
		outcome entimportscanfile.Outcome
		msg     string
		movieID uint32
	)
	switch {
	case f.SourcePath == "":
		outcome, msg, movieID = s.commitTitleOnly(ctx, f, profile)
	case !s.fileUsable(scan, f.SourcePath):
		// The scan flagged this row for review; accepting it must not record a
		// file this process cannot open or one outside the library it walks.
		outcome, msg, movieID = s.commitTitleOnly(ctx, f, profile)
		note = joinNotes(note, fmt.Sprintf(
			"the file at %s is missing or outside the library; the title was added without it",
			f.SourcePath,
		))
	case f.Classification == entimportscanfile.ClassificationExisting:
		outcome, msg, movieID = s.commitAttach(ctx, scan, f)
		if movieID == 0 && outcome != entimportscanfile.OutcomeFailed {
			movieID = f.ExistingMovieID
		}
	default:
		outcome, msg, movieID = s.commitNew(ctx, scan, f, profile)
	}
	if outcome == entimportscanfile.OutcomeFailed || movieID == 0 {
		return outcome, msg, movieID
	}
	return outcome, joinNotes(
		msg,
		note,
		s.applyMovieState(ctx, movieID, f, profile),
	), movieID
}

// migratedProfile checks the profile the operator mapped at review time
// still exists. A name that resolves to nothing would be stored verbatim and
// silently read as the default forever; a deleted profile must not fail a
// real title either, so it falls back to the default and says so.
func migratedProfile(name string) (string, string) {
	if name == "" {
		return "", ""
	}
	if e, ok := config.ResolveQualityProfile(name); ok && e.Name == name {
		return name, ""
	}
	return "", fmt.Sprintf(
		"quality profile %q no longer exists; the default was used", name,
	)
}

func joinNotes(notes ...string) string {
	var out []string
	for _, n := range notes {
		if n != "" {
			out = append(out, n)
		}
	}
	return strings.Join(out, "; ")
}

// applyMovieState carries the source's monitored flag and profile onto the
// movie. The returned string is empty on success and otherwise says what
// could not be applied — the title itself is already committed.
func (s *Service) applyMovieState(
	ctx context.Context, movieID uint32, f *ent.ImportScanFile, profile string,
) string {
	monitored := f.Monitored
	params := movie.UpdateParams{Monitored: &monitored}
	if profile != "" {
		params.QualityProfile = &profile
	}
	if _, err := s.movieSvc.Update(ctx, movieID, params); err != nil {
		slog.WarnContext(ctx, "migration: movie flags not applied",
			"movie.id", movieID, "error", err)
		return fmt.Sprintf("could not apply monitoring and profile: %v", err)
	}
	return ""
}

// commitTitleOnly handles a row the source tracks without a usable file.
// There is nothing to link and nothing to move: the title exists, and the
// missing search finds it if it is monitored.
func (s *Service) commitTitleOnly(
	ctx context.Context, f *ent.ImportScanFile, profile string,
) (entimportscanfile.Outcome, string, uint32) {
	tmdbID := f.DecisionTmdbID
	if tmdbID == 0 {
		tmdbID = f.TmdbID
	}
	m, existed, err := s.addOrFindMovie(ctx, tmdbID, profile)
	if err != nil {
		return commitFail("add movie", err, 0)
	}
	if existed {
		return entimportscanfile.OutcomeAttached, "", m.ID
	}
	return entimportscanfile.OutcomeCreated, "", m.ID
}

// addOrFindMovie adds the movie, or returns the existing row when another scan
// (or another file in this one) added it while this scan sat in review. The
// desired end state — the movie is in the library — is already true, so
// failing the file would strand a real file over a race.
//
// existed reports the second case. profile is only applied to a movie Add
// creates; an existing one keeps its own.
func (s *Service) addOrFindMovie(
	ctx context.Context,
	tmdbID uint32,
	profile string,
) (*ent.Movie, bool, error) {
	m, _, err := s.movieSvc.Add(ctx, tmdbID, profile)
	if err == nil {
		return m, false, nil
	}
	if !errors.Is(err, movie.ErrMovieExists) {
		return nil, false, err
	}
	existing, ferr := s.store.FindMovieByTMDBID(ctx, tmdbID)
	if ferr != nil || existing == nil {
		return nil, false, err
	}
	return existing, true, nil
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
	existing, err := s.store.ListMediaFilesByMovieID(ctx, f.ExistingMovieID)
	if err != nil {
		return commitFail("list existing files", err, 0)
	}
	for _, mf := range existing {
		if mf.Path == f.SourcePath {
			return entimportscanfile.OutcomeAttached, "", f.ExistingMovieID
		}
	}
	// A movie holds at most one media file: accepting a scanned file for an
	// already-known movie replaces whatever file it currently has.
	for _, mf := range existing {
		if err := os.Remove(mf.Path); err != nil && !os.IsNotExist(err) {
			slog.WarnContext(ctx, "attach replace: remove old file failed",
				"path", mf.Path, "error", err)
		} else if err == nil {
			// Only the failure was logged, so the deletion that actually
			// happened left nothing behind to explain a file disappearing
			// mid-commit.
			slog.InfoContext(ctx, "attach replace: replaced an existing file",
				"movie.id", f.ExistingMovieID,
				"old_path", mf.Path,
				"new_path", f.SourcePath)
		}
		if err := s.store.DeleteMediaFile(ctx, mf.ID); err != nil {
			return commitFail("delete replaced file", err, 0)
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
	if scan.Mode == entimportscan.ModeRename {
		imported, err := s.importSvc.ImportMovieWithMode(
			ctx,
			filepath.Dir(f.SourcePath),
			m,
			string(scan.ImportMode),
		)
		if err != nil {
			return commitFail("import movie", err, 0)
		}
		params.Path = imported.Path
		params.Size = imported.Size
		params.Quality = imported.Parsed.Resolution
		params.ReleaseGroup = imported.Parsed.Group
		params.Parsed = &imported.Parsed
	}
	return s.linkAndMarkAvailable(
		ctx, params, entimportscanfile.OutcomeAttached, 0,
	)
}

func (s *Service) commitAdoptInPlace(
	ctx context.Context,
	f *ent.ImportScanFile,
	tmdbID uint32,
	profile string,
) (entimportscanfile.Outcome, string, uint32) {
	m, _, err := s.addOrFindMovie(ctx, tmdbID, profile)
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
	profile string,
) (entimportscanfile.Outcome, string, uint32) {
	m, _, err := s.addOrFindMovie(ctx, tmdbID, profile)
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
