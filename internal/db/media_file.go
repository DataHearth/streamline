package db

import (
	"context"
	"fmt"
	"time"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/episode"
	"github.com/datahearth/streamline/ent/mediafile"
	"github.com/datahearth/streamline/ent/movie"
	"github.com/datahearth/streamline/internal/ffmpeg"
)

type CreateMediaFileParams struct {
	Path         string
	Size         int64
	Quality      string
	Format       string
	ReleaseGroup string
	MovieID      uint32 // set for movie files; mutually exclusive with EpisodeID
	EpisodeID    uint32 // set for episode files (e.g. series adoption)
	Source       mediafile.Source
	Probe        *ffmpeg.Info // set to stamp probed_at at create time; nil leaves probed_at NULL for the backfill
}

// applyProbe copies probe info onto a MediaFile create/update builder. One
// place, because three write paths and the backfill job all set these fields.
// A nil info still stamps probed_at, so a failed probe is not retried forever.
func applyProbe[T interface {
	SetContainer(string) T
	SetDurationSeconds(uint32) T
	SetVideoCodec(string) T
	SetWidth(uint16) T
	SetHeight(uint16) T
	SetAudioCodec(string) T
	SetAudioChannels(uint8) T
	SetBitrate(uint32) T
	SetProbedAt(time.Time) T
}](q T, info *ffmpeg.Info) T {
	q = q.SetProbedAt(time.Now())
	if info == nil {
		return q
	}
	return q.SetContainer(info.Container).
		SetDurationSeconds(info.DurationSec).
		SetVideoCodec(info.VideoCodec).
		SetWidth(info.Width).
		SetHeight(info.Height).
		SetAudioCodec(info.AudioCodec).
		SetAudioChannels(info.AudioChannels).
		SetBitrate(info.BitrateBPS)
}

func (db *DB) CreateMediaFile(
	ctx context.Context,
	p CreateMediaFileParams,
) (*ent.MediaFile, error) {
	q := db.client.MediaFile.Create().
		SetPath(p.Path).
		SetSize(p.Size).
		SetQuality(p.Quality).
		SetFormat(p.Format).
		SetReleaseGroup(p.ReleaseGroup)
	if p.MovieID != 0 {
		q = q.SetMovieID(p.MovieID)
	}
	if p.EpisodeID != 0 {
		q = q.SetEpisodeID(p.EpisodeID)
	}
	if p.Source != "" {
		q = q.SetSource(p.Source)
	}
	if p.Probe != nil {
		q = applyProbe(q, p.Probe)
	}
	return q.Save(ctx)
}

// ListUnprobedMediaFiles returns up to limit rows that have never been probed
// (probed_at IS NULL), oldest-first, for the backfill job to work through.
func (db *DB) ListUnprobedMediaFiles(
	ctx context.Context,
	limit int,
) ([]*ent.MediaFile, error) {
	rows, err := db.client.MediaFile.Query().
		Where(mediafile.ProbedAtIsNil()).
		Order(ent.Asc(mediafile.FieldID)).
		Limit(limit).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list unprobed media_files: %w", err)
	}
	return rows, nil
}

// StampMediaFileProbe records a probe attempt's result. A nil info (failed
// probe) still sets probed_at, so ListUnprobedMediaFiles never re-selects it.
func (db *DB) StampMediaFileProbe(
	ctx context.Context,
	id uint32,
	info *ffmpeg.Info,
) error {
	if err := applyProbe(db.client.MediaFile.UpdateOneID(id), info).
		Exec(ctx); err != nil {
		return fmt.Errorf("stamp probe on media_file %d: %w", id, err)
	}
	return nil
}

// FindMediaFileByID returns a single MediaFile by ID, or ent NotFound.
func (db *DB) FindMediaFileByID(
	ctx context.Context,
	id uint32,
) (*ent.MediaFile, error) {
	return db.client.MediaFile.Get(ctx, id)
}

// FindMediaFileByEpisodeID returns the MediaFile owned by an episode (an
// episode has at most one), or ent NotFound when it has none.
func (db *DB) FindMediaFileByEpisodeID(
	ctx context.Context,
	episodeID uint32,
) (*ent.MediaFile, error) {
	return db.client.MediaFile.Query().
		Where(mediafile.HasEpisodeWith(episode.ID(episodeID))).
		Only(ctx)
}

func (db *DB) MovieHasMediaFile(ctx context.Context, tmdbID uint32) (bool, error) {
	n, err := db.client.MediaFile.Query().
		Where(mediafile.HasMovieWith(movie.TmdbID(tmdbID))).
		Count(ctx)
	if err != nil {
		return false, fmt.Errorf("count media_files for tmdb_id %d: %w", tmdbID, err)
	}
	return n > 0, nil
}

func (db *DB) ListAllMediaFilesWithOwners(
	ctx context.Context,
) ([]*ent.MediaFile, error) {
	rows, err := db.client.MediaFile.Query().
		WithMovie(withLeanMovie).
		WithEpisode().
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list media_files with owners: %w", err)
	}
	return rows, nil
}

func (db *DB) ListMediaFilesByMovieID(
	ctx context.Context,
	movieID uint32,
) ([]*ent.MediaFile, error) {
	rows, err := db.client.MediaFile.Query().
		Where(mediafile.HasMovieWith(movie.ID(movieID))).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list media_files for movie %d: %w", movieID, err)
	}
	return rows, nil
}

