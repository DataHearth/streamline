package db

import (
	"context"
	"fmt"
	"time"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/downloadrecord"
	"github.com/datahearth/streamline/ent/episode"
	"github.com/datahearth/streamline/ent/mediafile"
	"github.com/datahearth/streamline/ent/movie"
	"github.com/datahearth/streamline/ent/season"
)

// withEpisodeContext eager-loads the Episode edge of an importing record along
// with its Season and TVShow, plus all of the show's seasons and their episodes
// — giving the importer the full episode set needed to match season-pack files
// (and anime absolute numbers) back to episodes.
func withEpisodeContext(q *ent.EpisodeQuery) {
	q.WithSeason(func(sq *ent.SeasonQuery) {
		sq.WithTvShow(func(tq *ent.TVShowQuery) {
			tq.WithSeasons(func(ssq *ent.SeasonQuery) { ssq.WithEpisodes() })
		})
	})
}

type CreateDownloadRecordParams struct {
	Title              string
	Size               int64
	TorrentHash        string
	Status             downloadrecord.Status
	MovieID            uint32
	EpisodeID          uint32
	DownloadClientName string
	IndexerName        string
	// Adoption proposals persist these so the pending queue and a later
	// import have the parsed quality, on-disk path, and human reason.
	SavePath      string
	Quality       string
	FailureReason string
}

func (db *DB) CreateDownloadRecord(
	ctx context.Context,
	p CreateDownloadRecordParams,
) (*ent.DownloadRecord, error) {
	b := db.client.DownloadRecord.Create().
		SetTitle(p.Title).
		SetSize(p.Size).
		SetTorrentHash(p.TorrentHash).
		SetStatus(p.Status).
		SetDownloadClientName(p.DownloadClientName).
		SetIndexerName(p.IndexerName)
	if p.SavePath != "" {
		b = b.SetSavePath(p.SavePath)
	}
	if p.Quality != "" {
		b = b.SetQuality(p.Quality)
	}
	if p.FailureReason != "" {
		b = b.SetFailureReason(p.FailureReason)
	}
	if p.MovieID != 0 {
		b = b.SetMovieID(p.MovieID)
	}
	if p.EpisodeID != 0 {
		b = b.SetEpisodeID(p.EpisodeID)
	}
	return b.Save(ctx)
}

// AllDownloadRecordHashes returns the set of non-empty torrent hashes across
// every download_record (any status). The adoption pass uses it to skip
// torrents streamline already tracks.
func (db *DB) AllDownloadRecordHashes(
	ctx context.Context,
) (map[string]struct{}, error) {
	hashes, err := db.client.DownloadRecord.Query().
		Where(downloadrecord.TorrentHashNEQ("")).
		Select(downloadrecord.FieldTorrentHash).
		Strings(ctx)
	if err != nil {
		return nil, fmt.Errorf("list download record hashes: %w", err)
	}
	set := make(map[string]struct{}, len(hashes))
	for _, h := range hashes {
		set[h] = struct{}{}
	}
	return set, nil
}

// ListPendingDownloadRecords returns all status=pending records with Movie and
// Episode (+ its season and show) edges eager-loaded for the needs-attention
// queue, newest first.
func (db *DB) ListPendingDownloadRecords(
	ctx context.Context,
) ([]*ent.DownloadRecord, error) {
	return db.client.DownloadRecord.Query().
		Where(downloadrecord.StatusEQ(downloadrecord.StatusPending)).
		WithMovie(func(mq *ent.MovieQuery) { mq.WithMediaFiles() }).
		WithEpisode(func(q *ent.EpisodeQuery) {
			q.WithMediaFiles()
			q.WithSeason(func(sq *ent.SeasonQuery) { sq.WithTvShow() })
		}).
		Order(ent.Desc(downloadrecord.FieldCreateTime)).
		All(ctx)
}

// DeleteStalePendingAdoptions removes pending adoption proposals for clientName
// whose torrent_hash is no longer among liveHashes (the client's current
// managed torrents). An empty liveHashes means the client reported zero
// torrents, so every pending proposal for it is stale. Returns the count
// removed. Call only with a client that listed successfully — otherwise a
// transient outage would purge valid proposals.
func (db *DB) DeleteStalePendingAdoptions(
	ctx context.Context,
	clientName string,
	liveHashes []string,
) (int, error) {
	q := db.client.DownloadRecord.Delete().Where(
		downloadrecord.StatusEQ(downloadrecord.StatusPending),
		downloadrecord.DownloadClientNameEQ(clientName),
	)
	if len(liveHashes) > 0 {
		q = q.Where(downloadrecord.TorrentHashNotIn(liveHashes...))
	}
	return q.Exec(ctx)
}

