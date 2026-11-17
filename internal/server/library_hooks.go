package server

import (
	"context"
	"log/slog"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/mediafile"
	"github.com/datahearth/streamline/internal/extension"
)

// registerLibraryHooks tells the extensions about every movie, series and
// video file that is created, updated or deleted, whatever code path does it:
// an add, a metadata refresh, a re-identify, an import, a rename, a delete.
// The mutation is reported once it succeeded, and possibly before its
// transaction commits or for one that rolls back: the extensions are called
// after a window and compare against what the views then hold.
func registerLibraryHooks(
	client *ent.Client,
	changed func(extension.LibraryChange),
) {
	client.Movie.Use(titleHook(
		func(m ent.Mutation) (titleMutation, bool) {
			mm, ok := m.(*ent.MovieMutation)
			return mm, ok
		},
		func(v ent.Value) uint32 {
			if row, ok := v.(*ent.Movie); ok {
				return row.ID
			}
			return 0
		},
		func(ids []uint32) { changed(extension.LibraryChange{Movies: ids}) },
	))
	client.TVShow.Use(titleHook(
		func(m ent.Mutation) (titleMutation, bool) {
			tm, ok := m.(*ent.TVShowMutation)
			return tm, ok
		},
		func(v ent.Value) uint32 {
			if row, ok := v.(*ent.TVShow); ok {
				return row.ID
			}
			return 0
		},
		func(ids []uint32) { changed(extension.LibraryChange{Series: ids}) },
	))
	client.MediaFile.Use(videoFileHook(changed))
}

type titleMutation interface {
	ent.Mutation
	IDs(ctx context.Context) ([]uint32, error)
}

func titleHook(
	cast func(ent.Mutation) (titleMutation, bool),
	created func(ent.Value) uint32,
	report func([]uint32),
) ent.Hook {
	return func(next ent.Mutator) ent.Mutator {
		return ent.MutateFunc(
			func(ctx context.Context, m ent.Mutation) (ent.Value, error) {
				tm, ok := cast(m)
				if !ok {
					return next.Mutate(ctx, m)
				}
				var ids []uint32
				// Read first: a delete leaves nothing to ask afterwards.
				if !tm.Op().Is(ent.OpCreate) {
					var err error
					if ids, err = tm.IDs(ctx); err != nil {
						slog.WarnContext(ctx,
							"Library hook could not read the changed titles",
							"error", err)
					}
				}
				val, err := next.Mutate(ctx, m)
				if err != nil {
					return val, err
				}
				if tm.Op().Is(ent.OpCreate) {
					if id := created(val); id != 0 {
						ids = append(ids, id)
					}
				}
				if len(ids) > 0 {
					report(ids)
				}
				return val, nil
			},
		)
	}
}

// videoFileHook reports the movie or episode of a file that appeared, moved,
// changed size, went missing, came back or was deleted. Every other update
// (a probe, a tag state) leaves what an extension reads of the file alone,
// and track and book files have no video owner.
func videoFileHook(changed func(extension.LibraryChange)) ent.Hook {
	return func(next ent.Mutator) ent.Mutator {
		return ent.MutateFunc(
			func(ctx context.Context, m ent.Mutation) (ent.Value, error) {
				mf, ok := m.(*ent.MediaFileMutation)
				if !ok {
					return next.Mutate(ctx, m)
				}
				var c extension.LibraryChange
				switch {
				case mf.Op().Is(ent.OpCreate):
					if id, ok := mf.MovieID(); ok {
						c.Movies = []uint32{id}
					}
					if id, ok := mf.EpisodeID(); ok {
						c.Episodes = []uint32{id}
					}
				case mf.Op().Is(ent.OpDelete | ent.OpDeleteOne), fileMoved(mf):
					c = videoOwners(ctx, mf)
				}
				val, err := next.Mutate(ctx, m)
				if err == nil {
					changed(c)
				}
				return val, err
			},
		)
	}
}

func fileMoved(mf *ent.MediaFileMutation) bool {
	_, path := mf.Path()
	_, size := mf.Size()
	_, missing := mf.MissingSince()
	return path || size || missing || mf.MissingSinceCleared()
}

func videoOwners(
	ctx context.Context,
	mf *ent.MediaFileMutation,
) extension.LibraryChange {
	var c extension.LibraryChange
	ids, err := mf.IDs(ctx)
	if err != nil || len(ids) == 0 {
		return c
	}
	rows, err := mf.Client().MediaFile.Query().
		Where(mediafile.IDIn(ids...)).
		WithMovie().WithEpisode().
		All(ctx)
	if err != nil {
		slog.WarnContext(ctx, "Library hook could not read the changed files",
			"error", err)
		return c
	}
	for _, row := range rows {
		if row.Edges.Movie != nil {
			c.Movies = append(c.Movies, row.Edges.Movie.ID)
		}
		if row.Edges.Episode != nil {
			c.Episodes = append(c.Episodes, row.Edges.Episode.ID)
		}
	}
	return c
}
