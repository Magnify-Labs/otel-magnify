package store

import (
	"crypto/sha256"
	"fmt"
	"testing"
	"time"

	"github.com/magnify-labs/otel-magnify/pkg/models"
)

func TestCreateConfig(t *testing.T) {
	db := newTestDB(t)

	content := "receivers:\n  otlp:\n    protocols:\n      grpc:"
	hash := fmt.Sprintf("%x", sha256.Sum256([]byte(content)))

	cfg := models.Config{
		ID:        hash,
		Name:      "collector-base",
		Content:   content,
		CreatedAt: time.Now().UTC().Truncate(time.Second),
		CreatedBy: "admin@test.com",
	}

	if err := db.CreateConfig(cfg); err != nil {
		t.Fatalf("CreateConfig: %v", err)
	}

	got, err := db.GetConfig(hash)
	if err != nil {
		t.Fatalf("GetConfig: %v", err)
	}
	if got.Name != "collector-base" {
		t.Errorf("Name = %q, want collector-base", got.Name)
	}
	if got.Content != content {
		t.Errorf("Content mismatch")
	}
}

func TestListConfigs(t *testing.T) {
	db := newTestDB(t)

	for i := range 3 {
		content := fmt.Sprintf("config-%d", i)
		hash := fmt.Sprintf("%x", sha256.Sum256([]byte(content)))
		err := db.CreateConfig(models.Config{
			ID: hash, Name: fmt.Sprintf("cfg-%d", i), Content: content,
			CreatedAt: time.Now().UTC(), CreatedBy: "test",
		})
		if err != nil {
			t.Fatal(err)
		}
	}

	configs, err := db.ListConfigs()
	if err != nil {
		t.Fatal(err)
	}
	if len(configs) != 3 {
		t.Errorf("len = %d, want 3", len(configs))
	}
}

func TestCreateConfig_PersistsLibraryMetadata(t *testing.T) {
	db := newTestDB(t)

	cfg := models.Config{
		ID:          "saved-with-meta",
		Name:        "Saved with metadata",
		Content:     "receivers:\n  otlp: {}",
		CreatedAt:   time.Now().UTC().Truncate(time.Second),
		CreatedBy:   "admin@test.com",
		Kind:        models.ConfigKindDraft,
		Status:      models.ConfigStatusDraft,
		Category:    "custom",
		Stack:       "kubernetes",
		Description: "draft collector config",
		Variables: []models.ConfigVariable{
			{Name: "endpoint", Label: "Endpoint", Type: "string", Required: true},
		},
		Tags: []string{"draft", "collector"},
	}
	if err := db.CreateConfig(cfg); err != nil {
		t.Fatalf("CreateConfig: %v", err)
	}

	got, err := db.GetConfig(cfg.ID)
	if err != nil {
		t.Fatalf("GetConfig: %v", err)
	}
	if got.Kind != models.ConfigKindDraft || got.Status != models.ConfigStatusDraft || got.Category != "custom" || got.Stack != "kubernetes" || got.Description != "draft collector config" {
		t.Fatalf("metadata mismatch: %+v", got)
	}
	if len(got.Variables) != 1 || got.Variables[0].Name != "endpoint" || len(got.Tags) != 2 || got.Tags[0] != "draft" {
		t.Fatalf("JSON metadata mismatch: variables=%+v tags=%+v", got.Variables, got.Tags)
	}
}
