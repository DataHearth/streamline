package tvshow

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/internal/db"
	"github.com/datahearth/streamline/internal/library"
	"github.com/datahearth/streamline/internal/otelx"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// RenameService computes and applies episode-file renames for a series using
// the library naming pattern. libraryRoot is config.Library.SeriesPath; naming
// is config.Library.SeriesNaming (passed in at construction so tests don't
// depend on the singleton).
type RenameService struct {
	db          db.Store
	libraryRoot string
	naming      string
}

func NewRenameService(store db.Store, libraryRoot, naming string) *RenameService {
	return &RenameService{db: store, libraryRoot: libraryRoot, naming: naming}
}

// Preview returns the rename plan without applying it. Empty Operations means
// every file already matches its target.
func (r *RenameService) Preview(
	ctx context.Context, seriesID uint32,
) (library.RenamePlan, error) {
	ctx, span := tracer.Start(ctx, "tvshow.rename.preview",
		trace.WithAttributes(attribute.Int64("series.id", int64(seriesID))))
	defer span.End()

	plan, err := r.buildPlan(ctx, seriesID)
	if err != nil {
		return library.RenamePlan{}, otelx.RecordSpanError(span, err)
	}
	span.SetAttributes(attribute.Int("rename.op_count", len(plan.Operations)))
	return plan, nil
}

// Apply runs the rename plan: moves files on disk and updates the DB rows.
// Per-operation errors halt the loop with a partial-state error message; the
// caller is expected to surface the message to the user and let them retry.
func (r *RenameService) Apply(
	ctx context.Context, seriesID uint32,
) (library.RenamePlan, error) {
	ctx, span := tracer.Start(ctx, "tvshow.rename.apply",
		trace.WithAttributes(attribute.Int64("series.id", int64(seriesID))))
	defer span.End()

	plan, err := r.buildPlan(ctx, seriesID)
	if err != nil {
		return library.RenamePlan{}, otelx.RecordSpanError(span, err)
	}
	for _, op := range plan.Operations {
		if err := os.MkdirAll(filepath.Dir(op.To), 0o755); err != nil {
			return library.RenamePlan{}, otelx.RecordSpanError(span,
				fmt.Errorf("mkdir %s: %w", filepath.Dir(op.To), err))
		}
		if err := os.Rename(op.From, op.To); err != nil {
			return library.RenamePlan{}, otelx.RecordSpanError(span,
				fmt.Errorf("rename %s → %s: %w", op.From, op.To, err))
		}
		if err := r.db.UpdateMediaFilePath(ctx, op.MediaFileID, op.To); err != nil {
			return library.RenamePlan{}, otelx.RecordSpanError(span,
				fmt.Errorf("update media_file %d: %w", op.MediaFileID, err))
		}
	}
	span.SetAttributes(attribute.Int("rename.op_count", len(plan.Operations)))
	return plan, nil
}

func (r *RenameService) buildPlan(
	ctx context.Context, seriesID uint32,
) (library.RenamePlan, error) {
	show, err := r.db.FindTVShowByID(ctx, seriesID)
	if err != nil {
		if ent.IsNotFound(err) {
			return library.RenamePlan{}, fmt.Errorf("series %d not found", seriesID)
		}
		return library.RenamePlan{}, fmt.Errorf("find series: %w", err)
	}
	var plan library.RenamePlan
	for _, se := range show.Edges.Seasons {
		for _, ep := range se.Edges.Episodes {
			for _, f := range ep.Edges.MediaFiles {
				target := r.target(show, se.Number, ep, f.Path)
				if target == f.Path {
					continue
				}
				plan.Operations = append(plan.Operations, library.RenameOperation{
					MediaFileID: f.ID,
					From:        f.Path,
					To:          target,
				})
			}
		}
	}
	return plan, nil
}

// target computes the canonical path using the same primitives the importer
// relies on (library.BuildEpisodeVars + ApplyTemplate + SanitizePath) so that
// renames land at the importer's destination. Sanitisation is per-segment to
// preserve directory separators.
func (r *RenameService) target(
	show *ent.TVShow, season uint16, ep *ent.Episode, currentPath string,
) string {
	parsed := library.Parse(filepath.Base(currentPath))
	if parsed.Extension == "" {
		parsed.Extension = strings.TrimPrefix(filepath.Ext(currentPath), ".")
	}
	vars := library.BuildEpisodeVars(
		show.Title, show.Year, season, ep.Number, ep.Title, parsed,
	)
	rel := library.ApplyTemplate(r.naming, vars)
	segments := strings.Split(rel, "/")
	for i, seg := range segments {
		segments[i] = library.SanitizePath(seg)
	}
	return filepath.Join(append([]string{r.libraryRoot}, segments...)...)
}
