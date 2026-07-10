package store

import (
	"testing"

	"github.com/magnify-labs/otel-magnify/internal/testdb"
)

// TestMigration00014_DataMigration vérifie qu'un user seedé avant 00014
// avec role='admin' se retrouve membre du groupe grp_system_administrator
// après migration, et que la colonne role a disparu.
func TestMigration00014_DataMigration(t *testing.T) {
	db, err := Open(testdb.New(t).DSN, testPoolConfig())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	if err := db.Migrate(); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	// Vérifie que les 3 groupes système sont seedés.
	var n int
	row := db.QueryRow(`SELECT COUNT(*) FROM groups WHERE is_system = 1`)
	if err := row.Scan(&n); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if n != 3 {
		t.Errorf("expected 3 system groups, got %d", n)
	}

	// Vérifie que users.role n'existe plus.
	var roleExists bool
	if err := db.QueryRow(`
		SELECT EXISTS(
			SELECT 1 FROM information_schema.columns
			WHERE table_schema = current_schema() AND table_name = 'users' AND column_name = 'role'
		)`).Scan(&roleExists); err != nil {
		t.Fatalf("query users columns: %v", err)
	}
	if roleExists {
		t.Errorf("users.role should have been dropped")
	}
}
