package db

import (
	"context"
	"fmt"
	"time"

	entsql "entgo.io/ent/dialect/sql"

	"github.com/datahearth/streamline/ent/album"
	"github.com/datahearth/streamline/ent/book"
	"github.com/datahearth/streamline/ent/episode"
	"github.com/datahearth/streamline/ent/mediafile"
	"github.com/datahearth/streamline/ent/movie"
	"github.com/datahearth/streamline/ent/season"
	"github.com/datahearth/streamline/ent/track"
	"github.com/datahearth/streamline/ent/tvshow"
	"github.com/datahearth/streamline/internal/utils/numeric"
)

type ArrivalKind string

const (
	ArrivalMovie  ArrivalKind = "movie"
	ArrivalSeries ArrivalKind = "series"
	ArrivalAlbum  ArrivalKind = "album"
	ArrivalBook   ArrivalKind = "book"
)

// Arrival is one import into the library: the files of one movie, show,
// album or book format that landed within additionWindow of the newest of
// them. ID is the movie, show, album or book.
type Arrival struct {
	Kind  ArrivalKind
	At    time.Time
	ID    uint32
	Title string
	Year  uint16
	// Movie: the largest file of the import, which the caller parses the
	// resolution off, as the movie list does.
	Path string
	// Series.
	Addition *Addition
	// Album.
	ArtistID    uint32
	ArtistName  string
	ReleaseType album.Type
	// Book. SeriesID is zero for a standalone book.
	Author   string
	BookKind book.Kind
	Format   mediafile.BookKind
	SeriesID uint32
	Volume   *float64
}

// arrivalScanPage is how many files one round of the newest-first walk reads.
// A page is cheap; a season pack or an audiobook folder can span several.
const arrivalScanPage = 500

type arrivalFile struct {
	ID       uint32             `sql:"id"`
	At       time.Time          `sql:"create_time"`
	Path     string             `sql:"path"`
	Size     int64              `sql:"size"`
	BookKind mediafile.BookKind `sql:"book_kind"`
	MovieID  *uint32            `sql:"movie_id"`
	BookID   *uint32            `sql:"book_id"`
	ShowID   *uint32            `sql:"show_id"`
	Season   uint16             `sql:"season_number"`
	Episode  uint16             `sql:"episode_number"`
	EpTitle  string             `sql:"episode_title"`
	AlbumID  *uint32            `sql:"album_id"`
}

type arrivalKey struct {
	kind   ArrivalKind
	id     uint32
	format mediafile.BookKind
}

type arrivalBatch struct {
	key    arrivalKey
	newest time.Time
	files  []arrivalFile
}

// RecentArrivals lists the newest limit imports across the four media, newest
// first. A file's create_time is when it was imported (an upgrade is a new
// row; a rename or a transcode swap updates the old one), so the walk reads
// media_files newest first and folds each file into its owner's open batch.
// It stops once limit batches exist and the next file is too old to join any
// of them.
func (db *DB) RecentArrivals(ctx context.Context, limit int) ([]Arrival, error) {
	var (
		batches []*arrivalBatch
		open    = make(map[arrivalKey]*arrivalBatch)
	)
walk:
	for offset := 0; ; offset += arrivalScanPage {
		files, err := db.arrivalFiles(ctx, offset)
		if err != nil {
			return nil, err
		}
		for _, f := range files {
			if len(batches) >= limit && batches[limit-1].newest.Sub(f.At) > additionWindow {
				break walk
			}
			key, ok := f.key()
			if !ok {
				continue
			}
			if b := open[key]; b != nil && b.newest.Sub(f.At) <= additionWindow {
				b.files = append(b.files, f)
				continue
			}
			b := &arrivalBatch{key: key, newest: f.At, files: []arrivalFile{f}}
			batches = append(batches, b)
			open[key] = b
		}
		if len(files) < arrivalScanPage {
			break
		}
	}
	if len(batches) > limit {
		batches = batches[:limit]
	}
	return db.hydrateArrivals(ctx, batches)
}

func (f arrivalFile) key() (arrivalKey, bool) {
	switch {
	case f.MovieID != nil:
		return arrivalKey{kind: ArrivalMovie, id: *f.MovieID}, true
	case f.ShowID != nil:
		return arrivalKey{kind: ArrivalSeries, id: *f.ShowID}, true
	case f.AlbumID != nil:
		return arrivalKey{kind: ArrivalAlbum, id: *f.AlbumID}, true
	case f.BookID != nil:
		return arrivalKey{kind: ArrivalBook, id: *f.BookID, format: f.BookKind}, true
	}
	return arrivalKey{}, false
}

