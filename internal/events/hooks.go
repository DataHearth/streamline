package events

import (
	"context"
	"fmt"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/downloadrecord"
	"github.com/datahearth/streamline/ent/episode"
	"github.com/datahearth/streamline/ent/importscanfile"
	"github.com/datahearth/streamline/ent/importscanshow"
	"github.com/datahearth/streamline/ent/movie"
)

// owner is the resolved (scope, id) pair an event hangs off. A zero id means
// "nothing to attribute this to" — the hook returns without recording rather
// than writing an ownerless row.
type owner struct {
	scope Scope
	id    uint32
}

func (o owner) ok() bool { return o.id != 0 }

// Register installs runtime mutation hooks on the supplied client and
// captures it as the package default for tx-less Record calls.
func Register(client *ent.Client) {
	defaultClient = client

	client.DownloadRecord.Use(downloadRecordHook())
	client.MediaFile.Use(mediaFileHook())
	client.ImportScanFile.Use(importScanFileHook())
	client.ImportScanShow.Use(importScanShowHook())
}

func downloadRecordHook() ent.Hook {
	return func(next ent.Mutator) ent.Mutator {
		return ent.MutateFunc(
			func(ctx context.Context, m ent.Mutation) (ent.Value, error) {
				dm, ok := m.(*ent.DownloadRecordMutation)
				if !ok {
					return next.Mutate(ctx, m)
				}
				val, err := next.Mutate(ctx, m)
				if err != nil {
					return val, err
				}
				c := dm.Client()
				switch dm.Op() {
				case ent.OpCreate:
					o, err := downloadRecordOwner(ctx, c, dm)
					if err != nil {
						return val, fmt.Errorf(
							"events.downloadRecordHook: resolve owner: %w", err,
						)
					}
					if !o.ok() {
						return val, nil
					}
					if err := Record(
						ctx,
						c,
						TypeGrabbed,
						o.scope,
						o.id,
						downloadCreatePayload(dm),
					); err != nil {
						return val, err
					}
				default:
					if !dm.Op().Is(ent.OpUpdate | ent.OpUpdateOne) {
						return val, nil
					}
					status, changed := dm.Status()
					if !changed {
						return val, nil
					}
					o, err := downloadRecordOwner(ctx, c, dm)
					if err != nil {
						return val, fmt.Errorf(
							"events.downloadRecordHook: resolve owner: %w", err,
						)
					}
					if !o.ok() {
						return val, nil
					}
					var t Type
					switch status {
					case downloadrecord.StatusCompleted:
						t = TypeDownloadCompleted
					case downloadrecord.StatusFailed:
						t = TypeDownloadFailed
					default:
						return val, nil
					}
					if err := Record(
						ctx, c, t, o.scope, o.id, downloadStatusPayload(dm),
					); err != nil {
						return val, err
					}
				}
				return val, nil
			},
		)
	}
}

func mediaFileHook() ent.Hook {
	return func(next ent.Mutator) ent.Mutator {
		return ent.MutateFunc(
			func(ctx context.Context, m ent.Mutation) (ent.Value, error) {
				mf, ok := m.(*ent.MediaFileMutation)
				if !ok {
					return next.Mutate(ctx, m)
				}
				val, err := next.Mutate(ctx, m)
				if err != nil {
					return val, err
				}
				if !mf.Op().Is(ent.OpCreate) {
					return val, nil
				}
				var o owner
				switch {
				case hasID(mf.MovieID):
					o = owner{ScopeMovie, mustID(mf.MovieID)}
				case hasID(mf.EpisodeID):
					o = owner{ScopeEpisode, mustID(mf.EpisodeID)}
				default:
					return val, nil
				}
				if err := Record(
					ctx,
					mf.Client(),
					TypeImported,
					o.scope,
					o.id,
					mediaFileCreatePayload(mf),
				); err != nil {
					return val, err
				}
				return val, nil
			},
		)
	}
}

