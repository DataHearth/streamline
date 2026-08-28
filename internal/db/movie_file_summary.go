package db

import (
	"context"
	"fmt"

	entsql "entgo.io/ent/dialect/sql"

	"github.com/datahearth/streamline/ent/mediafile"
	"github.com/datahearth/streamline/ent/movie"
)

// MovieFileSummary is what a list response needs to know about a movie's
// files: how many, how big, and what the primary one is. PrimaryPath is the
// path of the largest attached file — the caller parses resolution and codec
// off it, since neither is stored.
type MovieFileSummary struct {
	FileCount   uint32
	SizeBytes   int64
	PrimaryPath string
}

// MovieFileSummaries rolls up the files of the given movies in one lean pass.
// The list view renders a size and a quality column per row; eager-loading
// every file of every movie on the page to compute two numbers is what this
// replaces — the same trade EpisodeCounts makes for the series list.
//
// A movie with no files is absent from the map, which reads back as a zero
// summary and is what the API omits.
func (db *DB) MovieFileSummaries(
	ctx context.Context,
	movieIDs []uint32,
) (map[uint32]MovieFileSummary, error) {
	out := make(map[uint32]MovieFileSummary, len(movieIDs))
	if len(movieIDs) == 0 {
		return out, nil
	}
	var rows []struct {
		MovieID uint32 `sql:"movie_id"`
		Size    int64  `sql:"size"`
		Path    string `sql:"path"`
	}
	err := db.client.MediaFile.Query().
		Where(mediafile.HasMovieWith(movie.IDIn(movieIDs...))).
		Modify(func(s *entsql.Selector) {
			s.Select(
				entsql.As(s.C(mediafile.MovieColumn), "movie_id"),
				s.C(mediafile.FieldSize),
				s.C(mediafile.FieldPath),
			)
		}).
		Scan(ctx, &rows)
	if err != nil {
		return nil, fmt.Errorf("movie file summaries: %w", err)
	}

	// Largest file wins the primary slot, matching what the detail views
	// treat as the movie's file when there is more than one.
	primarySize := make(map[uint32]int64, len(movieIDs))
	for _, r := range rows {
		sum := out[r.MovieID]
		sum.FileCount++
		sum.SizeBytes += r.Size
		if sum.PrimaryPath == "" || r.Size > primarySize[r.MovieID] {
			sum.PrimaryPath = r.Path
			primarySize[r.MovieID] = r.Size
		}
		out[r.MovieID] = sum
	}
	return out, nil
}
