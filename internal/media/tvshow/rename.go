package tvshow

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/db"
	"github.com/datahearth/streamline/internal/events"
	"github.com/datahearth/streamline/internal/library"
	"github.com/datahearth/streamline/internal/mediaserver"
	"github.com/datahearth/streamline/internal/otelx"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// RenameService computes and applies episode-file renames for a series using
// the library naming pattern. The root (config.Library.SeriesPath) and the
// template (config.Library.SeriesNaming) are read from the live config on every
// call: both change at runtime (a path migration re-points the root, Settings →
// Library edits the template), and a rename must land where the library is now.
type RenameService struct {
	db db.Store
	ms mediaserver.Refresher
}

func NewRenameService(store db.Store, ms mediaserver.Refresher) *RenameService {
	return &RenameService{db: store, ms: ms}
}

// renameRules is the library root and naming template one Preview or Apply
// works with, read once so a config change mid-call cannot split a plan
// between two roots.
type renameRules struct {
	root   string
	naming string
}

func currentRenameRules() renameRules {
	lib := config.Get().Library
	return renameRules{root: lib.SeriesPath, naming: lib.SeriesNaming}
}

// Preview returns the rename plan without applying it. Empty Operations means
// every file already matches its target.
func (r *RenameService) Preview(
	ctx context.Context, seriesID uint32,
) (library.RenamePlan, error) {
	ctx, span := tracer.Start(ctx, "tvshow.rename.preview",
		trace.WithAttributes(attribute.Int64("series.id", int64(seriesID))))
	defer span.End()

	plan, err := r.buildPlan(ctx, seriesID, currentRenameRules())
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

	rules := currentRenameRules()
	plan, err := r.buildPlan(ctx, seriesID, rules)
	if err != nil {
		return library.RenamePlan{}, otelx.RecordSpanError(span, err)
	}
	// See the movie twin: deferred so a half-applied plan still rescans.
	if len(plan.Operations) > 0 {
		defer mediaserver.RefreshInBackground(
			ctx, r.ms, "series", rules.root, plan.Paths()...,
		)
	}
	for _, op := range plan.Operations {
		if err := library.MkdirLibraryDir(filepath.Dir(op.To)); err != nil {
			return library.RenamePlan{}, otelx.RecordSpanError(span,
				fmt.Errorf("mkdir %s: %w", filepath.Dir(op.To), err))
		}
		if err := os.Rename(op.From, op.To); err != nil {
			return library.RenamePlan{}, otelx.RecordSpanError(span,
				fmt.Errorf("rename %s → %s: %w", op.From, op.To, err))
		}
		library.MoveSidecars(ctx, op.From, op.To)
		// The move can empty the directory the file came from — a path that split
		// a title into a directory at each colon leaves one behind per title on a
		// re-rename pass. Prune before the DB write: the file is already at
		// op.To, so a failure here must not abort the rename.
		library.PruneEmptyDirs(ctx, filepath.Dir(op.From), rules.root)
		if err := r.db.UpdateMediaFilePath(ctx, op.MediaFileID, op.To); err != nil {
			return library.RenamePlan{}, otelx.RecordSpanError(span,
				fmt.Errorf("update media_file %d: %w", op.MediaFileID, err))
		}
	}
	if len(plan.Operations) > 0 {
		if err := events.Record(
			ctx, nil, events.TypeFileRenamed, events.ScopeSeries, seriesID,
			map[string]any{
				"seasons":  renamedSeasons(plan.Operations),
				"episodes": len(plan.Operations),
			},
		); err != nil {
			slog.WarnContext(ctx, "record rename event failed",
				"tvshow.id", seriesID, "error", err)
		}
	}
	span.SetAttributes(attribute.Int("rename.op_count", len(plan.Operations)))
	return plan, nil
}

// renamedSeasons returns the sorted, de-duplicated season numbers touched by
// a rename plan, for the series-scoped event payload.
func renamedSeasons(ops []library.RenameOperation) []uint16 {
	seen := make(map[uint16]struct{}, len(ops))
	var seasons []uint16
	for _, op := range ops {
		if _, ok := seen[op.Season]; ok {
			continue
		}
		seen[op.Season] = struct{}{}
		seasons = append(seasons, op.Season)
	}
	slices.Sort(seasons)
	return seasons
}

func (r *RenameService) buildPlan(
	ctx context.Context, seriesID uint32, rules renameRules,
) (library.RenamePlan, error) {
	show, err := r.db.FindTVShowByID(ctx, seriesID)
	if err != nil {
		if ent.IsNotFound(err) {
			return library.RenamePlan{}, fmt.Errorf(
				"series %d: %w", seriesID, ErrSeriesNotFound,
			)
		}
		return library.RenamePlan{}, fmt.Errorf("find series: %w", err)
	}
	var plan library.RenamePlan
	for _, se := range show.Edges.Seasons {
		for _, ep := range se.Edges.Episodes {
			for _, f := range ep.Edges.MediaFiles {
				if f.MissingSince != nil {
					continue
				}
				target := renameTarget(rules, show, se.Number, ep, f)
				if target == f.Path {
					continue
				}
				// See the movie twin: the template is the admin's, and a
				// literal ".." in it would walk the file out of the root.
				if !library.PathUnderRoot(target, rules.root) {
					slog.WarnContext(
						ctx,
						"rename skipped: the naming template puts this file outside the library",
						"episode.id",
						ep.ID,
						"path",
						f.Path,
						"target",
						target,
					)
					continue
				}
				plan.Operations = append(plan.Operations, library.RenameOperation{
					MediaFileID: f.ID,
					From:        f.Path,
					To:          target,
					EpisodeID:   ep.ID,
					Season:      se.Number,
				})
			}
		}
	}
	return plan, nil
}

// renameTarget computes the canonical path using the same primitives the importer
// relies on (library.BuildEpisodeVars + ApplyTemplate + SanitizePath) so that
// renames land at the importer's destination. Sanitisation is per-segment to
// preserve directory separators.
//
// The release facts come from the row rather than from the current basename:
// re-parsing a name this service wrote loses every token the template omits,
// which for the default template is all of them but the quality — and then
// loses the quality too on the pass after that.
func renameTarget(
	rules renameRules,
	show *ent.TVShow,
	season uint16,
	ep *ent.Episode,
	f *ent.MediaFile,
) string {
	parsed := library.ParsedFromMediaFile(f)
	vars := library.BuildEpisodeVars(
		show.Title, show.Year, show.TvdbID, season, ep.Number, ep.Title, parsed,
	)
	rel := library.ApplyTemplate(rules.naming, vars)
	segments := strings.Split(rel, "/")
	for i, seg := range segments {
		segments[i] = library.SanitizePath(seg)
	}
	return filepath.Join(append([]string{rules.root}, segments...)...)
}
