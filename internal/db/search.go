package db

import (
	"context"
	"fmt"

	entsql "entgo.io/ent/dialect/sql"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/album"
	"github.com/datahearth/streamline/ent/episode"
	"github.com/datahearth/streamline/ent/mediafile"
	"github.com/datahearth/streamline/ent/track"
)

// TitleSearchParams pages a folded title match: the search surfaces' albums
// and episodes, which have no list page of their own.
type TitleSearchParams struct {
	// Query matches the title, folded on both sides. Empty matches every row.
	Query  string
	Offset uint32
	Limit  uint16
}

// countRow scans a COUNT(*) straight into the API's width.
type countRow struct {
	Total uint32 `sql:"total"`
}

// byMatch ranks a title equal to the query first, then one starting with it,
// then the rest. Without it a bare "discovery" listed five remixes and a
// bootleg of Daft Punk's, newest first, and not the album itself. An empty
// query ranks nothing.
func byMatch(col, query string) func(*entsql.Selector) {
	return func(s *entsql.Selector) {
		if query == "" {
			return
		}
		f := foldText(query)
		s.OrderExpr(entsql.ExprP(
			"CASE WHEN fold("+s.C(
				col,
			)+") = ? THEN 0 WHEN fold("+s.C(
				col,
			)+") LIKE ? THEN 1 ELSE 2 END",
			f,
			f+"%",
		))
	}
}

// SearchAlbums pages the albums whose title holds the query, each with its
// artist loaded, the closest title first. An artist's whole discography is
// album rows, so among equal matches the albums holding a file come first:
// those are the ones a search is usually for.
func (db *DB) SearchAlbums(
	ctx context.Context,
	p TitleSearchParams,
) ([]*ent.Album, uint32, error) {
	q := db.client.Album.Query()
	if p.Query != "" {
		q = q.Where(func(s *entsql.Selector) {
			s.Where(foldContains(s, album.FieldTitle, p.Query))
		})
	}
	var counted []countRow
	err := q.Clone().
		Modify(func(s *entsql.Selector) { s.Select(entsql.As("COUNT(*)", "total")) }).
		Scan(ctx, &counted)
	if err != nil {
		return nil, 0, fmt.Errorf("count albums: %w", err)
	}
	if len(counted) == 0 || counted[0].Total == 0 {
		return []*ent.Album{}, 0, nil
	}
	total := counted[0].Total
	rows, err := q.
		Order(
			byMatch(album.FieldTitle, p.Query),
			func(s *entsql.Selector) {
				s.OrderExpr(entsql.Expr(fmt.Sprintf(
					"EXISTS (SELECT 1 FROM %s t JOIN %s f ON f.%s = t.id WHERE t.%s = %s) DESC",
					track.Table,
					mediafile.Table,
					mediafile.TrackColumn,
					track.AlbumColumn,
					s.C(album.FieldID),
				)))
			},
			album.ByMonitored(entsql.OrderDesc()),
			album.ByReleaseDate(entsql.OrderDesc()),
			album.ByID(),
		).
		Offset(int(p.Offset)).
		Limit(int(p.Limit)).
		WithArtist().
		All(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("search albums: %w", err)
	}
	return rows, total, nil
}

// SearchEpisodes pages the episodes whose title holds the query, each with
// its season and show loaded, the closest title first. Among equal matches the
// episodes holding a file come first, then the most recently aired; an
// undated one sorts last, as SQLite orders NULL lowest.
func (db *DB) SearchEpisodes(
	ctx context.Context,
	p TitleSearchParams,
) ([]*ent.Episode, uint32, error) {
	q := db.client.Episode.Query()
	if p.Query != "" {
		q = q.Where(func(s *entsql.Selector) {
			s.Where(foldContains(s, episode.FieldTitle, p.Query))
		})
	}
	var counted []countRow
	err := q.Clone().
		Modify(func(s *entsql.Selector) { s.Select(entsql.As("COUNT(*)", "total")) }).
		Scan(ctx, &counted)
	if err != nil {
		return nil, 0, fmt.Errorf("count episodes: %w", err)
	}
	if len(counted) == 0 || counted[0].Total == 0 {
		return []*ent.Episode{}, 0, nil
	}
	total := counted[0].Total
	rows, err := q.
		Order(
			byMatch(episode.FieldTitle, p.Query),
			func(s *entsql.Selector) {
				s.OrderExpr(entsql.Expr(fmt.Sprintf(
					"EXISTS (SELECT 1 FROM %s f WHERE f.%s = %s) DESC",
					mediafile.Table, mediafile.EpisodeColumn, s.C(episode.FieldID),
				)))
			},
			episode.ByAirDate(entsql.OrderDesc()),
			episode.ByID(),
		).
		Offset(int(p.Offset)).
		Limit(int(p.Limit)).
		WithSeason(func(sq *ent.SeasonQuery) { sq.WithTvShow() }).
		All(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("search episodes: %w", err)
	}
	return rows, total, nil
}