func (db *DB) CountMovieMediaFiles(ctx context.Context) (int, error) {
	n, err := db.client.MediaFile.Query().
		Where(mediafile.HasMovie()).
		Count(ctx)
	if err != nil {
		return 0, fmt.Errorf("count movie media_files: %w", err)
	}
	return n, nil
}

func (db *DB) CountEpisodeMediaFiles(ctx context.Context) (int, error) {
	n, err := db.client.MediaFile.Query().
		Where(mediafile.HasEpisode()).
		Count(ctx)
	if err != nil {
		return 0, fmt.Errorf("count episode media_files: %w", err)
	}
	return n, nil
}

func (db *DB) ListMediaFilesByPathPrefix(
	ctx context.Context,
	prefix string,
) ([]*ent.MediaFile, error) {
	rows, err := db.client.MediaFile.Query().
		Where(mediafile.PathHasPrefix(prefix)).
		Order(ent.Asc(mediafile.FieldPath)).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list media_files under %s: %w", prefix, err)
	}
	return rows, nil
}

func (db *DB) UpdateMediaFilePath(
	ctx context.Context,
	id uint32,
	path string,
) error {
	if err := db.client.MediaFile.UpdateOneID(id).
		SetPath(path).
		Exec(ctx); err != nil {
		return fmt.Errorf("update media_file %d path: %w", id, err)
	}
	return nil
}

// StartMediaFileGraceClock stamps last_seen_at for a row that never had one,
// so a missing file gets a full grace window instead of instant deletion. It
// deliberately leaves missing_since alone: clearing it here made the next
// tick's MarkMediaFileMissing report "first" again and drift_detected fired
// twice for one disappearance.
func (db *DB) StartMediaFileGraceClock(ctx context.Context, id uint32) error {
	if err := db.client.MediaFile.UpdateOneID(id).
		SetLastSeenAt(time.Now()).
		Exec(ctx); err != nil {
		return fmt.Errorf("start grace clock for media_file %d: %w", id, err)
	}
	return nil
}

// BumpMediaFilesLastSeen is BumpMediaFileLastSeen over a batch. drift-check
// verifies the whole library every tick; issuing the bumps row by row held
// SQLite's single connection through thousands of UPDATEs and every API
// request queued behind them. Chunked so the IN list stays well under
// SQLite's bound-variable cap.
func (db *DB) BumpMediaFilesLastSeen(ctx context.Context, ids []uint32) error {
	const chunkSize = 500
	now := time.Now()
	for start := 0; start < len(ids); start += chunkSize {
		chunk := ids[start:min(start+chunkSize, len(ids))]
		if err := db.client.MediaFile.Update().
			Where(mediafile.IDIn(chunk...)).
			SetLastSeenAt(now).
			ClearMissingSince().
			Exec(ctx); err != nil {
			return fmt.Errorf(
				"bump last_seen_at for %d media_files: %w", len(chunk), err,
			)
		}
	}
	return nil
}

// MarkMediaFileMissing stamps missing_since, reporting whether this call was
// the one that set it. Only the first missing tick returns true, which is the
// edge the drift_detected event hangs off.
func (db *DB) MarkMediaFileMissing(
	ctx context.Context,
	id uint32,
) (bool, error) {
	n, err := db.client.MediaFile.Update().
		Where(mediafile.ID(id), mediafile.MissingSinceIsNil()).
		SetMissingSince(time.Now()).
		Save(ctx)
	if err != nil {
		return false, fmt.Errorf("mark media_file %d missing: %w", id, err)
	}
	return n > 0, nil
}

// DeleteMediaFile removes a MediaFile row without touching any owner. Used by
// drift_check to reap rows whose owner is gone (legacy orphans left behind by
// the pre-cascade movie delete).
func (db *DB) DeleteMediaFile(ctx context.Context, id uint32) error {
	if err := db.client.MediaFile.DeleteOneID(id).Exec(ctx); err != nil {
		return fmt.Errorf("delete media_file %d: %w", id, err)
	}
	return nil
}

// DeleteMediaFileAndRevertMovie removes the MediaFile row and flips the
// owning movie's status back to "wanted" inside a single transaction. Used
// by drift_check when a tracked file disappears from disk.
func (db *DB) DeleteMediaFileAndRevertMovie(
	ctx context.Context,
	mediaFileID, movieID uint32,
) error {
	tx, err := db.client.Tx(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	if err := tx.MediaFile.DeleteOneID(mediaFileID).Exec(ctx); err != nil {
		tx.Rollback()
		return fmt.Errorf("delete media_file %d: %w", mediaFileID, err)
	}
	if err := tx.Movie.UpdateOneID(movieID).
		SetStatus(movie.StatusWanted).
		Exec(ctx); err != nil {
		tx.Rollback()
		return fmt.Errorf("revert movie %d to wanted: %w", movieID, err)
	}
	return tx.Commit()
}

// DeleteMediaFileAndRevertEpisode removes the MediaFile row and flips the
// owning episode's status back to "wanted" inside a single transaction.
func (db *DB) DeleteMediaFileAndRevertEpisode(
	ctx context.Context,
	mediaFileID, episodeID uint32,
) error {
	tx, err := db.client.Tx(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	if err := tx.MediaFile.DeleteOneID(mediaFileID).Exec(ctx); err != nil {
		tx.Rollback()
		return fmt.Errorf("delete media_file %d: %w", mediaFileID, err)
	}
	if err := tx.Episode.UpdateOneID(episodeID).
		SetStatus(episode.StatusWanted).
		Exec(ctx); err != nil {
		tx.Rollback()
		return fmt.Errorf("revert episode %d to wanted: %w", episodeID, err)
	}
	return tx.Commit()
}
