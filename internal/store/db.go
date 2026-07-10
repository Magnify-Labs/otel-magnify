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
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// PoolConfig controls the database connection pool.
type PoolConfig struct {
	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime time.Duration
}

// DB wraps sql.DB and rebinds community store queries to PostgreSQL parameters.
type DB struct {
	*sql.DB
}

// Open opens a database connection and verifies it is reachable.
func Open(dsn string, pool PoolConfig) (*DB, error) {
	if strings.TrimSpace(dsn) == "" {
		return nil, fmt.Errorf("DB_DSN environment variable is required")
	}

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	db.SetMaxOpenConns(pool.MaxOpenConns)
	db.SetMaxIdleConns(pool.MaxIdleConns)
	db.SetConnMaxLifetime(pool.ConnMaxLifetime)
	if err := db.Ping(); err != nil {
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
	fsys, err := fs.Sub(migrationsFS, "migrations")
	if err != nil {
		return fmt.Errorf("migrations fs: %w", err)
	}

	provider, err := goose.NewProvider(goose.DialectPostgres, d.DB, fsys)
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
