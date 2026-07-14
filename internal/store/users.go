package store

import (
	"database/sql"
	"errors"
	"fmt"

	"github.com/magnify-labs/otel-magnify/pkg/ext"
	"github.com/magnify-labs/otel-magnify/pkg/models"
)

// CreateUser inserts a new user row.
func (d *DB) CreateUser(u models.User) error {
	_, err := d.Exec(`
		INSERT INTO users (id, email, password_hash, tenant_id)
		VALUES (?, ?, ?, ?)`,
		u.ID, u.Email, u.PasswordHash, u.TenantID,
	)
	return err
}

// CreateInitialAdmin atomically creates the first user and attaches it to the
// administrator group. It is idempotent only when the same email already
// belongs to an administrator; it never resets an existing password or
// promotes an existing non-administrator account.
func (d *DB) CreateInitialAdmin(user models.User) (bool, error) {
	tx, err := d.Begin()
	if err != nil {
		return false, fmt.Errorf("create initial admin: begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// Serialise first-user creation across concurrently starting replicas.
	// PostgreSQL INSERTs take ROW EXCLUSIVE, which conflicts with this lock.
	if _, err := tx.Exec(`LOCK TABLE users IN SHARE ROW EXCLUSIVE MODE`); err != nil {
		return false, fmt.Errorf("create initial admin: lock users: %w", err)
	}

	var existingID string
	err = tx.QueryRow(`SELECT id FROM users WHERE email = ?`, user.Email).Scan(&existingID)
	switch {
	case err == nil:
		var administrator bool
		if err := tx.QueryRow(`
			SELECT EXISTS (
				SELECT 1
				FROM user_groups ug
				INNER JOIN groups g ON g.id = ug.group_id
				WHERE ug.user_id = ? AND g.name = 'administrator'
			)`, existingID).Scan(&administrator); err != nil {
			return false, fmt.Errorf("create initial admin: check administrator membership: %w", err)
		}
		if !administrator {
			return false, fmt.Errorf("create initial admin: user %q already exists without administrator membership", user.Email)
		}
		return false, nil
	case !errors.Is(err, sql.ErrNoRows):
		return false, fmt.Errorf("create initial admin: find existing email: %w", err)
	}

	var userCount int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&userCount); err != nil {
		return false, fmt.Errorf("create initial admin: count users: %w", err)
	}
	if userCount != 0 {
		return false, fmt.Errorf("create initial admin: database already contains a user")
	}

	var administratorGroupID string
	if err := tx.QueryRow(`SELECT id FROM groups WHERE name = 'administrator'`).Scan(&administratorGroupID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, fmt.Errorf("create initial admin: administrator group is missing: %w", err)
		}
		return false, fmt.Errorf("create initial admin: load administrator group: %w", err)
	}

	if _, err := tx.Exec(`
		INSERT INTO users (id, email, password_hash, tenant_id)
		VALUES (?, ?, ?, ?)`,
		user.ID, user.Email, user.PasswordHash, user.TenantID,
	); err != nil {
		return false, fmt.Errorf("create initial admin: insert user: %w", err)
	}
	if _, err := tx.Exec(`
		INSERT INTO user_groups (user_id, group_id)
		VALUES (?, ?)`, user.ID, administratorGroupID,
	); err != nil {
		return false, fmt.Errorf("create initial admin: attach administrator group: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("create initial admin: commit: %w", err)
	}
	return true, nil
}

// GetUserByEmail returns the user with the given email, wrapping ext.ErrUserNotFound on miss.
func (d *DB) GetUserByEmail(email string) (models.User, error) {
	var u models.User
	err := d.QueryRow(`
		SELECT id, email, password_hash, tenant_id
		FROM users WHERE email = ?`, email,
	).Scan(&u.ID, &u.Email, &u.PasswordHash, &u.TenantID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return u, fmt.Errorf("get user by email %s: %w", email, ext.ErrUserNotFound)
		}
		return u, fmt.Errorf("get user by email %s: %w", email, err)
	}
	return u, nil
}

// UpdateUser overwrites email, password_hash, and tenant_id of the row matching u.ID; returns sql.ErrNoRows when no row matches.
func (d *DB) UpdateUser(u models.User) error {
	res, err := d.Exec(`
		UPDATE users
		SET email = ?, password_hash = ?, tenant_id = ?
		WHERE id = ?`,
		u.Email, u.PasswordHash, u.TenantID, u.ID,
	)
	if err != nil {
		return fmt.Errorf("update user %s: %w", u.ID, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("update user %s (rows affected): %w", u.ID, err)
	}
	if n == 0 {
		return fmt.Errorf("update user %s: %w", u.ID, sql.ErrNoRows)
	}
	return nil
}
