package store

import (
	"database/sql"
	"errors"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"

	"github.com/magnify-labs/otel-magnify/pkg/ext"
	"github.com/magnify-labs/otel-magnify/pkg/models"
)

func TestCreateUser(t *testing.T) {
	db := newTestDB(t)

	hash, _ := bcrypt.GenerateFromPassword([]byte("secret"), bcrypt.DefaultCost)
	user := models.User{
		ID:           "user-001",
		Email:        "admin@test.com",
		PasswordHash: string(hash),
	}

	if err := db.CreateUser(user); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	got, err := db.GetUserByEmail("admin@test.com")
	if err != nil {
		t.Fatalf("GetUserByEmail: %v", err)
	}
	if bcrypt.CompareHashAndPassword([]byte(got.PasswordHash), []byte("secret")) != nil {
		t.Error("password hash mismatch")
	}
}

func TestGetUserByEmail_NotFound_ReturnsErrUserNotFound(t *testing.T) {
	db := newTestDB(t)

	_, err := db.GetUserByEmail("nobody@example.com")
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if !errors.Is(err, ext.ErrUserNotFound) {
		t.Fatalf("expected errors.Is(err, ext.ErrUserNotFound) to be true; got err=%v", err)
	}
}

func TestUpdateUser_UpdatesFields(t *testing.T) {
	db := newTestDB(t)

	hash, _ := bcrypt.GenerateFromPassword([]byte("secret"), bcrypt.DefaultCost)
	original := models.User{
		ID:           "user-001",
		Email:        "alice@test.com",
		PasswordHash: string(hash),
	}
	if err := db.CreateUser(original); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	if err := db.UpdateUser(original); err != nil {
		t.Fatalf("UpdateUser: %v", err)
	}

	got, err := db.GetUserByEmail("alice@test.com")
	if err != nil {
		t.Fatalf("GetUserByEmail: %v", err)
	}
	// PasswordHash must be preserved when the caller passes it unchanged.
	if got.PasswordHash != string(hash) {
		t.Error("PasswordHash was unexpectedly modified")
	}
}

func TestUpdateUser_NotFound(t *testing.T) {
	db := newTestDB(t)

	err := db.UpdateUser(models.User{
		ID:    "ghost-999",
		Email: "ghost@test.com",
	})
	if !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("err = %v, want sql.ErrNoRows", err)
	}
}

func TestCreateInitialAdmin_CreatesUserAndAdministratorMembership(t *testing.T) {
	db := newTestDB(t)
	user := models.User{
		ID:           "initial-admin-001",
		Email:        "initial-admin@example.invalid",
		PasswordHash: "first-password-hash",
	}

	created, err := db.CreateInitialAdmin(user)
	if err != nil {
		t.Fatalf("CreateInitialAdmin: %v", err)
	}
	if !created {
		t.Fatal("CreateInitialAdmin created = false, want true")
	}

	got, err := db.GetUserByEmail(user.Email)
	if err != nil {
		t.Fatalf("GetUserByEmail: %v", err)
	}
	if got.ID != user.ID || got.PasswordHash != user.PasswordHash {
		t.Fatalf("created user = %#v, want ID %q and original password hash", got, user.ID)
	}
	groups, err := db.GetUserGroups(got.ID)
	if err != nil {
		t.Fatalf("GetUserGroups: %v", err)
	}
	if len(groups) != 1 || groups[0].Name != "administrator" {
		t.Fatalf("groups = %#v, want administrator membership", groups)
	}
}

func TestCreateInitialAdmin_IsIdempotentWithoutResettingPassword(t *testing.T) {
	db := newTestDB(t)
	first := models.User{
		ID:           "initial-admin-001",
		Email:        "initial-admin@example.invalid",
		PasswordHash: "first-password-hash",
	}
	if created, err := db.CreateInitialAdmin(first); err != nil || !created {
		t.Fatalf("first CreateInitialAdmin = (%v, %v), want (true, nil)", created, err)
	}

	second := models.User{
		ID:           "initial-admin-002",
		Email:        first.Email,
		PasswordHash: "replacement-password-hash",
	}
	created, err := db.CreateInitialAdmin(second)
	if err != nil {
		t.Fatalf("second CreateInitialAdmin: %v", err)
	}
	if created {
		t.Fatal("second CreateInitialAdmin created = true, want false")
	}

	got, err := db.GetUserByEmail(first.Email)
	if err != nil {
		t.Fatalf("GetUserByEmail: %v", err)
	}
	if got.ID != first.ID || got.PasswordHash != first.PasswordHash {
		t.Fatalf("idempotent seed changed credentials: got %#v", got)
	}
}

