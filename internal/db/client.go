package db

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"log/slog"
	"path/filepath"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/XSAM/otelsql"
	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/dbbackup"
	semconv "go.opentelemetry.io/otel/semconv/v1.40.0"

	_ "modernc.org/sqlite"
)

// Open opens the SQLite database with OTel instrumentation wired via otelsql.
// Every ent query becomes a child span with db.* semconv attributes, and
// connection-pool stats are registered with the global meter provider.
func Open(ctx context.Context, dsn string) (*ent.Client, error) {
	// busy_timeout makes concurrent writers wait up to N ms on a lock instead
	// of immediately returning SQLITE_BUSY — required under WAL when multiple
	// goroutines (e.g. async session touches + request handlers) serialize
	// through the single writer.
	// _timezone=UTC normalizes time.Time values to UTC at bind/scan, so
	// SQLite's TEXT-based comparisons (e.g. WHERE create_time < ?) line up
	// across rows regardless of the caller's local timezone.
	var connStr string
	memory := dsn == ":memory:" || dsn == "memory"
	if !memory {
		connStr = "file:" + dsn +
			"?_pragma=journal_mode(WAL)" +
			"&_pragma=synchronous(NORMAL)" +
			"&_pragma=foreign_keys(1)" +
			"&_pragma=busy_timeout(5000)" +
			// Negative cache_size is KiB rather than pages: 8 MB of page
			// cache against a ~2 MB default. modernc has no mmap, so every
			// cache miss is a real pread through the pure-Go VFS — profiling
			// a 610-movie library spent 7.4s of a 45s load window in pread
			// alone. 8 MB is a bounded, affordable trade at a 256 MB target.
			"&_pragma=cache_size(-8000)" +
			// The WAL is truncated back to this after a checkpoint. Without
			// it the file only ever grows to its high-water mark and stays
			// there, which on an install doing steady bookkeeping writes
			// means a permanently large WAL for a transient burst.
			"&_pragma=journal_size_limit(67108864)" +
			"&_timezone=UTC"
	} else {
		// Named + shared-cache so every pool connection sees the same DB.
		// Anonymous `file:?mode=memory` is private-per-conn — second conn
		// sees zero tables. Unique name prevents cross-Open bleed.
		var b [8]byte
		_, _ = rand.Read(b[:])
		connStr = "file:mem_" + hex.EncodeToString(b[:]) +
			"?mode=memory&cache=shared" +
			"&_pragma=foreign_keys(1)" +
			"&_pragma=busy_timeout(5000)" +
			"&_timezone=UTC"
	}

	// otelsql wraps every driver call to record a span and a metric. Against
	// the no-op instruments an unconfigured install gets, that work is still
	// done in full — an attribute set allocated and sorted per call — and the
	// scheduler alone issues thousands of statements an hour while idle. With
	// no endpoint there is nothing downstream to receive any of it, so the
	// raw driver is opened instead.
	//
	// Read once, here: observability.Setup makes the same call on the same key
	// at boot, so a config change to otel.endpoint needs a restart either way.
	instrumented := config.Get() != nil && config.Get().OTel.Endpoint != ""

	var db *sql.DB
	var err error
	if instrumented {
		db, err = otelsql.Open("sqlite", connStr,
			otelsql.WithAttributes(semconv.DBSystemNameSQLite),
			otelsql.WithSpanOptions(otelsql.SpanOptions{
				OmitConnResetSession: true,
				OmitConnPrepare:      true,
				OmitRows:             true,
			}),
		)
	} else {
		db, err = sql.Open("sqlite", connStr)
	}
	if err != nil {
		return nil, err
	}

	if memory {
		// Shared-cache memory DB lives only while ≥1 conn is open. Pin an
		// idle conn so the pool never drains and wipes the DB.
		db.SetMaxIdleConns(1)
		db.SetConnMaxIdleTime(0)
	} else {
		// SQLite has a single writer. Cap the pool to one connection so all
		// access serializes in Go's pool instead of racing for the write lock
		// and returning SQLITE_BUSY ("database is locked") under concurrent
		// writes (scheduled-job burst + session touches), which busy_timeout
		// alone can't cover on slow (network) storage.
		db.SetMaxOpenConns(1)
	}

	if instrumented {
		if _, err := otelsql.RegisterDBStatsMetrics(db,
			otelsql.WithAttributes(semconv.DBSystemNameSQLite),
		); err != nil {
			db.Close()
			return nil, fmt.Errorf("register db stats metrics: %w", err)
		}
	}

	if !memory {
		if err := runMigrations(ctx, db, preMigrationSnapshot(dsn, db)); err != nil {
			db.Close()
			return nil, fmt.Errorf("apply migrations: %w", err)
		}
		// sqlite_stat1 is otherwise never populated, leaving the planner to
		// guess on every multi-index and subquery decision. optimize ANALYZEs
		// only the tables whose stats are missing or stale, which is why it is
		// safe here: on a settled database it does nothing, and right after a
		// migration that added indexes it does exactly the work needed.
		// Failure is not fatal — bad statistics are slow, not wrong.
		if _, err := db.ExecContext(ctx, "PRAGMA optimize"); err != nil {
			slog.WarnContext(ctx, "PRAGMA optimize failed", "error", err)
		}
	}

	drv := entsql.OpenDB(dialect.SQLite, db)
	client := ent.NewClient(ent.Driver(drv))

	if memory {
		// In-memory DBs are throwaway per-test fixtures; bypass versioned
		// migrations and rely on ent's auto-migrate for speed and parity
		// with previous test setup.
		if err := client.Schema.Create(ctx); err != nil {
			client.Close()
			return nil, err
		}
	}

	return client, nil
}

// preMigrationSnapshot returns the hook runMigrations calls before applying
// anything: a VACUUM INTO copy under <data_dir>/backups, then the pre-migration
// retention pass. Nil when backup.pre_migration is off. Without a loaded config
// (tests, tools) it follows the defaults: snapshot, keep five.
//
// A failed snapshot fails the boot rather than migrating without one. That is
// the point of it: with automated image updates nobody is there to take a copy
// by hand, and a bad migration would leave nothing to roll back to.
func preMigrationSnapshot(
	dsn string,
	db *sql.DB,
) func(context.Context, uint) error {
	keep := 5
	if cfg := config.Get(); cfg != nil {
		if !cfg.Backup.PreMigration {
			return nil
		}
		keep = int(cfg.Backup.Retention.PreMigration)
	}
	dir := dbbackup.Dir(filepath.Dir(dsn))
	return func(ctx context.Context, from uint) error {
		slog.InfoContext(ctx, "backing up the database before migrating",
			"schema.version_before", from, "backup.dir", dir)
		b, err := dbbackup.Snapshot(ctx, db, dir, dbbackup.KindPreMigration, from)
		if err != nil {
			return fmt.Errorf(
				"back up the database before migrating (nothing was migrated; "+
					"check free space and permissions in %s, or set backup.pre_migration: false "+
					"to migrate without a copy): %w",
				dir, err)
		}
		slog.InfoContext(ctx, "database backed up before migrating",
			"backup.path", b.Path, "backup.size_bytes", b.Size)
		removed, err := dbbackup.Prune(dir, dbbackup.KindPreMigration, keep)
		if err != nil {
			slog.WarnContext(
				ctx,
				"could not prune pre-migration backups",
				"error",
				err,
			)
		}
		if len(removed) > 0 {
			slog.InfoContext(ctx, "pruned pre-migration backups", "removed", removed)
		}
		return nil
	}
}
