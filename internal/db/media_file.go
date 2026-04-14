package db

import (
	"context"
	"fmt"
	"time"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/episode"
	"github.com/datahearth/streamline/ent/mediafile"
	"github.com/datahearth/streamline/ent/movie"
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
	return q.Save(ctx)
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

func (db *DB) ListAllMediaFilesWithMovie(
	ctx context.Context,
) ([]*ent.MediaFile, error) {
	rows, err := db.client.MediaFile.Query().
		WithMovie().
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list media_files with movie: %w", err)
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

func (db *DB) BumpMediaFileLastSeen(ctx context.Context, id uint32) error {
	if err := db.client.MediaFile.UpdateOneID(id).
		SetLastSeenAt(time.Now()).
		Exec(ctx); err != nil {
		return fmt.Errorf("bump last_seen_at for media_file %d: %w", id, err)
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