func TestCreateInitialAdmin_RejectsExistingNonAdministrator(t *testing.T) {
	db := newTestDB(t)
	user := models.User{
		ID:           "existing-user",
		Email:        "existing@example.invalid",
		PasswordHash: "existing-password-hash",
	}
	if err := db.CreateUser(user); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	created, err := db.CreateInitialAdmin(models.User{
		ID:           "replacement-admin",
		Email:        user.Email,
		PasswordHash: "replacement-password-hash",
	})
	if err == nil || !strings.Contains(err.Error(), "without administrator membership") {
		t.Fatalf("CreateInitialAdmin error = %v, want existing non-administrator error", err)
	}
	if created {
		t.Fatal("CreateInitialAdmin created = true, want false")
	}
	groups, groupErr := db.GetUserGroups(user.ID)
	if groupErr != nil {
		t.Fatalf("GetUserGroups: %v", groupErr)
	}
	if len(groups) != 0 {
		t.Fatalf("existing user was promoted: groups = %#v", groups)
	}
}

func TestCreateInitialAdmin_RejectsNonEmptyDatabase(t *testing.T) {
	db := newTestDB(t)
	if err := db.CreateUser(models.User{
		ID:           "existing-user",
		Email:        "existing@example.invalid",
		PasswordHash: "existing-password-hash",
	}); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	created, err := db.CreateInitialAdmin(models.User{
		ID:           "initial-admin",
		Email:        "initial-admin@example.invalid",
		PasswordHash: "initial-password-hash",
	})
	if err == nil || !strings.Contains(err.Error(), "database already contains a user") {
		t.Fatalf("CreateInitialAdmin error = %v, want non-empty database error", err)
	}
	if created {
		t.Fatal("CreateInitialAdmin created = true, want false")
	}
	if _, lookupErr := db.GetUserByEmail("initial-admin@example.invalid"); !errors.Is(lookupErr, ext.ErrUserNotFound) {
		t.Fatalf("initial admin unexpectedly persisted: %v", lookupErr)
	}
}

func TestCreateInitialAdmin_RollsBackWhenAdministratorGroupIsMissing(t *testing.T) {
	db := newTestDB(t)
	if _, err := db.Exec(`DELETE FROM groups WHERE name = ?`, "administrator"); err != nil {
		t.Fatalf("delete administrator group: %v", err)
	}

	created, err := db.CreateInitialAdmin(models.User{
		ID:           "initial-admin",
		Email:        "initial-admin@example.invalid",
		PasswordHash: "initial-password-hash",
	})
	if err == nil || !strings.Contains(err.Error(), "administrator group") {
		t.Fatalf("CreateInitialAdmin error = %v, want missing administrator group error", err)
	}
	if created {
		t.Fatal("CreateInitialAdmin created = true, want false")
	}
	if _, lookupErr := db.GetUserByEmail("initial-admin@example.invalid"); !errors.Is(lookupErr, ext.ErrUserNotFound) {
		t.Fatalf("initial admin persisted after failed transaction: %v", lookupErr)
	}
}

func TestCreateInitialAdmin_ConcurrentStartsCreateExactlyOneAdministrator(t *testing.T) {
	db := newTestDB(t)
	type result struct {
		created bool
		err     error
	}

	start := make(chan struct{})
	results := make(chan result, 2)
	for _, user := range []models.User{
		{ID: "concurrent-admin-001", Email: "first@example.invalid", PasswordHash: "first-password-hash"},
		{ID: "concurrent-admin-002", Email: "second@example.invalid", PasswordHash: "second-password-hash"},
	} {
		go func() {
			<-start
			created, err := db.CreateInitialAdmin(user)
			results <- result{created: created, err: err}
		}()
	}
	close(start)

	createdCount := 0
	errorCount := 0
	for range 2 {
		result := <-results
		if result.created {
			createdCount++
		}
		if result.err != nil {
			errorCount++
			if !strings.Contains(result.err.Error(), "database already contains a user") {
				t.Fatalf("concurrent CreateInitialAdmin error = %v, want non-empty database error", result.err)
			}
		}
	}
	if createdCount != 1 || errorCount != 1 {
		t.Fatalf("concurrent results: created=%d errors=%d, want one of each", createdCount, errorCount)
	}

	var userCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&userCount); err != nil {
		t.Fatalf("count users: %v", err)
	}
	var administratorCount int
	if err := db.QueryRow(`
		SELECT COUNT(*)
		FROM user_groups ug
		INNER JOIN groups g ON g.id = ug.group_id
		WHERE g.name = 'administrator'`).Scan(&administratorCount); err != nil {
		t.Fatalf("count administrators: %v", err)
	}
	if userCount != 1 || administratorCount != 1 {
		t.Fatalf("persisted users=%d administrators=%d, want 1 and 1", userCount, administratorCount)
	}
}
