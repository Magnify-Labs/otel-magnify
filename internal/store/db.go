package store

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"strconv"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib" // registers the "pgx" driver with database/sql for Postgres
	"github.com/pressly/goose/v3"
	"github.com/pressly/goose/v3/lock"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

const (
	migrationLockRetrySeconds       = uint64(1)
	migrationLockFailureThreshold   = uint64(30)
	migrationUnlockFailureThreshold = uint64(5)
)

// PoolConfig controls the database connection pool.
type PoolConfig struct {
	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxIdleTime time.Duration
	ConnMaxLifetime time.Duration
}

// DB wraps sql.DB and rebinds community store queries to PostgreSQL parameters.
type DB struct {
	*sql.DB
}

// Open opens a database connection and verifies it is reachable.
func Open(dsn string, pool PoolConfig) (*DB, error) {
	return OpenContext(context.Background(), dsn, pool)
}

// OpenContext opens a database connection and verifies it is reachable within the caller's context.
func OpenContext(ctx context.Context, dsn string, pool PoolConfig) (*DB, error) {
	if strings.TrimSpace(dsn) == "" {
		return nil, fmt.Errorf("DB_DSN environment variable is required")
	}
	if pool.MaxOpenConns <= 0 {
		return nil, fmt.Errorf("MaxOpenConns must be greater than 0")
	}
	if pool.MaxIdleConns < 0 {
		return nil, fmt.Errorf("MaxIdleConns must be non-negative")
	}
	if pool.MaxIdleConns > pool.MaxOpenConns {
		return nil, fmt.Errorf("MaxIdleConns must not exceed MaxOpenConns")
	}

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	db.SetMaxOpenConns(pool.MaxOpenConns)
	db.SetMaxIdleConns(pool.MaxIdleConns)
	db.SetConnMaxIdleTime(pool.ConnMaxIdleTime)
	db.SetConnMaxLifetime(pool.ConnMaxLifetime)
	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := db.PingContext(pingCtx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping db: %w", err)
	}
	return &DB{DB: db}, nil
}

// SQLDB exposes the underlying *sql.DB for callers (typically the
// enterprise edition bootstrap) that need to run additional migrations
// or direct queries against the same database handle.
func (d *DB) SQLDB() *sql.DB {
	return d.DB
}

// Exec executes a query after converting question-mark placeholders to PostgreSQL parameters.
func (d *DB) Exec(query string, args ...any) (sql.Result, error) {
	return d.DB.Exec(rebind(query), args...)
}

// Query executes a query after converting question-mark placeholders to PostgreSQL parameters.
func (d *DB) Query(query string, args ...any) (*sql.Rows, error) {
	return d.DB.Query(rebind(query), args...)
}

// QueryRow executes a query after converting question-mark placeholders to PostgreSQL parameters.
func (d *DB) QueryRow(query string, args ...any) *sql.Row {
	return d.DB.QueryRow(rebind(query), args...)
}

// ExecPostgres executes a PostgreSQL query with native $n placeholders.
func (d *DB) ExecPostgres(query string, args ...any) (sql.Result, error) {
	return d.DB.Exec(query, args...)
}

// QueryPostgres executes a PostgreSQL query with native $n placeholders.
func (d *DB) QueryPostgres(query string, args ...any) (*sql.Rows, error) {
	return d.DB.Query(query, args...)
}

// QueryRowPostgres executes a PostgreSQL query with native $n placeholders.
func (d *DB) QueryRowPostgres(query string, args ...any) *sql.Row {
	return d.DB.QueryRow(query, args...)
}

// Begin starts a transaction whose query methods rebind PostgreSQL parameters.
func (d *DB) Begin() (*Tx, error) {
	tx, err := d.DB.Begin()
	if err != nil {
		return nil, err
	}
	return &Tx{Tx: tx}, nil
}

// Tx wraps sql.Tx and rebinds community store queries to PostgreSQL parameters.
type Tx struct {
	*sql.Tx
}

type queryRower interface {
	QueryRow(query string, args ...any) *sql.Row
}

// Exec executes a query after converting question-mark placeholders to PostgreSQL parameters.
func (tx *Tx) Exec(query string, args ...any) (sql.Result, error) {
	return tx.Tx.Exec(rebind(query), args...)
}

