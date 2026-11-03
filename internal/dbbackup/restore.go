package dbbackup

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/datahearth/streamline/internal/otelx"
	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

// Validated is what Validate learned about a backup file.
type Validated struct {
	Path    string
	Version uint
	Size    int64
}

// RestoreResult reports what Restore did.
type RestoreResult struct {
	// Restored is the backup that now sits at the database path.
	Restored Validated
	// Replaced is the backup the previous database was kept as; empty when
	// there was no database to replace.
	Replaced *Backup
}

// Validate checks that path is a database streamline can boot from: it opens,
// passes SQLite's quick check, carries a clean schema_migrations row, and its
// schema version is not newer than latest — the newest migration this binary
// knows. An older version is fine; the next boot migrates it forward.
func Validate(ctx context.Context, path string, latest uint) (Validated, error) {
	info, err := os.Stat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return Validated{}, fmt.Errorf("%w: %s", ErrNotFound, path)
	}
	if err != nil {
		return Validated{}, err
	}
	if !info.Mode().IsRegular() {
		return Validated{}, fmt.Errorf("%s is not a regular file", path)
	}
	// A copy taken with cp next to a live WAL holds part of its data in the
	// -wal file, which the restore would not carry. Snapshot output never has
	// one.
	if wal, err := os.Stat(path + "-wal"); err == nil && wal.Size() > 0 {
		return Validated{}, fmt.Errorf(
			"%s has a non-empty %s beside it, so the file alone is not the whole database; "+
				"restore a backup streamline wrote, or open this one with sqlite3 once to fold the WAL in",
			path,
			filepath.Base(path)+"-wal",
		)
	}

	// immutable: never take a lock or create a -shm beside the file, which
	// may sit on a read-only mount.
	conn, err := sql.Open("sqlite", "file:"+path+"?mode=ro&immutable=1")
	if err != nil {
		return Validated{}, err
	}
	defer conn.Close()

	var check string
	if err := conn.QueryRowContext(ctx, "PRAGMA quick_check").
		Scan(&check); err != nil {
		return Validated{}, fmt.Errorf(
			"%s is not a readable SQLite database: %w",
			path,
			err,
		)
	}
	if check != "ok" {
		return Validated{}, fmt.Errorf(
			"%s fails SQLite's integrity check: %s",
			path,
			check,
		)
	}
	var tables int
	if err := conn.QueryRowContext(ctx,
		`SELECT count(*) FROM sqlite_master
		 WHERE type = 'table' AND name = 'schema_migrations'`,
	).Scan(&tables); err != nil {
		return Validated{}, fmt.Errorf("read %s: %w", path, err)
	}
	if tables == 0 {
		return Validated{}, fmt.Errorf(
			"%s is not a streamline database (no schema_migrations table)", path)
	}
	version, dirty, err := SchemaVersion(ctx, conn)
	if err != nil {
		return Validated{}, err
	}
	if version == 0 {
		return Validated{}, fmt.Errorf("%s records no schema version", path)
	}
	if dirty {
		return Validated{}, fmt.Errorf(
			"%s was taken in the middle of a failed migration (version %d is marked dirty)",
			path,
			version,
		)
	}
	if version > latest {
		return Validated{}, fmt.Errorf(
			"%s holds schema version %d, newer than this binary's %d; restore it with the streamline release that wrote it",
			path,
			version,
			latest,
		)
	}
	return Validated{Path: path, Version: version, Size: info.Size()}, nil
}

