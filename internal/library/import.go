package library

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/otelx"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

var (
	tracer = otel.Tracer("github.com/datahearth/streamline/internal/library")
	meter  = otel.Meter("github.com/datahearth/streamline/internal/library")

	imports        metric.Int64Counter
	importDuration metric.Float64Histogram
)

func init() {
	imports = otelx.Must(meter.Int64Counter(
		"streamline.library.imports",
		metric.WithDescription("Movie import attempts by outcome"),
	))
	importDuration = otelx.Must(meter.Float64Histogram(
		"streamline.library.import.duration",
		metric.WithDescription("Movie import duration"),
		metric.WithUnit("s"),
	))

	ctx := context.Background()
	imports.Add(ctx, 0)
	importDuration.Record(ctx, 0)
}

type ImportService struct {
	config *config.LibraryConfig
}

func NewImportService(cfg *config.LibraryConfig) *ImportService {
	return &ImportService{config: cfg}
}

// ImportedFile describes a placed media file. Returned by ImportMovie so the
// caller (importer.Worker) can persist the MediaFile row in the same atomic
// DB transaction as the DownloadRecord + Movie status transitions.
type ImportedFile struct {
	Path   string
	Size   int64
	Parsed ParseResult
}

// FindMediaFile scans dir for video files above 50MB, skipping any whose
// basename matches \bsample\b. Returns the absolute path to the sole
// candidate. Errors: ErrNoMedia (none found, none filtered), ErrSampleOnly
// (all candidates were samples), ErrMultipleMedia (>1 candidate after
// filtering). When dir is a single file, it is returned directly provided it
// passes the same filters.
func FindMediaFile(dir string) (string, error) {
	info, err := os.Stat(dir)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		if !MediaExts[filepath.Ext(dir)] || info.Size() < MinMediaSize ||
			SampleRe.MatchString(filepath.Base(dir)) {
			return "", ErrNoMedia
		}
		return dir, nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", err
	}
	var (
		candidates []string
		sawSample  bool
	)
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !MediaExts[filepath.Ext(name)] {
			continue
		}
		info, err := e.Info()
		if err != nil || info.Size() < MinMediaSize {
			continue
		}
		if SampleRe.MatchString(name) {
			sawSample = true
			continue
		}
		candidates = append(candidates, filepath.Join(dir, name))
	}
	switch {
	case len(candidates) == 1:
		return candidates[0], nil
	case len(candidates) > 1:
		return "", ErrMultipleMedia
	case sawSample:
		return "", ErrSampleOnly
	default:
		return "", ErrNoMedia
	}
}

// ListVideoFiles returns every video file directly under dir that passes the
// size + sample filters (same rules as FindMediaFile). Used by the importer to
// enumerate a season pack's individual episode files.
func ListVideoFiles(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !MediaExts[filepath.Ext(name)] {
			continue
		}
		info, err := e.Info()
		if err != nil || info.Size() < MinMediaSize {
			continue
		}
		if SampleRe.MatchString(name) {
			continue
		}
		out = append(out, filepath.Join(dir, name))
	}
	return out, nil
}

// ListVideoFilesRecursive returns every importable video file under dir and its
// subdirectories, applying the same ext / min-size / sample filters as
// ListVideoFiles. Unlike ListVideoFiles it descends into season folders, so it
// handles the Show/Season NN/episode layout. Unreadable descendants are skipped;
// only an unreadable root produces an error.
func ListVideoFilesRecursive(dir string) ([]string, error) {
	var out []string
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			if p == dir {
				return walkErr
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if !MediaExts[strings.ToLower(filepath.Ext(p))] {
			return nil
		}
		info, err := d.Info()
		if err != nil || info.Size() < MinMediaSize {
			return nil
		}
		if SampleRe.MatchString(filepath.Base(p)) {
			return nil
		}
		out = append(out, p)
		return nil
	})
	return out, err
}

