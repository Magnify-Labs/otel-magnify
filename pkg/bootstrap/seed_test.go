package bootstrap

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	"github.com/magnify-labs/otel-magnify/internal/store"
	"github.com/magnify-labs/otel-magnify/internal/testdb"
	"github.com/magnify-labs/otel-magnify/pkg/models"
)

func TestSeedAdmin_AttachesAdministratorGroup(t *testing.T) {
	t.Setenv("SEED_ADMIN_EMAIL", "ops@example.com")
	t.Setenv("SEED_ADMIN_PASSWORD", "verylongpassword1234")

	db, err := store.Open(testdb.New(t).DSN, testPoolConfig())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := db.Migrate(); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	if err := seedAdmin(db); err != nil {
		t.Fatalf("seedAdmin: %v", err)
	}

	user, err := db.GetUserByEmail("ops@example.com")
	if err != nil {
		t.Fatalf("GetUserByEmail: %v", err)
	}
	if _, err := uuid.Parse(user.ID); err != nil {
		t.Fatalf("seeded user ID %q is not a UUID: %v", user.ID, err)
	}
	groups, err := db.GetUserGroups(user.ID)
	if err != nil {
		t.Fatalf("GetUserGroups: %v", err)
	}
	if len(groups) != 1 || groups[0].Name != "administrator" {
		t.Fatalf("expected [administrator], got %v", groups)
	}
}

func TestSeedAdmin_NoCredentialsIsANoop(t *testing.T) {
	t.Setenv("SEED_ADMIN_EMAIL", "")
	t.Setenv("SEED_ADMIN_PASSWORD", "")

	db := newSeedTestDB(t)
	if err := seedAdmin(db); err != nil {
		t.Fatalf("seedAdmin: %v", err)
	}
}

func TestSeedAdmin_RejectsPartialCredentials(t *testing.T) {
	tests := []struct {
		name     string
		email    string
		password string
		missing  string
	}{
		{name: "missing email", password: "long-enough-initial-password", missing: "SEED_ADMIN_EMAIL"},
		{name: "missing password", email: "ops@example.com", missing: "SEED_ADMIN_PASSWORD"},
		{name: "whitespace email", email: "  ", password: "long-enough-initial-password", missing: "SEED_ADMIN_EMAIL"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("SEED_ADMIN_EMAIL", tt.email)
			t.Setenv("SEED_ADMIN_PASSWORD", tt.password)
			db := newSeedTestDB(t)

			err := seedAdmin(db)
			if err == nil || !strings.Contains(err.Error(), tt.missing) {
				t.Fatalf("seedAdmin error = %v, want missing %s", err, tt.missing)
			}
		})
	}
}

func TestSeedAdmin_RejectsShortInitialPassword(t *testing.T) {
	t.Setenv("SEED_ADMIN_EMAIL", "ops@example.com")
	t.Setenv("SEED_ADMIN_PASSWORD", "too-short")
	db := newSeedTestDB(t)

	err := seedAdmin(db)
	if err == nil || !strings.Contains(err.Error(), "at least 12 characters") {
		t.Fatalf("seedAdmin error = %v, want minimum password length error", err)
	}
}

func TestSeedAdmin_IsIdempotentAndDoesNotResetRotatedPassword(t *testing.T) {
	t.Setenv("SEED_ADMIN_EMAIL", "ops@example.com")
	t.Setenv("SEED_ADMIN_PASSWORD", "initial-password-long-enough")
	db := newSeedTestDB(t)

	if err := seedAdmin(db); err != nil {
		t.Fatalf("first seedAdmin: %v", err)
	}
	user, err := db.GetUserByEmail("ops@example.com")
	if err != nil {
		t.Fatalf("GetUserByEmail: %v", err)
	}
	rotatedHash, err := bcrypt.GenerateFromPassword([]byte("rotated-password-long-enough"), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("GenerateFromPassword: %v", err)
	}
	user.PasswordHash = string(rotatedHash)
	if err := db.UpdateUser(user); err != nil {
		t.Fatalf("UpdateUser: %v", err)
	}

	if err := seedAdmin(db); err != nil {
		t.Fatalf("second seedAdmin: %v", err)
	}
	got, err := db.GetUserByEmail("ops@example.com")
	if err != nil {
		t.Fatalf("GetUserByEmail after second seed: %v", err)
	}
	if bcrypt.CompareHashAndPassword([]byte(got.PasswordHash), []byte("rotated-password-long-enough")) != nil {
		t.Fatal("second seed reset the rotated administrator password")
	}
}

func TestSeedAdmin_PropagatesNonEmptyDatabaseError(t *testing.T) {
	t.Setenv("SEED_ADMIN_EMAIL", "ops@example.com")
	t.Setenv("SEED_ADMIN_PASSWORD", "initial-password-long-enough")
	db := newSeedTestDB(t)
	if err := db.CreateUser(models.User{
		ID:           uuid.NewString(),
		Email:        "existing@example.invalid",
		PasswordHash: "existing-password-hash",
	}); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	err := seedAdmin(db)
	if err == nil || !strings.Contains(err.Error(), "database already contains a user") {
		t.Fatalf("seedAdmin error = %v, want non-empty database error", err)
	}
}

func newSeedTestDB(t *testing.T) *store.DB {
	t.Helper()
	db, err := store.Open(testdb.New(t).DSN, testPoolConfig())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.Migrate(); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	return db
}

func testPoolConfig() store.PoolConfig {
	return store.PoolConfig{
		MaxOpenConns:    2,
		MaxIdleConns:    1,
		ConnMaxLifetime: time.Minute,
	}
}
