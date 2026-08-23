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
	entepisode "github.com/datahearth/streamline/ent/episode"
	entimportscan "github.com/datahearth/streamline/ent/importscan"
	entimportscanshow "github.com/datahearth/streamline/ent/importscanshow"
	entmediafile "github.com/datahearth/streamline/ent/mediafile"
	enttvshow "github.com/datahearth/streamline/ent/tvshow"
	"github.com/datahearth/streamline/internal/db"
	"github.com/datahearth/streamline/internal/library"
)

// runCommitSeries adopts every reviewed show in a series scan: creates (or
// reuses) the TV show and links the episode files already on disk. Runs
// sequentially — a scan holds tens of shows, not thousands of files.
func (s *Service) runCommitSeries(ctx context.Context, scan *ent.ImportScan) {
	ctx, span := tracer.Start(ctx, "bulkimport.run_commit_series",
		trace.WithAttributes(attribute.Int64("scan.id", int64(scan.ID))))
	defer span.End()

	defer func() {
		if r := recover(); r != nil {
			s.markScanFailed(ctx, scan.ID, fmt.Sprintf("panic: %v", r))
		}
	}()

	shows, err := s.store.ListImportScanShowsForCommit(ctx, scan.ID)
	if err != nil {
		s.markScanFailed(ctx, scan.ID, err.Error())
		return
	}

	var success, failed uint32
	for _, sh := range shows {
		outcome, msg, createdID := s.commitShow(ctx, scan, sh)
		if uerr := s.store.UpdateImportScanShowOutcome(
			ctx,
			sh.ID,
			outcome,
			db.UpdateScanShowOutcomeOpts{
				Message:         msg,
				CreatedTvshowID: createdID,
			},
		); uerr != nil {
			slog.ErrorContext(ctx, "series commit: failed to record show outcome",
				"scan.id", scan.ID, "show.id", sh.ID, "error", uerr)
		}
		switch outcome {
		case entimportscanshow.OutcomeCreated, entimportscanshow.OutcomeAttached:
			success++
		case entimportscanshow.OutcomeFailed:
			failed++
		}
	}

	committedAt := time.Now()
	if err := s.store.UpdateImportScanStatus(
		ctx,
		scan.ID,
		entimportscan.StatusCompleted,
		db.UpdateScanStatusOpts{
			CommittedAt:        &committedAt,
			CommitSuccessCount: &success,
			CommitFailedCount:  &failed,
		},
	); err != nil {
		slog.ErrorContext(ctx, "series commit: failed to flip scan to completed",
			"scan.id", scan.ID, "error", err)
	}
	slog.InfoContext(
		ctx,
		"series commit finished",
		"scan.id",
		scan.ID,
		"commit.success_count",
		success,
		"commit.failed_count",
		failed,
	)
}

// commitShow adopts one show folder: resolve/create the show, then link each
// on-disk file to its episode. An in-place scan records the file where it lies;
// a rename scan transfers it into the series library first. Missing episodes
// stay wanted.
func (s *Service) commitShow(
	ctx context.Context, scan *ent.ImportScan, sc *ent.ImportScanShow,
) (entimportscanshow.Outcome, string, uint32) {
	ctx, span := tracer.Start(ctx, "bulkimport.commit_show",
		trace.WithAttributes(
			attribute.Int64("show.id", int64(sc.ID)),
			attribute.String("show.folder", sc.FolderPath)))
	defer span.End()

	show, reused, outcome, msg, id := s.resolveShow(ctx, sc)
	if show == nil {
		return outcome, msg, id
	}

	files, err := library.ListVideoFilesRecursive(sc.FolderPath)
	if err != nil {
		return commitShowFail("list folder", err, show.ID)
	}
	// Match the whole folder before anything moves. A folder whose files mostly
	// fail to match is far more likely bound to the wrong show than to be a show
	// with missing metadata, and adopting it anyway wrote 7 of 76 files into a
	// same-named series and counted it a success.
	plan, unmatched := planEpisodes(show, files)
	if len(plan) <= unmatched {
		return entimportscanshow.OutcomeFailed, fmt.Sprintf(
			"only %d of %d files matched an episode — folder likely belongs to another show",
			len(plan),
			len(files),
		), show.ID
	}

	success := entimportscanshow.OutcomeCreated
	if reused {
		success = entimportscanshow.OutcomeAttached
	}
	matched := 0
	for _, m := range plan {
		f, season, target := m.path, m.season, m.episode
		parsed := library.Parse(filepath.Base(f))
		// Stat before the replace check: a path this loop already removed while
		// replacing (old copy + repack in one folder) must not tear down the
		// record its replacement just created.
		info, err := os.Stat(f)
		if err != nil {
			slog.WarnContext(
				ctx,
				"series adopt: stat failed",
				"file",
				f,
				"error",
				err,
			)
			continue
		}
		// An episode holds at most one media file: committing an accepted show
		// replaces whatever file a matched episode already has. The same path
		// being re-scanned is already adopted, so it needs no rewrite.
		if mf, err := s.store.FindMediaFileByEpisodeID(ctx, target.ID); err == nil {
			if mf.Path == f {
				continue
			}
			if rmErr := os.Remove(mf.Path); rmErr != nil && !os.IsNotExist(rmErr) {
				slog.WarnContext(ctx, "series adopt: remove old file failed",
					"path", mf.Path, "error", rmErr)
			}
			if dErr := s.store.DeleteMediaFile(ctx, mf.ID); dErr != nil {
				slog.WarnContext(ctx, "series adopt: delete replaced file failed",
					"episode.id", target.ID, "error", dErr)
				continue
			}
		} else if !ent.IsNotFound(err) {
			slog.WarnContext(ctx, "series adopt: media file lookup failed",
				"episode.id", target.ID, "error", err)
			continue
		}
		path, size := f, info.Size()
		if scan.Mode == entimportscan.ModeRename {
			imported, err := s.importSvc.ImportEpisodeWithMode(
				ctx, f, show, season, target, string(scan.ImportMode),
			)
			if err != nil {
				slog.WarnContext(ctx, "series adopt: import episode failed",
					"file", f, "episode.id", target.ID, "error", err)
				continue
			}
			path, size, parsed = imported.Path, imported.Size, imported.Parsed
		}
		if _, err := s.store.CreateMediaFile(ctx, db.CreateMediaFileParams{
			EpisodeID:    target.ID,
			Path:         path,
			Size:         size,
			Quality:      parsed.Resolution,
			Format:       parsed.Extension,
			ReleaseGroup: parsed.Group,
			Source:       entmediafile.SourceWizard,
		}); err != nil {
			slog.WarnContext(ctx, "series adopt: create media file failed",
				"episode.id", target.ID, "error", err)
			continue
		}
		if err := s.store.SetEpisodeStatus(
			ctx,
			target.ID,
			entepisode.StatusAvailable,
		); err != nil {
			slog.WarnContext(ctx, "series adopt: flip episode status failed",
				"episode.id", target.ID, "error", err)
		}
		matched++
	}
	slog.InfoContext(ctx, "series adopted",
		"tvshow.id", show.ID, "matched", matched, "files", len(files))
	return success, "", show.ID
}

