package capabilities

import (
	"reflect"
	"strings"
	"testing"
)

func TestNewRejectsInvalidEntries(t *testing.T) {
	tests := []struct {
		name    string
		entries []Capability
		wantErr string
	}{
		{"duplicate id", []Capability{{ID: "a", State: StateEnabled}, {ID: "a", State: StateEnabled}}, "duplicate capability id"},
		{"unknown state", []Capability{{ID: "a", State: State("unknown")}}, "unknown capability state"},
		{"enabled reason", []Capability{{ID: "a", State: StateEnabled, ReasonCode: ReasonNotEnabled}}, "enabled capability"},
		{"disabled without reason", []Capability{{ID: "a", State: StateDisabled}}, "requires reason_code"},
		{"read only without reason", []Capability{{ID: "a", State: StateReadOnly}}, "requires reason_code"},
		{"unknown reason", []Capability{{ID: "a", State: StateDisabled, ReasonCode: ReasonCode("unknown")}}, "unknown reason_code"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := New(tt.entries)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("New() error = %v, want substring %q", err, tt.wantErr)
			}
		})
	}
}

func TestRegistrySortsAndCopiesSnapshots(t *testing.T) {
	input := []Capability{
		{ID: "zeta", State: StateDisabled, ReasonCode: ReasonNotEnabled},
		{ID: "alpha", State: StateEnabled},
	}
	registry, err := New(input)
	if err != nil {
		t.Fatalf("New(): %v", err)
	}
	input[0].ID = "mutated-input"

	first := registry.Document()
	if first.APIVersion != APIVersion || first.Capabilities == nil {
		t.Fatalf("Document() = %#v", first)
	}
	if got := []string{first.Capabilities[0].ID, first.Capabilities[1].ID}; !reflect.DeepEqual(got, []string{"alpha", "zeta"}) {
		t.Fatalf("ids = %v", got)
	}
	first.Capabilities[0].ID = "mutated-output"
	if got := registry.Document().Capabilities[0].ID; got != "alpha" {
		t.Fatalf("registry mutated through snapshot: %q", got)
	}
	if !registry.Enabled("alpha") || registry.Enabled("zeta") || registry.Enabled("missing") {
		t.Fatalf("Enabled() returned an inconsistent state")
	}
}

func TestFromFeaturesPreservesFalseAndCopiesInput(t *testing.T) {
	features := map[string]bool{"enabled": true, "disabled": false}
	registry := FromFeatures(features)
	features["enabled"] = false

	legacy := registry.LegacyFeatures()
	if !legacy["enabled"] {
		t.Fatalf("enabled = false, want true")
	}
	if got, ok := legacy["disabled"]; !ok || got {
		t.Fatalf("disabled = (%v, %v), want (false, true)", got, ok)
	}
	legacy["enabled"] = false
	if !registry.LegacyFeatures()["enabled"] {
		t.Fatal("registry mutated through legacy projection")
	}
}

func TestZeroRegistryUsesNonNilEmptyCollections(t *testing.T) {
	var registry Registry
	if got := registry.Document().Capabilities; got == nil || len(got) != 0 {
		t.Fatalf("capabilities = %#v, want non-nil empty slice", got)
	}
	if got := registry.LegacyFeatures(); got == nil || len(got) != 0 {
		t.Fatalf("features = %#v, want non-nil empty map", got)
	}
}
