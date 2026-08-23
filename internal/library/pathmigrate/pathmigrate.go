// Package pathmigrate re-roots a library: it rewrites every stored path under
// one root prefix to sit under a new one, optionally moving the files as it
// goes. Backs the "library path migration" action in advanced settings.
package pathmigrate

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/db"
	"github.com/datahearth/streamline/internal/library"
	"github.com/datahearth/streamline/internal/observability"
	"github.com/datahearth/streamline/internal/otelx"
)

var tracer = otel.Tracer(
	"github.com/datahearth/streamline/internal/library/pathmigrate",
)

// Root names the configured library root a migration targets.
type Root string

const (
	RootMovies    Root = "movies"
	RootSeries    Root = "series"
	RootDownloads Root = "downloads"
)

// sampleLimit caps how many rewrites Preview returns so a 50k-file library
// doesn't ship 50k strings to the browser just to show what will happen.
const sampleLimit = 20

var (
	ErrMigrationRunning = errors.New("a path migration is already running")
	ErrInvalidPath      = errors.New(
		"both paths must be absolute and different",
	)
	// ErrMoveUnsupported guards the download root: its rows point at torrent
	// data the download client still owns, and moving it out from under the
	// client breaks seeding. Re-pointing after the operator remaps the
	// client's own volume is the supported flow.
	ErrMoveUnsupported = errors.New(
		"moving files is not supported for the download root",
	)
	ErrUnknownRoot = errors.New("unknown library root")
)

// Params describes one migration. From defaults to the root's currently
// configured value, which is the normal case — the operator only types where
// the library moved to.
type Params struct {
	Root      Root
	From      string
	To        string
	MoveFiles bool
}

// Rewrite is a single stored path and where it will end up.
type Rewrite struct {
	From string
	To   string
}

// RootState is one configured root, how much of the library is stored under
// it, and how much exists for that root's media type at all. Tracked == 0
// only means the config drifted when Total > 0 — an idle download queue is
// legitimately empty, and so is a fresh library.
type RootState struct {
	Root    Root
	Path    string
	Tracked int
	Total   int
}

// Preview is the dry run: what Start would do, without doing it. Skipped
// counts rows whose file isn't where the migration expects it (source missing
// in move mode, destination missing otherwise) — those rows are left alone.
type Preview struct {
	Root    Root
	From    string
	To      string
	Total   int
	Skipped int
	Samples []Rewrite
	CanMove bool
}

// Status is the live (or last) run. Only one migration runs at a time, so a
// single value covers the whole server.
type Status struct {
	Running    bool
	Root       Root
	From       string
	To         string
	MoveFiles  bool
	Total      int
	Done       int
	Skipped    int
	Current    string
	Error      string
	StartedAt  time.Time
	FinishedAt time.Time
}

type Service struct {
	store  db.Store
	mu     sync.Mutex
	status Status
}

func NewService(store db.Store) *Service {
	return &Service{store: store}
}

func (s *Service) Status() Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.status
}

// Roots reports each configured root together with how many stored paths
// currently sit under it. Tracked == 0 on a non-empty library means the config
// and the database disagree — the root was re-pointed without the stored paths
// following — and the operator has to name the old prefix by hand, because
// nothing on the server still knows it.
func (s *Service) Roots(ctx context.Context) ([]RootState, error) {
	ctx, span := tracer.Start(ctx, "pathmigrate.roots")
	defer span.End()

	out := make([]RootState, 0, 3)
	for _, r := range []Root{RootMovies, RootSeries, RootDownloads} {
		path, err := currentRoot(r)
		if err != nil {
			return nil, otelx.RecordSpanError(span, err)
		}
		path = filepath.Clean(path)
		// Counting through collect keeps the "what counts as under this root"
		// rule in exactly one place; the rewrites it builds are discarded.
		tracked, err := s.collect(ctx, Params{Root: r, From: path, To: path})
		if err != nil {
			return nil, otelx.RecordSpanError(span, err)
		}
		total, err := s.countAll(ctx, r)
		if err != nil {
			return nil, otelx.RecordSpanError(span, err)
		}
		out = append(out, RootState{
			Root:    r,
			Path:    path,
			Tracked: len(tracked),
			Total:   total,
		})
	}
	return out, nil
}