func (db *DB) arrivalFiles(ctx context.Context, offset int) ([]arrivalFile, error) {
	var rows []arrivalFile
	err := db.client.MediaFile.Query().
		Order(mediafile.ByCreateTime(entsql.OrderDesc()), mediafile.ByID(entsql.OrderDesc())).
		Offset(offset).
		Limit(arrivalScanPage).
		Modify(func(s *entsql.Selector) {
			b := entsql.Dialect(s.Dialect())
			ep := b.Table(episode.Table).As("arr_episode")
			se := b.Table(season.Table).As("arr_season")
			tr := b.Table(track.Table).As("arr_track")
			s.LeftJoin(ep).On(s.C(mediafile.EpisodeColumn), ep.C(episode.FieldID))
			s.LeftJoin(se).On(ep.C(episode.SeasonColumn), se.C(season.FieldID))
			s.LeftJoin(tr).On(s.C(mediafile.TrackColumn), tr.C(track.FieldID))
			s.Select(
				s.C(mediafile.FieldID),
				s.C(mediafile.FieldCreateTime),
				s.C(mediafile.FieldPath),
				s.C(mediafile.FieldSize),
				entsql.As(coalesce(s.C(mediafile.FieldBookKind), "''"), "book_kind"),
				entsql.As(s.C(mediafile.MovieColumn), "movie_id"),
				entsql.As(s.C(mediafile.BookColumn), "book_id"),
				entsql.As(se.C(season.TvShowColumn), "show_id"),
				entsql.As(coalesce(se.C(season.FieldNumber), "0"), "season_number"),
				entsql.As(
					coalesce(ep.C(episode.FieldNumber), "0"),
					"episode_number",
				),
				entsql.As(coalesce(ep.C(episode.FieldTitle), "''"), "episode_title"),
				entsql.As(tr.C(track.AlbumColumn), "album_id"),
			)
		}).
		Scan(ctx, &rows)
	if err != nil {
		return nil, fmt.Errorf("arrival files: %w", err)
	}
	return rows, nil
}

// coalesce keeps the columns a LEFT JOIN or an optional field leaves NULL
// scannable into plain types.
func coalesce(col, def string) string {
	return fmt.Sprintf("COALESCE(%s, %s)", col, def)
}

func (db *DB) hydrateArrivals(
	ctx context.Context,
	batches []*arrivalBatch,
) ([]Arrival, error) {
	ids := make(map[ArrivalKind][]uint32)
	for _, b := range batches {
		ids[b.key.kind] = append(ids[b.key.kind], b.key.id)
	}

	movies, err := db.client.Movie.Query().
		Where(movie.IDIn(ids[ArrivalMovie]...)).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("arrival movies: %w", err)
	}
	shows, err := db.client.TVShow.Query().
		Where(tvshow.IDIn(ids[ArrivalSeries]...)).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("arrival shows: %w", err)
	}
	seasonSizes, err := db.SeasonEpisodeCounts(ctx, ids[ArrivalSeries])
	if err != nil {
		return nil, err
	}
	albums, err := db.client.Album.Query().
		Where(album.IDIn(ids[ArrivalAlbum]...)).
		WithArtist().
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("arrival albums: %w", err)
	}
	books, err := db.client.Book.Query().
		Where(book.IDIn(ids[ArrivalBook]...)).
		WithSeries().
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("arrival books: %w", err)
	}

	byID := func(kind ArrivalKind, id uint32) arrivalKey { return arrivalKey{kind: kind, id: id} }
	base := make(
		map[arrivalKey]Arrival,
		len(movies)+len(shows)+len(albums)+len(books),
	)
	for _, m := range movies {
		base[byID(ArrivalMovie, m.ID)] = Arrival{Title: m.Title, Year: m.Year}
	}
	for _, s := range shows {
		base[byID(ArrivalSeries, s.ID)] = Arrival{Title: s.Title, Year: s.Year}
	}
	for _, a := range albums {
		out := Arrival{Title: a.Title, ReleaseType: a.Type}
		if a.ReleaseDate != nil {
			out.Year = numeric.SaturateU16(a.ReleaseDate.Year())
		}
		if ar := a.Edges.Artist; ar != nil {
			out.ArtistID, out.ArtistName = ar.ID, ar.Name
		}
		base[byID(ArrivalAlbum, a.ID)] = out
	}
	for _, b := range books {
		out := Arrival{Title: b.Title, Author: b.AuthorName, BookKind: b.Kind}
		if b.ReleaseYear != nil {
			out.Year = *b.ReleaseYear
		}
		if s := b.Edges.Series; s != nil {
			out.SeriesID, out.Volume = s.ID, b.SeriesPosition
		}
		base[byID(ArrivalBook, b.ID)] = out
	}

	out := make([]Arrival, 0, len(batches))
	for _, b := range batches {
		a, ok := base[byID(b.key.kind, b.key.id)]
		if !ok {
			continue
		}
		a.Kind, a.ID, a.At = b.key.kind, b.key.id, b.newest
		switch b.key.kind {
		case ArrivalMovie:
			var size int64 = -1
			for _, f := range b.files {
				if f.Size > size {
					a.Path, size = f.Path, f.Size
				}
			}
		case ArrivalSeries:
			eps := make([]episodeArrival, 0, len(b.files))
			for _, f := range b.files {
				eps = append(eps, episodeArrival{
					season: f.Season, number: f.Episode, title: f.EpTitle, at: f.At,
				})
			}
			a.Addition = newAddition(eps, seasonSizes[b.key.id])
		case ArrivalBook:
			a.Format = b.key.format
		}
		out = append(out, a)
	}
	return out, nil
}
