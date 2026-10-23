package book

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/library"
	"github.com/datahearth/streamline/internal/otelx"
)

func slotFiles(b *ent.Book, kind string) []*ent.MediaFile {
	var out []*ent.MediaFile
	for _, f := range b.Edges.MediaFiles {
		if string(f.BookKind) == kind {
			out = append(out, f)
		}
	}
	return out
}

// commonDir is the deepest directory that holds every file.
func commonDir(files []*ent.MediaFile) string {
	if len(files) == 0 {
		return ""
	}
	dir := filepath.Dir(files[0].Path)
	for _, f := range files[1:] {
		for !library.PathUnderRoot(f.Path, dir) {
			parent := filepath.Dir(dir)
			if parent == dir {
				return dir
			}
			dir = parent
		}
	}
	return dir
}

// planBook lists the renames that bring one book's files in line with the
// naming templates. An ebook file moves to the template path with its own
// extension. An audiobook folder moves whole to the template path, each file
// keeping its name and its place inside the folder.
func planBook(
	ctx context.Context,
	b *ent.Book,
	lib config.LibraryConfig,
) []library.RenameOperation {
	var ops []library.RenameOperation
	for _, kind := range slotKinds {
		files := slotFiles(b, kind)
		if len(files) == 0 {
			continue
		}
		root := library.BookRoot(lib, kind)
		dest, err := library.BookDestination(
			root, library.BookTemplate(lib, b, kind), library.NamingForBook(b, kind),
		)
		if err != nil {
			slog.WarnContext(
				ctx,
				"rename skipped: the naming template puts this book outside the library",
				"book.id",
				b.ID,
				"book.kind",
				kind,
				"error",
				err,
			)
			continue
		}
		var targets []string
		if kind == slotAudiobook {
			from := commonDir(files)
			for _, f := range files {
				rel, err := filepath.Rel(from, f.Path)
				if err != nil {
					targets = append(targets, f.Path)
					continue
				}
				targets = append(targets, filepath.Join(dest, rel))
			}
		} else {
			for _, f := range files {
				targets = append(targets, dest+strings.ToLower(filepath.Ext(f.Path)))
			}
		}
		taken := map[string]bool{}
		for i, f := range files {
			to := targets[i]
			if to == f.Path || taken[to] || !library.PathUnderRoot(to, root) {
				continue
			}
			if _, err := os.Stat(to); err == nil {
				slog.WarnContext(ctx, "rename skipped: the target already exists",
					"book.id", b.ID, "path", f.Path, "target", to)
				continue
			}
			taken[to] = true
			ops = append(ops, library.RenameOperation{
				MediaFileID: f.ID, From: f.Path, To: to,
			})
		}
	}
	return ops
}

// applyRenames moves the files and updates their rows. The first failure halts
// the loop with a partial-state error the caller shows to retry.
func (s *Service) applyRenames(
	ctx context.Context,
	ops []library.RenameOperation,
) error {
	lib := config.Get().Library
	if len(ops) > 0 {
		defer s.refreshLibraries(ctx)
	}
	for _, op := range ops {
		if err := library.MkdirLibraryDir(filepath.Dir(op.To)); err != nil {
			return fmt.Errorf("mkdir %s: %w", filepath.Dir(op.To), err)
		}
		if err := os.Rename(op.From, op.To); err != nil {
			return fmt.Errorf("rename %s -> %s: %w", op.From, op.To, err)
		}
		for _, root := range []string{lib.EbookPath, lib.AudiobookPath} {
			if library.PathUnderRoot(op.From, root) {
				library.PruneEmptyDirs(ctx, filepath.Dir(op.From), root)
			}
		}
		if err := s.db.UpdateMediaFilePath(ctx, op.MediaFileID, op.To); err != nil {
			return fmt.Errorf("update media_file %d: %w", op.MediaFileID, err)
		}
	}
	return nil
}

// RenameBook returns the rename plan of a book and, unless preview is set,
// applies it. Empty operations means every file is already where it belongs.
// Books have no media-server refresh to trigger.
func (s *Service) RenameBook(
	ctx context.Context,
	id uint32,
	preview bool,
) (library.RenamePlan, error) {
	ctx, span := tracer.Start(ctx, "book.rename",
		trace.WithAttributes(
			attribute.Int("book.id", int(id)),
			attribute.Bool("preview", preview),
		))
	defer span.End()

	b, err := s.GetBook(ctx, id)
	if err != nil {
		return library.RenamePlan{}, otelx.RecordSpanError(span, err)
	}
	plan := library.RenamePlan{Operations: planBook(ctx, b, config.Get().Library)}
	span.SetAttributes(attribute.Int("rename.op_count", len(plan.Operations)))
	if preview {
		return plan, nil
	}
	if err := s.applyRenames(ctx, plan.Operations); err != nil {
		return library.RenamePlan{}, otelx.RecordSpanError(span, err)
	}
	return plan, nil
}

// RenameSeries is RenameBook over every volume of a series.
func (s *Service) RenameSeries(
	ctx context.Context,
	id uint32,
	preview bool,
) (library.RenamePlan, error) {
	ctx, span := tracer.Start(ctx, "book.rename_series",
		trace.WithAttributes(
			attribute.Int("series.id", int(id)),
			attribute.Bool("preview", preview),
		))
	defer span.End()

	series, err := s.GetSeries(ctx, id)
	if err != nil {
		return library.RenamePlan{}, otelx.RecordSpanError(span, err)
	}
	lib := config.Get().Library
	plan := library.RenamePlan{}
	for _, v := range series.Edges.Volumes {
		v.Edges.Series = series
		plan.Operations = append(plan.Operations, planBook(ctx, v, lib)...)
	}
	span.SetAttributes(attribute.Int("rename.op_count", len(plan.Operations)))
	if preview {
		return plan, nil
	}
	if err := s.applyRenames(ctx, plan.Operations); err != nil {
		return library.RenamePlan{}, otelx.RecordSpanError(span, err)
	}
	return plan, nil
}