// FindPendingDownloadRecordByID returns a single status=pending record with its
// media edges, or ent NotFound.
func (db *DB) FindPendingDownloadRecordByID(
	ctx context.Context,
	id uint32,
) (*ent.DownloadRecord, error) {
	return db.client.DownloadRecord.Query().
		Where(
			downloadrecord.ID(id),
			downloadrecord.StatusEQ(downloadrecord.StatusPending),
		).
		WithMovie().
		WithEpisode().
		Only(ctx)
}

// LatestImportedRecordForMovie returns the most recent record for a movie that
// carries a torrent hash (so file-delete can remove the source torrent). ent
// NotFound when none.
func (db *DB) LatestImportedRecordForMovie(
	ctx context.Context,
	movieID uint32,
) (*ent.DownloadRecord, error) {
	return db.client.DownloadRecord.Query().
		Where(
			downloadrecord.HasMovieWith(movie.ID(movieID)),
			downloadrecord.TorrentHashNEQ(""),
		).
		Order(ent.Desc(downloadrecord.FieldCreateTime)).
		First(ctx)
}

// LatestImportedRecordForEpisode is the episode twin of the above.
func (db *DB) LatestImportedRecordForEpisode(
	ctx context.Context,
	episodeID uint32,
) (*ent.DownloadRecord, error) {
	return db.client.DownloadRecord.Query().
		Where(
			downloadrecord.HasEpisodeWith(episode.ID(episodeID)),
			downloadrecord.TorrentHashNEQ(""),
		).
		Order(ent.Desc(downloadrecord.FieldCreateTime)).
		First(ctx)
}

// ListMoviesForAdoption returns every movie as an adoption-match candidate.
// The set is small (in-memory matched against untracked torrent names), so a
// full fetch is fine.
func (db *DB) ListMoviesForAdoption(ctx context.Context) ([]*ent.Movie, error) {
	return db.client.Movie.Query().All(ctx)
}

// ListTvShowsForAdoption returns every show with its seasons → episodes →
// media files eager-loaded, so the adoption pass can match a torrent to an
// episode and check whether that episode already has a file in-memory.
func (db *DB) ListTvShowsForAdoption(ctx context.Context) ([]*ent.TVShow, error) {
	return db.client.TVShow.Query().
		WithSeasons(func(sq *ent.SeasonQuery) {
			sq.WithEpisodes(func(eq *ent.EpisodeQuery) { eq.WithMediaFiles() })
		}).
		All(ctx)
}

// ListDownloadingRecords returns every download_record whose status is
// "downloading". Used by the download manager's status-poll loop.
func (db *DB) ListDownloadingRecords(
	ctx context.Context,
) ([]*ent.DownloadRecord, error) {
	return db.client.DownloadRecord.Query().
		Where(downloadrecord.StatusEQ(downloadrecord.StatusDownloading)).
		All(ctx)
}

// ListDownloadingRecordsWithMovie returns every "downloading"
// download_record with its Movie edge preloaded. Used by the orphan-torrent
// reconciliation pass.
func (db *DB) ListDownloadingRecordsWithMovie(
	ctx context.Context,
) ([]*ent.DownloadRecord, error) {
	return db.client.DownloadRecord.Query().
		Where(downloadrecord.StatusEQ(downloadrecord.StatusDownloading)).
		WithMovie().
		All(ctx)
}

func (db *DB) UpdateDownloadRecordStatus(
	ctx context.Context,
	id uint32,
	status downloadrecord.Status,
) error {
	return db.client.DownloadRecord.UpdateOneID(id).SetStatus(status).Exec(ctx)
}

func (db *DB) MarkDownloadRecordReplaceExisting(
	ctx context.Context,
	id uint32,
) error {
	return db.client.DownloadRecord.UpdateOneID(id).
		SetReplaceExisting(true).
		Exec(ctx)
}

type RecordImportSuccessParams struct {
	RecordID uint32
	MovieID  uint32
	File     MediaFileRow
}

type MediaFileRow struct {
	Path         string
	Size         int64
	Quality      string
	Format       string
	ReleaseGroup string
}

type RecordImportFailureParams struct {
	RecordID uint32
	// Exactly one of MovieID / EpisodeID is set, identifying the media this
	// record imports. On terminal failure the movie flips to failed; the
	// episode flips back to wanted so the next search re-grabs it.
	MovieID   uint32
	EpisodeID uint32
	Terminal  bool
	Reason    string
	Attempts  uint8
}

