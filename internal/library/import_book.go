package library

import (
	"context"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/library/ebookmeta"
	"github.com/datahearth/streamline/internal/otelx"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

type bookCandidate struct {
	path string
	rel  string
	size int64
}

// ImportEbook places the best-scoring ebook under srcPath (a file or a
// directory) into the ebook library. Does not touch the DB.
func (s *ImportService) ImportEbook(
	ctx context.Context,
	srcPath string,
	b *ent.Book,
	profile config.BookQualityProfileEntry,
	replace bool,
) (ImportedFile, error) {
	lib := config.Get().Library
	ctx, span := tracer.Start(ctx, "library.import_ebook",
		trace.WithAttributes(
			attribute.Int64("book.id", int64(b.ID)),
			attribute.String("import.mode", lib.ImportMode),
		),
	)
	defer span.End()
	outcome := "success"
	defer recordBookImport(ctx, "ebook", lib.ImportMode, time.Now(), &outcome)

	cands, sawSample, err := collectBookFiles(srcPath, ebookmeta.EbookExtensions)
	if err != nil {
		outcome = "stat_failed"
		return ImportedFile{}, otelx.RecordSpanError(span, err)
	}
	var (
		best  bookCandidate
		score = -1
	)
	for _, c := range cands {
		sc := ScoreEbookRelease(ParseBookRelease(filepath.Base(c.path)), profile)
		if sc < 0 {
			slog.DebugContext(ctx, "ebook format outside the profile, skipping",
				"file", filepath.Base(c.path), "book.id", b.ID)
			continue
		}
		if sc > score || (sc == score && c.size > best.size) {
			best, score = c, sc
		}
	}
	if score < 0 {
		outcome = "no_media"
		return ImportedFile{}, otelx.RecordSpanError(
			span,
			noBookMedia(sawSample, len(cands)),
		)
	}
	for _, c := range cands {
		if c.path != best.path {
			slog.DebugContext(ctx, "ebook left in the download",
				"file", filepath.Base(c.path), "book.id", b.ID)
		}
	}

	base, err := BookDestination(
		lib.EbookPath, BookTemplate(lib, b, slotEbook), NamingForBook(b, slotEbook),
	)
	if err != nil {
		outcome = "unsafe_path"
		return ImportedFile{}, otelx.RecordSpanError(span, err)
	}
	destPath := base + strings.ToLower(filepath.Ext(best.path))
	if !PathUnderRoot(destPath, lib.EbookPath) {
		outcome = "unsafe_path"
		return ImportedFile{}, otelx.RecordSpanError(span, ErrUnsafePath)
	}
	span.SetAttributes(attribute.String("dest.path", destPath))

	if err := MkdirLibraryDir(filepath.Dir(destPath)); err != nil {
		outcome = "mkdir_failed"
		return ImportedFile{}, otelx.RecordSpanError(
			span, fmt.Errorf("create library dir: %w", err),
		)
	}
	if existing, err := os.Stat(destPath); err == nil {
		srcInfo, statErr := os.Stat(best.path)
		if statErr == nil && os.SameFile(existing, srcInfo) {
			return ImportedFile{
				Path: destPath, Size: existing.Size(), Parsed: bookParsed(destPath),
			}, nil
		}
		if !replace {
			outcome = "dest_exists"
			return ImportedFile{}, otelx.RecordSpanError(span, ErrDestExists)
		}
	}
	if err := transferFile(best.path, destPath, lib.ImportMode); err != nil {
		outcome = "transfer_failed"
		return ImportedFile{}, otelx.RecordSpanError(
			span, fmt.Errorf("transfer file: %w", err),
		)
	}
	info, err := os.Stat(destPath)
	if err != nil {
		outcome = "stat_dest_failed"
		return ImportedFile{}, otelx.RecordSpanError(
			span, fmt.Errorf("stat imported file: %w", err),
		)
	}
	span.SetAttributes(attribute.Int64("file.size", info.Size()))
	slog.InfoContext(ctx, "ebook transferred",
		"media_file.src", best.path, "media_file.dst", destPath,
		"import.mode", lib.ImportMode, "book.id", b.ID)
	return ImportedFile{
		Path: destPath, Size: info.Size(), Parsed: bookParsed(destPath),
	}, nil
}

// ImportAudiobook places every audio file under srcPath into one folder of
// the audiobook library, keeping each file's name and relative sub-path.
// Does not touch the DB.
func (s *ImportService) ImportAudiobook(
	ctx context.Context,
	srcPath string,
	b *ent.Book,
	replace bool,
) ([]ImportedFile, error) {
	lib := config.Get().Library
	ctx, span := tracer.Start(ctx, "library.import_audiobook",
		trace.WithAttributes(
			attribute.Int64("book.id", int64(b.ID)),
			attribute.String("import.mode", lib.ImportMode),
		),
	)
	defer span.End()
	outcome := "success"
	defer recordBookImport(ctx, "audiobook", lib.ImportMode, time.Now(), &outcome)

	cands, sawSample, err := collectBookFiles(srcPath, ebookmeta.AudiobookExtensions)
	if err != nil {
		outcome = "stat_failed"
		return nil, otelx.RecordSpanError(span, err)
	}
	if len(cands) == 0 {
		outcome = "no_media"
		return nil, otelx.RecordSpanError(span, noBookMedia(sawSample, 0))
	}

	destDir, err := BookDestination(
		lib.AudiobookPath, BookTemplate(lib, b, slotAudiobook),
		NamingForBook(b, slotAudiobook),
	)
	if err != nil {
		outcome = "unsafe_path"
		return nil, otelx.RecordSpanError(span, err)
	}
	span.SetAttributes(attribute.String("dest.path", destDir))

	entries, readErr := os.ReadDir(destDir)
	if readErr == nil && len(entries) > 0 && !replace {
		outcome = "dest_exists"
		return nil, otelx.RecordSpanError(span, ErrDestExists)
	}
	destWasEmpty := readErr != nil || len(entries) == 0
	if err := MkdirLibraryDir(destDir); err != nil {
		outcome = "mkdir_failed"
		return nil, otelx.RecordSpanError(
			span, fmt.Errorf("create library dir: %w", err),
		)
	}

	// A folder is the unit: a half-placed audiobook would make every retry
	// fail on ErrDestExists, so a failure mid-loop removes what this run put
	// there before returning.
	placed := make([]ImportedFile, 0, len(cands))
	var created []string
	undo := func() { removePlaced(ctx, created, destDir, destWasEmpty) }
	for _, c := range cands {
		dst := filepath.Join(destDir, c.rel)
		if !PathUnderRoot(dst, destDir) {
			outcome = "unsafe_path"
			undo()
			return nil, otelx.RecordSpanError(span, ErrUnsafePath)
		}
		if err := MkdirLibraryDir(filepath.Dir(dst)); err != nil {
			outcome = "mkdir_failed"
			undo()
			return nil, otelx.RecordSpanError(
				span, fmt.Errorf("create library dir: %w", err),
			)
		}
		if existing, err := os.Stat(dst); err == nil {
			srcInfo, statErr := os.Stat(c.path)
			if statErr == nil && os.SameFile(existing, srcInfo) {
				placed = append(placed, ImportedFile{
					Path: dst, Size: existing.Size(), Parsed: bookParsed(dst),
				})
				continue
			}
			if !replace {
				outcome = "dest_exists"
				undo()
				return nil, otelx.RecordSpanError(span, ErrDestExists)
			}
		}
		if err := transferFile(c.path, dst, lib.ImportMode); err != nil {
			outcome = "transfer_failed"
			undo()
			return nil, otelx.RecordSpanError(
				span, fmt.Errorf("transfer file: %w", err),
			)
		}
		created = append(created, dst)
		info, err := os.Stat(dst)
		if err != nil {
			outcome = "stat_dest_failed"
			undo()
			return nil, otelx.RecordSpanError(
				span, fmt.Errorf("stat imported file: %w", err),
			)
		}
		placed = append(placed, ImportedFile{
			Path: dst, Size: info.Size(), Parsed: bookParsed(dst),
		})
	}
	slog.InfoContext(ctx, "audiobook transferred",
		"media_file.dst", destDir, "files", len(placed),
		"import.mode", lib.ImportMode, "book.id", b.ID)
	return placed, nil
}

// removePlaced deletes the files one failed audiobook import placed and, when
// the destination folder did not exist or was empty before the run, the
// folder itself. Nothing here can fail the import further, so errors are
// logged, not returned.
func removePlaced(
	ctx context.Context,
	created []string,
	destDir string,
	destWasEmpty bool,
) {
	for _, p := range created {
		if err := os.Remove(p); err != nil {
			slog.WarnContext(ctx, "audiobook import: could not remove placed file",
				"media_file.path", p, "error", err)
		}
	}
	if !destWasEmpty {
		return
	}
	if err := os.RemoveAll(destDir); err != nil {
		slog.WarnContext(ctx, "audiobook import: could not remove destination",
			"media_file.dst", destDir, "error", err)
	}
}

func recordBookImport(
	ctx context.Context,
	kind, mode string,
	start time.Time,
	outcome *string,
) {
	attrs := metric.WithAttributes(
		attribute.String("media.kind", kind),
		attribute.String("import.mode", mode),
		attribute.String("outcome", *outcome),
	)
	importDuration.Record(ctx, time.Since(start).Seconds(), attrs)
	imports.Add(ctx, 1, attrs)
}

func noBookMedia(sawSample bool, candidates int) error {
	if sawSample && candidates == 0 {
		return ErrSampleOnly
	}
	return ErrNoMedia
}

func bookParsed(path string) ParseResult {
	return ParseResult{
		Extension: strings.TrimPrefix(strings.ToLower(filepath.Ext(path)), "."),
	}
}

// collectBookFiles lists the non-sample files under srcPath (or srcPath
// itself when it is a file) whose lowercase extension is in exts, sorted by
// relative path.
func collectBookFiles(
	srcPath string,
	exts map[string]struct{},
) ([]bookCandidate, bool, error) {
	info, err := os.Stat(srcPath)
	if err != nil {
		return nil, false, err
	}
	var (
		out       []bookCandidate
		sawSample bool
	)
	add := func(path, rel string, size int64) {
		if _, ok := exts[strings.ToLower(filepath.Ext(path))]; !ok {
			return
		}
		if SampleRe.MatchString(filepath.Base(path)) {
			sawSample = true
			return
		}
		out = append(out, bookCandidate{path: path, rel: rel, size: size})
	}
	if !info.IsDir() {
		add(srcPath, filepath.Base(srcPath), info.Size())
		return out, sawSample, nil
	}
	err = filepath.WalkDir(
		srcPath,
		func(p string, d fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				if p == srcPath {
					return walkErr
				}
				return nil
			}
			if d.IsDir() {
				return nil
			}
			fi, err := d.Info()
			if err != nil {
				return nil
			}
			rel, err := filepath.Rel(srcPath, p)
			if err != nil {
				return nil
			}
			add(p, rel, fi.Size())
			return nil
		},
	)
	if err != nil {
		return nil, false, err
	}
	slices.SortFunc(out, func(a, b bookCandidate) int {
		return strings.Compare(a.rel, b.rel)
	})
	return out, sawSample, nil
}