// episodeMatch is one folder file bound to the episode it belongs to.
type episodeMatch struct {
	path    string
	season  uint16
	episode *ent.Episode
}

// planEpisodes resolves every file in a show folder to an episode without
// touching disk, so the caller can judge the folder as a whole before the first
// transfer. Returns the matches plus how many files matched nothing.
func planEpisodes(show *ent.TVShow, files []string) ([]episodeMatch, int) {
	anime := show.Type == enttvshow.TypeAnime
	var plan []episodeMatch
	unmatched := 0
	for _, f := range files {
		parsed := library.Parse(filepath.Base(f))
		season, target := library.MatchEpisodeInSeason(
			parsed,
			show.Edges.Seasons,
			anime,
		)
		if target == nil {
			unmatched++
			continue
		}
		plan = append(plan, episodeMatch{path: f, season: season, episode: target})
	}
	return plan, unmatched
}

// resolveShow returns the eager-loaded show to adopt into, creating it from TVDB
// when no row for that tvdb id exists yet. reused reports that the show was
// already in the library. On failure it returns a nil show plus the outcome
// triple to record.
func (s *Service) resolveShow(
	ctx context.Context, sc *ent.ImportScanShow,
) (*ent.TVShow, bool, entimportscanshow.Outcome, string, uint32) {
	if sc.ExistingTvshowID != nil {
		found, err := s.store.FindTVShowByID(ctx, *sc.ExistingTvshowID)
		if err != nil {
			o, m, id := commitShowFail("load existing show", err, 0)
			return nil, false, o, m, id
		}
		return found, true, "", "", 0
	}

	// Reviewer's pick wins over the classifier's top match.
	tvdbID := uint32(0)
	if sc.DecisionTvdbID != nil {
		tvdbID = *sc.DecisionTvdbID
	} else if sc.TvdbID != nil {
		tvdbID = *sc.TvdbID
	}
	if tvdbID == 0 {
		return nil, false, entimportscanshow.OutcomeFailed, "no tvdb match to adopt", 0
	}

	// Resolve against current state rather than trusting the scan-time
	// classification: a reviewer pointing an unmatched folder at a show that is
	// already in the library — the normal way to fix a bad match — would
	// otherwise collide on tv_shows.tvdb_id and fail the whole entry.
	// FindTVShowByTVDBID reports "no such show" as a nil row with a nil error,
	// so the row has to be checked, not just the error.
	existing, err := s.store.FindTVShowByTVDBID(ctx, tvdbID)
	if err != nil {
		o, m, id := commitShowFail("look up show", err, 0)
		return nil, false, o, m, id
	}
	if existing != nil {
		found, ferr := s.store.FindTVShowByID(ctx, existing.ID)
		if ferr != nil {
			o, m, id := commitShowFail("load existing show", ferr, existing.ID)
			return nil, false, o, m, id
		}
		return found, true, "", "", 0
	}

	created, err := s.seriesAdder.Add(ctx, tvdbID, "")
	if err != nil {
		o, m, id := commitShowFail("add show", err, 0)
		return nil, false, o, m, id
	}
	found, err := s.store.FindTVShowByID(ctx, created.ID)
	if err != nil {
		o, m, id := commitShowFail("load created show", err, created.ID)
		return nil, false, o, m, id
	}
	return found, false, "", "", 0
}

func commitShowFail(
	label string, err error, tvshowID uint32,
) (entimportscanshow.Outcome, string, uint32) {
	return entimportscanshow.OutcomeFailed, fmt.Sprintf(
		"%s: %v",
		label,
		err,
	), tvshowID
}