// ListImportingDownloadRecords returns records currently in status=importing.
// Used by the import_scan scheduler job for restart-safety.
func (db *DB) ListImportingDownloadRecords(
	ctx context.Context,
) ([]*ent.DownloadRecord, error) {
	return db.client.DownloadRecord.Query().
		Where(downloadrecord.StatusEQ(downloadrecord.StatusImporting)).
		WithMovie().
		WithEpisode(withEpisodeContext).
		All(ctx)
}

// FindImportingDownloadRecordByID fetches a single importing record by ID with
// its Movie + DownloadClient edges eager-loaded. Returns ent.NotFound when the
// record is absent or no longer in status=importing (both treated as "nothing
// to do" by the worker).
func (db *DB) FindImportingDownloadRecordByID(
	ctx context.Context,
	id uint32,
) (*ent.DownloadRecord, error) {
	return db.client.DownloadRecord.Query().
		Where(
			downloadrecord.ID(id),
			downloadrecord.StatusEQ(downloadrecord.StatusImporting),
		).
		WithMovie().
		WithEpisode(withEpisodeContext).
		Only(ctx)
}

// RecordImportSuccess writes MediaFile row, flips DownloadRecord to completed,
// flips Movie to available — all in one tx. On error, caller retries.
func (db *DB) RecordImportSuccess(
	ctx context.Context,
	p RecordImportSuccessParams,
) error {
	tx, err := db.client.Tx(ctx)
	if err != nil {
		return err
	}

	if _, err := tx.MediaFile.Create().
		SetPath(p.File.Path).
		SetSize(p.File.Size).
		SetQuality(p.File.Quality).
		SetFormat(p.File.Format).
		SetReleaseGroup(p.File.ReleaseGroup).
		SetMovieID(p.MovieID).
		Save(ctx); err != nil {
		tx.Rollback()
		return fmt.Errorf("create media file: %w", err)
	}
	if err := tx.DownloadRecord.UpdateOneID(p.RecordID).
		SetStatus(downloadrecord.StatusCompleted).
		SetImportedAt(time.Now()).
		SetFailureReason("").
		Exec(ctx); err != nil {
		tx.Rollback()
		return fmt.Errorf("update download record: %w", err)
	}
	if err := tx.Movie.UpdateOneID(p.MovieID).
		SetStatus(movie.StatusAvailable).
		SetFailureReason("").
		Exec(ctx); err != nil {
		tx.Rollback()
		return fmt.Errorf("update movie: %w", err)
	}
	return tx.Commit()
}

type RecordEpisodeImportSuccessParams struct {
	RecordID  uint32
	EpisodeID uint32
	File      MediaFileRow
}

// RecordEpisodeImportSuccess mirrors RecordImportSuccess for the TV path:
// writes the MediaFile (owned by the episode), flips the DownloadRecord to
// completed, and marks the Episode available — all in one tx. Used per-file so
// a season pack records each matched episode independently.
func (db *DB) RecordEpisodeImportSuccess(
	ctx context.Context,
	p RecordEpisodeImportSuccessParams,
) error {
	tx, err := db.client.Tx(ctx)
	if err != nil {
		return err
	}
	if _, err := tx.MediaFile.Create().
		SetPath(p.File.Path).
		SetSize(p.File.Size).
		SetQuality(p.File.Quality).
		SetFormat(p.File.Format).
		SetReleaseGroup(p.File.ReleaseGroup).
		SetEpisodeID(p.EpisodeID).
		Save(ctx); err != nil {
		tx.Rollback()
		return fmt.Errorf("create media file: %w", err)
	}
	if err := tx.DownloadRecord.UpdateOneID(p.RecordID).
		SetStatus(downloadrecord.StatusCompleted).
		SetImportedAt(time.Now()).
		SetFailureReason("").
		Exec(ctx); err != nil {
		tx.Rollback()
		return fmt.Errorf("update download record: %w", err)
	}
	if err := tx.Episode.UpdateOneID(p.EpisodeID).
		SetStatus(episode.StatusAvailable).
		Exec(ctx); err != nil {
		tx.Rollback()
		return fmt.Errorf("update episode: %w", err)
	}
	return tx.Commit()
}

