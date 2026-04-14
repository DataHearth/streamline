//go:build ignore

// Versioned-migration generator. Run via:
//
//	go run -mod=mod ./ent/migrate <name>
//
// Diffs the current ent schema against the replayed migration history and
// writes a new pair of golang-migrate files (<ts>_<name>.up.sql and
// <ts>_<name>.down.sql) into internal/db/migrations/.
package main

import (
	"context"
	"database/sql"
	"log"
	"os"

	"github.com/datahearth/streamline/ent/migrate"

	"ariga.io/atlas/sql/sqltool"
	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/sql/schema"

	// Atlas hard-codes the sqlite3 driver name when opening the dev URL;
	// alias modernc.org/sqlite (already imported by the runtime) under that
	// name so the generator stays CGO-free.
	_ "ariga.io/atlas/sql/sqlite"
	sqlitedrv "modernc.org/sqlite"
)

const migrationsDir = "internal/db/migrations"

func init() {
	sql.Register("sqlite3", &sqlitedrv.Driver{})
}

func main() {
	if len(os.Args) != 2 {
		log.Fatalln("usage: go run -mod=mod ./ent/migrate <name>")
	}
	name := os.Args[1]

	if err := os.MkdirAll(migrationsDir, 0o755); err != nil {
		log.Fatalf("create migrations dir: %v", err)
	}
	dir, err := sqltool.NewGolangMigrateDir(migrationsDir)
	if err != nil {
		log.Fatalf("open migrations dir: %v", err)
	}

	opts := []schema.MigrateOption{
		schema.WithDir(dir),
		schema.WithMigrationMode(schema.ModeReplay),
		schema.WithDialect(dialect.SQLite),
		schema.WithFormatter(sqltool.GolangMigrateFormatter),
		schema.WithDropIndex(true),
		schema.WithDropColumn(true),
	}

	// Throwaway in-memory dev DB for replay; named so multiple pool conns
	// share state.
	devURL := "sqlite://atlas-dev?mode=memory&cache=shared&_pragma=foreign_keys(1)"
	if err := migrate.NamedDiff(
		context.Background(),
		devURL,
		name,
		opts...); err != nil {
		log.Fatalf("diff: %v", err)
	}
}
