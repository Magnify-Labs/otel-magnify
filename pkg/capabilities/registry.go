// Package capabilities defines the static capability contract advertised by an edition binary.
package capabilities

import (
	"fmt"
	"sort"
)

// APIVersion identifies the schema used by the public capability document.
const APIVersion = "v1"

// State describes whether an edition binary exposes a capability for use.
type State string

const (
	// StateEnabled marks a capability as available for use.
	StateEnabled State = "enabled"
	// StateDisabled marks a known capability as unavailable.
	StateDisabled State = "disabled"
	// StateReadOnly marks a capability whose mutation paths are unavailable.
	StateReadOnly State = "read_only"
)

// ReasonCode explains why a capability is not enabled.
type ReasonCode string

const (
	// ReasonNotEnabled indicates that the edition binary did not enable the capability.
	ReasonNotEnabled ReasonCode = "not_enabled"
	// ReasonPrerequisiteUnavailable indicates that a required dependency is unavailable.
	ReasonPrerequisiteUnavailable ReasonCode = "prerequisite_unavailable"
	// ReasonReadOnlyMode indicates that the capability is intentionally limited to reads.
	ReasonReadOnlyMode ReasonCode = "read_only_mode"
)

// Capability describes one entry in the versioned discovery document.
type Capability struct {
	ID         string     `json:"id"`
	State      State      `json:"state"`
	ReasonCode ReasonCode `json:"reason_code,omitempty"`
}

// Document is the public versioned capability-discovery response.
type Document struct {
	APIVersion   string       `json:"api_version"`
	Capabilities []Capability `json:"capabilities"`
}

// Registry stores a validated, deterministic snapshot of binary capabilities.
type Registry struct {
	entries []Capability
	states  map[string]State
}

// New validates capability entries and returns an independent registry snapshot.
func New(entries []Capability) (Registry, error) {
	copyOfEntries := append([]Capability(nil), entries...)
	seen := make(map[string]struct{}, len(copyOfEntries))
	for _, capability := range copyOfEntries {
		if _, exists := seen[capability.ID]; exists {
			return Registry{}, fmt.Errorf("duplicate capability id %q", capability.ID)
		}
		seen[capability.ID] = struct{}{}
		if err := validate(capability); err != nil {
			return Registry{}, err
		}
	}
	sort.Slice(copyOfEntries, func(i, j int) bool { return copyOfEntries[i].ID < copyOfEntries[j].ID })
	return build(copyOfEntries), nil
}

// FromFeatures converts the legacy boolean feature map into a registry snapshot.
func FromFeatures(features map[string]bool) Registry {
	entries := make([]Capability, 0, len(features))
	for id, enabled := range features {
		capability := Capability{ID: id, State: StateEnabled}
		if !enabled {
			capability.State = StateDisabled
			capability.ReasonCode = ReasonNotEnabled
		}
		entries = append(entries, capability)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].ID < entries[j].ID })
	return build(entries)
}

// Document returns a defensive copy of the versioned discovery document.
func (r Registry) Document() Document {
	entries := make([]Capability, len(r.entries))
	copy(entries, r.entries)
	return Document{APIVersion: APIVersion, Capabilities: entries}
}

// Enabled reports whether the registry marks an identifier as enabled.
func (r Registry) Enabled(id string) bool { return r.states[id] == StateEnabled }

// LegacyFeatures returns an independent boolean projection for legacy clients.
func (r Registry) LegacyFeatures() map[string]bool {
	features := make(map[string]bool, len(r.entries))
	for _, capability := range r.entries {
		features[capability.ID] = capability.State == StateEnabled
	}
	return features
}

func build(entries []Capability) Registry {
	states := make(map[string]State, len(entries))
	for _, capability := range entries {
		states[capability.ID] = capability.State
	}
	return Registry{entries: entries, states: states}
}

func validate(capability Capability) error {
	switch capability.State {
	case StateEnabled:
		if capability.ReasonCode != "" {
			return fmt.Errorf("enabled capability %q must not include reason_code", capability.ID)
		}
		return nil
	case StateDisabled, StateReadOnly:
		if capability.ReasonCode == "" {
			return fmt.Errorf("capability %q in state %q requires reason_code", capability.ID, capability.State)
		}
	default:
		return fmt.Errorf("unknown capability state %q for %q", capability.State, capability.ID)
	}

	switch capability.ReasonCode {
	case ReasonNotEnabled, ReasonPrerequisiteUnavailable, ReasonReadOnlyMode:
		return nil
	default:
		return fmt.Errorf("unknown reason_code %q for %q", capability.ReasonCode, capability.ID)
	}
}