// RecordImportFailure writes attempt counter on retryable; on terminal also
// flips DownloadRecord + Movie to failed with reason.
func (db *DB) RecordImportFailure(
	ctx context.Context,
	p RecordImportFailureParams,
) error {
	tx, err := db.client.Tx(ctx)
	if err != nil {
		return err
	}

	u := tx.DownloadRecord.UpdateOneID(p.RecordID).SetImportAttempts(p.Attempts)
	if p.Terminal {
		u = u.SetStatus(downloadrecord.StatusFailed).SetFailureReason(p.Reason)
	}
	if err := u.Exec(ctx); err != nil {
		tx.Rollback()
		return fmt.Errorf("update download record: %w", err)
	}
	if p.Terminal && p.MovieID != 0 {
		if err := tx.Movie.UpdateOneID(p.MovieID).
			SetStatus(movie.StatusFailed).
			SetFailureReason(p.Reason).
			Exec(ctx); err != nil {
			tx.Rollback()
			return fmt.Errorf("update movie: %w", err)
		}
	}
	if p.Terminal && p.EpisodeID != 0 {
		if err := tx.Episode.UpdateOneID(p.EpisodeID).
			SetStatus(episode.StatusWanted).
			Exec(ctx); err != nil {
			tx.Rollback()
			return fmt.Errorf("update episode: %w", err)
		}
	}
	return tx.Commit()
}

// DeleteCompletedDownloadRecordsBefore deletes records whose status is
// completed and whose imported_at is older than cutoff. Returns the number of
// rows deleted.
func (db *DB) DeleteCompletedDownloadRecordsBefore(
	ctx context.Context,
	cutoff time.Time,
) (int, error) {
	return db.client.DownloadRecord.Delete().
		Where(
			downloadrecord.StatusEQ(downloadrecord.StatusCompleted),
			downloadrecord.ImportedAtLT(cutoff),
		).
		Exec(ctx)
}

// DeleteFailedDownloadRecordsBefore deletes records whose status is failed and
// whose update_time is older than cutoff. Returns the number of rows deleted.
func (db *DB) DeleteFailedDownloadRecordsBefore(
	ctx context.Context,
	cutoff time.Time,
) (int, error) {
	return db.client.DownloadRecord.Delete().
		Where(
			downloadrecord.StatusEQ(downloadrecord.StatusFailed),
			downloadrecord.UpdateTimeLT(cutoff),
		).
		Exec(ctx)
}

// SetDownloadRecordSavePath persists save_path so import_scan can resume
// after a restart without re-querying the download client.
func (db *DB) SetDownloadRecordSavePath(
	ctx context.Context,
	id uint32,
	path string,
) error {
	return db.client.DownloadRecord.UpdateOneID(id).SetSavePath(path).Exec(ctx)
}

// ListActiveDownloadRecords returns records still in flight (downloading or
// importing) with movie / download_client / indexer edges eager-loaded.
// Powers the live queue snapshot.
func (db *DB) ListActiveDownloadRecords(
	ctx context.Context,
) ([]*ent.DownloadRecord, error) {
	return db.client.DownloadRecord.Query().
		Where(downloadrecord.StatusIn(
			downloadrecord.StatusDownloading,
			downloadrecord.StatusImporting,
		)).
		WithMovie().
		WithEpisode(func(q *ent.EpisodeQuery) {
			q.WithSeason(func(sq *ent.SeasonQuery) { sq.WithTvShow() })
		}).
		All(ctx)
}

// FindActiveDownloadRecordByID fetches an in-flight record by ID with movie +
// download_client edges. Returns ent.NotFound when absent or already terminal.
func (db *DB) FindActiveDownloadRecordByID(
	ctx context.Context,
	id uint32,
) (*ent.DownloadRecord, error) {
	return db.client.DownloadRecord.Query().
		Where(
			downloadrecord.ID(id),
			downloadrecord.StatusIn(
				downloadrecord.StatusDownloading,
				downloadrecord.StatusImporting,
			),
		).
		WithMovie().
		Only(ctx)
}

type DownloadHistoryResult struct {
	Records    []*ent.DownloadRecord
	NextCursor string
}

// ListDownloadHistory returns terminal records (completed or failed) newest
// first, keyset-paginated on (update_time, id). Reuses the activity cursor
// scheme.
func (db *DB) ListDownloadHistory(
	ctx context.Context,
	limit int,
	cursor string,
) (*DownloadHistoryResult, error) {
	if limit <= 0 {
		limit = defaultActivityLimit
	}
	q := db.client.DownloadRecord.Query().
		Where(downloadrecord.StatusIn(
			downloadrecord.StatusCompleted,
			downloadrecord.StatusFailed,
		)).
		Order(
			ent.Desc(downloadrecord.FieldUpdateTime),
			ent.Desc(downloadrecord.FieldID),
		).
		WithMovie().
		WithEpisode(func(q *ent.EpisodeQuery) {
			q.WithSeason(func(sq *ent.SeasonQuery) { sq.WithTvShow() })
		})

	if cursor != "" {
		ts, id, err := decodeActivityCursor(cursor)
		if err != nil {
			return nil, fmt.Errorf("download history: decode cursor: %w", err)
		}
		q = q.Where(downloadrecord.Or(
			downloadrecord.UpdateTimeLT(ts),
			downloadrecord.And(
				downloadrecord.UpdateTimeEQ(ts),
				downloadrecord.IDLT(id),
			),
		))
	}

	rows, err := q.Limit(limit + 1).All(ctx)
	if err != nil {
		return nil, fmt.Errorf("download history: query: %w", err)
	}
	res := &DownloadHistoryResult{}
	if len(rows) > limit {
		res.Records = rows[:limit]
		last := res.Records[limit-1]
		res.NextCursor = encodeActivityCursor(last.UpdateTime, last.ID)
	} else {
		res.Records = rows
	}
	return res, nil
}

