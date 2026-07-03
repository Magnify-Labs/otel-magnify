package opamp

import (
	"sync"
	"time"

	"github.com/magnify-labs/otel-magnify/pkg/models"
)

// Instance is a live pod view held in memory by the OpAMP server.
type Instance struct {
	InstanceUID         string    `json:"instance_uid"`
	PodName             string    `json:"pod_name,omitempty"`
	Version             string    `json:"version,omitempty"`
	ConnectedAt         time.Time `json:"connected_at"`
	LastMessageAt       time.Time `json:"last_message_at"`
	EffectiveConfigHash string    `json:"effective_config_hash,omitempty"`
	Healthy             bool      `json:"healthy"`
	AcceptsRemoteConfig bool      `json:"accepts_remote_config"`
	// RemoteConfigCapabilityKnown is true once the instance reported an OpAMP
	// capability bitset. A missing legacy/test payload leaves AcceptsRemoteConfig
	// at its Go zero value, which must not be treated as explicit read-only.
	RemoteConfigCapabilityKnown bool                       `json:"remote_config_capability_known,omitempty"`
	RemoteConfigStatus          *models.RemoteConfigStatus `json:"remote_config_status,omitempty"`
}

// InstanceRegistry is the in-memory source of truth for "who is currently
// connected to which workload". Not persisted; rebuilt from live OpAMP
// traffic after a server restart.
type InstanceRegistry struct {
	mu         sync.RWMutex
	byWorkload map[string]map[string]*Instance // workloadID -> instanceUID -> instance
	uidToWL    map[string]string               // instanceUID -> workloadID (heartbeat cache)
}

// NewInstanceRegistry returns an empty in-memory instance registry.
func NewInstanceRegistry() *InstanceRegistry {
	return &InstanceRegistry{
		byWorkload: make(map[string]map[string]*Instance),
		uidToWL:    make(map[string]string),
	}
}

// BindInstance stores or refreshes an instance. Returns true if the binding is
// fresh (uid was not in the registry yet) — callers use this to decide whether
// to emit a "connected" event.
//
// If the instance was previously bound to a DIFFERENT workload (shouldn't
// happen in practice but is defended against), it is removed from the old
// workload bucket.
func (r *InstanceRegistry) BindInstance(uid, workloadID string, ins Instance) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	prevWL, existed := r.uidToWL[uid]
	if existed && prevWL != workloadID {
		delete(r.byWorkload[prevWL], uid)
		if len(r.byWorkload[prevWL]) == 0 {
			delete(r.byWorkload, prevWL)
		}
	}
	ins.InstanceUID = uid
	if ins.ConnectedAt.IsZero() {
		ins.ConnectedAt = time.Now().UTC()
	}
	ins.LastMessageAt = time.Now().UTC()
	bucket, ok := r.byWorkload[workloadID]
	if !ok {
		bucket = make(map[string]*Instance)
		r.byWorkload[workloadID] = bucket
	}
	bucket[uid] = &ins
	r.uidToWL[uid] = workloadID
	return !existed
}

// UpdateInstance mutates an existing instance in-place. Returns false if the
// uid is unknown.
func (r *InstanceRegistry) UpdateInstance(uid string, mutate func(*Instance)) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	wl, ok := r.uidToWL[uid]
	if !ok {
		return false
	}
	inst := r.byWorkload[wl][uid]
	if inst == nil {
		return false
	}
	mutate(inst)
	inst.LastMessageAt = time.Now().UTC()
	return true
}

// UnbindInstance removes an instance from the registry. Returns the workload
// ID it belonged to, or empty string if the uid was unknown.
func (r *InstanceRegistry) UnbindInstance(uid string) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	wl := r.uidToWL[uid]
	if wl == "" {
		return ""
	}
	delete(r.byWorkload[wl], uid)
	if len(r.byWorkload[wl]) == 0 {
		delete(r.byWorkload, wl)
	}
	delete(r.uidToWL, uid)
	return wl
}

// LookupWorkload returns the workload ID for a given instance uid.
func (r *InstanceRegistry) LookupWorkload(uid string) (string, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	wl, ok := r.uidToWL[uid]
	return wl, ok
}

// Count returns the number of live instances for a workload.
func (r *InstanceRegistry) Count(workloadID string) int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.byWorkload[workloadID])
}

// Instances returns a snapshot copy of the live instances for a workload.
func (r *InstanceRegistry) Instances(workloadID string) []Instance {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Instance, 0, len(r.byWorkload[workloadID]))
	for _, v := range r.byWorkload[workloadID] {
		out = append(out, *v)
	}
	return out
}

// AggregatedStatus computes the aggregate status for a workload:
//   - "disconnected" if no live instance
//   - "degraded"     if any live instance is unhealthy
//   - "connected"    otherwise
func (r *InstanceRegistry) AggregatedStatus(workloadID string) string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	bucket := r.byWorkload[workloadID]
	if len(bucket) == 0 {
		return "disconnected"
	}
	for _, i := range bucket {
		if !i.Healthy {
			return "degraded"
		}
	}
	return "connected"
}

// PreviousVersion reports the Version currently stored for an instance uid.
// Used to detect version_changed events before a new Bind overwrites it.
func (r *InstanceRegistry) PreviousVersion(uid string) (string, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	wl := r.uidToWL[uid]
	if wl == "" {
		return "", false
	}
	if ins, ok := r.byWorkload[wl][uid]; ok {
		return ins.Version, true
	}
	return "", false
}
