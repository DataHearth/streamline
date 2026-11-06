package importer

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"slices"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/schema"
	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/library"
	"github.com/datahearth/streamline/internal/mediaserver"
	"github.com/datahearth/streamline/internal/otelx"
)

// importPackRecord imports a completed discography pack: its audio files are
// grouped by folder, each folder is matched to one of the albums the record
// was linked to by title (and year), and each match goes through the ordinary
// album import. The record completes when every match is done; the linked
// albums no folder named go back to wanted, with no failure counted against
// them. A folder naming another album of the artist is ignored, since nobody
// wanted that one; a folder naming none, or a second folder naming one album, holds
// the record so a person can say. Their folder_assignments then replace the
// matching.
func (w *Worker) importPackRecord(
	ctx context.Context,
	span trace.Span,
	rec *ent.DownloadRecord,
	libCfg config.LibraryConfig,
) error {
	artist := rec.Edges.Artist
	linked := rec.Edges.Albums
	span.SetAttributes(
		attribute.Int64("artist.id", int64(artist.ID)),
		attribute.Int("pack.albums", len(linked)),
	)
	files, err := listAudioFiles(rec.SavePath)
	if err != nil {
		return otelx.RecordSpanError(span, fmt.Errorf("list pack files: %w", err))
	}
	byDir, dirs := packFolders(w.sourceFilter(ctx, rec).Filter(files))
	if !rec.VerificationBypassed {
		reasons, err := w.folderHoldReasons(ctx, artist.ID, dirs, linked)
		if err != nil {
			return otelx.RecordSpanError(span, err)
		}
		if len(reasons) > 0 {
			return w.hold(ctx, span, rec, reasons)
		}
	}

	matched := map[uint32]struct{}{}
	var (
		errs    []error
		changed []string
	)
	for _, d := range dirs {
		alb, err := w.packAlbum(ctx, rec, d)
		if err != nil {
			return otelx.RecordSpanError(span, err)
		}
		if alb == nil {
			slog.InfoContext(ctx, "pack import: folder matched no linked album",
				"dir", d, "record.id", rec.ID)
			continue
		}
		if _, dup := matched[alb.ID]; dup {
			continue
		}
		matched[alb.ID] = struct{}{}
		placed, held, err := w.importAlbumFiles(
			ctx, span, rec, alb, byDir[d], libCfg, true,
		)
		changed = append(changed, placed...)
		switch {
		case held:
			return nil
		case errors.Is(err, library.ErrDestExists):
			// Every track of the folder already holds a file: a re-run after
			// a partial failure, or a pack that adds nothing.
		case err != nil:
			slog.WarnContext(ctx, "pack import: album import failed",
				"album.id", alb.ID, "dir", d, "error", err)
			errs = append(errs, fmt.Errorf("album %d: %w", alb.ID, err))
		}
	}
	if len(matched) == 0 {
		return otelx.RecordSpanError(span, ErrNoAlbumTracks)
	}
	if len(errs) > 0 {
		return otelx.RecordSpanError(span, errors.Join(errs...))
	}

	var unmatched []uint32
	for _, a := range linked {
		if _, ok := matched[a.ID]; !ok {
			unmatched = append(unmatched, a.ID)
		}
	}
	if err := w.db.CompletePackRecord(ctx, rec.ID, unmatched); err != nil {
		return otelx.RecordSpanError(
			span,
			fmt.Errorf("complete pack record: %w", err),
		)
	}
	slog.InfoContext(ctx, "imported discography pack",
		"artist.id", artist.ID, "albums", len(matched), "unmatched", len(unmatched))

	libCfg.ImportMode = albumImportMode(libCfg)
	w.cleanupTorrent(ctx, rec, libCfg)
	w.refreshMediaServers(ctx, mediaserver.KindMusic, libCfg.MusicPath, changed...)
	return nil
}