// DeleteDownloadRecord deletes one record by ID. Returns ent.NotFound when
// absent (handler maps to 404).
func (db *DB) DeleteDownloadRecord(ctx context.Context, id uint32) error {
	return db.client.DownloadRecord.DeleteOneID(id).Exec(ctx)
}

// DeleteAllCompletedDownloadRecords removes every completed record (the
// "Clear completed" action). Returns the number of rows deleted.
func (db *DB) DeleteAllCompletedDownloadRecords(
	ctx context.Context,
) (int, error) {
	return db.client.DownloadRecord.Delete().
		Where(downloadrecord.StatusEQ(downloadrecord.StatusCompleted)).
		Exec(ctx)
}

// RevertMovieToWantedIfNoFile flips a movie back to "wanted" only when it has
// no MediaFile, so cancelling a download doesn't clobber an available movie
// that already has a prior file from an upgrade grab.
func (db *DB) RevertMovieToWantedIfNoFile(
	ctx context.Context,
	movieID uint32,
) error {
	has, err := db.client.MediaFile.Query().
		Where(mediafile.HasMovieWith(movie.ID(movieID))).
		Exist(ctx)
	if err != nil {
		return fmt.Errorf("check media files: %w", err)
	}
	if has {
		return nil
	}
	return db.client.Movie.UpdateOneID(movieID).
		SetStatus(movie.StatusWanted).
		Exec(ctx)
}

// RevertOrphanedDownloadingEpisodes flips back to "wanted" every episode stuck
// in "downloading" that has no media file and whose season has no active
// (downloading/importing) download record. This reconciles the season-pack
// fan-out: a pack marks every episode downloading but links only one record, so
// cancelling or losing that record leaves the rest stranded. Granularity is the
// season — an episode is spared while any download in its season is still
// active, so it self-heals once that download settles. Returns rows reverted.
func (db *DB) RevertOrphanedDownloadingEpisodes(
	ctx context.Context,
) (int, error) {
	return db.client.Episode.Update().
		Where(
			episode.StatusEQ(episode.StatusDownloading),
			episode.Not(episode.HasMediaFiles()),
			episode.Not(episode.HasSeasonWith(
				season.HasEpisodesWith(
					episode.HasDownloadRecordsWith(
						downloadrecord.StatusIn(
							downloadrecord.StatusDownloading,
							downloadrecord.StatusImporting,
						),
					),
				),
			)),
		).
		SetStatus(episode.StatusWanted).
		Save(ctx)
}

// SyncSeasonDownloadStateForRecord reflects a download's live torrent state onto
// its episode badges: when paused, the record's-season episodes still in
// "downloading" flip to "paused"; when active again they flip back. Season-level
// so a paused season pack pauses all its episodes, not just the linked one.
// A no-op for movie records (no episode/season behind them).
func (db *DB) SyncSeasonDownloadStateForRecord(
	ctx context.Context,
	recordID uint32,
	paused bool,
) error {
	seasonID, err := db.client.Season.Query().
		Where(season.HasEpisodesWith(
			episode.HasDownloadRecordsWith(downloadrecord.ID(recordID)),
		)).
		FirstID(ctx)
	if ent.IsNotFound(err) {
		return nil // movie record or no episode link
	}
	if err != nil {
		return fmt.Errorf("find record season: %w", err)
	}

	from, to := episode.StatusDownloading, episode.StatusPaused
	if !paused {
		from, to = episode.StatusPaused, episode.StatusDownloading
	}
	if _, err := db.client.Episode.Update().
		Where(
			episode.HasSeasonWith(season.ID(seasonID)),
			episode.StatusEQ(from),
		).
		SetStatus(to).
		Save(ctx); err != nil {
		return fmt.Errorf("sync season download state: %w", err)
	}
	return nil
}
