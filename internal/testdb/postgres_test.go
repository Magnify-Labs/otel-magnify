package testdb

import (
	"database/sql"
	"os"
	"strconv"
	"testing"
)

func TestServerMajorVersion(t *testing.T) {
	want := os.Getenv("TEST_POSTGRES_MAJOR")
	if want == "" {
		t.Skip("TEST_POSTGRES_MAJOR is not set")
	}
	database := New(t)
	db, err := sql.Open("pgx", database.DSN)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("close database: %v", err)
		}
	})

	var got int
	if err := db.QueryRow(
		`SELECT current_setting('server_version_num')::integer / 10000`,
	).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if strconv.Itoa(got) != want {
		t.Fatalf("PostgreSQL major = %d, want %s", got, want)
	}
}
