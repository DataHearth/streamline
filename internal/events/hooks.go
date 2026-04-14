package events

import (
	"context"
	"fmt"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/downloadrecord"
	"github.com/datahearth/streamline/ent/importscanfile"
)

// Register installs runtime mutation hooks on the supplied client and
// captures it as the package default for tx-less Record calls.
func Register(client *ent.Client) {
	defaultClient = client

	client.DownloadRecord.Use(downloadRecordHook())
	client.MediaFile.Use(mediaFileHook())
	client.ImportScanFile.Use(importScanFileHook())
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
					movieID, ok := dm.MovieID()
					if !ok {
						return val, nil
					}
					if err := Record(
						ctx,
						c,
						TypeGrabbed,
						movieID,
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
					movieID, err := downloadRecordMovieID(ctx, c, dm)
					if err != nil {
						return val, fmt.Errorf(
							"events.downloadRecordHook: load movie id: %w",
							err,
						)
					}
					if movieID == 0 {
						return val, nil
					}
					switch status {
					case downloadrecord.StatusCompleted:
						if err := Record(
							ctx,
							c,
							TypeDownloadCompleted,
							movieID,
							downloadStatusPayload(dm),
						); err != nil {
							return val, err
						}
					case downloadrecord.StatusFailed:
						if err := Record(
							ctx,
							c,
							TypeDownloadFailed,
							movieID,
							downloadStatusPayload(dm),
						); err != nil {
							return val, err
						}
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
				movieID, ok := mf.MovieID()
				if !ok {
					return val, nil
				}
				c := mf.Client()
				if err := Record(
					ctx,
					c,
					TypeImported,
					movieID,
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

func downloadRecordMovieID(
	ctx context.Context,
	c *ent.Client,
	m *ent.DownloadRecordMutation,
) (uint32, error) {
	if id, ok := m.MovieID(); ok {
		return id, nil
	}
	ids, err := m.IDs(ctx)
	if err != nil {
		return 0, err
	}
	if len(ids) == 0 {
		return 0, nil
	}
	mid, err := c.DownloadRecord.Query().
		Where(downloadrecord.IDEQ(ids[0])).
		QueryMovie().
		OnlyID(ctx)
	if ent.IsNotFound(err) {
		// Episode-linked record (no movie edge): no movie event to record.
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	return mid, nil
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
