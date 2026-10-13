package db

import (
	"context"
	"time"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/album"
	"github.com/datahearth/streamline/ent/downloadrecord"
	entmediafile "github.com/datahearth/streamline/ent/mediafile"
	"github.com/datahearth/streamline/ent/predicate"
	"github.com/datahearth/streamline/ent/track"
)

// TagMark is how a write request treats the files it reaches. MarkStale is
// "the library's data may have changed": a file whose next hash matches what
// it carries is settled without being opened. MarkForce empties the stored
// hash so the next pass writes the file whatever it carries.
type TagMark uint8

const (
	MarkStale TagMark = iota
	MarkForce
)

// ListMediaFilesForTagSync returns up to limit track files waiting for the
// music tag writer, pending before deferred and oldest first, each with its
// track. limit 0 is no limit.
func (db *DB) ListMediaFilesForTagSync(
	ctx context.Context,
	limit int,
) ([]*ent.MediaFile, error) {
	q := db.client.MediaFile.Query().
		Where(
			entmediafile.HasTrack(),
			entmediafile.TagsStateIn(
				entmediafile.TagsStatePending,
				entmediafile.TagsStateDeferred,
			),
		).
		WithTrack(func(tq *ent.TrackQuery) {
			tq.WithAlbum()
		}).
		// Descending: "pending" sorts after "deferred", and a file waiting
		// for its first write goes before one waiting on a torrent.
		Order(ent.Desc(entmediafile.FieldTagsState), ent.Asc(entmediafile.FieldID))
	if limit > 0 {
		q = q.Limit(limit)
	}
	return q.All(ctx)
}

// ListAlbumTagFiles returns every track file of the album with its track.
func (db *DB) ListAlbumTagFiles(
	ctx context.Context,
	albumID uint32,
) ([]*ent.MediaFile, error) {
	return db.client.MediaFile.Query().
		Where(entmediafile.HasTrackWith(track.HasAlbumWith(album.IDEQ(albumID)))).
		WithTrack(func(tq *ent.TrackQuery) { tq.WithAlbum() }).
		Order(ent.Asc(entmediafile.FieldID)).
		All(ctx)
}

// TagWrite is what a tag write (or a pass that found nothing to change)
// leaves recorded on the file.
type TagWrite struct {
	// Hash is what was written; ArtHash the front cover Streamline now owns
	// in the file, empty when none.
	Hash, ArtHash string
	// Mtime (Unix nanoseconds) and Size are the file's right after.
	Mtime, Size int64
	At          time.Time
}

// SetMediaFileTagsWritten records a write: the hashes, the file's
// modification time and size right after, and a clean failure count.
func (db *DB) SetMediaFileTagsWritten(
	ctx context.Context,
	id uint32,
	w TagWrite,
) error {
	return db.client.MediaFile.UpdateOneID(id).
		SetTagsState(entmediafile.TagsStateWritten).
		SetTagsHash(w.Hash).
		SetTagsArtHash(w.ArtHash).
		SetTagsMtime(w.Mtime).
		SetTagsSize(w.Size).
		SetSize(w.Size).
		SetTaggedAt(w.At).
		SetTagsCheckedAt(w.At).
		SetTagFailures(0).
		SetTagsError("").
		Exec(ctx)
}

// SetMediaFileTagsDeferred parks a file a torrent still owns or shares.
func (db *DB) SetMediaFileTagsDeferred(ctx context.Context, id uint32) error {
	return db.client.MediaFile.UpdateOneID(id).
		SetTagsState(entmediafile.TagsStateDeferred).
		Exec(ctx)
}

// RecordMediaFileTagFailure counts one failed write; at maxFailures the file
// is failed and the writer stops trying until someone asks again.
func (db *DB) RecordMediaFileTagFailure(
	ctx context.Context,
	id uint32,
	msg string,
	maxFailures uint8,
) error {
	mf, err := db.client.MediaFile.Get(ctx, id)
	if err != nil {
		return err
	}
	u := db.client.MediaFile.UpdateOneID(id).
		SetTagsError(msg).
		AddTagFailures(1)
	if mf.TagFailures+1 >= maxFailures {
		u = u.SetTagsState(entmediafile.TagsStateFailed)
	}
	return u.Exec(ctx)
}

