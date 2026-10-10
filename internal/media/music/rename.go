package music

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/db"
	"github.com/datahearth/streamline/internal/library"
	"github.com/datahearth/streamline/internal/otelx"
	"github.com/datahearth/streamline/internal/utils/numeric"
)

// Renamer computes and applies track-file renames for an artist against
// library.music_naming, from database metadata only: no tag is read or
// rewritten. A file that is a hardlink of a seeding torrent keeps its inode;
// only the library path moves. Music has no media-server refresh.
type Renamer struct {
	db db.Store
}

func NewRenamer(store db.Store) *Renamer { return &Renamer{db: store} }

var _ library.Renamer = (*Renamer)(nil)

// Preview returns the plan without applying it. Empty Operations means every
// file already matches its target.
func (r *Renamer) Preview(
	ctx context.Context,
	artistID uint32,
) (library.RenamePlan, error) {
	ctx, span := tracer.Start(ctx, "music.rename.preview",
		trace.WithAttributes(attribute.Int64("artist.id", int64(artistID))))
	defer span.End()

	plan, err := r.buildPlan(ctx, artistID)
	if err != nil {
		return library.RenamePlan{}, otelx.RecordSpanError(span, err)
	}
	span.SetAttributes(attribute.Int("rename.op_count", len(plan.Operations)))
	return plan, nil
}

// Apply moves every file of the plan and updates its row, one file at a time,
// and stops at the first collision with ErrDestExists, reporting it. Files
// already moved stay moved: the plan is recomputed on the next call.
func (r *Renamer) Apply(
	ctx context.Context,
	artistID uint32,
) (library.RenamePlan, error) {
	ctx, span := tracer.Start(ctx, "music.rename.apply",
		trace.WithAttributes(attribute.Int64("artist.id", int64(artistID))))
	defer span.End()

	plan, err := r.buildPlan(ctx, artistID)
	if err != nil {
		return library.RenamePlan{}, otelx.RecordSpanError(span, err)
	}
	root := config.Get().Library.MusicPath
	for _, op := range plan.Operations {
		if err := r.move(ctx, op, root); err != nil {
			return library.RenamePlan{}, otelx.RecordSpanError(span, err)
		}
	}
	span.SetAttributes(attribute.Int("rename.op_count", len(plan.Operations)))
	return plan, nil
}

func (r *Renamer) move(
	ctx context.Context,
	op library.RenameOperation,
	root string,
) error {
	if to, err := os.Lstat(op.To); err == nil {
		from, ferr := os.Lstat(op.From)
		if ferr != nil || !os.SameFile(from, to) {
			return fmt.Errorf("rename %s: %w", op.To, library.ErrDestExists)
		}
	}
	if err := library.MkdirLibraryDir(filepath.Dir(op.To)); err != nil {
		return fmt.Errorf("mkdir %s: %w", filepath.Dir(op.To), err)
	}
	if err := os.Rename(op.From, op.To); err != nil {
		return fmt.Errorf("rename %s -> %s: %w", op.From, op.To, err)
	}
	if err := r.db.UpdateMediaFilePath(ctx, op.MediaFileID, op.To); err != nil {
		if back := os.Rename(op.To, op.From); back != nil {
			slog.ErrorContext(ctx, "rename: restoring a moved file failed",
				"media_file.id", op.MediaFileID, "from", op.To, "to", op.From,
				"error", back)
		}
		return fmt.Errorf("update media_file %d: %w", op.MediaFileID, err)
	}
	library.PruneEmptyDirs(ctx, filepath.Dir(op.From), root)
	return nil
}

func (r *Renamer) buildPlan(
	ctx context.Context,
	artistID uint32,
) (library.RenamePlan, error) {
	a, err := r.db.FindArtistByID(ctx, artistID)
	if err != nil {
		if ent.IsNotFound(err) {
			return library.RenamePlan{}, fmt.Errorf(
				"artist %d: %w", artistID, ErrArtistNotFound)
		}
		return library.RenamePlan{}, fmt.Errorf("find artist: %w", err)
	}
	lib := config.Get().Library
	var plan library.RenamePlan
	for _, alb := range a.Edges.Albums {
		multiDisc := slices.ContainsFunc(alb.Edges.Tracks, func(t *ent.Track) bool {
			return t.Disc > 1
		})
		var year uint16
		if alb.ReleaseDate != nil {
			year = numeric.SaturateU16(alb.ReleaseDate.Year())
		}
		for _, t := range alb.Edges.Tracks {
			for _, f := range t.Edges.MediaFiles {
				target := trackTarget(lib, a, alb, t, f, year, multiDisc)
				if target == f.Path {
					continue
				}
				// The template is the admin's, and a literal ".." in it walks
				// every file out of the root: leave the file where it is.
				if !library.PathUnderRoot(target, lib.MusicPath) {
					slog.WarnContext(
						ctx,
						"rename skipped: the naming template puts this file outside the library",
						"artist.id",
						artistID,
						"path",
						f.Path,
						"target",
						target,
					)
					continue
				}
				plan.Operations = append(plan.Operations, library.RenameOperation{
					MediaFileID: f.ID, From: f.Path, To: target,
				})
			}
		}
	}
	return plan, nil
}

// trackTarget renders the path the album importer would give the file, with
// the same variables and the same per-segment sanitising.
func trackTarget(
	lib config.LibraryConfig,
	a *ent.Artist,
	alb *ent.Album,
	t *ent.Track,
	f *ent.MediaFile,
	year uint16,
	multiDisc bool,
) string {
	ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(f.Path)), ".")
	vars := library.BuildAlbumTrackVars(
		a.Name, alb.Title, year, t.Disc, multiDisc, t.Position, t.Title, ext,
	)
	segments := strings.Split(library.ApplyTemplate(lib.MusicNaming, vars), "/")
	for i, seg := range segments {
		segments[i] = library.SanitizePath(seg)
	}
	return filepath.Join(append([]string{lib.MusicPath}, segments...)...)
}
