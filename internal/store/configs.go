package store

import (
	"encoding/json"
	"fmt"

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
	variables, err := json.Marshal(c.Variables)
	if err != nil {
		return fmt.Errorf("marshal config variables: %w", err)
	}
	tags, err := json.Marshal(c.Tags)
	if err != nil {
		return fmt.Errorf("marshal config tags: %w", err)
	}

	_, err = d.Exec(`
		INSERT INTO configs (id, name, content, created_at, created_by, kind, status, category, stack, description, variables, tags)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		c.ID, c.Name, c.Content, c.CreatedAt.UTC(), c.CreatedBy, c.Kind, c.Status, c.Category, c.Stack, c.Description, string(variables), string(tags),
	)
	return err
}

// GetConfig fetches a config library row by id, wrapping sql.ErrNoRows on miss.
func (d *DB) GetConfig(id string) (models.Config, error) {
	var c models.Config
	var variables, tags string
	err := d.QueryRow(`
		SELECT id, name, content, created_at, created_by,
		       COALESCE(kind, 'saved'), COALESCE(status, 'ready'), COALESCE(category, ''), COALESCE(stack, ''), COALESCE(description, ''),
		       COALESCE(variables, '[]'), COALESCE(tags, '[]')
		FROM configs WHERE id = ?`, id).
		Scan(&c.ID, &c.Name, &c.Content, &c.CreatedAt, &c.CreatedBy, &c.Kind, &c.Status, &c.Category, &c.Stack, &c.Description, &variables, &tags)
	if err != nil {
		return c, fmt.Errorf("get config %s: %w", id, err)
	}
	if err := decodeConfigJSONMetadata(&c, variables, tags); err != nil {
		return c, fmt.Errorf("get config %s: %w", id, err)
	}
	return c, nil
}

// ListConfigs returns all saved config library rows ordered by created_at desc.
func (d *DB) ListConfigs() ([]models.Config, error) {
	rows, err := d.Query(`
		SELECT id, name, content, created_at, created_by,
		       COALESCE(kind, 'saved'), COALESCE(status, 'ready'), COALESCE(category, ''), COALESCE(stack, ''), COALESCE(description, ''),
		       COALESCE(variables, '[]'), COALESCE(tags, '[]')
		FROM configs ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	//nolint:errcheck // deferred cleanup; rows fully iterated below
	defer rows.Close()

	var configs []models.Config
	for rows.Next() {
		var c models.Config
		var variables, tags string
		if err := rows.Scan(&c.ID, &c.Name, &c.Content, &c.CreatedAt, &c.CreatedBy, &c.Kind, &c.Status, &c.Category, &c.Stack, &c.Description, &variables, &tags); err != nil {
			return nil, err
		}
		if err := decodeConfigJSONMetadata(&c, variables, tags); err != nil {
			return nil, err
		}
		configs = append(configs, c)
	}
	return configs, rows.Err()
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

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}
