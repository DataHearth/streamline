package db

import (
	"context"
	"fmt"
	"slices"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/book"
	"github.com/datahearth/streamline/ent/bookseries"
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

// RootOfFile names the root a file belongs to, from its loaded owner edges. It
// reports false for a row with no owner.
func RootOfFile(row *ent.MediaFile) (LibraryRoot, bool) {
	switch {
	case row.Edges.Movie != nil:
		return RootMovies, true
	case row.Edges.Episode != nil:
		return RootSeries, true
	case row.Edges.Track != nil:
		return RootMusic, true
	case row.Edges.Book != nil:
		return BookRoot(string(row.BookKind)), true
	}
	return "", false
}

// AllConfirmedMissing reports whether a title's files in one place are all
// confirmed missing, the condition for marking it. A present file beside a
// missing one (the orphan scan adopted it during the wait) means the title has
// what it needs.
func AllConfirmedMissing(files []*ent.MediaFile) bool {
	return len(files) > 0 &&
		!slices.ContainsFunc(files, func(f *ent.MediaFile) bool {
			return f.MissingConfirmedAt == nil
		})
}

// PresentFiles leaves out rows whose file is missing from disk, for the checks
// that ask whether a title holds a file an import or a download must not touch.
func PresentFiles(files []*ent.MediaFile) []*ent.MediaFile {
	return slices.DeleteFunc(slices.Clone(files), func(f *ent.MediaFile) bool {
		return f.MissingSince != nil
	})
}

// bookKindIs reads book_kind alone: it is only set on book files, and a HasBook
// term beside it sends SQLite back to a table scan when one kind holds every
// row. The NOT NULL term is what lets the drift walk negate this: NOT (book_kind
// = 'ebook') alone is NULL for every file that is not a book, which dropped all
// of them from the walk while a book root was offline.
func bookKindIs(kind mediafile.BookKind) predicate.MediaFile {
	return mediafile.And(mediafile.BookKindNotNil(), mediafile.BookKindEQ(kind))
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
		return bookKindIs(mediafile.BookKindEbook)
	case RootAudiobooks:
		return bookKindIs(mediafile.BookKindAudiobook)
	}
	panic(fmt.Sprintf("unknown library root %q", root))
}

// rootOrder keeps the walk on an index: ordered by id alone SQLite scans the
// table for the first match, all of it for a root with no files, and this runs on
// every system info read and every gated job. A book root's index is book_kind,
// which already ends in the rowid.
func rootOrder(root LibraryRoot) []string {
	switch root {
	case RootMovies:
		return []string{mediafile.MovieColumn, mediafile.FieldID}
	case RootSeries:
		return []string{mediafile.EpisodeColumn, mediafile.FieldID}
	case RootMusic:
		return []string{mediafile.TrackColumn, mediafile.FieldID}
	}
	return []string{mediafile.FieldID}
}

// BookFileRoots names the roots holding a book's files.
func (db *DB) BookFileRoots(
	ctx context.Context,
	bookID uint32,
) ([]LibraryRoot, error) {
	return db.bookRootsHolding(ctx, mediafile.HasBookWith(book.ID(bookID)))
}

// BookSeriesFileRoots names the roots holding the files of a series' volumes.
func (db *DB) BookSeriesFileRoots(
	ctx context.Context,
	seriesID uint32,
) ([]LibraryRoot, error) {
	return db.bookRootsHolding(
		ctx,
		mediafile.HasBookWith(book.HasSeriesWith(bookseries.ID(seriesID))),
	)
}

func (db *DB) bookRootsHolding(
	ctx context.Context,
	holding predicate.MediaFile,
) ([]LibraryRoot, error) {
	kinds, err := db.client.MediaFile.Query().
		Where(holding, mediafile.BookKindNotNil()).
		Unique(true).
		Select(mediafile.FieldBookKind).
		Strings(ctx)
	if err != nil {
		return nil, fmt.Errorf("list book file roots: %w", err)
	}
	roots := make([]LibraryRoot, len(kinds))
	for i, kind := range kinds {
		roots[i] = BookRoot(kind)
	}
	return roots, nil
}

// CountRootFiles counts the file rows tracked under a root.
func (db *DB) CountRootFiles(ctx context.Context, root LibraryRoot) (int, error) {
	return db.countRootFiles(ctx, rootPredicate(root), root)
}

// CountLiveRootFiles counts a root's rows not confirmed missing.
func (db *DB) CountLiveRootFiles(
	ctx context.Context,
	root LibraryRoot,
) (int, error) {
	return db.countRootFiles(ctx, liveRootPredicate(root), root)
}

// liveRootPredicate leaves out the rows confirmed missing: they are already known
// to be gone, so they say nothing about whether the volume is there.
func liveRootPredicate(root LibraryRoot) predicate.MediaFile {
	return mediafile.And(rootPredicate(root), mediafile.MissingConfirmedAtIsNil())
}

func (db *DB) countRootFiles(
	ctx context.Context,
	rows predicate.MediaFile,
	root LibraryRoot,
) (int, error) {
	n, err := db.client.MediaFile.Query().Where(rows).Count(ctx)
	if err != nil {
		return 0, fmt.Errorf("count %s files: %w", root, err)
	}
	return n, nil
}

// RootFilePathAt returns the path of a root's offset-th file row in rootOrder, or
// "" past the end.
func (db *DB) RootFilePathAt(
	ctx context.Context,
	root LibraryRoot,
	offset int,
) (string, error) {
	return db.rootFilePathAt(ctx, rootPredicate(root), root, offset)
}

// LiveRootFilePathAt is RootFilePathAt over CountLiveRootFiles' rows.
func (db *DB) LiveRootFilePathAt(
	ctx context.Context,
	root LibraryRoot,
	offset int,
) (string, error) {
	return db.rootFilePathAt(ctx, liveRootPredicate(root), root, offset)
}

func (db *DB) rootFilePathAt(
	ctx context.Context,
	rows predicate.MediaFile,
	root LibraryRoot,
	offset int,
) (string, error) {
	path, err := db.client.MediaFile.Query().
		Where(rows).
		Order(ent.Asc(rootOrder(root)...)).
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
		Select(
			mediafile.FieldID,
			mediafile.FieldPath,
			mediafile.FieldMissingSince,
			mediafile.FieldMissingConfirmedAt,
		).
		Scan(ctx, &rows)
	if err != nil {
		return nil, fmt.Errorf("list %s files: %w", root, err)
	}
	return rows, nil
}
