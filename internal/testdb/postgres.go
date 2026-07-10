// Package testdb provides isolated PostgreSQL schemas for tests.
package testdb

import (
	"crypto/rand"
	"database/sql"
	"net/url"
	"os"
	"strings"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"
)

const schemaSuffixLength = 10

// Database identifies an isolated PostgreSQL schema and a DSN scoped to it.
type Database struct {
	DSN    string
	Schema string
}

// New creates an isolated PostgreSQL schema for a test and schedules its removal.
func New(t *testing.T) Database {
	t.Helper()

	baseDSN := os.Getenv("TEST_POSTGRES_DSN")
	if baseDSN == "" {
		t.Fatal("TEST_POSTGRES_DSN must be set")
	}

	schema, err := schemaName(t.Name())
	if err != nil {
		t.Fatalf("create PostgreSQL test schema name: %v", err)
	}

	db, err := sql.Open("pgx", baseDSN)
	if err != nil {
		t.Fatalf("open PostgreSQL test database: %v", err)
	}
	if _, err := db.Exec("CREATE SCHEMA " + quoteIdentifier(schema)); err != nil {
		_ = db.Close()
		t.Fatalf("create PostgreSQL test schema: %v", err)
	}
	t.Cleanup(func() {
		if _, err := db.Exec("DROP SCHEMA IF EXISTS " + quoteIdentifier(schema) + " CASCADE"); err != nil {
			t.Errorf("drop PostgreSQL test schema: %v", err)
		}
		_ = db.Close()
	})

	return Database{DSN: withSearchPath(t, baseDSN, schema), Schema: schema}
}

func schemaName(testName string) (string, error) {
	var b strings.Builder
	for _, ch := range strings.ToLower(testName) {
		if ch >= 'a' && ch <= 'z' || ch >= '0' && ch <= '9' {
			b.WriteRune(ch)
		} else {
			b.WriteByte('_')
		}
	}
	name := strings.Trim(b.String(), "_")
	if name == "" {
		name = "case"
	}
	if len(name) > 45 {
		name = name[:45]
	}

	suffix, err := randomSuffix(schemaSuffixLength)
	if err != nil {
		return "", err
	}
	return "test_" + name + "_" + suffix, nil
}

func randomSuffix(length int) (string, error) {
	const alphabet = "abcdefghijklmnopqrstuvwxyz0123456789"
	bytes := make([]byte, length)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	for i := range bytes {
		bytes[i] = alphabet[int(bytes[i])%len(alphabet)]
	}
	return string(bytes), nil
}

func withSearchPath(t *testing.T, dsn, schema string) string {
	t.Helper()
	parsed, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("parse TEST_POSTGRES_DSN: %v", err)
	}
	query := parsed.Query()
	query.Set("search_path", schema+",public")
	parsed.RawQuery = query.Encode()
	return parsed.String()
}

func quoteIdentifier(identifier string) string {
	return `"` + strings.ReplaceAll(identifier, `"`, `""`) + `"`
}