func packFolders(files []string) (map[string][]string, []string) {
	byDir := map[string][]string{}
	for _, f := range files {
		d := library.PackAlbumDir(f)
		byDir[d] = append(byDir[d], f)
	}
	dirs := make([]string, 0, len(byDir))
	for d := range byDir {
		dirs = append(dirs, d)
	}
	slices.Sort(dirs)
	return byDir, dirs
}

// packAlbum is the album a folder fills: the one a person chose when they did,
// nil for a folder they left out, else the linked album it names.
func (w *Worker) packAlbum(
	ctx context.Context,
	rec *ent.DownloadRecord,
	dir string,
) (*ent.Album, error) {
	if rec.FolderAssignments == nil {
		return library.MatchPackDir(dir, rec.Edges.Albums), nil
	}
	id, ok := rec.FolderAssignments[dir]
	if !ok {
		return nil, nil
	}
	if i := slices.IndexFunc(rec.Edges.Albums, func(a *ent.Album) bool {
		return a.ID == id
	}); i >= 0 {
		return rec.Edges.Albums[i], nil
	}
	alb, err := w.db.FindAlbumByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("load chosen album %d: %w", id, err)
	}
	return alb, nil
}

// folderHoldReasons lists the folders that name no album of the artist and the
// ones naming an album an earlier folder already took.
func (w *Worker) folderHoldReasons(
	ctx context.Context,
	artistID uint32,
	dirs []string,
	linked []*ent.Album,
) ([]schema.HoldReason, error) {
	var (
		out    []schema.HoldReason
		albums []*ent.Album
		taken  = map[uint32]struct{}{}
	)
	for _, d := range dirs {
		if alb := library.MatchPackDir(d, linked); alb != nil {
			if _, dup := taken[alb.ID]; dup {
				out = append(out, schema.HoldReason{
					File:     d,
					Check:    "folder",
					Expected: alb.Title,
					Actual:   filepath.Base(d),
				})
			}
			taken[alb.ID] = struct{}{}
			continue
		}
		if albums == nil {
			var err error
			if albums, err = w.db.ListArtistAlbumTitles(ctx, artistID); err != nil {
				return nil, fmt.Errorf("list artist albums: %w", err)
			}
		}
		if library.MatchPackDir(d, albums) == nil {
			out = append(out, schema.HoldReason{
				File: d, Check: "folder", Actual: filepath.Base(d),
			})
		}
	}
	return out, nil
}

// PlannedFolder is one folder of a held pack and the linked album it names;
// Album is nil when it names none of them.
type PlannedFolder struct {
	Path  string
	Album *ent.Album
}

// FolderPlan is what someone choosing a held pack's albums picks from.
type FolderPlan struct {
	Folders []PlannedFolder
	Albums  []*ent.Album
}

// PackFolderPlan lists the folders of a held discography pack and every album
// of its artist. ErrNotPack means no plan applies to the record.
func (w *Worker) PackFolderPlan(
	ctx context.Context,
	recordID uint32,
) (FolderPlan, error) {
	rec, err := w.db.FindHeldAlbumRecord(ctx, recordID)
	if err != nil {
		return FolderPlan{}, fmt.Errorf("load held record: %w", err)
	}
	if rec.Edges.Artist == nil {
		return FolderPlan{}, ErrNotPack
	}
	files, err := listAudioFiles(rec.SavePath)
	if err != nil {
		return FolderPlan{}, fmt.Errorf("list pack files: %w", err)
	}
	albums, err := w.db.ListArtistAlbumTitles(ctx, rec.Edges.Artist.ID)
	if err != nil {
		return FolderPlan{}, fmt.Errorf("list artist albums: %w", err)
	}
	_, dirs := packFolders(w.sourceFilter(ctx, rec).Filter(files))
	plan := FolderPlan{Albums: albums}
	for _, d := range dirs {
		plan.Folders = append(plan.Folders, PlannedFolder{
			Path: d, Album: library.MatchPackDir(d, rec.Edges.Albums),
		})
	}
	return plan, nil
}
