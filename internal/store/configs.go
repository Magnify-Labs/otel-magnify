package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/magnify-labs/otel-magnify/pkg/models"
)

// CreateConfig inserts a new config library row.
func (d *DB) CreateConfig(c models.Config) error {
	if c.Kind == "" {
		c.Kind = models.ConfigKindSaved
	}
	if c.Status == "" {
		c.Status = models.ConfigStatusReady
	}
	if c.SourceType == "" {
		c.SourceType = models.ConfigSourceManual
	}
	variables, err := json.Marshal(c.Variables)
	if err != nil {
		return fmt.Errorf("marshal config variables: %w", err)
	}
	tags, err := json.Marshal(c.Tags)
	if err != nil {
		return fmt.Errorf("marshal config tags: %w", err)
	}

	_, err = d.Exec(`
		INSERT INTO configs (
			id, name, content, created_at, created_by, kind, status, category, stack, description, variables, tags,
			source_type, git_url, git_provider, git_ref, git_path, commit_sha, imported_at
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		c.ID, c.Name, c.Content, c.CreatedAt.UTC(), c.CreatedBy, c.Kind, c.Status, c.Category, c.Stack, c.Description, string(variables), string(tags),
		c.SourceType, nullIfEmpty(c.GitURL), nullIfEmpty(c.GitProvider), nullIfEmpty(c.GitRef), nullIfEmpty(c.GitPath), nullIfEmpty(c.CommitSHA), timePtrValue(c.ImportedAt),
	)
	return err
}

// GetConfig fetches a config library row by id, wrapping sql.ErrNoRows on miss.
func (d *DB) GetConfig(id string) (models.Config, error) {
	var c models.Config
	err := scanConfig(d.QueryRow(`
		SELECT id, name, content, created_at, created_by,
		       COALESCE(kind, 'saved'), COALESCE(status, 'ready'), COALESCE(category, ''), COALESCE(stack, ''), COALESCE(description, ''),
		       COALESCE(variables, '[]'), COALESCE(tags, '[]'), COALESCE(source_type, 'manual'),
		       git_url, git_provider, git_ref, git_path, commit_sha, imported_at
		FROM configs WHERE id = ?`, id), &c)
	if err != nil {
		return c, fmt.Errorf("get config %s: %w", id, err)
	}
	return c, nil
}

// ListConfigs returns all saved config library rows ordered by created_at desc.
func (d *DB) ListConfigs() ([]models.Config, error) {
	rows, err := d.Query(`
		SELECT id, name, content, created_at, created_by,
		       COALESCE(kind, 'saved'), COALESCE(status, 'ready'), COALESCE(category, ''), COALESCE(stack, ''), COALESCE(description, ''),
		       COALESCE(variables, '[]'), COALESCE(tags, '[]'), COALESCE(source_type, 'manual'),
		       git_url, git_provider, git_ref, git_path, commit_sha, imported_at
		FROM configs ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	//nolint:errcheck // deferred cleanup; rows fully iterated below
	defer rows.Close()

	var configs []models.Config
	for rows.Next() {
		var c models.Config
		if err := scanConfig(rows, &c); err != nil {
			return nil, err
		}
		configs = append(configs, c)
	}
	return configs, rows.Err()
}

type configScanner interface {
	Scan(dest ...any) error
}

func scanConfig(row configScanner, c *models.Config) error {
	var variables, tags string
	var gitURL, gitProvider, gitRef, gitPath, commitSHA sql.NullString
	var importedAt sql.NullTime
	if err := row.Scan(
		&c.ID, &c.Name, &c.Content, &c.CreatedAt, &c.CreatedBy,
		&c.Kind, &c.Status, &c.Category, &c.Stack, &c.Description, &variables, &tags, &c.SourceType,
		&gitURL, &gitProvider, &gitRef, &gitPath, &commitSHA, &importedAt,
	); err != nil {
		return err
	}
	if err := decodeConfigJSONMetadata(c, variables, tags); err != nil {
		return err
	}
	if c.SourceType == "" {
		c.SourceType = models.ConfigSourceManual
	}
	c.GitURL = gitURL.String
	c.GitProvider = gitProvider.String
	c.GitRef = gitRef.String
	c.GitPath = gitPath.String
	c.CommitSHA = commitSHA.String
	if importedAt.Valid {
		v := importedAt.Time.UTC()
		c.ImportedAt = &v
	}
	return nil
}

func decodeConfigJSONMetadata(c *models.Config, variablesJSON, tagsJSON string) error {
	if c.Kind == "" {
		c.Kind = models.ConfigKindSaved
	}
	if c.Status == "" {
		c.Status = models.ConfigStatusReady
	}
	if variablesJSON == "" {
		variablesJSON = "[]"
	}
	if tagsJSON == "" {
		tagsJSON = "[]"
	}
	if err := json.Unmarshal([]byte(variablesJSON), &c.Variables); err != nil {
		return fmt.Errorf("decode variables: %w", err)
	}
	if err := json.Unmarshal([]byte(tagsJSON), &c.Tags); err != nil {
		return fmt.Errorf("decode tags: %w", err)
	}
	return nil
}

func timePtrValue(t *time.Time) any {
	if t == nil {
		return nil
	}
	return t.UTC()
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// RecordGitOpsValidationStatus stores a normalized provider webhook validation signal.
func (d *DB) RecordGitOpsValidationStatus(status models.GitOpsValidationStatus) error {
	if status.ObservedAt.IsZero() {
		status.ObservedAt = time.Now().UTC()
	}
	_, err := d.Exec(`
		INSERT INTO gitops_validation_statuses (
			provider, event, action, status, source_path, source_ref, commit_sha, observed_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		status.Provider, status.Event, status.Action, status.Status, status.SourcePath, status.SourceRef, status.CommitSHA, status.ObservedAt.UTC(),
	)
	return err
}

// GetLatestGitOpsValidationStatus returns the newest stored provider validation signal for a path/ref/commit.
func (d *DB) GetLatestGitOpsValidationStatus(provider, sourcePath, sourceRef, commitSHA string) (*models.GitOpsValidationStatus, error) {
	row := d.QueryRow(`
		SELECT provider, event, action, status, source_path, source_ref, commit_sha, observed_at
		FROM gitops_validation_statuses
		WHERE provider = ? AND source_path = ? AND source_ref = ? AND commit_sha = ?
		ORDER BY observed_at DESC, id DESC
		LIMIT 1`, provider, sourcePath, sourceRef, commitSHA)
	var status models.GitOpsValidationStatus
	if err := row.Scan(&status.Provider, &status.Event, &status.Action, &status.Status, &status.SourcePath, &status.SourceRef, &status.CommitSHA, &status.ObservedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &status, nil
}
