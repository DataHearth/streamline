package library

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"syscall"
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
		metric.WithDescription("Media import attempts by kind and outcome"),
	))
	importDuration = otelx.Must(meter.Float64Histogram(
		"streamline.library.import.duration",
		metric.WithDescription("Media import duration"),
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

// findMediaFile scans dir for video files above 50MB, skipping any whose
// basename matches \bsample\b. Returns the absolute path to the sole
// candidate. Errors: ErrNoMedia (none found, none filtered), ErrSampleOnly
// (all candidates were samples), ErrMultipleMedia (>1 candidate after
// filtering). When dir is a single file, it is returned directly provided it
// passes the same filters.
func findMediaFile(dir string) (string, error) {
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
// size + sample filters (same rules as findMediaFile). Used by the importer to
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
// Does not touch the DB. Errors from findMediaFile pass through as-is so the
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

	return placeFile(ctx, span, placement{
		kind:   "movie",
		src:    srcDir,
		root:   s.config.MoviePath,
		naming: s.config.MovieNaming,
		mode:   mode,
		buildVars: func(parsed ParseResult) map[string]string {
			return BuildMovieVars(m.Title, m.Year, m.TmdbID, imdbID, parsed)
		},
		owner: []any{"movie.id", m.ID},
	})
}

// ImportEpisode places a single episode file into the series library. srcFile
// is a concrete file path (resolved by the caller — for a season pack the
// caller matches each file to its episode before calling this). The dest path
// is rendered from SeriesNaming + SeriesPath. Does not touch the DB.
func (s *ImportService) ImportEpisode(
	ctx context.Context,
	srcFile string,
	show *ent.TVShow,
	season uint16,
	ep *ent.Episode,
) (ImportedFile, error) {
	return s.ImportEpisodeWithMode(ctx, srcFile, show, season, ep, "")
}

// ImportEpisodeWithMode is ImportEpisode with an explicit transfer-mode
// override. Empty mode falls back to s.config.ImportMode. Valid values:
// hardlink, copy, move.
func (s *ImportService) ImportEpisodeWithMode(
	ctx context.Context,
	srcFile string,
	show *ent.TVShow,
	season uint16,
	ep *ent.Episode,
	modeOverride string,
) (ImportedFile, error) {
	mode := modeOverride
	if mode == "" {
		mode = s.config.ImportMode
	}
	ctx, span := tracer.Start(ctx, "library.import_episode",
		trace.WithAttributes(
			attribute.Int64("tvshow.id", int64(show.ID)),
			attribute.Int("season", int(season)),
			attribute.Int("episode", int(ep.Number)),
			attribute.String("import.mode", mode),
		),
	)
	defer span.End()

	return placeFile(ctx, span, placement{
		kind:   "episode",
		src:    srcFile,
		root:   s.config.SeriesPath,
		naming: s.config.SeriesNaming,
		mode:   mode,
		buildVars: func(parsed ParseResult) map[string]string {
			return BuildEpisodeVars(
				show.Title,
				show.Year,
				season,
				ep.Number,
				ep.Title,
				parsed,
			)
		},
		owner: []any{"tvshow.id", show.ID, "episode.id", ep.ID},
	})
}

// placement describes one file placement: where to look for the source, how to
// render the destination, and how to move the bytes there.
type placement struct {
	kind      string // movie | episode — metric dimension
	src       string // a file, or a directory holding exactly one media file
	root      string // library root the destination must stay inside
	naming    string
	mode      string
	buildVars func(ParseResult) map[string]string
	// owner identifies the movie/episode as slog key/value pairs.
	owner []any
}

// placeFile resolves the media file under p.src, renders its destination from
// the naming template, transfers it, and records the import metrics. Shared by
// the movie and episode paths so both report identically — the only real
// difference between them is which template vars get built.
func placeFile(
	ctx context.Context,
	span trace.Span,
	p placement,
) (ImportedFile, error) {
	start := time.Now()
	outcome := "success"
	defer func() {
		attrs := metric.WithAttributes(
			attribute.String("media.kind", p.kind),
			attribute.String("import.mode", p.mode),
			attribute.String("outcome", outcome),
		)
		importDuration.Record(ctx, time.Since(start).Seconds(), attrs)
		imports.Add(ctx, 1, attrs)
	}()

	srcFile, err := findMediaFile(p.src)
	if err != nil {
		outcome = "no_media"
		return ImportedFile{}, otelx.RecordSpanError(span, err)
	}
	parsed := Parse(filepath.Base(srcFile))

	segments := strings.Split(ApplyTemplate(p.naming, p.buildVars(parsed)), "/")
	for i, seg := range segments {
		segments[i] = SanitizePath(seg)
	}
	destPath := filepath.Join(p.root, filepath.Join(segments...))

	absRoot, _ := filepath.Abs(p.root)
	absDest, _ := filepath.Abs(destPath)
	if !strings.HasPrefix(absDest, absRoot+string(filepath.Separator)) &&
		absDest != absRoot {
		outcome = "unsafe_path"
		return ImportedFile{}, otelx.RecordSpanError(span, ErrUnsafePath)
	}
	span.SetAttributes(attribute.String("dest.path", destPath))

	if err := MkdirLibraryDir(filepath.Dir(destPath)); err != nil {
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

	if err := transferFile(srcFile, destPath, p.mode); err != nil {
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
	slog.InfoContext(ctx, "media file transferred", append([]any{
		"media_file.src", srcFile,
		"media_file.dst", destPath,
		"import.mode", p.mode,
	}, p.owner...)...)

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

// MkdirLibraryDir creates a media library directory. The mode is 0755 —
// world-readable on purpose, and the one place that says so: Plex, Jellyfin
// and Emby read the library from another uid, typically another container.
func MkdirLibraryDir(path string) error {
	//nolint:gosec // see above: media servers read the library from another uid
	return os.MkdirAll(path, 0o755)
}

// MoveFile relocates src to dst, creating dst's parent first. os.Rename does
// it in a single metadata op when both sides share a filesystem; EXDEV means
// they don't, and the only way across is a full copy. A failed copy takes the
// half-written destination with it so no truncated media file is left behind
// at a path the library considers valid.
func MoveFile(ctx context.Context, src, dst string) error {
	if err := MkdirLibraryDir(filepath.Dir(dst)); err != nil {
		return fmt.Errorf("mkdir %s: %w", filepath.Dir(dst), err)
	}
	err := os.Rename(src, dst)
	if err == nil || !errors.Is(err, syscall.EXDEV) {
		return err
	}
	if err := copyFile(src, dst); err != nil {
		if rmErr := os.Remove(dst); rmErr != nil && !os.IsNotExist(rmErr) {
			slog.WarnContext(ctx, "could not remove partial copy",
				"path", dst, "error", rmErr)
		}
		return err
	}
	return os.Remove(src)
}

func copyFile(src, dst string) error {
	//nolint:gosec // every caller hands it operator-scoped paths: placeFile
	// renders dst through SanitizePath plus the ErrUnsafePath root check, and
	// pathmigrate rewrites stored DB paths against operator-configured roots
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	//nolint:gosec // see above: both callers constrain dst to operator roots
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
