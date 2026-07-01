package store

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"log"

	_ "github.com/jackc/pgx/v5/stdlib" // registers the "pgx" driver with database/sql for Postgres
	"github.com/pressly/goose/v3"
	_ "modernc.org/sqlite" // registers the pure-Go "sqlite" driver with database/sql
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// DB wraps sql.DB with the driver name so Migrate can select the right dialect.
type DB struct {
	*sql.DB
	driver string
}

// Open opens a database connection and verifies it is reachable.
// driver is "sqlite" or "pgx"; dsn is the data source name.
func Open(driver, dsn string) (*DB, error) {
	db, err := sql.Open(driver, dsn)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("ping db: %w", err)
	}
	if driver == "sqlite" {
		// Foreign key enforcement is off by default in SQLite.
		// WAL mode improves concurrent read performance.
		if _, err := db.Exec("PRAGMA foreign_keys = ON; PRAGMA journal_mode = WAL; PRAGMA busy_timeout = 5000;"); err != nil {
			return nil, fmt.Errorf("sqlite pragmas: %w", err)
		}
	}
	return &DB{DB: db, driver: driver}, nil
}

// SQLDB exposes the underlying *sql.DB for callers (typically the
// enterprise edition bootstrap) that need to run additional migrations
// or direct queries against the same database handle.
func (d *DB) SQLDB() *sql.DB {
	return d.DB
}

// Migrate runs all pending goose migrations embedded in the binary.
//
// On SQLite we temporarily disable foreign_keys for the duration of the run
// because table-rebuild migrations (e.g. 00011 agents→workloads) must drop
// and recreate parent tables; with foreign_keys=ON those DROPs abort.
// `PRAGMA foreign_keys` is a no-op inside a transaction, so it must be
// toggled at the connection level before goose opens its per-migration tx.
// Re-enabled on exit, including on migration failure.
func (d *DB) Migrate() error {
	fsys, err := fs.Sub(migrationsFS, "migrations")
	if err != nil {
		return fmt.Errorf("migrations fs: %w", err)
	}

	dialect := goose.DialectSQLite3
	if d.driver == "pgx" {
		dialect = goose.DialectPostgres
	}

	if d.driver == "sqlite" {
		if _, err := d.Exec("PRAGMA foreign_keys = OFF;"); err != nil {
			return fmt.Errorf("disable foreign_keys for migration: %w", err)
		}
		defer func() {
			if _, err := d.Exec("PRAGMA foreign_keys = ON;"); err != nil {
				log.Printf("re-enable foreign_keys after migration: %v", err)
			}
		}()
	}

	provider, err := goose.NewProvider(dialect, d.DB, fsys)
	if err != nil {
		return fmt.Errorf("goose provider: %w", err)
	}

	if _, err := provider.Up(context.Background()); err != nil {
		return fmt.Errorf("goose up: %w", err)
	}
	if err := d.sanitizeLegacyRemoteConfigStatuses(); err != nil {
		return err
	}
	return nil
}