func importScanFileHook() ent.Hook {
	return func(next ent.Mutator) ent.Mutator {
		return ent.MutateFunc(
			func(ctx context.Context, m ent.Mutation) (ent.Value, error) {
				isf, ok := m.(*ent.ImportScanFileMutation)
				if !ok {
					return next.Mutate(ctx, m)
				}
				val, err := next.Mutate(ctx, m)
				if err != nil {
					return val, err
				}
				if !isf.Op().Is(ent.OpUpdate | ent.OpUpdateOne) {
					return val, nil
				}
				outcome, changed := isf.Outcome()
				if !changed || outcome != importscanfile.OutcomeFailed {
					return val, nil
				}
				c := isf.Client()
				movieID, err := importScanFileMovieID(ctx, c, isf)
				if err != nil {
					return val, fmt.Errorf(
						"events.importScanFileHook: load movie id: %w",
						err,
					)
				}
				if movieID == 0 {
					return val, nil
				}
				if err := Record(
					ctx,
					c,
					TypeImportFailed,
					ScopeMovie,
					movieID,
					importScanFilePayload(isf),
				); err != nil {
					return val, err
				}
				return val, nil
			},
		)
	}
}

// importScanShowHook is the series twin of importScanFileHook. A failed show
// entry is recorded against the series it was meant to land in; a failure with
// no series resolved yet (an unmatched folder) has nowhere to hang and is
// left to the scan's own outcome_message.
func importScanShowHook() ent.Hook {
	return func(next ent.Mutator) ent.Mutator {
		return ent.MutateFunc(
			func(ctx context.Context, m ent.Mutation) (ent.Value, error) {
				iss, ok := m.(*ent.ImportScanShowMutation)
				if !ok {
					return next.Mutate(ctx, m)
				}
				val, err := next.Mutate(ctx, m)
				if err != nil {
					return val, err
				}
				if !iss.Op().Is(ent.OpUpdate | ent.OpUpdateOne) {
					return val, nil
				}
				outcome, changed := iss.Outcome()
				if !changed || outcome != importscanshow.OutcomeFailed {
					return val, nil
				}
				c := iss.Client()
				showID, err := importScanShowTVShowID(ctx, c, iss)
				if err != nil {
					return val, fmt.Errorf(
						"events.importScanShowHook: load tv show id: %w",
						err,
					)
				}
				if showID == 0 {
					return val, nil
				}
				if err := Record(
					ctx,
					c,
					TypeImportFailed,
					ScopeSeries,
					showID,
					importScanShowPayload(iss),
				); err != nil {
					return val, err
				}
				return val, nil
			},
		)
	}
}

func downloadCreatePayload(m *ent.DownloadRecordMutation) map[string]any {
	p := map[string]any{}
	if v, ok := m.Title(); ok {
		p["release_title"] = v
	}
	if v, ok := m.Quality(); ok && v != "" {
		p["quality"] = v
	}
	if v, ok := m.Size(); ok {
		p["size_bytes"] = v
	}
	return p
}

func downloadStatusPayload(m *ent.DownloadRecordMutation) map[string]any {
	p := map[string]any{}
	if v, ok := m.FailureReason(); ok && v != "" {
		p["reason"] = v
	}
	return p
}

func mediaFileCreatePayload(m *ent.MediaFileMutation) map[string]any {
	p := map[string]any{}
	if v, ok := m.Path(); ok {
		p["path"] = v
	}
	if v, ok := m.Quality(); ok && v != "" {
		p["quality"] = v
	}
	if v, ok := m.Size(); ok {
		p["size_bytes"] = v
	}
	if v, ok := m.Source(); ok {
		p["source"] = string(v)
	}
	return p
}

func importScanFilePayload(m *ent.ImportScanFileMutation) map[string]any {
	p := map[string]any{}
	if v, ok := m.SourcePath(); ok {
		p["path"] = v
	}
	if v, ok := m.OutcomeMessage(); ok && v != "" {
		p["error"] = v
	}
	return p
}