// MarkAlbumTags queues the track files of the given albums for the music tag
// writer and returns how many it queued. Only albums whose tags are managed
// (Album.tags_managed) are reached. A deferred file stays deferred: its tags
// are computed when it is finally written. MarkStale reaches written, failed
// and never-tagged files and keeps their hash; MarkForce also empties it.
func (db *DB) MarkAlbumTags(
	ctx context.Context,
	albumIDs []uint32,
	mark TagMark,
) (int, error) {
	if len(albumIDs) == 0 {
		return 0, nil
	}
	return db.markTags(ctx, mark,
		entmediafile.HasTrackWith(track.HasAlbumWith(
			album.IDIn(albumIDs...),
			album.TagsManaged(true),
		)),
	)
}

// MarkAllMusicTags is MarkAlbumTags over every managed album.
func (db *DB) MarkAllMusicTags(ctx context.Context, mark TagMark) (int, error) {
	return db.markTags(ctx, mark,
		entmediafile.HasTrackWith(track.HasAlbumWith(album.TagsManaged(true))),
	)
}

// SetAlbumTagsManaged sets whether the tag writer re-queues the album's files
// on its own.
func (db *DB) SetAlbumTagsManaged(
	ctx context.Context,
	id uint32,
	managed bool,
) error {
	return db.client.Album.UpdateOneID(id).SetTagsManaged(managed).Exec(ctx)
}

// SetAlbumArtSidecar records the hash of the cover file the tag writer left
// beside the album's files, empty once it removed it.
func (db *DB) SetAlbumArtSidecar(ctx context.Context, id uint32, hash string) error {
	return db.client.Album.UpdateOneID(id).SetArtSidecarHash(hash).Exec(ctx)
}

// SetArtistArtSidecar records the hash of the folder.jpg the tag writer left
// in the artist's folder, empty once it removed it.
func (db *DB) SetArtistArtSidecar(
	ctx context.Context,
	id uint32,
	hash string,
) error {
	return db.client.Artist.UpdateOneID(id).SetArtSidecarHash(hash).Exec(ctx)
}

func (db *DB) markTags(
	ctx context.Context,
	mark TagMark,
	scope ...predicate.MediaFile,
) (int, error) {
	ps := append([]predicate.MediaFile{
		entmediafile.Or(
			entmediafile.TagsStateIsNil(),
			entmediafile.TagsStateIn(
				entmediafile.TagsStateWritten,
				entmediafile.TagsStateFailed,
				entmediafile.TagsStatePending,
			),
		),
	}, scope...)
	u := db.client.MediaFile.Update().
		Where(ps...).
		SetTagsState(entmediafile.TagsStatePending).
		SetTagFailures(0).
		SetTagsError("")
	if mark == MarkForce {
		u = u.SetTagsHash("")
	}
	n, err := u.Save(ctx)
	if err != nil || mark != MarkForce {
		return n, err
	}
	// A deferred file stays deferred, but loses its hash all the same.
	if _, err := db.client.MediaFile.Update().
		Where(append([]predicate.MediaFile{
			entmediafile.TagsStateEQ(entmediafile.TagsStateDeferred),
		}, scope...)...).
		SetTagsHash("").
		Save(ctx); err != nil {
		return n, err
	}
	return n, nil
}

// AlbumTorrentHashes is the torrent hash of every download record behind the
// album: its own grabs and the discography packs that expected it.
func (db *DB) AlbumTorrentHashes(
	ctx context.Context,
	albumID uint32,
) ([]string, error) {
	return db.client.DownloadRecord.Query().
		Where(downloadrecord.Or(
			downloadrecord.HasAlbumWith(album.IDEQ(albumID)),
			downloadrecord.HasAlbumsWith(album.IDEQ(albumID)),
		)).
		Select(downloadrecord.FieldTorrentHash).
		Strings(ctx)
}
