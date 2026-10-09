package db

import (
	"context"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/album"
	"github.com/datahearth/streamline/ent/artist"
	"github.com/datahearth/streamline/ent/author"
	"github.com/datahearth/streamline/ent/book"
	"github.com/datahearth/streamline/ent/downloadrecord"
	"github.com/datahearth/streamline/ent/mediafile"
	"github.com/datahearth/streamline/ent/predicate"
	"github.com/datahearth/streamline/ent/track"
)

// ListUpgradeCandidateAlbums returns the monitored albums that already hold at
// least one file and that no live download record covers, with the artist and
// every track's files loaded: the music twin of ListUpgradeCandidateShows. The
// feed scanner compares an incoming release's tier against the worst tier on
// disk.
func (db *DB) ListUpgradeCandidateAlbums(ctx context.Context) ([]*ent.Album, error) {
	return db.client.Album.Query().
		Where(
			album.Monitored(true),
			album.HasTracksWith(track.HasMediaFiles()),
			album.Not(album.HasDownloadRecordsWith(liveRecord())),
			album.Not(album.HasPackRecordsWith(liveRecord())),
		).
		WithArtist().
		WithTracks(func(q *ent.TrackQuery) { q.WithMediaFiles() }).
		All(ctx)
}

// SetLiveAlbumRecordReplaceMode flags the album's newest downloading record so
// the importer replaces per track. A grab hands back no record id through the
// music service, so the scanner finds the one it just created by its album.
func (db *DB) SetLiveAlbumRecordReplaceMode(
	ctx context.Context,
	albumID uint32,
	mode downloadrecord.ReplaceMode,
) error {
	id, err := db.client.DownloadRecord.Query().
		Where(
			downloadrecord.HasAlbumWith(album.ID(albumID)),
			downloadrecord.StatusEQ(downloadrecord.StatusDownloading),
		).
		Order(ent.Desc(downloadrecord.FieldID)).
		FirstID(ctx)
	if err != nil {
		return err
	}
	return db.SetDownloadRecordReplaceMode(ctx, id, mode)
}

// SetLiveArtistRecordReplaceMode flags the artist's newest downloading
// discography record, the pack twin of SetLiveAlbumRecordReplaceMode.
func (db *DB) SetLiveArtistRecordReplaceMode(
	ctx context.Context,
	artistID uint32,
	mode downloadrecord.ReplaceMode,
) error {
	id, err := db.client.DownloadRecord.Query().
		Where(
			downloadrecord.HasArtistWith(artist.ID(artistID)),
			downloadrecord.StatusEQ(downloadrecord.StatusDownloading),
		).
		Order(ent.Desc(downloadrecord.FieldID)).
		FirstID(ctx)
	if err != nil {
		return err
	}
	return db.SetDownloadRecordReplaceMode(ctx, id, mode)
}

// ListUpgradeCandidateBooks returns the books of monitored authors that hold
// at least one file of a slot no live record covers, with the author and every
// file loaded. The query says some slot is a candidate; the caller re-checks
// each slot.
func (db *DB) ListUpgradeCandidateBooks(ctx context.Context) ([]*ent.Book, error) {
	// A slot is a candidate when it holds a file and nothing is already coming
	// for it.
	slot := func(kind mediafile.BookKind, rec downloadrecord.BookKind) predicate.Book {
		return book.And(
			book.HasMediaFilesWith(mediafile.BookKindEQ(kind)),
			book.Not(book.HasDownloadRecordsWith(
				downloadrecord.BookKindEQ(rec),
				downloadrecord.StatusIn(
					downloadrecord.StatusDownloading,
					downloadrecord.StatusImporting,
				),
			)),
		)
	}
	return db.client.Book.Query().
		Where(
			book.HasAuthorWith(author.Monitored(true)),
			book.Or(
				slot(mediafile.BookKindEbook, downloadrecord.BookKindEbook),
				slot(mediafile.BookKindAudiobook, downloadrecord.BookKindAudiobook),
			),
		).
		WithAuthor().
		WithMediaFiles().
		All(ctx)
}

// SetLiveBookRecordReplaceMode flags the newest downloading record of one book
// slot, as SetLiveAlbumRecordReplaceMode does for an album.
func (db *DB) SetLiveBookRecordReplaceMode(
	ctx context.Context,
	bookID uint32,
	kind downloadrecord.BookKind,
	mode downloadrecord.ReplaceMode,
) error {
	id, err := db.client.DownloadRecord.Query().
		Where(
			downloadrecord.HasBookWith(book.ID(bookID)),
			downloadrecord.BookKindEQ(kind),
			downloadrecord.StatusEQ(downloadrecord.StatusDownloading),
		).
		Order(ent.Desc(downloadrecord.FieldID)).
		FirstID(ctx)
	if err != nil {
		return err
	}
	return db.SetDownloadRecordReplaceMode(ctx, id, mode)
}