// Restore replaces the database at dbPath with the backup at src, keeping the
// database it replaces in dir as a pre-restore backup. It refuses to run while
// anything else holds the database open (ErrInUse), and leaves the current
// database in place when any step before the swap fails.
func Restore(
	ctx context.Context,
	src, dbPath, dir string,
	latest uint,
) (RestoreResult, error) {
	ctx, span := tracer.Start(ctx, "dbbackup.restore")
	defer span.End()

	srcAbs, err := filepath.Abs(src)
	if err != nil {
		return RestoreResult{}, otelx.RecordSpanError(span, err)
	}
	dbAbs, err := filepath.Abs(dbPath)
	if err != nil {
		return RestoreResult{}, otelx.RecordSpanError(span, err)
	}
	if srcAbs == dbAbs {
		return RestoreResult{}, otelx.RecordSpanError(span,
			errors.New("the backup is the live database itself"))
	}

	v, err := Validate(ctx, src, latest)
	if err != nil {
		return RestoreResult{}, otelx.RecordSpanError(span, err)
	}
	span.SetAttributes(versionAttr(v.Version))
	res := RestoreResult{Restored: v}

	if err := ensureDir(dir); err != nil {
		return RestoreResult{}, otelx.RecordSpanError(span, err)
	}

	// Stage the copy first, beside the database so the final rename stays on
	// one filesystem: nothing has been touched if this fails.
	staged := dbPath + ".restoring"
	if err := copyFile(src, staged); err != nil {
		_ = os.Remove(staged)
		return RestoreResult{}, otelx.RecordSpanError(span, err)
	}

	if _, err := os.Stat(dbPath); err == nil {
		kept, err := detachLive(ctx, dbPath, dir)
		if err != nil {
			_ = os.Remove(staged)
			return RestoreResult{}, otelx.RecordSpanError(span, err)
		}
		res.Replaced = &kept
	} else if !errors.Is(err, fs.ErrNotExist) {
		_ = os.Remove(staged)
		return RestoreResult{}, otelx.RecordSpanError(span, err)
	}

	if err := os.Rename(staged, dbPath); err != nil {
		_ = os.Remove(staged)
		if res.Replaced != nil {
			// Put the database back where the server expects it.
			_ = os.Rename(res.Replaced.Path, dbPath)
		}
		return RestoreResult{}, otelx.RecordSpanError(span,
			fmt.Errorf("move the restored database into place: %w", err))
	}
	syncDir(filepath.Dir(dbPath))
	return res, nil
}

// detachLive moves the live database aside into dir as a pre-restore backup.
//
// It first switches the database out of WAL mode, which does two jobs: SQLite
// refuses the switch while any other connection has the database open — the
// check that streamline is not running — and it folds the WAL into the main
// file and removes it, so the one file moved is the whole database.
func detachLive(ctx context.Context, dbPath, dir string) (Backup, error) {
	// A short timeout: a server that is up holds the file open for good, so
	// waiting longer only delays the same answer.
	conn, err := openExisting(dbPath, "_pragma=busy_timeout(1000)")
	if err != nil {
		return Backup{}, err
	}
	version, _, verr := SchemaVersion(ctx, conn)
	var mode string
	err = conn.QueryRowContext(ctx, "PRAGMA journal_mode = DELETE").Scan(&mode)
	if cerr := conn.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		if isBusy(err) {
			return Backup{}, ErrInUse
		}
		return Backup{}, fmt.Errorf("prepare %s for replacement: %w", dbPath, err)
	}
	if !strings.EqualFold(mode, "delete") {
		return Backup{}, ErrInUse
	}
	if verr != nil {
		// The version only names the kept file; an unreadable one must not
		// stand between the operator and the restore they asked for.
		version = 0
	}

	_, name, err := freeName(dir, KindPreRestore, version, time.Now().UTC())
	if err != nil {
		return Backup{}, err
	}
	kept := filepath.Join(dir, name)
	if err := os.Rename(dbPath, kept); err != nil {
		return Backup{}, fmt.Errorf("keep the current database as %s: %w", kept, err)
	}
	if err := os.Chmod(kept, fileMode); err != nil {
		return Backup{}, fmt.Errorf("restrict %s: %w", kept, err)
	}
	// Leftovers from a crash would be taken for the restored database's own.
	for _, suffix := range []string{"-wal", "-shm", "-journal"} {
		_ = os.Remove(dbPath + suffix)
	}
	syncDir(dir)
	b, err := Find(dir, name)
	if err != nil {
		return Backup{}, err
	}
	return b, nil
}

func isBusy(err error) bool {
	var se *sqlite.Error
	if !errors.As(err, &se) {
		return false
	}
	// The low byte is the primary code; extended codes add detail above it.
	switch se.Code() & 0xff {
	case sqlite3.SQLITE_BUSY, sqlite3.SQLITE_LOCKED:
		return true
	}
	return false
}

func copyFile(src, dst string) error {
	//nolint:gosec // the operator's own restore argument
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	_ = os.Remove(dst)
	//nolint:gosec // dbPath + ".restoring"
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, fileMode)
	if err != nil {
		return fmt.Errorf("create %s: %w", dst, err)
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return fmt.Errorf("copy %s: %w", src, err)
	}
	if err := out.Sync(); err != nil {
		_ = out.Close()
		return fmt.Errorf("sync %s: %w", dst, err)
	}
	return out.Close()
}
