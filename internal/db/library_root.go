package db

import (
	"context"
	"fmt"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/mediafile"
	"github.com/datahearth/streamline/ent/predicate"
)

// LibraryRoot names one of the five configured library folders. A file belongs
// to a root by its owner type rather than its path prefix, so a file whose root
// moved is still counted under it.
type LibraryRoot string

const (
	RootMovies     LibraryRoot = "movies"
	RootSeries     LibraryRoot = "series"
	RootMusic      LibraryRoot = "music"
	RootEbooks     LibraryRoot = "ebooks"
	RootAudiobooks LibraryRoot = "audiobooks"
)

var LibraryRoots = []LibraryRoot{
	RootMovies,
	RootSeries,
	RootMusic,
	RootEbooks,
	RootAudiobooks,
}

// BookRoot names the root a book slot's files live under.
func BookRoot(slot string) LibraryRoot {
	if slot == string(mediafile.BookKindAudiobook) {
		return RootAudiobooks
	}
	return RootEbooks
}

func rootPredicate(root LibraryRoot) predicate.MediaFile {
	switch root {
	case RootMovies:
		return mediafile.HasMovie()
	case RootSeries:
		return mediafile.HasEpisode()
	case RootMusic:
		return mediafile.HasTrack()
	case RootEbooks:
		return mediafile.And(
			mediafile.HasBook(),
			mediafile.BookKindEQ(mediafile.BookKindEbook),
		)
	case RootAudiobooks:
		return mediafile.And(
			mediafile.HasBook(),
			mediafile.BookKindEQ(mediafile.BookKindAudiobook),
		)
	}
	panic(fmt.Sprintf("unknown library root %q", root))
}

func rootOwnerColumn(root LibraryRoot) string {
	switch root {
	case RootMovies:
		return mediafile.MovieColumn
	case RootSeries:
		return mediafile.EpisodeColumn
	case RootMusic:
		return mediafile.TrackColumn
	}
	return mediafile.BookColumn
}

// CountRootFiles counts the file rows tracked under a root.
func (db *DB) CountRootFiles(ctx context.Context, root LibraryRoot) (int, error) {
	n, err := db.client.MediaFile.Query().Where(rootPredicate(root)).Count(ctx)
	if err != nil {
		return 0, fmt.Errorf("count %s files: %w", root, err)
	}
	return n, nil
}

// RootFilePathAt returns the path of a root's offset-th file row, or "" past the
// end. Ordered by owner so the walk stays on the partial owner index: ordered by
// id alone SQLite scans the table for the first match, all of it for a root with
// no files, and this runs on every system info read and every gated job.
// ponytail: a book root still walks the other kind's rows to find its own, an
// index on book_kind if a large one-kind book library ever shows it.
func (db *DB) RootFilePathAt(
	ctx context.Context,
	root LibraryRoot,
	offset int,
) (string, error) {
	path, err := db.client.MediaFile.Query().
		Where(rootPredicate(root)).
		Order(ent.Asc(rootOwnerColumn(root), mediafile.FieldID)).
		Offset(offset).
		Limit(1).
		Select(mediafile.FieldPath).
		String(ctx)
	if ent.IsNotFound(err) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("read %s file at %d: %w", root, offset, err)
	}
	return path, nil
}

// ListRootFilesForDrift returns one keyset page of a root's file rows, the same
// projection the drift check walks.
func (db *DB) ListRootFilesForDrift(
	ctx context.Context,
	root LibraryRoot,
	afterID uint32,
	limit int,
) ([]DriftRow, error) {
	var rows []DriftRow
	err := db.client.MediaFile.Query().
		Where(rootPredicate(root), mediafile.IDGT(afterID)).
		Order(ent.Asc(mediafile.FieldID)).
		Limit(limit).
		Select(mediafile.FieldID, mediafile.FieldPath, mediafile.FieldMissingSince).
		Scan(ctx, &rows)
	if err != nil {
		return nil, fmt.Errorf("list %s files: %w", root, err)
	}
	return rows, nil
}
