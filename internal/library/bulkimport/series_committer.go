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
		outcome, msg, createdID := s.commitShow(ctx, sh)
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
		case entimportscanshow.OutcomeCreated:
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
// on-disk file to its episode in place. Missing episodes stay wanted.
func (s *Service) commitShow(
	ctx context.Context, sc *ent.ImportScanShow,
) (entimportscanshow.Outcome, string, uint32) {
	ctx, span := tracer.Start(ctx, "bulkimport.commit_show",
		trace.WithAttributes(
			attribute.Int64("show.id", int64(sc.ID)),
			attribute.String("show.folder", sc.FolderPath)))
	defer span.End()

	show, outcome, msg, id := s.resolveShow(ctx, sc)
	if show == nil {
		return outcome, msg, id
	}

	files, err := library.ListVideoFilesRecursive(sc.FolderPath)
	if err != nil {
		return commitShowFail("list folder", err, show.ID)
	}
	anime := show.Type == enttvshow.TypeAnime
	matched := 0
	for _, f := range files {
		parsed := library.Parse(filepath.Base(f))
		target := library.MatchEpisode(parsed, show.Edges.Seasons, anime)
		if target == nil {
			slog.WarnContext(ctx, "series adopt: file matched no episode",
				"file", filepath.Base(f), "tvshow.id", show.ID)
			continue
		}
		// Don't double-link an episode that already has a file (existing shows).
		if _, err := s.store.FindMediaFileByEpisodeID(ctx, target.ID); err == nil {
			continue
		} else if !ent.IsNotFound(err) {
			slog.WarnContext(ctx, "series adopt: media file lookup failed",
				"episode.id", target.ID, "error", err)
			continue
		}
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
		if _, err := s.store.CreateMediaFile(ctx, db.CreateMediaFileParams{
			EpisodeID:    target.ID,
			Path:         f,
			Size:         info.Size(),
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
	return entimportscanshow.OutcomeCreated, "", show.ID
}

// resolveShow returns the eager-loaded show to adopt into, creating it from TVDB
// when the row isn't linked to an existing show. On failure it returns a nil
// show plus the outcome triple to record.
func (s *Service) resolveShow(
	ctx context.Context, sc *ent.ImportScanShow,
) (*ent.TVShow, entimportscanshow.Outcome, string, uint32) {
	if sc.ExistingTvshowID != nil {
		show, err := s.store.FindTVShowByID(ctx, *sc.ExistingTvshowID)
		if err != nil {
			o, m, id := commitShowFail("load existing show", err, 0)
			return nil, o, m, id
		}
		return show, "", "", 0
	}

	// Reviewer's pick wins over the classifier's top match.
	tvdbID := uint32(0)
	if sc.DecisionTvdbID != nil {
		tvdbID = *sc.DecisionTvdbID
	} else if sc.TvdbID != nil {
		tvdbID = *sc.TvdbID
	}
	if tvdbID == 0 {
		return nil, entimportscanshow.OutcomeFailed, "no tvdb match to adopt", 0
	}

	created, err := s.seriesAdder.Add(ctx, tvdbID, "")
	if err != nil {
		o, m, id := commitShowFail("add show", err, 0)
		return nil, o, m, id
	}
	show, err := s.store.FindTVShowByID(ctx, created.ID)
	if err != nil {
		o, m, id := commitShowFail("load created show", err, created.ID)
		return nil, o, m, id
	}
	return show, "", "", 0
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

func (s *Service) markScanFailed(ctx context.Context, scanID uint32, reason string) {
	now := time.Now()
	if err := s.store.UpdateImportScanStatus(
		ctx,
		scanID,
		entimportscan.StatusFailed,
		db.UpdateScanStatusOpts{
			FailureReason: &reason,
			CommittedAt:   &now,
		},
	); err != nil {
		slog.ErrorContext(ctx, "series commit: failed to mark scan failed",
			"scan.id", scanID, "error", err)
	}
}