// ImportMovie finds the media file under srcDir (or uses srcDir if it is
// already a file), renders the destination path from the naming template,
// transfers the file with the configured mode, and returns ImportedFile.
// Does not touch the DB. Errors from FindMediaFile pass through as-is so the
// worker can classify them.
func (s *ImportService) ImportMovie(
	ctx context.Context,
	srcDir string,
	m *ent.Movie,
	imdbID string,
) (ImportedFile, error) {
	return s.ImportMovieWithMode(ctx, srcDir, m, imdbID, "")
}

// ImportMovieWithMode is ImportMovie with an explicit transfer-mode override.
// Empty mode falls back to s.config.ImportMode. Valid values: hardlink, copy, move.
func (s *ImportService) ImportMovieWithMode(
	ctx context.Context,
	srcDir string,
	m *ent.Movie,
	imdbID string,
	modeOverride string,
) (ImportedFile, error) {
	mode := modeOverride
	if mode == "" {
		mode = s.config.ImportMode
	}
	ctx, span := tracer.Start(ctx, "library.import_movie",
		trace.WithAttributes(
			attribute.Int64("movie.id", int64(m.ID)),
			attribute.String("movie.title", m.Title),
			attribute.String("import.mode", mode),
		),
	)
	defer span.End()

	start := time.Now()
	outcome := "success"
	defer func() {
		attrs := metric.WithAttributes(
			attribute.String("import.mode", mode),
			attribute.String("outcome", outcome),
		)
		importDuration.Record(ctx, time.Since(start).Seconds(), attrs)
		imports.Add(ctx, 1, attrs)
	}()

	srcFile, err := FindMediaFile(srcDir)
	if err != nil {
		outcome = "no_media"
		return ImportedFile{}, otelx.RecordSpanError(span, err)
	}
	parsed := Parse(filepath.Base(srcFile))
	vars := BuildMovieVars(m.Title, m.Year, m.TmdbID, imdbID, parsed)
	relPath := ApplyTemplate(s.config.MovieNaming, vars)

	segments := strings.Split(relPath, "/")
	for i, seg := range segments {
		segments[i] = SanitizePath(seg)
	}
	relJoined := filepath.Join(segments...)
	destPath := filepath.Join(s.config.MoviePath, relJoined)

	absRoot, _ := filepath.Abs(s.config.MoviePath)
	absDest, _ := filepath.Abs(destPath)
	if !strings.HasPrefix(absDest, absRoot+string(filepath.Separator)) &&
		absDest != absRoot {
		outcome = "unsafe_path"
		return ImportedFile{}, otelx.RecordSpanError(span, ErrUnsafePath)
	}
	span.SetAttributes(attribute.String("dest.path", destPath))

	if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
		outcome = "mkdir_failed"
		return ImportedFile{}, otelx.RecordSpanError(
			span,
			fmt.Errorf("create library dir: %w", err),
		)
	}

	if existing, err := os.Stat(destPath); err == nil {
		srcInfo, statErr := os.Stat(srcFile)
		if statErr == nil && os.SameFile(existing, srcInfo) {
			return ImportedFile{
				Path:   destPath,
				Size:   existing.Size(),
				Parsed: parsed,
			}, nil
		}
		outcome = "dest_exists"
		return ImportedFile{}, otelx.RecordSpanError(span, ErrDestExists)
	}

	if err := transferFile(srcFile, destPath, mode); err != nil {
		outcome = "transfer_failed"
		return ImportedFile{}, otelx.RecordSpanError(
			span,
			fmt.Errorf("transfer file: %w", err),
		)
	}

	info, err := os.Stat(destPath)
	if err != nil {
		outcome = "stat_dest_failed"
		return ImportedFile{}, otelx.RecordSpanError(
			span,
			fmt.Errorf("stat imported file: %w", err),
		)
	}
	span.SetAttributes(attribute.Int64("file.size", info.Size()))
	slog.InfoContext(ctx, "media file transferred",
		"media_file.src", srcFile,
		"media_file.dst", destPath,
		"import.mode", mode,
		"movie.id", m.ID,
	)

	return ImportedFile{Path: destPath, Size: info.Size(), Parsed: parsed}, nil
}

