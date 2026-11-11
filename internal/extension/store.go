package extension

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

const storePoolSize = 4

var ErrCoreInMemory = errors.New(
	"extension store needs a core database file, not an in-memory one",
)

const errResultTooLarge = "result larger than 16 MiB; page the query"

type Store struct {
	db *sql.DB
}

type Conn struct {
	c *sql.Conn
}

type storeConnector struct {
	drv *sqlite.Driver
	dsn string
}

func (c storeConnector) Connect(context.Context) (driver.Conn, error) {
	return c.drv.Open(c.dsn)
}

func (c storeConnector) Driver() driver.Driver { return c.drv }

func OpenStore(
	ctx context.Context,
	extDBPath, coreDBPath string,
	views []View,
) (*Store, error) {
	if isInMemory(coreDBPath) {
		return nil, ErrCoreInMemory
	}
	if err := os.MkdirAll(filepath.Dir(extDBPath), 0o700); err != nil {
		return nil, fmt.Errorf("create extension data dir: %w", err)
	}

	drv := &sqlite.Driver{}
	drv.RegisterConnectionHook(connectionHook(coreDBPath, views))

	db := sql.OpenDB(storeConnector{drv: drv, dsn: storeDSN(extDBPath)})
	db.SetMaxOpenConns(storePoolSize)
	db.SetMaxIdleConns(storePoolSize)

	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("open extension store: %w", err)
	}
	return &Store{db: db}, nil
}

func connectionHook(coreDBPath string, views []View) sqlite.ConnectionHookFn {
	attach := "ATTACH DATABASE '" +
		strings.ReplaceAll(coreURI(coreDBPath), "'", "''") +
		"' AS " + coreSchema
	policy := newPolicy(views)

	return func(c sqlite.ExecQuerierContext, _ string) error {
		ctx := context.Background()
		if _, err := c.ExecContext(ctx, attach, nil); err != nil {
			return fmt.Errorf("attach core database: %w", err)
		}
		for _, v := range views {
			stmt := "CREATE TEMP VIEW " + v.viewName() + " AS " + v.SQL
			if _, err := c.ExecContext(ctx, stmt, nil); err != nil {
				return fmt.Errorf("create view %s: %w", v.viewName(), err)
			}
		}
		reg, ok := c.(sqlite.AuthorizerRegisterer)
		if !ok {
			return errors.New("sqlite driver connection has no authorizer")
		}
		return reg.RegisterAuthorizer(policy)
	}
}

var uriEscaper = strings.NewReplacer("%", "%25", "?", "%3f", "#", "%23")

func coreURI(path string) string {
	return "file:" + uriEscaper.Replace(path) + "?mode=ro"
}

func storeDSN(path string) string {
	return "file:" + uriEscaper.Replace(path) +
		"?_pragma=journal_mode(WAL)" +
		"&_pragma=synchronous(NORMAL)" +
		"&_pragma=busy_timeout(5000)" +
		"&_pragma=foreign_keys(1)" +
		"&_timezone=UTC"
}

func isInMemory(path string) bool {
	return path == "" || path == ":memory:" || path == "memory" ||
		strings.Contains(path, "mode=memory")
}

func (s *Store) Conn(ctx context.Context) (*Conn, error) {
	c, err := s.db.Conn(ctx)
	if err != nil {
		return nil, fmt.Errorf("take extension store connection: %w", err)
	}
	// One value a guest builds (zeroblob, a doubling concatenation) is
	// allocated in the host before any row reaches the result cap.
	_, err = sqlite.Limit(c, sqlite3.SQLITE_LIMIT_LENGTH, MaxResponseBody)
	if err != nil {
		return nil, errors.Join(
			fmt.Errorf("limit extension store connection: %w", err),
			c.Close(),
		)
	}
	return &Conn{c: c}, nil
}

func (s *Store) Close() error { return s.db.Close() }

func statementCtx(ctx context.Context) (context.Context, context.CancelFunc) {
	if dl, ok := ctx.Deadline(); ok && time.Until(dl) <= SQLTimeout {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, SQLTimeout)
}

func (c *Conn) Query(
	ctx context.Context,
	query string,
	args []any,
) (SQLResult, error) {
	ctx, cancel := statementCtx(ctx)
	defer cancel()

	rows, err := c.c.QueryContext(ctx, query, args...)
	if err != nil {
		return SQLResult{Error: err.Error()}, nil
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		return SQLResult{Error: err.Error()}, nil
	}
	res := SQLResult{Columns: cols, Rows: [][]any{}}
	size := 0
	for rows.Next() {
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return SQLResult{Error: err.Error()}, nil
		}
		for i, v := range vals {
			switch t := v.(type) {
			case time.Time:
				vals[i] = t.Format(time.RFC3339Nano)
			case string:
				size += len(t)
			case []byte:
				size += len(t)
			}
			size += 8
		}
		if size > MaxResponseBody {
			return SQLResult{Error: errResultTooLarge}, nil
		}
		res.Rows = append(res.Rows, vals)
	}
	if err := rows.Err(); err != nil {
		return SQLResult{Error: err.Error()}, nil
	}
	return res, nil
}

func (c *Conn) Exec(
	ctx context.Context,
	query string,
	args []any,
) (SQLResult, error) {
	ctx, cancel := statementCtx(ctx)
	defer cancel()

	r, err := c.c.ExecContext(ctx, query, args...)
	if err != nil {
		return SQLResult{Error: err.Error()}, nil
	}
	var res SQLResult
	if res.RowsAffected, err = r.RowsAffected(); err != nil {
		return SQLResult{Error: err.Error()}, nil
	}
	if res.LastInsertID, err = r.LastInsertId(); err != nil {
		return SQLResult{Error: err.Error()}, nil
	}
	return res, nil
}

// Release rolls back what a guest left open. A connection that cannot be
// rolled back is discarded rather than pooled holding a transaction.
func (c *Conn) Release() error {
	ctx, cancel := context.WithTimeout(context.Background(), SQLTimeout)
	defer cancel()

	_, err := c.c.ExecContext(ctx, "ROLLBACK")
	if err != nil && !strings.Contains(err.Error(), "no transaction is active") {
		rawErr := c.c.Raw(func(any) error { return driver.ErrBadConn })
		closeErr := c.c.Close()
		return errors.Join(fmt.Errorf("roll back extension transaction: %w", err),
			ignoreBadConn(rawErr), closeErr)
	}
	return c.c.Close()
}

func ignoreBadConn(err error) error {
	if errors.Is(err, driver.ErrBadConn) {
		return nil
	}
	return err
}
