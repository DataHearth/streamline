// Package dbbackup takes, lists, prunes and restores consistent snapshots of
// the SQLite database.
//
// A snapshot is written with VACUUM INTO, which reads the database inside one
// read transaction and writes a self-contained file: no -wal or -shm to carry
// along, and safe to take while the server is writing, since WAL readers never
// block the writer. Every file lands in <data_dir>/backups, named after its
// kind, the schema version it holds and the moment it was taken, so a listing
// and the retention pass need nothing but the directory.
//
// The package never imports config or db: the server, the migration runner and
// the CLI all hand it paths and numbers, and the migration runner in
// particular calls it from inside db.Open.
package dbbackup

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/datahearth/streamline/internal/otelx"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"

	_ "modernc.org/sqlite"
)

var tracer = otel.Tracer("github.com/datahearth/streamline/internal/dbbackup")

// Kind says why a backup was taken. Retention is counted per kind, so a burst
// of manual backups never rotates out the snapshot taken before a migration.
type Kind string

const (
	KindScheduled    Kind = "scheduled"
	KindPreMigration Kind = "pre-migration"
	KindManual       Kind = "manual"
	// KindPreRestore is the database a restore replaced. It is never pruned:
	// there is at most one per restore, and it is the only way back from
	// restoring the wrong file.
	KindPreRestore Kind = "pre-restore"
)

// Kinds lists every kind, in the order a listing groups them.
var Kinds = []Kind{KindScheduled, KindPreMigration, KindManual, KindPreRestore}

const (
	// DirName is the backups directory under data_dir.
	DirName = "backups"

	// The files hold password hashes, API key hashes and live sessions.
	dirMode  = 0o700
	fileMode = 0o600

	timeLayout = "20060102T150405Z"

	partialPrefix = ".streamline-"
	partialSuffix = ".partial"
	// partialMaxAge is how old a partial file must be before Prune takes it
	// for the leftover of a crashed snapshot rather than one in progress.
	partialMaxAge = 24 * time.Hour
)

// nameRe matches the files Snapshot writes. The optional -N suffix breaks a tie
// between two snapshots of one kind taken in the same second.
var nameRe = regexp.MustCompile(
	`^streamline-(scheduled|pre-migration|manual|pre-restore)-(\d+)-(\d{8}T\d{6}Z)(?:-(\d+))?\.db$`,
)

var (
	// ErrInUse is returned by Restore when another connection holds the live
	// database open — streamline is still running.
	ErrInUse = errors.New("the database is in use; stop streamline before restoring")
	// ErrNotFound is returned when the backup to restore does not exist.
	ErrNotFound = errors.New("backup not found")
)

// Backup describes one file in the backups directory.
type Backup struct {
	Name      string
	Path      string
	Kind      Kind
	Version   uint
	CreatedAt time.Time
	Size      int64
}

// Execer is the one method Snapshot needs, so it can run on the migration
// runner's own *sql.DB, a *sql.Conn, or a dedicated connection alike.
type Execer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// Querier reads the schema version.
type Querier interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// Dir is the backups directory for a data_dir.
func Dir(dataDir string) string {
	return filepath.Join(dataDir, DirName)
}

