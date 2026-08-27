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
	"github.com/datahearth/streamline/ent/predicate"
	"github.com/datahearth/streamline/ent/schema"
	"github.com/datahearth/streamline/ent/season"
	"github.com/datahearth/streamline/internal/ffmpeg"
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

func (db *DB) SetDownloadRecordReplaceMode(
	ctx context.Context,
	id uint32,
	mode downloadrecord.ReplaceMode,
) error {
	return db.client.DownloadRecord.UpdateOneID(id).
		SetReplaceMode(mode).
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
	Probe        *ffmpeg.Info // nil leaves probed_at NULL for the backfill
}

type RecordImportFailureParams struct {
	RecordID uint32
	// Exactly one of MovieID / EpisodeID is set, identifying the media this
	// record imports. On terminal failure the movie flips to failed; the
	// episode flips back to wanted so the next search re-grabs it — unless it
	// already holds a media file, in which case "wanted" would be a lie and
	// the episode is left as-is.
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

// FindDownloadRecordByID returns one record by ID whatever its status, so a
// caller can tell "no such record" from "wrong state for this action".
func (db *DB) FindDownloadRecordByID(
	ctx context.Context,
	id uint32,
) (*ent.DownloadRecord, error) {
	return db.client.DownloadRecord.Get(ctx, id)
}

// HoldDownloadRecord flips an importing record to held with the reasons the
// verifier produced, stopping the import until a user resolves it.
func (db *DB) HoldDownloadRecord(
	ctx context.Context,
	id uint32,
	reasons []schema.HoldReason,
) error {
	return db.client.DownloadRecord.UpdateOneID(id).
		SetStatus(downloadrecord.StatusHeld).
		SetHoldReasons(reasons).
		Exec(ctx)
}

// FindHeldDownloadRecordByID fetches a single held record by ID with its Movie
// and Episode context eager-loaded. Returns ent.NotFound when the record is
// absent or no longer held.
func (db *DB) FindHeldDownloadRecordByID(
	ctx context.Context,
	id uint32,
) (*ent.DownloadRecord, error) {
	return db.client.DownloadRecord.Query().
		Where(
			downloadrecord.ID(id),
			downloadrecord.StatusEQ(downloadrecord.StatusHeld),
		).
		WithMovie().
		WithEpisode(withEpisodeContext).
		Only(ctx)
}

// ReleaseHeldDownloadRecord flips a held record back to importing with
// verification bypassed, so the importer's re-run imports it as-is.
func (db *DB) ReleaseHeldDownloadRecord(ctx context.Context, id uint32) error {
	return db.client.DownloadRecord.UpdateOneID(id).
		SetStatus(downloadrecord.StatusImporting).
		SetVerificationBypassed(true).
		ClearHoldReasons().
		Exec(ctx)
}

// FailHeldDownloadRecord finalizes a held record the user rejected. requeue
// reverts the movie to wanted so a search finds a replacement; without it the
// movie stays failed, the user having judged the release themselves. Episodes
// revert to wanted either way, mirroring RecordImportFailure.
func (db *DB) FailHeldDownloadRecord(
	ctx context.Context,
	id uint32,
	reason string,
	requeue bool,
) error {
	rec, err := db.FindHeldDownloadRecordByID(ctx, id)
	if err != nil {
		return err
	}

	tx, err := db.client.Tx(ctx)
	if err != nil {
		return err
	}
	if err := tx.DownloadRecord.UpdateOneID(id).
		SetStatus(downloadrecord.StatusFailed).
		SetFailureReason(reason).
		ClearHoldReasons().
		Exec(ctx); err != nil {
		tx.Rollback()
		return fmt.Errorf("update download record: %w", err)
	}
	if rec.Edges.Movie != nil {
		status := movie.StatusFailed
		if requeue {
			status = movie.StatusWanted
		}
		if err := tx.Movie.UpdateOneID(rec.Edges.Movie.ID).
			SetStatus(status).
			SetFailureReason(reason).
			Exec(ctx); err != nil {
			tx.Rollback()
			return fmt.Errorf("update movie: %w", err)
		}
	}
	if rec.Edges.Episode != nil {
		if err := tx.Episode.UpdateOneID(rec.Edges.Episode.ID).
			SetStatus(episode.StatusWanted).
			Exec(ctx); err != nil {
			tx.Rollback()
			return fmt.Errorf("update episode: %w", err)
		}
	}
	return tx.Commit()
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

	mc := tx.MediaFile.Create().
		SetPath(p.File.Path).
		SetSize(p.File.Size).
		SetQuality(p.File.Quality).
		SetFormat(p.File.Format).
		SetReleaseGroup(p.File.ReleaseGroup).
		SetMovieID(p.MovieID)
	if p.File.Probe != nil {
		mc = applyProbe(mc, p.File.Probe)
	}
	if _, err := mc.Save(ctx); err != nil {
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
	ec := tx.MediaFile.Create().
		SetPath(p.File.Path).
		SetSize(p.File.Size).
		SetQuality(p.File.Quality).
		SetFormat(p.File.Format).
		SetReleaseGroup(p.File.ReleaseGroup).
		SetEpisodeID(p.EpisodeID)
	if p.File.Probe != nil {
		ec = applyProbe(ec, p.File.Probe)
	}
	if _, err := ec.Save(ctx); err != nil {
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
		// wanted means "we do not have this" — an episode a failed grab was
		// meant to replace still has its prior file, so it stays available
		// instead (this is how ErrEpisodeHasFile reaches here: the importer
		// declined every episode the release selected, but each one already
		// holds a file).
		hasFile, err := tx.MediaFile.Query().
			Where(mediafile.HasEpisodeWith(episode.ID(p.EpisodeID))).
			Exist(ctx)
		if err != nil {
			tx.Rollback()
			return fmt.Errorf("check episode media files: %w", err)
		}
		if !hasFile {
			if err := tx.Episode.UpdateOneID(p.EpisodeID).
				SetStatus(episode.StatusWanted).
				Exec(ctx); err != nil {
				tx.Rollback()
				return fmt.Errorf("update episode: %w", err)
			}
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

// CountLiveDownloadRecords counts records whose save_path something will
// still read: in-flight downloads and pending adoption proposals. Terminal
// rows (completed, failed, dismissed) keep the path they were created with
// forever, so counting them would hold the boot drift warning on long after
// the download root legitimately moved.
func (db *DB) CountLiveDownloadRecords(ctx context.Context) (int, error) {
	n, err := db.client.DownloadRecord.Query().
		Where(downloadrecord.StatusIn(
			downloadrecord.StatusDownloading,
			downloadrecord.StatusImporting,
			downloadrecord.StatusHeld,
			downloadrecord.StatusPending,
		)).
		Count(ctx)
	if err != nil {
		return 0, fmt.Errorf("count live download_records: %w", err)
	}
	return n, nil
}

func (db *DB) ListDownloadRecordsByPathPrefix(
	ctx context.Context,
	prefix string,
) ([]*ent.DownloadRecord, error) {
	rows, err := db.client.DownloadRecord.Query().
		Where(downloadrecord.SavePathHasPrefix(prefix)).
		Order(ent.Asc(downloadrecord.FieldSavePath)).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list download_records under %s: %w", prefix, err)
	}
	return rows, nil
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
			// Held records are finished downloading but not done: the queue is
			// where a user is told one is waiting on them.
			downloadrecord.StatusHeld,
		)).
		WithMovie().
		WithEpisode(func(q *ent.EpisodeQuery) {
			q.WithSeason(func(sq *ent.SeasonQuery) { sq.WithTvShow() })
		}).
		All(ctx)
}

// FindActiveDownloadRecordByID fetches an in-flight record by ID with movie +
// download_client edges. Returns ent.NotFound when absent or already terminal.
// Held records match: the queue shows them, so the queue verbs have to be able
// to tell a held row apart from a missing one.
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
				downloadrecord.StatusHeld,
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

// inFlightEpisodeStatuses are the states an episode only holds while a grab is
// still running, so a sweep that finds one with no live record behind it knows
// the row is stranded. "paused" is in flight too: the torrent still exists.
var inFlightEpisodeStatuses = []episode.Status{
	episode.StatusDownloading,
	episode.StatusImporting,
	episode.StatusPaused,
}

// noActiveSeasonRecord excludes an episode whose season has a download record
// still in flight. Shared by both RevertOrphanedDownloadingEpisodes arms — the
// held-record case applies equally to both: a held record awaits a decision,
// so reverting its episodes would let the missing-search grab a duplicate
// release while it pends.
func noActiveSeasonRecord() predicate.Episode {
	return episode.Not(episode.HasSeasonWith(
		season.HasEpisodesWith(
			episode.HasDownloadRecordsWith(
				downloadrecord.StatusIn(
					downloadrecord.StatusDownloading,
					downloadrecord.StatusImporting,
					downloadrecord.StatusHeld,
				),
			),
		),
	))
}

// RevertOrphanedDownloadingEpisodes reconciles episodes stuck in "downloading"
// with no active download record behind them — the season-pack fan-out marks
// every episode in a pack downloading but links only one record, so cancelling
// or losing that record (or, historically, an upgrade grab that never
// resolved) leaves the rest stranded. Granularity is the season — an episode
// is spared while any download in its season is still active, so it
// self-heals once that download settles.
//
// Two arms, not one query: an episode with no media file has never had
// anything, so it goes back to "wanted"; an episode that already has a file
// (an upgrade target left stranded by a since-fixed bug that marked it
// downloading) goes back to "available" instead — "wanted" would claim we
// don't have it. Returns rows reverted across both.
func (db *DB) RevertOrphanedDownloadingEpisodes(
	ctx context.Context,
) (int, error) {
	toWanted, err := db.client.Episode.Update().
		Where(
			episode.StatusIn(inFlightEpisodeStatuses...),
			episode.Not(episode.HasMediaFiles()),
			noActiveSeasonRecord(),
		).
		SetStatus(episode.StatusWanted).
		Save(ctx)
	if err != nil {
		return toWanted, err
	}
	toAvailable, err := db.client.Episode.Update().
		Where(
			episode.StatusIn(inFlightEpisodeStatuses...),
			episode.HasMediaFiles(),
			noActiveSeasonRecord(),
		).
		SetStatus(episode.StatusAvailable).
		Save(ctx)
	return toWanted + toAvailable, err
}

// MarkEpisodeDownloading flips one episode to "downloading" after a grab, and
// only from "wanted": an episode that already has a file is a replace target,
// and claiming it is downloading would strand it as "wanted" — file and all —
// if the grab is later reverted. Reports whether the row moved.
func (db *DB) MarkEpisodeDownloading(
	ctx context.Context,
	id uint32,
) (bool, error) {
	n, err := db.client.Episode.Update().
		Where(
			episode.ID(id),
			episode.StatusEQ(episode.StatusWanted),
		).
		SetStatus(episode.StatusDownloading).
		Save(ctx)
	if err != nil {
		return false, fmt.Errorf("mark episode %d downloading: %w", id, err)
	}
	return n > 0, nil
}

// MarkRecordEpisodesImporting moves a record's in-flight episodes to
// "importing" as the download hands off to the importer. Season-scoped like
// SyncSeasonDownloadStateForRecord, since a pack imports the whole season
// behind its single episode link, and it only moves rows already in
// downloading/paused — an episode that is available (a replace target) or
// wanted (never part of this grab) is none of this record's business.
func (db *DB) MarkRecordEpisodesImporting(
	ctx context.Context,
	recordID uint32,
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
	if _, err := db.client.Episode.Update().
		Where(
			episode.HasSeasonWith(season.ID(seasonID)),
			episode.StatusIn(
				episode.StatusDownloading,
				episode.StatusPaused,
			),
		).
		SetStatus(episode.StatusImporting).
		Save(ctx); err != nil {
		return fmt.Errorf("mark record episodes importing: %w", err)
	}
	return nil
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
