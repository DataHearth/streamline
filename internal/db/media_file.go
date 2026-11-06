package db

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"time"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/album"
	"github.com/datahearth/streamline/ent/book"
	"github.com/datahearth/streamline/ent/episode"
	"github.com/datahearth/streamline/ent/mediafile"
	"github.com/datahearth/streamline/ent/movie"
	"github.com/datahearth/streamline/ent/predicate"
	"github.com/datahearth/streamline/ent/track"
	"github.com/datahearth/streamline/internal/ffmpeg"
	"github.com/datahearth/streamline/internal/library"
)

type CreateMediaFileParams struct {
	Path         string
	Size         int64
	Quality      string
	Format       string
	ReleaseGroup string
	MovieID      uint32 // set for movie files; mutually exclusive with EpisodeID
	EpisodeID    uint32 // set for episode files (e.g. series adoption)
	BookID       uint32 // set for book files, together with BookKind
	BookKind     mediafile.BookKind
	Source       mediafile.Source
	// Parsed is the release name's parse, when the caller has it. Nil falls
	// back to parsing Path's basename — see applyParsed.
	Parsed *library.ParseResult
	Probe  *ffmpeg.Info // set to stamp probed_at at create time; nil leaves probed_at NULL for the backfill
	// QueueTranscode rides the same create as the file: config.TranscodeEligible
	// decides it, so a profile change never needs a backfill of missed rows.
	QueueTranscode bool
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
	SetAudioTracks(uint8) T
	SetAudioLangs(string) T
	SetSubLangs(string) T
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
		SetBitrate(info.BitrateBPS).
		SetAudioTracks(info.AudioTracks).
		SetAudioLangs(info.AudioLangs).
		SetSubLangs(info.SubLangs)
}

// applyParsed writes the parsed_* columns from the release name's parse when
// the caller has it, falling back to the stored path's basename. The two
// disagree for anything the renamer touched: the destination name is rendered
// from the naming template, and the default one keeps only the resolution, so
// the source and codec tokens are gone before this reads them back.
func applyParsed[T interface {
	SetParsedSource(string) T
	SetParsedResolution(string) T
	SetParsedCodec(string) T
}](q T, parsed *library.ParseResult, path string) T {
	if parsed == nil {
		p := library.Parse(filepath.Base(path))
		parsed = &p
	}
	return q.SetParsedSource(parsed.Source).
		SetParsedResolution(parsed.Resolution).
		SetParsedCodec(parsed.Codec)
}

func (db *DB) CreateMediaFile(
	ctx context.Context,
	p CreateMediaFileParams,
) (*ent.MediaFile, error) {
	q := applyParsed(db.client.MediaFile.Create().
		SetPath(p.Path).
		SetSize(p.Size).
		SetQuality(p.Quality).
		SetFormat(p.Format).
		SetReleaseGroup(p.ReleaseGroup), p.Parsed, p.Path)
	if p.MovieID != 0 {
		q = q.SetMovieID(p.MovieID)
	}
	if p.EpisodeID != 0 {
		q = q.SetEpisodeID(p.EpisodeID)
	}
	if p.BookID != 0 {
		q = q.SetBookID(p.BookID)
	}
	if p.BookKind != "" {
		q = q.SetBookKind(p.BookKind)
	}
	if p.Source != "" {
		q = q.SetSource(p.Source)
	}
	if p.Probe != nil {
		q = applyProbe(q, p.Probe)
	}
	mf, err := q.Save(ctx)
	if err != nil {
		return nil, err
	}
	if p.QueueTranscode {
		if _, err := db.client.TranscodeJob.Create().
			SetMediaFile(mf).
			Save(ctx); err != nil {
			return nil, fmt.Errorf("queue transcode job: %w", err)
		}
	}
	return mf, nil
}