// countAll returns how many rows exist for a root's media type regardless of
// where they are stored. media_files is shared by movies and episodes, so it
// is split by owner rather than counted whole.
func (s *Service) countAll(ctx context.Context, r Root) (int, error) {
	switch r {
	case RootMovies:
		return s.store.CountMovieMediaFiles(ctx)
	case RootSeries:
		return s.store.CountEpisodeMediaFiles(ctx)
	case RootDownloads:
		records, err := s.store.CountDownloadRecords(ctx)
		if err != nil {
			return 0, err
		}
		sessions, err := s.store.CountTorrentSessions(ctx)
		if err != nil {
			return 0, err
		}
		return records + sessions, nil
	}
	return 0, ErrUnknownRoot
}

func (s *Service) Preview(ctx context.Context, p Params) (Preview, error) {
	ctx, span := tracer.Start(ctx, "pathmigrate.preview")
	defer span.End()

	p, err := s.resolve(p)
	if err != nil {
		return Preview{}, otelx.RecordSpanError(span, err)
	}
	targets, err := s.collect(ctx, p)
	if err != nil {
		return Preview{}, otelx.RecordSpanError(span, err)
	}

	out := Preview{
		Root:    p.Root,
		From:    p.From,
		To:      p.To,
		Total:   len(targets),
		CanMove: p.Root != RootDownloads,
	}
	for _, t := range targets {
		if !onDisk(t, p.MoveFiles) {
			out.Skipped++
		}
		if len(out.Samples) < sampleLimit {
			out.Samples = append(out.Samples, Rewrite{From: t.from, To: t.to})
		}
	}
	span.SetAttributes(
		attribute.Int("migration.total", out.Total),
		attribute.Int("migration.skipped", out.Skipped),
	)
	return out, nil
}

// Start validates, snapshots the affected rows, and runs the migration in the
// background. The originating request returns immediately; progress is polled
// through Status.
func (s *Service) Start(ctx context.Context, p Params) (Status, error) {
	ctx, span := tracer.Start(ctx, "pathmigrate.start")
	defer span.End()

	p, err := s.resolve(p)
	if err != nil {
		return Status{}, otelx.RecordSpanError(span, err)
	}
	targets, err := s.collect(ctx, p)
	if err != nil {
		return Status{}, otelx.RecordSpanError(span, err)
	}

	s.mu.Lock()
	if s.status.Running {
		s.mu.Unlock()
		return Status{}, otelx.RecordSpanError(span, ErrMigrationRunning)
	}
	s.status = Status{
		Running:   true,
		Root:      p.Root,
		From:      p.From,
		To:        p.To,
		MoveFiles: p.MoveFiles,
		Total:     len(targets),
		StartedAt: time.Now(),
	}
	out := s.status
	s.mu.Unlock()

	slog.InfoContext(ctx, "library path migration started",
		"migration.root", p.Root, "migration.from", p.From,
		"migration.to", p.To, "migration.move_files", p.MoveFiles,
		"migration.total", len(targets))

	go s.run(context.WithoutCancel(ctx), p, targets)
	return out, nil
}

func (s *Service) run(ctx context.Context, p Params, targets []target) {
	ctx, span := tracer.Start(ctx, "pathmigrate.run",
		trace.WithAttributes(attribute.String("migration.root", string(p.Root))))
	defer span.End()

	var runErr error
	for _, t := range targets {
		s.progress(t.from)
		if !onDisk(t, p.MoveFiles) {
			s.skip()
			continue
		}
		if p.MoveFiles {
			if err := library.MoveFile(ctx, t.from, t.to); err != nil {
				runErr = fmt.Errorf("move %s → %s: %w", t.from, t.to, err)
				break
			}
		}
		if err := t.save(ctx, t.to); err != nil {
			runErr = fmt.Errorf("persist %s: %w", t.to, err)
			break
		}
		s.advance()
	}
	if runErr == nil {
		runErr = s.persistRoot(ctx, p)
	}
	if runErr != nil {
		runErr = otelx.RecordSpanError(span, runErr)
	}
	s.finish(ctx, runErr)
}