func importScanShowPayload(m *ent.ImportScanShowMutation) map[string]any {
	p := map[string]any{}
	if v, ok := m.FolderPath(); ok {
		p["path"] = v
	}
	if v, ok := m.OutcomeMessage(); ok && v != "" {
		p["error"] = v
	}
	return p
}

func hasID(f func() (uint32, bool)) bool {
	id, ok := f()
	return ok && id != 0
}

func mustID(f func() (uint32, bool)) uint32 {
	id, _ := f()
	return id
}

// downloadRecordOwner resolves the movie or episode a record belongs to. On a
// create the mutation carries the edge; on an update it usually does not, so
// the row is queried back — movie first, then episode.
func downloadRecordOwner(
	ctx context.Context,
	c *ent.Client,
	m *ent.DownloadRecordMutation,
) (owner, error) {
	if hasID(m.MovieID) {
		return owner{ScopeMovie, mustID(m.MovieID)}, nil
	}
	if hasID(m.EpisodeID) {
		return owner{ScopeEpisode, mustID(m.EpisodeID)}, nil
	}
	ids, err := m.IDs(ctx)
	if err != nil {
		return owner{}, err
	}
	if len(ids) == 0 {
		return owner{}, nil
	}
	row, err := c.DownloadRecord.Query().
		Where(downloadrecord.IDEQ(ids[0])).
		// Only the owner's id is read below, and this runs inside the
		// caller's transaction on every download-record mutation.
		WithMovie(func(q *ent.MovieQuery) { q.Select(movie.FieldID) }).
		WithEpisode(func(q *ent.EpisodeQuery) { q.Select(episode.FieldID) }).
		Only(ctx)
	if ent.IsNotFound(err) {
		return owner{}, nil
	}
	if err != nil {
		return owner{}, err
	}
	switch {
	case row.Edges.Movie != nil:
		return owner{ScopeMovie, row.Edges.Movie.ID}, nil
	case row.Edges.Episode != nil:
		return owner{ScopeEpisode, row.Edges.Episode.ID}, nil
	}
	return owner{}, nil
}

func importScanFileMovieID(
	ctx context.Context,
	c *ent.Client,
	m *ent.ImportScanFileMutation,
) (uint32, error) {
	if id, ok := m.CreatedMovieID(); ok && id != 0 {
		return id, nil
	}
	if id, ok := m.ExistingMovieID(); ok && id != 0 {
		return id, nil
	}
	ids, err := m.IDs(ctx)
	if err != nil {
		return 0, err
	}
	if len(ids) == 0 {
		return 0, nil
	}
	row, err := c.ImportScanFile.Query().
		Where(importscanfile.IDIn(ids...)).
		Select(importscanfile.FieldCreatedMovieID, importscanfile.FieldExistingMovieID).
		First(ctx)
	if err != nil {
		return 0, err
	}
	if row.CreatedMovieID != 0 {
		return row.CreatedMovieID, nil
	}
	return row.ExistingMovieID, nil
}

func importScanShowTVShowID(
	ctx context.Context,
	c *ent.Client,
	m *ent.ImportScanShowMutation,
) (uint32, error) {
	if id, ok := m.CreatedTvshowID(); ok && id != 0 {
		return id, nil
	}
	if id, ok := m.ExistingTvshowID(); ok && id != 0 {
		return id, nil
	}
	ids, err := m.IDs(ctx)
	if err != nil {
		return 0, err
	}
	if len(ids) == 0 {
		return 0, nil
	}
	row, err := c.ImportScanShow.Query().
		Where(importscanshow.IDIn(ids...)).
		Select(
			importscanshow.FieldCreatedTvshowID,
			importscanshow.FieldExistingTvshowID,
		).
		First(ctx)
	if err != nil {
		return 0, err
	}
	if row.CreatedTvshowID != nil && *row.CreatedTvshowID != 0 {
		return *row.CreatedTvshowID, nil
	}
	if row.ExistingTvshowID != nil {
		return *row.ExistingTvshowID, nil
	}
	return 0, nil
}
