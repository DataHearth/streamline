package db

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"log/slog"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/XSAM/otelsql"
	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/internal/config"
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
		if err := runMigrations(db); err != nil {
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