// ListUnprobedMediaFiles returns up to limit video rows that have never been
// probed (probed_at IS NULL), oldest-first, for the backfill job to work
// through. Track and book files are left out: the probe reads a video stream,
// fails every one of them with "no video stream", and nothing reads its
// columns outside movies and episodes.
func (db *DB) ListUnprobedMediaFiles(
	ctx context.Context,
	limit int,
) ([]*ent.MediaFile, error) {
	rows, err := db.client.MediaFile.Query().
		Where(
			mediafile.ProbedAtIsNil(),
			mediafile.MissingSinceIsNil(),
			mediafile.Or(mediafile.HasMovie(), mediafile.HasEpisode()),
		).
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
//
// path, when non-empty, also backfills the parsed_* columns. The probe job is
// already walking and writing rows created before those columns existed, so it
// is the cheapest place to fill them — no separate migration pass. Callers pass
// it only for a row that has none: a basename the renamer wrote carries fewer
// tokens than the release name did, so re-deriving over a filled row loses the
// source and codec the importer stored.
func (db *DB) StampMediaFileProbe(
	ctx context.Context,
	id uint32,
	path string,
	info *ffmpeg.Info,
) error {
	q := applyProbe(db.client.MediaFile.UpdateOneID(id), info)
	if path != "" {
		q = applyParsed(q, nil, path)
	}
	if err := q.Exec(ctx); err != nil {
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
		Where(
			mediafile.HasMovieWith(movie.TmdbID(tmdbID)),
			mediafile.MissingSinceIsNil(),
		).
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
		WithMovie().
		WithEpisode().
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list media_files with owners: %w", err)
	}
	return rows, nil
}

// DriftRow is the columns the drift check needs to stat a file and do
// its grace arithmetic. The owner edges are not here: they are read only on
// the rare branch where a file has actually gone missing.
type DriftRow struct {
	ID                 uint32     `json:"id"`
	Path               string     `json:"path"`
	MissingSince       *time.Time `json:"missing_since"`
	MissingConfirmedAt *time.Time `json:"missing_confirmed_at"`
}

// ListMediaFilesForDrift returns one keyset page of drift rows, ordered by id,
// starting after afterID. Paged and projected because the drift check runs
// every 15 minutes over the whole table: loading every row with both owner
// edges attached was tens of MB of transient garbage per tick on a library of
// a few thousand files, to stat a path and compare a timestamp.
func (db *DB) ListMediaFilesForDrift(
	ctx context.Context,
	afterID uint32,
	limit int,
	skip ...LibraryRoot,
) ([]DriftRow, error) {
	q := db.client.MediaFile.Query().
		Order(ent.Asc(mediafile.FieldID)).
		Limit(limit)
	if afterID > 0 {
		q = q.Where(mediafile.IDGT(afterID))
	}
	if len(skip) > 0 {
		skipped := make([]predicate.MediaFile, len(skip))
		for i, root := range skip {
			skipped[i] = rootPredicate(root)
		}
		q = q.Where(mediafile.Not(mediafile.Or(skipped...)))
	}
	var rows []DriftRow
	err := q.Select(
		mediafile.FieldID,
		mediafile.FieldPath,
		mediafile.FieldMissingSince,
		mediafile.FieldMissingConfirmedAt,
	).Scan(ctx, &rows)
	if err != nil {
		return nil, fmt.Errorf("list media_files for drift: %w", err)
	}
	return rows, nil
}

// FindMediaFileWithOwners loads one row with its owner edges — the shape the
// drift check needs once a file turns out to be missing, which is the rare
// case worth paying an extra query for. The episode edge carries its season
// and show, and the track edge its album: drift is reported per show and per
// album, so the sweep needs those ids, not just the episode or the track.
func (db *DB) FindMediaFileWithOwners(
	ctx context.Context,
	id uint32,
) (*ent.MediaFile, error) {
	return db.client.MediaFile.Query().
		Where(mediafile.IDEQ(id)).
		WithMovie().
		WithEpisode(func(q *ent.EpisodeQuery) {
			q.WithSeason(func(sq *ent.SeasonQuery) { sq.WithTvShow() })
		}).
		WithTrack(func(q *ent.TrackQuery) {
			q.Select(track.FieldID).
				WithAlbum(func(aq *ent.AlbumQuery) { aq.Select(album.FieldID) })
		}).
		WithBook(func(q *ent.BookQuery) { q.Select(book.FieldID) }).
		Only(ctx)
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

func (db *DB) ListMediaFilesByBook(
	ctx context.Context,
	bookID uint32,
	kind mediafile.BookKind,
) ([]*ent.MediaFile, error) {
	rows, err := db.client.MediaFile.Query().
		Where(
			mediafile.HasBookWith(book.ID(bookID)),
			mediafile.BookKindEQ(kind),
		).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list media_files for book %d: %w", bookID, err)
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

// UpdateMediaFileAfterTranscode writes a transcode's outcome onto the file it
// replaced in place: the new path/size/format, size_before for the detail
// page's savings, transcoded_at, and probe — the encode's own probe, which the
// worker already holds from verifying it.
//
// The probe is not optional in practice. Nulling the columns instead left the
// row scoring off parsed_codec until the media-probe backfill reached it, so an
// h264→hevc transcode read as x264 for up to a backfill cycle and the RSS feed
// could grab an "upgrade" over a file it had just produced.
func (db *DB) UpdateMediaFileAfterTranscode(
	ctx context.Context,
	id uint32,
	path string,
	size, sizeBefore int64,
	format string,
	probe *ffmpeg.Info,
) error {
	q := applyProbe(db.client.MediaFile.UpdateOneID(id), probe)
	if err := q.
		SetPath(path).
		SetSize(size).
		SetFormat(format).
		SetSizeBefore(sizeBefore).
		SetTranscodedAt(time.Now()).
		Exec(ctx); err != nil {
		return fmt.Errorf("update media_file %d after transcode: %w", id, err)
	}
	return nil
}

// ClearMediaFilesMissing clears both missing columns on files seen on disk
// again. A
// file that never went missing is not written at all: stamping every row each
// tick rewrote the whole table into the WAL every 15 minutes.
func (db *DB) ClearMediaFilesMissing(ctx context.Context, ids []uint32) error {
	if err := db.client.MediaFile.Update().
		Where(mediafile.IDIn(ids...)).
		ClearMissingSince().
		ClearMissingConfirmedAt().
		Exec(ctx); err != nil {
		return fmt.Errorf(
			"clear missing_since on %d media_files: %w", len(ids), err,
		)
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

// ConfirmMediaFileMissing stamps missing_confirmed_at once, so the wait keeps
// counting from the first tick past grace.
func (db *DB) ConfirmMediaFileMissing(ctx context.Context, id uint32) error {
	if err := db.client.MediaFile.Update().
		Where(mediafile.ID(id), mediafile.MissingConfirmedAtIsNil()).
		SetMissingConfirmedAt(time.Now()).
		Exec(ctx); err != nil {
		return fmt.Errorf("confirm media_file %d missing: %w", id, err)
	}
	return nil
}

// HoldsOtherFile reports whether the owner of row still has another file on
// disk filling the same place: the movie, the episode, the track or the book
// slot. It reads the row's loaded owner edges (FindMediaFileWithOwners).
func (db *DB) HoldsOtherFile(
	ctx context.Context,
	row *ent.MediaFile,
) (bool, error) {
	return holdsOtherFile(ctx, db.client.MediaFile, row)
}

func holdsOtherFile(
	ctx context.Context,
	files *ent.MediaFileClient,
	row *ent.MediaFile,
) (bool, error) {
	q := files.Query().
		Where(mediafile.IDNEQ(row.ID), mediafile.MissingSinceIsNil())
	switch {
	case row.Edges.Movie != nil:
		q = q.Where(mediafile.HasMovieWith(movie.ID(row.Edges.Movie.ID)))
	case row.Edges.Episode != nil:
		q = q.Where(mediafile.HasEpisodeWith(episode.ID(row.Edges.Episode.ID)))
	case row.Edges.Track != nil:
		q = q.Where(mediafile.HasTrackWith(track.ID(row.Edges.Track.ID)))
	case row.Edges.Book != nil:
		q = q.Where(
			mediafile.HasBookWith(book.ID(row.Edges.Book.ID)),
			mediafile.BookKindEQ(row.BookKind),
		)
	default:
		return false, nil
	}
	held, err := q.Exist(ctx)
	if err != nil {
		return false, fmt.Errorf(
			"check other files of media_file %d: %w", row.ID, err,
		)
	}
	return held, nil
}

// ListMissingFiles returns one page of the rows confirmed missing, oldest
// confirmation first, and how many there are.
func (db *DB) ListMissingFiles(
	ctx context.Context,
	limit, offset int,
) ([]*ent.MediaFile, int, error) {
	confirmed := db.client.MediaFile.Query().
		Where(mediafile.MissingConfirmedAtNotNil())
	total, err := confirmed.Clone().Count(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("count missing files: %w", err)
	}
	rows, err := confirmed.
		Order(
			ent.Asc(mediafile.FieldMissingConfirmedAt),
			ent.Asc(mediafile.FieldID),
		).
		Limit(limit).
		Offset(offset).
		WithMovie().
		WithEpisode(func(q *ent.EpisodeQuery) {
			q.WithSeason(func(sq *ent.SeasonQuery) { sq.WithTvShow() })
		}).
		WithTrack(func(q *ent.TrackQuery) {
			q.WithAlbum(func(aq *ent.AlbumQuery) { aq.WithArtist() })
		}).
		WithBook().
		All(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("list missing files: %w", err)
	}
	return rows, total, nil
}

// ForgetMediaFile removes a row and stops monitoring what it belongs to, so the
// missing searches do not download a file deleted on purpose. An owner still
// holding a file in that place keeps its status. The file_removed event is not
// suppressed: it is the title's history of this.
func (db *DB) ForgetMediaFile(ctx context.Context, row *ent.MediaFile) error {
	tx, err := db.client.Tx(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	if err := forgetInTx(ctx, tx, row); err != nil {
		tx.Rollback()
		return fmt.Errorf("forget media_file %d: %w", row.ID, err)
	}
	return tx.Commit()
}

func forgetInTx(ctx context.Context, tx *ent.Tx, row *ent.MediaFile) error {
	if err := tx.MediaFile.DeleteOneID(row.ID).Exec(ctx); err != nil {
		return err
	}
	held, err := holdsOtherFile(ctx, tx.MediaFile, row)
	if err != nil {
		return err
	}
	switch {
	case row.Edges.Movie != nil:
		id := row.Edges.Movie.ID
		if !held {
			if err := revertMovieStatus(ctx, tx, id); err != nil {
				return err
			}
		}
		return tx.Movie.UpdateOneID(id).SetMonitored(false).Exec(ctx)
	case row.Edges.Episode != nil:
		id := row.Edges.Episode.ID
		if !held {
			if err := revertEpisodeStatus(ctx, tx, id); err != nil {
				return err
			}
		}
		return tx.Episode.UpdateOneID(id).
			SetMonitored(false).SetMonitorPinned(true).Exec(ctx)
	case row.Edges.Track != nil && row.Edges.Track.Edges.Album != nil:
		id := row.Edges.Track.Edges.Album.ID
		if !held {
			if err := revertAlbumStatus(ctx, tx, id); err != nil {
				return err
			}
		}
		return tx.Album.UpdateOneID(id).
			SetMonitored(false).SetMonitorPinned(true).Exec(ctx)
	case row.Edges.Book != nil:
		id := row.Edges.Book.ID
		if err := revertBookSlotStatus(ctx, tx, id, row.BookKind); err != nil {
			return err
		}
		u := tx.Book.UpdateOneID(id)
		if row.BookKind == mediafile.BookKindAudiobook {
			return u.SetAudiobookMonitored(false).Exec(ctx)
		}
		return u.SetEbookMonitored(false).Exec(ctx)
	}
	return nil
}

func revertMovieStatus(ctx context.Context, tx *ent.Tx, id uint32) error {
	if err := tx.Movie.UpdateOneID(id).
		SetStatus(movie.StatusWanted).
		Exec(ctx); err != nil {
		return fmt.Errorf("revert movie %d to wanted: %w", id, err)
	}
	return nil
}

func revertEpisodeStatus(ctx context.Context, tx *ent.Tx, id uint32) error {
	if err := tx.Episode.UpdateOneID(id).
		SetStatus(episode.StatusWanted).
		Exec(ctx); err != nil {
		return fmt.Errorf("revert episode %d to wanted: %w", id, err)
	}
	return nil
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
	if err := revertMovieStatus(ctx, tx, movieID); err != nil {
		tx.Rollback()
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	// Logged here rather than left to callers: drift_check logs its own
	// reverts, but the importer's upgrade-replace path calls straight into
	// this and logged nothing, so "why did this go back to wanted" had no
	// answer on the path that most often causes it.
	slog.InfoContext(ctx, "media file removed, movie reverted to wanted",
		"media_file.id", mediaFileID, "movie.id", movieID)
	return nil
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
	if err := revertEpisodeStatus(ctx, tx, episodeID); err != nil {
		tx.Rollback()
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	slog.InfoContext(ctx, "media file removed, episode reverted to wanted",
		"media_file.id", mediaFileID, "episode.id", episodeID)
	return nil
}

// DeleteMediaFileAndRevertAlbum removes a track's MediaFile row and moves its
// album from available to wanted inside a single transaction. An album in any
// other status keeps it: what it is waiting on still stands.
func (db *DB) DeleteMediaFileAndRevertAlbum(
	ctx context.Context,
	mediaFileID, albumID uint32,
) error {
	tx, err := db.client.Tx(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	if err := tx.MediaFile.DeleteOneID(mediaFileID).Exec(ctx); err != nil {
		tx.Rollback()
		return fmt.Errorf("delete media_file %d: %w", mediaFileID, err)
	}
	if err := revertAlbumStatus(ctx, tx, albumID); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

func revertAlbumStatus(ctx context.Context, tx *ent.Tx, id uint32) error {
	if err := tx.Album.Update().
		Where(album.IDEQ(id), album.StatusEQ(album.StatusAvailable)).
		SetStatus(album.StatusWanted).
		Exec(ctx); err != nil {
		return fmt.Errorf("revert album %d to wanted: %w", id, err)
	}
	return nil
}

// DeleteMediaFileAndRevertBookSlot removes a book's MediaFile row and, once the
// slot holds no file, moves it from available to wanted inside a single
// transaction. A slot that still holds one stays available: the importer
// refuses a grab into it with ErrDestExists, so wanted would only burn a
// download.
func (db *DB) DeleteMediaFileAndRevertBookSlot(
	ctx context.Context,
	mediaFileID, bookID uint32,
	kind mediafile.BookKind,
) error {
	tx, err := db.client.Tx(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	if err := tx.MediaFile.DeleteOneID(mediaFileID).Exec(ctx); err != nil {
		tx.Rollback()
		return fmt.Errorf("delete media_file %d: %w", mediaFileID, err)
	}
	if err := revertBookSlotStatus(ctx, tx, bookID, kind); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

// revertBookSlotStatus moves a slot left holding no file from available to
// wanted.
func revertBookSlotStatus(
	ctx context.Context,
	tx *ent.Tx,
	bookID uint32,
	kind mediafile.BookKind,
) error {
	held, err := tx.MediaFile.Query().
		Where(mediafile.HasBookWith(book.ID(bookID)), mediafile.BookKindEQ(kind)).
		Exist(ctx)
	if err != nil {
		return fmt.Errorf("check book %d slot files: %w", bookID, err)
	}
	if held {
		return nil
	}
	u := tx.Book.Update().Where(book.ID(bookID))
	switch kind {
	case mediafile.BookKindEbook:
		u = u.Where(book.EbookStatusEQ(book.EbookStatusAvailable)).
			SetEbookStatus(book.EbookStatusWanted)
	case mediafile.BookKindAudiobook:
		u = u.Where(book.AudiobookStatusEQ(book.AudiobookStatusAvailable)).
			SetAudiobookStatus(book.AudiobookStatusWanted)
	default:
		return fmt.Errorf("revert book %d: unknown kind %q", bookID, kind)
	}
	if err := u.Exec(ctx); err != nil {
		return fmt.Errorf("revert book %d %s slot: %w", bookID, kind, err)
	}
	return nil
}