// ImportEpisode places a single episode file into the series library. srcFile
// is a concrete file path (resolved by the caller — for a season pack the
// caller matches each file to its episode before calling this). The dest path
// is rendered from SeriesNaming + SeriesPath. Mirrors ImportMovieWithMode; does
// not touch the DB.
func (s *ImportService) ImportEpisode(
	ctx context.Context,
	srcFile string,
	show *ent.TVShow,
	season uint16,
	ep *ent.Episode,
) (ImportedFile, error) {
	mode := s.config.ImportMode
	ctx, span := tracer.Start(ctx, "library.import_episode",
		trace.WithAttributes(
			attribute.Int64("tvshow.id", int64(show.ID)),
			attribute.Int("season", int(season)),
			attribute.Int("episode", int(ep.Number)),
			attribute.String("import.mode", mode),
		),
	)
	defer span.End()

	file, err := FindMediaFile(srcFile)
	if err != nil {
		return ImportedFile{}, otelx.RecordSpanError(span, err)
	}
	parsed := Parse(filepath.Base(file))
	vars := BuildEpisodeVars(
		show.Title,
		show.Year,
		season,
		ep.Number,
		ep.Title,
		parsed,
	)
	relPath := ApplyTemplate(s.config.SeriesNaming, vars)

	segments := strings.Split(relPath, "/")
	for i, seg := range segments {
		segments[i] = SanitizePath(seg)
	}
	destPath := filepath.Join(s.config.SeriesPath, filepath.Join(segments...))

	absRoot, _ := filepath.Abs(s.config.SeriesPath)
	absDest, _ := filepath.Abs(destPath)
	if !strings.HasPrefix(absDest, absRoot+string(filepath.Separator)) &&
		absDest != absRoot {
		return ImportedFile{}, otelx.RecordSpanError(span, ErrUnsafePath)
	}
	span.SetAttributes(attribute.String("dest.path", destPath))

	if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
		return ImportedFile{}, otelx.RecordSpanError(
			span,
			fmt.Errorf("create library dir: %w", err),
		)
	}

	if existing, err := os.Stat(destPath); err == nil {
		srcInfo, statErr := os.Stat(file)
		if statErr == nil && os.SameFile(existing, srcInfo) {
			return ImportedFile{
				Path:   destPath,
				Size:   existing.Size(),
				Parsed: parsed,
			}, nil
		}
		return ImportedFile{}, otelx.RecordSpanError(span, ErrDestExists)
	}

	if err := transferFile(file, destPath, mode); err != nil {
		return ImportedFile{}, otelx.RecordSpanError(
			span,
			fmt.Errorf("transfer file: %w", err),
		)
	}

	info, err := os.Stat(destPath)
	if err != nil {
		return ImportedFile{}, otelx.RecordSpanError(
			span,
			fmt.Errorf("stat imported file: %w", err),
		)
	}
	span.SetAttributes(attribute.Int64("file.size", info.Size()))
	slog.InfoContext(ctx, "episode file transferred",
		"media_file.src", file,
		"media_file.dst", destPath,
		"import.mode", mode,
		"tvshow.id", show.ID,
	)
	return ImportedFile{Path: destPath, Size: info.Size(), Parsed: parsed}, nil
}

func transferFile(src, dst, mode string) error {
	switch mode {
	case "hardlink":
		return os.Link(src, dst)
	case "move":
		return os.Rename(src, dst)
	case "copy":
		return copyFile(src, dst)
	default:
		return fmt.Errorf("unknown import mode: %s", mode)
	}
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	return err
}

// Importer is the consumer-facing surface needed by callers that import a
// movie file into the managed library (e.g. internal/library/hygiene's
// orphan auto-import path). *ImportService implements it.
type Importer interface {
	ImportMovieWithMode(
		ctx context.Context,
		srcDir string,
		m *ent.Movie,
		imdbID string,
		modeOverride string,
	) (ImportedFile, error)
}

var _ Importer = (*ImportService)(nil)