// persistRoot moves the configured root along with the rows, by applying the
// migration to the root itself rather than assuming the root *is* From. That
// keeps parent-prefix runs honest: migrating /mnt → /mnt/streamline leaves the
// movie root at /mnt/streamline/movies, not at /mnt/streamline.
//
// A root outside the migrated prefix is left alone — the operator moved a
// subtree, and the library still lives where it did. A read-only instance is
// configured externally: the operator has already edited the root themselves
// and only wants the stored paths fixed up, so ErrReadOnly is not a failure.
func (s *Service) persistRoot(ctx context.Context, p Params) error {
	root, err := currentRoot(p.Root)
	if err != nil {
		return err
	}
	root = filepath.Clean(root)
	if !under(root, p.From) {
		slog.InfoContext(
			ctx,
			"configured root sits outside the migrated prefix, left it untouched",
			"migration.root",
			p.Root,
			"migration.configured",
			root,
		)
		return nil
	}
	moved := rewrite(root, p.From, p.To)

	patch := config.LibraryPatch{}
	switch p.Root {
	case RootMovies:
		patch.MoviePath = &moved
	case RootSeries:
		patch.SeriesPath = &moved
	case RootDownloads:
		patch.DownloadPath = &moved
	}
	if _, err := config.UpdateLibrary(ctx, patch); err != nil {
		if errors.Is(err, config.ErrReadOnly) {
			slog.InfoContext(ctx, "config is read-only, left the root untouched",
				"migration.root", p.Root, "migration.to", p.To)
			return nil
		}
		return fmt.Errorf("persist %s root: %w", p.Root, err)
	}
	return nil
}

// currentRoot returns the configured path for one root family.
func currentRoot(r Root) (string, error) {
	cfg := config.Get()
	switch r {
	case RootMovies:
		return cfg.Library.MoviePath, nil
	case RootSeries:
		return cfg.Library.SeriesPath, nil
	case RootDownloads:
		return cfg.Library.DownloadPath, nil
	}
	return "", ErrUnknownRoot
}

func (s *Service) resolve(p Params) (Params, error) {
	current, err := currentRoot(p.Root)
	if err != nil {
		return Params{}, err
	}
	if strings.TrimSpace(p.From) == "" {
		p.From = current
	}
	p.From = filepath.Clean(strings.TrimSpace(p.From))
	p.To = filepath.Clean(strings.TrimSpace(p.To))
	if !filepath.IsAbs(p.From) || !filepath.IsAbs(p.To) || p.From == p.To {
		return Params{}, ErrInvalidPath
	}
	if p.MoveFiles && p.Root == RootDownloads {
		return Params{}, ErrMoveUnsupported
	}
	return p, nil
}

// target is one stored path plus the write that re-points its row. Carrying
// the write as a closure keeps the movie/series/download row kinds from
// leaking into the run loop.
type target struct {
	from string
	to   string
	save func(ctx context.Context, path string) error
}

func (s *Service) collect(ctx context.Context, p Params) ([]target, error) {
	if p.Root == RootDownloads {
		return s.collectDownloads(ctx, p)
	}
	files, err := s.store.ListMediaFilesByPathPrefix(ctx, p.From)
	if err != nil {
		return nil, err
	}
	out := make([]target, 0, len(files))
	for _, f := range files {
		if !under(f.Path, p.From) {
			continue
		}
		id := f.ID
		out = append(out, target{
			from: f.Path,
			to:   rewrite(f.Path, p.From, p.To),
			save: func(ctx context.Context, path string) error {
				return s.store.UpdateMediaFilePath(ctx, id, path)
			},
		})
	}
	return out, nil
}