// Snapshot writes a consistent copy of the database conn is open on into dir,
// as a kind backup holding schema version. The copy is written under a hidden
// partial name and renamed into place once complete and synced, so a crash
// never leaves a truncated file that a listing would offer for restore.
func Snapshot(
	ctx context.Context,
	conn Execer,
	dir string,
	kind Kind,
	version uint,
) (Backup, error) {
	ctx, span := tracer.Start(ctx, "dbbackup.snapshot")
	defer span.End()
	span.SetAttributes(
		attribute.String("backup.kind", string(kind)),
		versionAttr(version),
	)

	if err := ensureDir(dir); err != nil {
		return Backup{}, otelx.RecordSpanError(span, err)
	}

	now := time.Now().UTC()
	final, name, err := freeName(dir, kind, version, now)
	if err != nil {
		return Backup{}, otelx.RecordSpanError(span, err)
	}
	partial := filepath.Join(dir, partialPrefix+name+partialSuffix)

	// VACUUM INTO accepts an existing file only when it is empty, which lets
	// the file be created here with owner-only permissions rather than with
	// whatever the umask grants the SQLite VFS.
	//nolint:gosec // dir is data_dir/backups, name is ours
	f, err := os.OpenFile(partial, os.O_WRONLY|os.O_CREATE|os.O_EXCL, fileMode)
	if err != nil {
		return Backup{}, otelx.RecordSpanError(span,
			fmt.Errorf("create %s: %w", partial, err))
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(partial)
		return Backup{}, otelx.RecordSpanError(span, err)
	}

	started := time.Now()
	if _, err := conn.ExecContext(ctx, "VACUUM INTO ?", partial); err != nil {
		_ = os.Remove(partial)
		return Backup{}, otelx.RecordSpanError(span,
			fmt.Errorf("vacuum into %s: %w", partial, err))
	}
	if err := syncFile(partial); err != nil {
		_ = os.Remove(partial)
		return Backup{}, otelx.RecordSpanError(span, err)
	}
	if err := os.Rename(partial, final); err != nil {
		_ = os.Remove(partial)
		return Backup{}, otelx.RecordSpanError(span,
			fmt.Errorf("rename %s: %w", partial, err))
	}
	syncDir(dir)

	info, err := os.Stat(final)
	if err != nil {
		return Backup{}, otelx.RecordSpanError(span, err)
	}
	span.SetAttributes(
		attribute.Int64("backup.size_bytes", info.Size()),
		attribute.Int64("backup.duration_ms", time.Since(started).Milliseconds()),
	)
	return Backup{
		Name:      name,
		Path:      final,
		Kind:      kind,
		Version:   version,
		CreatedAt: now.Truncate(time.Second),
		Size:      info.Size(),
	}, nil
}

// SnapshotFile snapshots the database at dbPath through a connection of its
// own, so it never contends for the server's single pooled connection and works
// as well with the server stopped. It reads the schema version itself and
// never migrates: `streamline backup` run by a newer binary must copy the
// database as it is.
func SnapshotFile(
	ctx context.Context,
	dbPath, dir string,
	kind Kind,
) (Backup, error) {
	conn, err := openExisting(dbPath, "")
	if err != nil {
		return Backup{}, err
	}
	defer conn.Close()
	version, _, err := SchemaVersion(ctx, conn)
	if err != nil {
		return Backup{}, err
	}
	return Snapshot(ctx, conn, dir, kind, version)
}

// SchemaVersion reads the version golang-migrate recorded, and whether a
// migration was interrupted. A database with no schema_migrations table has
// never been migrated and reads as version 0.
func SchemaVersion(ctx context.Context, q Querier) (uint, bool, error) {
	var exists int
	if err := q.QueryRowContext(ctx,
		`SELECT count(*) FROM sqlite_master
		 WHERE type = 'table' AND name = 'schema_migrations'`,
	).Scan(&exists); err != nil {
		return 0, false, fmt.Errorf("read schema version: %w", err)
	}
	if exists == 0 {
		return 0, false, nil
	}
	var (
		version int64
		dirty   bool
	)
	err := q.QueryRowContext(ctx,
		`SELECT version, dirty FROM schema_migrations LIMIT 1`,
	).Scan(&version, &dirty)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("read schema version: %w", err)
	}
	if version < 0 {
		return 0, dirty, nil
	}
	return uint(version), dirty, nil
}

// List returns every backup in dir, newest first. A missing directory is an
// empty list; files that do not follow the naming scheme are ignored.
func List(dir string) ([]Backup, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", dir, err)
	}
	out := make([]Backup, 0, len(entries))
	for _, e := range entries {
		if !e.Type().IsRegular() {
			continue
		}
		b, ok := parseName(e.Name())
		if !ok {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		b.Path = filepath.Join(dir, e.Name())
		b.Size = info.Size()
		out = append(out, b)
	}
	slices.SortFunc(out, func(a, b Backup) int {
		if c := b.CreatedAt.Compare(a.CreatedAt); c != 0 {
			return c
		}
		return strings.Compare(b.Name, a.Name)
	})
	return out, nil
}

// Find resolves a backup by its file name inside dir. It refuses anything that
// is not a plain backup name, so a caller handing it user input can never
// reach outside dir.
func Find(dir, name string) (Backup, error) {
	b, ok := parseName(name)
	if !ok || filepath.Base(name) != name {
		return Backup{}, ErrNotFound
	}
	path := filepath.Join(dir, name)
	info, err := os.Stat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return Backup{}, ErrNotFound
	}
	if err != nil {
		return Backup{}, err
	}
	if !info.Mode().IsRegular() {
		return Backup{}, ErrNotFound
	}
	b.Path = path
	b.Size = info.Size()
	return b, nil
}