// Query executes a query after converting question-mark placeholders to PostgreSQL parameters.
func (tx *Tx) Query(query string, args ...any) (*sql.Rows, error) {
	return tx.Tx.Query(rebind(query), args...)
}

// QueryRow executes a query after converting question-mark placeholders to PostgreSQL parameters.
func (tx *Tx) QueryRow(query string, args ...any) *sql.Row {
	return tx.Tx.QueryRow(rebind(query), args...)
}

// ExecPostgres executes a PostgreSQL query with native $n placeholders.
func (tx *Tx) ExecPostgres(query string, args ...any) (sql.Result, error) {
	return tx.Tx.Exec(query, args...)
}

// QueryPostgres executes a PostgreSQL query with native $n placeholders.
func (tx *Tx) QueryPostgres(query string, args ...any) (*sql.Rows, error) {
	return tx.Tx.Query(query, args...)
}

// QueryRowPostgres executes a PostgreSQL query with native $n placeholders.
func (tx *Tx) QueryRowPostgres(query string, args ...any) *sql.Row {
	return tx.Tx.QueryRow(query, args...)
}

func rebind(query string) string {
	var b strings.Builder
	b.Grow(len(query))

	placeholder := 1
	inSingleQuote := false
	inDoubleQuote := false
	inLineComment := false
	inBlockComment := false

	for i := 0; i < len(query); i++ {
		ch := query[i]
		next := byte(0)
		if i+1 < len(query) {
			next = query[i+1]
		}

		switch {
		case inLineComment:
			b.WriteByte(ch)
			if ch == '\n' {
				inLineComment = false
			}
		case inBlockComment:
			b.WriteByte(ch)
			if ch == '*' && next == '/' {
				b.WriteByte(next)
				i++
				inBlockComment = false
			}
		case inSingleQuote:
			b.WriteByte(ch)
			if ch == '\'' {
				if next == '\'' {
					b.WriteByte(next)
					i++
				} else {
					inSingleQuote = false
				}
			}
		case inDoubleQuote:
			b.WriteByte(ch)
			if ch == '"' {
				if next == '"' {
					b.WriteByte(next)
					i++
				} else {
					inDoubleQuote = false
				}
			}
		case ch == '-' && next == '-':
			b.WriteByte(ch)
			b.WriteByte(next)
			i++
			inLineComment = true
		case ch == '/' && next == '*':
			b.WriteByte(ch)
			b.WriteByte(next)
			i++
			inBlockComment = true
		case ch == '\'':
			b.WriteByte(ch)
			inSingleQuote = true
		case ch == '"':
			b.WriteByte(ch)
			inDoubleQuote = true
		case ch == '?':
			b.WriteByte('$')
			b.WriteString(strconv.Itoa(placeholder))
			placeholder++
		default:
			b.WriteByte(ch)
		}
	}

	return b.String()
}

// Migrate runs all pending goose migrations embedded in the binary.
func (d *DB) Migrate() error {
	return d.MigrateContext(context.Background())
}

// MigrateContext runs all pending goose migrations within the caller's context.
func (d *DB) MigrateContext(ctx context.Context) error {
	locker, err := newMigrationLocker(migrationLockFailureThreshold)
	if err != nil {
		return fmt.Errorf("goose migration locker: %w", err)
	}
	return d.migrate(ctx, locker)
}

func newMigrationLocker(failureThreshold uint64) (lock.SessionLocker, error) {
	return lock.NewPostgresSessionLocker(
		lock.WithLockTimeout(migrationLockRetrySeconds, failureThreshold),
		lock.WithUnlockTimeout(migrationLockRetrySeconds, migrationUnlockFailureThreshold),
	)
}

func (d *DB) migrate(ctx context.Context, locker lock.SessionLocker) error {
	fsys, err := fs.Sub(migrationsFS, "migrations")
	if err != nil {
		return fmt.Errorf("migrations fs: %w", err)
	}

	provider, err := goose.NewProvider(
		goose.DialectPostgres,
		d.DB,
		fsys,
		goose.WithGoMigrations(migration00026),
		goose.WithSessionLocker(locker),
	)
	if err != nil {
		return fmt.Errorf("goose provider: %w", err)
	}

	if _, err := provider.Up(ctx); err != nil {
		return fmt.Errorf("goose up: %w", err)
	}
	return nil
}
