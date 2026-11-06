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
	"github.com/datahearth/streamline/ent/mediafile"
	"github.com/datahearth/streamline/internal/db"
	"github.com/datahearth/streamline/internal/events"
	"github.com/datahearth/streamline/internal/library"
	"github.com/datahearth/streamline/internal/library/roothealth"
	"github.com/datahearth/streamline/internal/otelx"
	"github.com/datahearth/streamline/internal/scheduler"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

// RunDriftCheck verifies every tracked MediaFile is still present on disk.
// A missing file starts a grace clock at missing_since; once the file has been
// gone for at least cfg.DriftGraceTicks intervals the row is deleted and its
// owner reverts to "wanted". The clock starts when a tick
// first sees the file gone, not when one last saw it: after downtime longer
// than the window, a volume not yet mounted would otherwise revert the whole
// library on the first tick.
func (s *Service) RunDriftCheck(ctx context.Context, interval time.Duration) error {
	graceWindow := interval * time.Duration(s.cfg.DriftGraceTicks)

	ctx, span := tracer.Start(ctx, "hygiene.drift_check")
	defer span.End()

	offline, err := roothealth.AllOffline(ctx)
	if err != nil {
		return otelx.RecordSpanError(span, err)
	}
	skip := make([]db.LibraryRoot, len(offline))
	for i, status := range offline {
		skip[i] = status.Root
	}

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
	var returned []uint32
	for {
		rows, err := s.store.ListMediaFilesForDrift(
			ctx,
			afterID,
			driftPageSize,
			skip...)
		if err != nil {
			return otelx.RecordSpanError(span, err)
		}
		if len(rows) == 0 {
			break
		}
		total += len(rows)
		var verified int64
		for _, row := range rows {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if s.checkDrift(ctx, row, graceWindow, acc) {
				verified++
				if row.MissingSince != nil {
					returned = append(returned, row.ID)
				}
			}
			afterID = row.ID
		}
		scheduler.Progress(ctx, total, 0)
		driftVerified.Add(ctx, verified)
		if len(returned) > 0 {
			if err := s.store.ClearMediaFilesMissing(ctx, returned); err != nil {
				return otelx.RecordSpanError(span, err)
			}
			returned = returned[:0]
		}
	}
	span.SetAttributes(attribute.Int("rows", total))
	return nil
}

// checkDrift reports whether the row's file is present on disk. Missing and
// erroring rows are handled here and report false.
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
	group, season, aggregated := driftGroupOf(row)
	if first {
		// The aggregated payload carries no path, so this line is the only
		// place the individual file is named.
		slog.InfoContext(ctx, "drift detected a missing file",
			"media_file.id", row.ID, "media_file.path", row.Path)
		if aggregated {
			acc.detect(group, season)
		} else {
			s.recordDrift(ctx, row, events.TypeDriftDetected, map[string]any{
				"path": row.Path,
			})
		}
	}

	// row was read before MarkMediaFileMissing, so nil means this tick is the
	// one that started the clock.
	if row.MissingSince == nil ||
		time.Since(*row.MissingSince) < graceWindow {
		return
	}
	// The skip list is from the start of the sweep, and a clock can be old
	// because an earlier outage started it and offline ticks never cleared it,
	// so a mount dying now must not make this past grace at once.
	if root, owned := db.RootOfFile(row); owned {
		status, err := roothealth.Offline(ctx, root)
		if err != nil {
			slog.WarnContext(ctx, "could not check the library root before a revert",
				"media_file.id", row.ID, "error", err)
			return
		}
		if status != nil {
			return
		}
	}
	s.confirmMissing(ctx, row, *row.MissingSince, acc)
}

// ErrRootOnline refuses AcceptMissing on a root that is not offline: the volume
// may have returned while the dialog was open.
var ErrRootOnline = errors.New("this library root is online; nothing was removed")

// ErrAcceptRunning refuses a second walk while one is removing rows.
var ErrAcceptRunning = errors.New("missing files are already being removed")

// confirmMissing records a file as gone for good and removes its row, reverting
// its owner. It reports whether the row was removed.
func (s *Service) confirmMissing(
	ctx context.Context,
	row *ent.MediaFile,
	missingSince time.Time,
	acc driftAccumulator,
) bool {
	slog.InfoContext(ctx, "drift confirmed a missing file",
		"media_file.id", row.ID, "media_file.path", row.Path,
		"missing_since", missingSince.UTC())
	if group, season, aggregated := driftGroupOf(row); aggregated {
		acc.confirm(group, season, missingSince)
	} else {
		s.recordDrift(ctx, row, events.TypeDriftConfirmed, map[string]any{
			"path":          row.Path,
			"missing_since": missingSince.UTC(),
		})
	}

	switch {
	case row.Edges.Movie != nil:
		return s.revertMovie(ctx, row.ID, row.Edges.Movie.ID)
	case row.Edges.Episode != nil:
		return s.revertEpisode(ctx, row.ID, row.Edges.Episode.ID)
	case row.Edges.Track != nil && row.Edges.Track.Edges.Album != nil:
		return s.revertAlbum(ctx, row.ID, row.Edges.Track.Edges.Album.ID)
	case row.Edges.Book != nil:
		return s.revertBookSlot(ctx, row.ID, row.Edges.Book.ID, row.BookKind)
	default:
		return s.deleteOrphan(ctx, row)
	}
}