func (s *Service) collectDownloads(
	ctx context.Context,
	p Params,
) ([]target, error) {
	records, err := s.store.ListDownloadRecordsByPathPrefix(ctx, p.From)
	if err != nil {
		return nil, err
	}
	var out []target
	for _, r := range records {
		if !under(r.SavePath, p.From) {
			continue
		}
		id := r.ID
		out = append(out, target{
			from: r.SavePath,
			to:   rewrite(r.SavePath, p.From, p.To),
			save: func(ctx context.Context, path string) error {
				return s.store.SetDownloadRecordSavePath(ctx, id, path)
			},
		})
	}

	sessions, err := s.store.ListTorrentSessionsByPathPrefix(ctx, p.From)
	if err != nil {
		return nil, err
	}
	for _, sess := range sessions {
		if !under(sess.SavePath, p.From) {
			continue
		}
		hash := sess.InfoHash
		out = append(out, target{
			from: sess.SavePath,
			to:   rewrite(sess.SavePath, p.From, p.To),
			save: func(ctx context.Context, path string) error {
				return s.store.SetTorrentSessionSavePath(ctx, hash, path)
			},
		})
	}
	return out, nil
}

func (s *Service) progress(current string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.status.Current = current
}

func (s *Service) advance() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.status.Done++
}

func (s *Service) skip() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.status.Skipped++
}

func (s *Service) finish(ctx context.Context, err error) {
	s.mu.Lock()
	s.status.Running = false
	s.status.Current = ""
	s.status.FinishedAt = time.Now()
	if err != nil {
		s.status.Error = err.Error()
	}
	done, skipped := s.status.Done, s.status.Skipped
	s.mu.Unlock()

	if err != nil {
		slog.ErrorContext(ctx, "library path migration failed",
			"error", err, "migration.done", done, "migration.skipped", skipped)
		return
	}
	slog.InfoContext(ctx, "library path migration finished",
		"migration.done", done, "migration.skipped", skipped)
}

// onDisk reports whether the file this target expects is actually there: the
// source when the migration moves files, otherwise the destination the
// operator claims to have already moved it to. Rows that fail the check are
// skipped rather than re-pointed at a path with nothing behind it.
func onDisk(t target, moveFiles bool) bool {
	path := t.to
	if moveFiles {
		path = t.from
	}
	_, err := os.Stat(path)
	return err == nil
}

// under keeps /media/movies from claiming rows stored under /media/movies2:
// the SQL prefix filter is deliberately loose so an exact root match is still
// returned, and this narrows it to real descendants plus the root itself.
func under(path, root string) bool {
	return path == root ||
		strings.HasPrefix(path, root+string(filepath.Separator))
}

func rewrite(path, from, to string) string {
	if path == from {
		return to
	}
	rel := strings.TrimPrefix(path, from+string(filepath.Separator))
	return filepath.Join(to, rel)
}

// WarnOnDrift logs a CRITICAL line for every root whose stored paths all sit
// somewhere other than where the config now points. Nothing else notices that
// situation: the records still exist, but every path dangles, and drift-check
// deletes them once drift_grace_ticks elapses. Remounting a claim under a new
// prefix is the ordinary way to reach it, and the fix — POST
// /library/path-migration — is only reachable by an operator who knows to look.
func (s *Service) WarnOnDrift(ctx context.Context) {
	roots, err := s.Roots(ctx)
	if err != nil {
		slog.WarnContext(ctx, "could not check library paths against stored records",
			"error", err)
		return
	}
	for _, r := range roots {
		if r.Total == 0 || r.Tracked > 0 {
			continue
		}
		//nolint:sloglint // LogAttrs takes slog.Attr by API design
		slog.LogAttrs(ctx, observability.LevelCritical,
			"library root does not match any stored path — records will be pruned",
			slog.String("library.root", string(r.Root)),
			slog.String("library.path", r.Path),
			slog.Int("records.total", r.Total),
			slog.String("remedy", "POST /api/v1/library/path-migration to re-root"),
		)
	}
}