// Prune deletes the oldest backups of kind in dir beyond the newest keep, and
// returns the names it removed. keep 0 keeps every one, and pre-restore
// backups are never pruned. It also clears partial files a crashed snapshot
// left behind.
func Prune(dir string, kind Kind, keep int) ([]string, error) {
	clearPartials(dir)
	if keep <= 0 || kind == KindPreRestore {
		return nil, nil
	}
	all, err := List(dir)
	if err != nil {
		return nil, err
	}
	var (
		removed []string
		errs    []error
		seen    int
	)
	for _, b := range all {
		if b.Kind != kind {
			continue
		}
		seen++
		if seen <= keep {
			continue
		}
		if err := os.Remove(b.Path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			errs = append(errs, err)
			continue
		}
		removed = append(removed, b.Name)
	}
	return removed, errors.Join(errs...)
}

// versionAttr carries the schema version as a string: it is a migration
// timestamp (20261025050514), so nothing is lost, and no conversion can
// overflow.
func versionAttr(v uint) attribute.KeyValue {
	return attribute.String("db.schema.version", strconv.FormatUint(uint64(v), 10))
}

func parseName(name string) (Backup, bool) {
	m := nameRe.FindStringSubmatch(name)
	if m == nil {
		return Backup{}, false
	}
	version, err := strconv.ParseUint(m[2], 10, 64)
	if err != nil {
		return Backup{}, false
	}
	at, err := time.Parse(timeLayout, m[3])
	if err != nil {
		return Backup{}, false
	}
	return Backup{
		Name:      name,
		Kind:      Kind(m[1]),
		Version:   uint(version),
		CreatedAt: at,
	}, true
}

// freeName picks the first name for a snapshot that is not taken yet.
func freeName(
	dir string,
	kind Kind,
	version uint,
	at time.Time,
) (string, string, error) {
	base := fmt.Sprintf("streamline-%s-%d-%s", kind, version, at.Format(timeLayout))
	for i := range 100 {
		name := base + ".db"
		if i > 0 {
			name = fmt.Sprintf("%s-%d.db", base, i)
		}
		path := filepath.Join(dir, name)
		if _, err := os.Lstat(path); errors.Is(err, fs.ErrNotExist) {
			return path, name, nil
		}
	}
	return "", "", fmt.Errorf("no free backup name for %s in %s", base, dir)
}

// ensureDir creates dir owner-only, and narrows an existing one: a directory
// made by hand, or by an older umask, would otherwise expose every snapshot.
func ensureDir(dir string) error {
	if err := os.MkdirAll(dir, dirMode); err != nil {
		return fmt.Errorf("create %s: %w", dir, err)
	}
	info, err := os.Stat(dir)
	if err != nil {
		return err
	}
	if info.Mode().Perm() != dirMode {
		if err := os.Chmod(dir, dirMode); err != nil {
			return fmt.Errorf("restrict %s to its owner: %w", dir, err)
		}
	}
	return nil
}

func clearPartials(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		n := e.Name()
		if !strings.HasPrefix(n, partialPrefix) ||
			!strings.HasSuffix(n, partialSuffix) {
			continue
		}
		info, err := e.Info()
		if err != nil || time.Since(info.ModTime()) < partialMaxAge {
			continue
		}
		_ = os.Remove(filepath.Join(dir, n))
	}
}

// openExisting opens a connection on a database file that must already exist:
// sql.Open on a missing path would quietly create an empty database there.
// params are extra DSN query parameters.
func openExisting(path, params string) (*sql.DB, error) {
	info, err := os.Stat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("no database at %s", path)
	}
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%s is not a regular file", path)
	}
	dsn := "file:" + path + "?_pragma=busy_timeout(5000)"
	if params != "" {
		dsn += "&" + params
	}
	conn, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	conn.SetMaxOpenConns(1)
	return conn, nil
}

func syncFile(path string) error {
	//nolint:gosec // a path this package built
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return fmt.Errorf("sync %s: %w", path, err)
	}
	return f.Close()
}

// syncDir makes a rename durable. Best effort: not every filesystem supports
// syncing a directory, and the file itself is already on disk.
func syncDir(dir string) {
	//nolint:gosec // a path this package built
	d, err := os.Open(dir)
	if err != nil {
		return
	}
	_ = d.Sync()
	_ = d.Close()
}