// AcceptMissing removes the rows of an offline root whose file is not found,
// ignoring the grace window: an admin has said the files are really gone. The
// check samples at most 20 rows, so an offline root can still hold files, and a
// file that is there or that cannot be checked keeps its row. Afterwards every
// remaining row resolves or none remain, so the root reads online with no state
// stored for it.
func (s *Service) AcceptMissing(
	ctx context.Context,
	root db.LibraryRoot,
) (int, error) {
	ctx, span := tracer.Start(ctx, "hygiene.accept_missing",
		trace.WithAttributes(attribute.String("library.root", string(root))))
	defer span.End()

	if !s.acceptMu.TryLock() {
		return 0, otelx.RecordSpanError(span, ErrAcceptRunning)
	}
	defer s.acceptMu.Unlock()

	status, err := roothealth.Offline(ctx, root)
	if err != nil {
		return 0, otelx.RecordSpanError(span, err)
	}
	if status == nil {
		return 0, otelx.RecordSpanError(span, ErrRootOnline)
	}

	acc := newDriftAccumulator()
	defer acc.flush(ctx)
	var (
		afterID uint32
		removed int
	)
	for {
		rows, err := s.store.ListRootFilesForDrift(ctx, root, afterID, driftPageSize)
		if err != nil {
			return removed, otelx.RecordSpanError(span, err)
		}
		if len(rows) == 0 {
			break
		}
		for _, lean := range rows {
			if err := ctx.Err(); err != nil {
				return removed, otelx.RecordSpanError(span, err)
			}
			afterID = lean.ID
			if _, err := os.Stat(lean.Path); !errors.Is(err, fs.ErrNotExist) {
				continue
			}
			row, err := s.store.FindMediaFileWithOwners(ctx, lean.ID)
			if err != nil {
				slog.WarnContext(ctx, "load media file owners failed",
					"media_file.id", lean.ID, "error", err)
				continue
			}
			since := time.Now()
			if row.MissingSince != nil {
				since = *row.MissingSince
			}
			if s.confirmMissing(ctx, row, since, acc) {
				removed++
			}
		}
	}
	span.SetAttributes(attribute.Int("removed", removed))
	return removed, nil
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

// driftGroup is the owner a file's drift is folded into: a show, an album or
// one slot of a book. Files of these go missing together, and a row per file
// per tick buried the rest of the history under a single incident.
type driftGroup struct {
	scope    events.Scope
	id       uint32
	bookKind string
}

// driftGroupOf resolves the group a file's drift belongs to, with the season
// when that is a show, and reports whether it has one. A movie or ownerless
// row does not, and neither does an episode whose season or show edge failed
// to load — that keeps the per-episode event rather than dropping the drift
// record on the floor.
func driftGroupOf(row *ent.MediaFile) (driftGroup, uint16, bool) {
	switch {
	case row.Edges.Episode != nil:
		ep := row.Edges.Episode
		if ep.Edges.Season == nil || ep.Edges.Season.Edges.TvShow == nil {
			return driftGroup{}, 0, false
		}
		return driftGroup{
			scope: events.ScopeSeries,
			id:    ep.Edges.Season.Edges.TvShow.ID,
		}, ep.Edges.Season.Number, true
	case row.Edges.Track != nil && row.Edges.Track.Edges.Album != nil:
		return driftGroup{
			scope: events.ScopeAlbum,
			id:    row.Edges.Track.Edges.Album.ID,
		}, 0, true
	case row.Edges.Book != nil:
		return driftGroup{
			scope:    events.ScopeBook,
			id:       row.Edges.Book.ID,
			bookKind: string(row.BookKind),
		}, 0, true
	default:
		return driftGroup{}, 0, false
	}
}

// driftTally is one group's worth of files that drifted the same way in one
// sweep.
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

func (t *driftTally) payload(g driftGroup) map[string]any {
	var payload map[string]any
	switch g.scope {
	case events.ScopeAlbum:
		payload = map[string]any{"tracks": t.files}
	case events.ScopeBook:
		payload = map[string]any{"files": t.files, "book_kind": g.bookKind}
	default:
		payload = map[string]any{
			"seasons":  slices.Sorted(maps.Keys(t.seasons)),
			"episodes": t.files,
		}
	}
	if !t.missingSince.IsZero() {
		payload["missing_since"] = t.missingSince.UTC()
	}
	return payload
}

// driftAccumulator folds a sweep's drift into one event per group per type.
type driftAccumulator struct {
	detected  map[driftGroup]*driftTally
	confirmed map[driftGroup]*driftTally
}

func newDriftAccumulator() driftAccumulator {
	return driftAccumulator{
		detected:  make(map[driftGroup]*driftTally),
		confirmed: make(map[driftGroup]*driftTally),
	}
}

func (a driftAccumulator) detect(g driftGroup, season uint16) {
	tallyFor(a.detected, g).add(season, time.Time{})
}

func (a driftAccumulator) confirm(
	g driftGroup,
	season uint16,
	missingSince time.Time,
) {
	tallyFor(a.confirmed, g).add(season, missingSince)
}

func tallyFor(tallies map[driftGroup]*driftTally, g driftGroup) *driftTally {
	t, ok := tallies[g]
	if !ok {
		t = &driftTally{seasons: make(map[uint16]struct{})}
		tallies[g] = t
	}
	return t
}

func (a driftAccumulator) flush(ctx context.Context) {
	recordGroupDrift(ctx, events.TypeDriftDetected, a.detected)
	recordGroupDrift(ctx, events.TypeDriftConfirmed, a.confirmed)
}

func recordGroupDrift(
	ctx context.Context,
	t events.Type,
	tallies map[driftGroup]*driftTally,
) {
	for g, tally := range tallies {
		if err := events.Record(
			ctx, nil, t, g.scope, g.id, tally.payload(g),
		); err != nil {
			slog.WarnContext(ctx, "record drift event failed",
				"owner.scope", g.scope.String(), "owner.id", g.id,
				"event.type", string(t), "error", err)
		}
	}
}

func (s *Service) revertMovie(
	ctx context.Context,
	mediaFileID, movieID uint32,
) bool {
	ctx = events.SuppressFileRemoved(ctx)
	if err := s.store.DeleteMediaFileAndRevertMovie(
		ctx,
		mediaFileID,
		movieID,
	); err != nil {
		slog.ErrorContext(ctx, "drift revert failed",
			"media_file.id", mediaFileID, "movie.id", movieID, "error", err)
		return false
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
	return true
}

func (s *Service) revertEpisode(
	ctx context.Context,
	mediaFileID, episodeID uint32,
) bool {
	ctx = events.SuppressFileRemoved(ctx)
	if err := s.store.DeleteMediaFileAndRevertEpisode(
		ctx,
		mediaFileID,
		episodeID,
	); err != nil {
		slog.ErrorContext(ctx, "drift revert failed",
			"media_file.id", mediaFileID, "episode.id", episodeID, "error", err)
		return false
	}
	driftReverted.Add(ctx, 1, metric.WithAttributes(
		attribute.String("media.kind", "episode"),
	))
	slog.InfoContext(ctx, "drift reverted media file",
		"media_file.id", mediaFileID, "episode.id", episodeID)
	return true
}

func (s *Service) revertAlbum(
	ctx context.Context,
	mediaFileID, albumID uint32,
) bool {
	defer library.LockAlbumFiles(albumID)()
	ctx = events.SuppressFileRemoved(ctx)
	if err := s.store.DeleteMediaFileAndRevertAlbum(
		ctx,
		mediaFileID,
		albumID,
	); err != nil {
		slog.ErrorContext(ctx, "drift revert failed",
			"media_file.id", mediaFileID, "album.id", albumID, "error", err)
		return false
	}
	driftReverted.Add(ctx, 1, metric.WithAttributes(
		attribute.String("media.kind", "album"),
	))
	slog.InfoContext(ctx, "drift reverted media file",
		"media_file.id", mediaFileID, "album.id", albumID)
	return true
}

func (s *Service) revertBookSlot(
	ctx context.Context,
	mediaFileID, bookID uint32,
	kind mediafile.BookKind,
) bool {
	ctx = events.SuppressFileRemoved(ctx)
	if err := s.store.DeleteMediaFileAndRevertBookSlot(
		ctx,
		mediaFileID,
		bookID,
		kind,
	); err != nil {
		slog.ErrorContext(ctx, "drift revert failed",
			"media_file.id", mediaFileID, "book.id", bookID, "error", err)
		return false
	}
	driftReverted.Add(ctx, 1, metric.WithAttributes(
		attribute.String("media.kind", "book"),
	))
	slog.InfoContext(ctx, "drift reverted media file",
		"media_file.id", mediaFileID, "book.id", bookID,
		"book.kind", string(kind))
	return true
}

// deleteOrphan reaps a row whose owner is gone. Movie deletes predating the
// cascading FKs left media_files rows behind with a NULL owner; without this
// they would be re-examined, and warned about, on every tick forever.
func (s *Service) deleteOrphan(ctx context.Context, row *ent.MediaFile) bool {
	if err := s.store.DeleteMediaFile(ctx, row.ID); err != nil {
		slog.ErrorContext(ctx, "drift orphan delete failed",
			"media_file.id", row.ID, "error", err)
		return false
	}
	driftOrphansDeleted.Add(ctx, 1)
	slog.InfoContext(ctx, "drift deleted an ownerless media file",
		"media_file.id", row.ID, "media_file.path", row.Path)
	return true
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
