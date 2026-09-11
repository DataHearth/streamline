package hygiene

import (
	"context"
	"errors"
	"io/fs"
	"log/slog"
	"maps"
	"os"
	"slices"
	"time"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/internal/db"
	"github.com/datahearth/streamline/internal/events"
	"github.com/datahearth/streamline/internal/otelx"
	"github.com/datahearth/streamline/internal/scheduler"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// RunDriftCheck verifies every tracked MediaFile is still present on disk.
// Missing files start (or advance) a grace clock keyed off last_seen_at; once
// the file has been gone for at least cfg.DriftGraceTicks intervals the row is
// deleted and its owning movie or episode reverts to "wanted".
func (s *Service) RunDriftCheck(ctx context.Context, interval time.Duration) error {
	graceWindow := interval * time.Duration(s.cfg.DriftGraceTicks)

	ctx, span := tracer.Start(ctx, "hygiene.drift_check")
	defer span.End()

	// Walked a page at a time over a three-column projection rather than
	// materialising the whole table with both owner edges attached. The check
	// stats a path and compares a timestamp; the owners are only consulted on
	// the rare branch where a file has actually gone, and are fetched there.
	var (
		afterID uint32
		total   int
	)
	// Deferred rather than flushed after the loop: the rows already marked
	// missing would otherwise lose their event for good on an aborted sweep,
	// since the next tick sees them as no longer newly missing.
	acc := newDriftAccumulator()
	defer acc.flush(ctx)
	present := make([]uint32, 0, driftPageSize)
	for {
		rows, err := s.store.ListMediaFilesForDrift(ctx, afterID, driftPageSize)
		if err != nil {
			return otelx.RecordSpanError(span, err)
		}
		if len(rows) == 0 {
			break
		}
		total += len(rows)
		for _, row := range rows {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if s.checkDrift(ctx, row, graceWindow, acc) {
				present = append(present, row.ID)
			}
			afterID = row.ID
		}
		scheduler.Progress(ctx, total, 0)
		// Flushed per page so the bump list cannot itself grow with the
		// library — the batching that keeps these off the single SQLite
		// connection one-by-one is inside BumpMediaFilesLastSeen.
		if len(present) > 0 {
			if err := s.store.BumpMediaFilesLastSeen(ctx, present); err != nil {
				return otelx.RecordSpanError(span, err)
			}
			driftVerified.Add(ctx, int64(len(present)))
			present = present[:0]
		}
	}
	span.SetAttributes(attribute.Int("rows", total))
	return nil
}

// checkDrift reports whether the row's file is present on disk; the caller
// batches the last_seen bookkeeping for present rows. Missing and erroring
// rows are handled here and report false.
// driftPageSize is how many rows one keyset page carries. Big enough that a
// modest library is one or two queries, small enough that the slice never
// scales with the table.
const driftPageSize = 500

func (s *Service) checkDrift(
	ctx context.Context,
	row db.DriftRow,
	graceWindow time.Duration,
	acc driftAccumulator,
) bool {
	_, statErr := os.Stat(row.Path)
	switch {
	case statErr == nil:
		return true
	case errors.Is(statErr, fs.ErrNotExist):
		s.handleMissing(ctx, row, graceWindow, acc)
	default:
		slog.WarnContext(ctx, "stat failed (transient)",
			"path", row.Path, "error", statErr)
		driftStatErrors.Add(ctx, 1, metric.WithAttributes(
			attribute.String("error_kind", classifyStatErr(statErr)),
		))
	}
	return false
}

func (s *Service) handleMissing(
	ctx context.Context,
	lean db.DriftRow,
	graceWindow time.Duration,
	acc driftAccumulator,
) {
	driftDrifted.Add(ctx, 1)

	// The owners are needed from here on — to attribute the event and, past
	// the grace window, to revert the right media. This is the one branch
	// that pays for them, which is why the walk itself does not.
	row, err := s.store.FindMediaFileWithOwners(ctx, lean.ID)
	if err != nil {
		slog.WarnContext(ctx, "load media file owners failed",
			"media_file.id", lean.ID, "error", err)
		return
	}

	first, err := s.store.MarkMediaFileMissing(ctx, row.ID)
	if err != nil {
		slog.WarnContext(ctx, "mark missing failed",
			"media_file.id", row.ID, "error", err)
	}
	showID, season, aggregated := episodeOwner(row)
	if first {
		// The aggregated payload carries no path, so this line is the only
		// place the individual file is named.
		slog.InfoContext(ctx, "drift detected a missing file",
			"media_file.id", row.ID, "media_file.path", row.Path)
		if aggregated {
			acc.detect(showID, season)
		} else {
			s.recordDrift(ctx, row, events.TypeDriftDetected, map[string]any{
				"path": row.Path,
			})
		}
	}

	// First-tick free pass: NULL last_seen_at → start grace clock. The write
	// must leave missing_since in place or the next tick re-detects the same
	// disappearance.
	if row.LastSeenAt == nil {
		if err := s.store.StartMediaFileGraceClock(ctx, row.ID); err != nil {
			slog.WarnContext(ctx, "start grace clock failed",
				"media_file_id", row.ID, "error", err)
		}
		return
	}
	if time.Since(*row.LastSeenAt) < graceWindow {
		return
	}

	slog.InfoContext(ctx, "drift confirmed a missing file",
		"media_file.id", row.ID, "media_file.path", row.Path,
		"missing_since", row.LastSeenAt.UTC())
	if aggregated {
		acc.confirm(showID, season, *row.LastSeenAt)
	} else {
		s.recordDrift(ctx, row, events.TypeDriftConfirmed, map[string]any{
			"path":          row.Path,
			"missing_since": row.LastSeenAt.UTC(),
		})
	}

	switch {
	case row.Edges.Movie != nil:
		s.revertMovie(ctx, row.ID, row.Edges.Movie.ID)
	case row.Edges.Episode != nil:
		s.revertEpisode(ctx, row.ID, row.Edges.Episode.ID)
	default:
		s.deleteOrphan(ctx, row)
	}
}

// recordDrift attributes a drift event to the row's owner. An ownerless row
// (a legacy orphan, reaped below) has nobody to tell, so it is skipped.
func (s *Service) recordDrift(
	ctx context.Context,
	row *ent.MediaFile,
	t events.Type,
	payload map[string]any,
) {
	var scope events.Scope
	var id uint32
	switch {
	case row.Edges.Movie != nil:
		scope, id = events.ScopeMovie, row.Edges.Movie.ID
	case row.Edges.Episode != nil:
		scope, id = events.ScopeEpisode, row.Edges.Episode.ID
	default:
		return
	}
	if err := events.Record(ctx, nil, t, scope, id, payload); err != nil {
		slog.WarnContext(ctx, "record drift event failed",
			"media_file.id", row.ID, "event.type", string(t), "error", err)
	}
}

// episodeOwner resolves the show and season an episode-owned file hangs off,
// and reports whether the file's drift belongs in the per-show aggregate. A
// movie or ownerless row is not aggregated, and neither is an episode whose
// season or show edge failed to load — that keeps the per-episode event rather
// than dropping the drift record on the floor.
func episodeOwner(row *ent.MediaFile) (uint32, uint16, bool) {
	ep := row.Edges.Episode
	if ep == nil || ep.Edges.Season == nil || ep.Edges.Season.Edges.TvShow == nil {
		return 0, 0, false
	}
	return ep.Edges.Season.Edges.TvShow.ID, ep.Edges.Season.Number, true
}

// driftTally is one show's worth of episode files that drifted the same way in
// one sweep.
type driftTally struct {
	seasons      map[uint16]struct{}
	files        int
	missingSince time.Time
}

func (t *driftTally) add(season uint16, missingSince time.Time) {
	t.seasons[season] = struct{}{}
	t.files++
	if !missingSince.IsZero() &&
		(t.missingSince.IsZero() || missingSince.Before(t.missingSince)) {
		t.missingSince = missingSince
	}
}

func (t *driftTally) payload() map[string]any {
	payload := map[string]any{
		"seasons":  slices.Sorted(maps.Keys(t.seasons)),
		"episodes": t.files,
	}
	if !t.missingSince.IsZero() {
		payload["missing_since"] = t.missingSince.UTC()
	}
	return payload
}

// driftAccumulator folds a sweep's episode-owned drift into one event per show
// per type. A show whose folder disappears drifts every episode it has at
// once, and a row per file per tick buried the rest of the history under a
// single incident.
type driftAccumulator struct {
	detected  map[uint32]*driftTally
	confirmed map[uint32]*driftTally
}

func newDriftAccumulator() driftAccumulator {
	return driftAccumulator{
		detected:  make(map[uint32]*driftTally),
		confirmed: make(map[uint32]*driftTally),
	}
}

func (a driftAccumulator) detect(showID uint32, season uint16) {
	tallyFor(a.detected, showID).add(season, time.Time{})
}

func (a driftAccumulator) confirm(
	showID uint32,
	season uint16,
	missingSince time.Time,
) {
	tallyFor(a.confirmed, showID).add(season, missingSince)
}

func tallyFor(tallies map[uint32]*driftTally, showID uint32) *driftTally {
	t, ok := tallies[showID]
	if !ok {
		t = &driftTally{seasons: make(map[uint16]struct{})}
		tallies[showID] = t
	}
	return t
}

func (a driftAccumulator) flush(ctx context.Context) {
	recordShowDrift(ctx, events.TypeDriftDetected, a.detected)
	recordShowDrift(ctx, events.TypeDriftConfirmed, a.confirmed)
}

func recordShowDrift(
	ctx context.Context,
	t events.Type,
	tallies map[uint32]*driftTally,
) {
	for showID, tally := range tallies {
		if err := events.Record(
			ctx, nil, t, events.ScopeSeries, showID, tally.payload(),
		); err != nil {
			slog.WarnContext(ctx, "record drift event failed",
				"tvshow.id", showID, "event.type", string(t), "error", err)
		}
	}
}

func (s *Service) revertMovie(ctx context.Context, mediaFileID, movieID uint32) {
	ctx = events.SuppressFileRemoved(ctx)
	if err := s.store.DeleteMediaFileAndRevertMovie(
		ctx,
		mediaFileID,
		movieID,
	); err != nil {
		slog.ErrorContext(ctx, "drift revert failed",
			"media_file.id", mediaFileID, "movie.id", movieID, "error", err)
		return
	}
	// The id belongs in the log, not on the counter: as a metric attribute it
	// is one time series per movie in the library.
	driftReverted.Add(ctx, 1, metric.WithAttributes(
		attribute.String("media.kind", "movie"),
	))
	// deleteOrphan below logs its own removals; this one only had the counter,
	// so "why did this movie go back to wanted overnight" was answerable from
	// the events table and not from the log stream an operator actually greps.
	slog.InfoContext(ctx, "drift reverted media file",
		"media_file.id", mediaFileID, "movie.id", movieID)
}

func (s *Service) revertEpisode(ctx context.Context, mediaFileID, episodeID uint32) {
	ctx = events.SuppressFileRemoved(ctx)
	if err := s.store.DeleteMediaFileAndRevertEpisode(
		ctx,
		mediaFileID,
		episodeID,
	); err != nil {
		slog.ErrorContext(ctx, "drift revert failed",
			"media_file.id", mediaFileID, "episode.id", episodeID, "error", err)
		return
	}
	driftReverted.Add(ctx, 1, metric.WithAttributes(
		attribute.String("media.kind", "episode"),
	))
	slog.InfoContext(ctx, "drift reverted media file",
		"media_file.id", mediaFileID, "episode.id", episodeID)
}

// deleteOrphan reaps a row whose owner is gone. Movie deletes predating the
// cascading FKs left media_files rows behind with a NULL owner; without this
// they would be re-examined, and warned about, on every tick forever.
func (s *Service) deleteOrphan(ctx context.Context, row *ent.MediaFile) {
	if err := s.store.DeleteMediaFile(ctx, row.ID); err != nil {
		slog.ErrorContext(ctx, "drift orphan delete failed",
			"media_file.id", row.ID, "error", err)
		return
	}
	driftOrphansDeleted.Add(ctx, 1)
	slog.InfoContext(ctx, "drift deleted an ownerless media file",
		"media_file.id", row.ID, "media_file.path", row.Path)
}

func classifyStatErr(err error) string {
	switch {
	case errors.Is(err, fs.ErrPermission):
		return "permission"
	case errors.Is(err, fs.ErrInvalid):
		return "invalid"
	default:
		return "io"
	}
}
