package db

import (
	"context"
	"errors"
	"strings"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
)

// router splits ent's statements over two pools: one connection for every
// write and every transaction, and a query_only pool for autocommit reads, so
// a read never queues behind a write waiting on slow storage. WAL lets the
// readers run beside the writer, and each read sees everything committed
// before it started.
type router struct {
	write *entsql.Driver
	read  *entsql.Driver
}

// isRead reports whether a statement may run on the read pool. ent sends
// INSERT … RETURNING through Query too, so the method says nothing; the
// statement does. A write that slips through fails on query_only rather than
// racing the writer.
func isRead(query string) bool {
	q := strings.TrimLeft(query, " \t\r\n")
	return len(q) >= 6 && strings.EqualFold(q[:6], "SELECT")
}

func (r router) Query(ctx context.Context, query string, args, v any) error {
	if isRead(query) {
		return r.read.Query(ctx, query, args, v)
	}
	return r.write.Query(ctx, query, args, v)
}

func (r router) Exec(ctx context.Context, query string, args, v any) error {
	return r.write.Exec(ctx, query, args, v)
}

func (r router) Tx(ctx context.Context) (dialect.Tx, error) {
	return r.write.Tx(ctx)
}

// BeginTx is what ent.Client.BeginTx type-asserts; without it that call panics.
func (r router) BeginTx(
	ctx context.Context,
	opts *entsql.TxOptions,
) (dialect.Tx, error) {
	return r.write.BeginTx(ctx, opts)
}

func (r router) Close() error {
	return errors.Join(r.read.Close(), r.write.Close())
}

func (r router) Dialect() string {
	return dialect.SQLite
}
