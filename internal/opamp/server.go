package opamp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/open-telemetry/opamp-go/protobufs"
	opampServer "github.com/open-telemetry/opamp-go/server"
	"github.com/open-telemetry/opamp-go/server/types"

	"github.com/magnify-labs/otel-magnify/pkg/models"
)

// reportsAvailableComponentsCap is the capability bit set by agents that will
// send AgentToServer.available_components. Defined in OpAMP spec >= v0.14.
const reportsAvailableComponentsCap = uint64(protobufs.AgentCapabilities_AgentCapabilities_ReportsAvailableComponents)

// acceptsRemoteConfigCap signals that the agent (or the supervisor fronting it)
// can apply a remote config. For bare collectors with the opamp extension this
// bit is unset — only opamp-supervisor sets it. Exposed in the Workload JSON
// so the UI can gate config editing affordances.
const acceptsRemoteConfigCap = uint64(protobufs.AgentCapabilities_AgentCapabilities_AcceptsRemoteConfig)

// Store is the narrow subset of store.DB the OpAMP server needs.
type Store interface {
	GetWorkload(id string) (models.Workload, error)
	UpsertWorkload(w models.Workload) error
	MarkWorkloadDisconnected(id string, retentionUntil time.Time) error
	ClearWorkloadRetention(id string) error

	GetConfig(id string) (models.Config, error)
	CreateConfig(c models.Config) error

	RecordWorkloadConfig(wc models.WorkloadConfig) error
	UpdateWorkloadConfigStatus(workloadID, configID, status, errorMessage string) error
	MarkWorkloadConfigSent(workloadID, configID string, sentAt time.Time) error
	UpdateWorkloadConfigInstanceStatus(workloadID, configID, instanceUID, status, errorMessage string, updatedAt time.Time) error
	GetLatestWorkloadConfig(workloadID string) (*models.WorkloadConfig, error)
	GetLastAppliedWorkloadConfig(workloadID string) (*models.WorkloadConfig, error)
	GetRollbackTarget(workloadID, excludeHash string) (*models.RollbackTarget, error)

	InsertWorkloadEvent(e models.WorkloadEvent) (int64, error)
}

// Notifier is called when a workload's state changes, to relay updates to the
// frontend WS hub.
type Notifier interface {
	BroadcastWorkloadUpdate(workload models.Workload, connectedInstances, driftedInstances int)
	BroadcastWorkloadEvent(event models.WorkloadEvent)
	BroadcastConfigStatus(workloadID string, status models.RemoteConfigStatus)
	BroadcastAutoRollback(workloadID, fromHash, toHash, reason, targetKind string)
}

// Options controls time-based server behavior. Zero values fall back to
// production defaults.
type Options struct {
	// DisconnectGrace is how long to wait after the last live instance of a
	// workload goes away before marking the workload as disconnected. This
	// smooths over K8s rolling restarts where pod A closes its connection
	// moments before pod B opens one.
	DisconnectGrace time.Duration
	// RetentionDuration is how long a disconnected workload stays around
	// before it becomes eligible for archival.
	RetentionDuration time.Duration
}

// Server wraps the opamp-go server and manages workload state.
type Server struct {
	opamp    opampServer.OpAMPServer
	store    Store
	notifier Notifier

	registry  *InstanceRegistry
	grace     *GraceController
	retention time.Duration

	mu        sync.RWMutex
	conns     map[string]types.Connection // instanceUID hex -> connection
	connToUID map[types.Connection]string // reverse map for O(1) lookup on close

	// pushFn sends a config YAML to a workload. Defaults to PushConfig;
	// overridable in tests so they can observe auto-push behavior without
	// wiring a real OpAMP connection.
	pushFn func(workloadID string, yaml []byte, targetInstanceUID string) error
}

// New creates a new OpAMP server. db and notifier can be nil (useful for
// testing).
func New(db Store, notifier Notifier, opts Options) *Server {
	if opts.DisconnectGrace <= 0 {
		opts.DisconnectGrace = 2 * time.Minute
	}
	if opts.RetentionDuration <= 0 {
		opts.RetentionDuration = 30 * 24 * time.Hour
	}
	s := &Server{
		opamp:     opampServer.New(nil),
		store:     db,
		notifier:  notifier,
		registry:  NewInstanceRegistry(),
		grace:     NewGraceController(opts.DisconnectGrace),
		retention: opts.RetentionDuration,
		conns:     make(map[string]types.Connection),
		connToUID: make(map[types.Connection]string),
	}
	s.pushFn = func(workloadID string, yaml []byte, target string) error {
		return s.PushConfig(context.Background(), workloadID, yaml, target)
	}
	return s
}

// ConnectedInstanceCount returns the number of currently connected instances.
func (s *Server) ConnectedInstanceCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.conns)
}

// Instances returns a snapshot of the live instances bound to a workload.
// Exposed for the REST handler GET /api/workloads/:id/instances.
func (s *Server) Instances(workloadID string) []Instance {
	return s.registry.Instances(workloadID)
}

// GetConnection returns the OpAMP connection for a given instance UID, or nil.
func (s *Server) GetConnection(instanceUID string) types.Connection {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.conns[instanceUID]
}

// PushConfig sends a remote config to one specific instance (when
// targetInstanceUID is set) or broadcasts it to every live instance of a
// workload.
func (s *Server) PushConfig(ctx context.Context, workloadID string, yamlContent []byte, targetInstanceUID string) error {
	configHash := sha256.Sum256(yamlContent)
	makeMsg := func(uid string) *protobufs.ServerToAgent {
		return &protobufs.ServerToAgent{
			InstanceUid: []byte(uid),
			RemoteConfig: &protobufs.AgentRemoteConfig{
				Config: &protobufs.AgentConfigMap{
					ConfigMap: map[string]*protobufs.AgentConfigFile{
						"": {Body: yamlContent, ContentType: "text/yaml"},
					},
				},
				ConfigHash: configHash[:],
			},
		}
	}

	if targetInstanceUID != "" {
		return s.sendToInstance(ctx, targetInstanceUID, makeMsg(targetInstanceUID))
	}

	instances := s.registry.Instances(workloadID)
	if len(instances) == 0 {
		return fmt.Errorf("workload %s has no connected instance", workloadID)
	}
	var firstErr error
	for _, i := range instances {
		if err := s.sendToInstance(ctx, i.InstanceUID, makeMsg(i.InstanceUID)); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (s *Server) sendToInstance(ctx context.Context, uid string, msg *protobufs.ServerToAgent) error {
	s.mu.RLock()
	conn := s.conns[uid]
	s.mu.RUnlock()
	if conn == nil {
		return fmt.Errorf("instance %s not connected", uid)
	}
	return conn.Send(ctx, msg)
}

// Attach mounts the OpAMP handler on an existing HTTP mux.
func (s *Server) Attach() (opampServer.HTTPHandlerFunc, opampServer.ConnContext, error) {
	connCallbacks := types.ConnectionCallbacks{
		OnConnected:       s.onConnected,
		OnMessage:         s.onMessage,
		OnConnectionClose: s.onConnectionClose,
	}

	settings := opampServer.Settings{
		Callbacks: types.Callbacks{
			OnConnecting: func(_ *http.Request) types.ConnectionResponse {
				return types.ConnectionResponse{
					Accept:              true,
					ConnectionCallbacks: connCallbacks,
				}
			},
		},
	}

	return s.opamp.Attach(settings)
}

// Stop gracefully shuts down the OpAMP server.
func (s *Server) Stop(ctx context.Context) error {
	return s.opamp.Stop(ctx)
}

func (s *Server) onConnected(_ context.Context, conn types.Connection) {
	log.Printf("OpAMP agent connected: %v", conn)
}

// flattenAttrs merges identifying and non-identifying OpAMP attributes into a
// flat map[string]string, skipping non-string and empty values.
func flattenAttrs(identifying, nonIdentifying []*protobufs.KeyValue) map[string]string {
	out := make(map[string]string, len(identifying)+len(nonIdentifying))
	for _, kv := range identifying {
		if kv == nil || kv.Value == nil {
			continue
		}
		if v := kv.Value.GetStringValue(); v != "" {
			out[kv.Key] = v
		}
	}
	for _, kv := range nonIdentifying {
		if kv == nil || kv.Value == nil {
			continue
		}
		if v := kv.Value.GetStringValue(); v != "" {
			out[kv.Key] = v
		}
	}
	return out
}

func (s *Server) onMessage(_ context.Context, conn types.Connection, msg *protobufs.AgentToServer) *protobufs.ServerToAgent {
	uid := hex.EncodeToString(msg.InstanceUid)

	// Track connection in both directions for O(1) lookup on close. A nil
	// conn slips in from unit tests — tolerate it so we still exercise the
	// message-handling logic.
	if conn != nil {
		s.mu.Lock()
		s.conns[uid] = conn
		s.connToUID[conn] = uid
		s.mu.Unlock()
	}

	var workloadID string
	var requestComponents bool

	if desc := msg.AgentDescription; desc != nil {
		attrs := flattenAttrs(desc.IdentifyingAttributes, desc.NonIdentifyingAttributes)
		fp := Fingerprint(attrs, uid)
		workloadID = fp.ID

		version := attrs["service.version"]
		// Capture the previous version BEFORE BindInstance overwrites it so
		// we can emit a version_changed event on rebind.
		prevVersion, _ := s.registry.PreviousVersion(uid)

		ins := Instance{
			PodName: attrs["k8s.pod.name"],
			Version: version,
			Healthy: true,
		}
		if msg.Health != nil && !msg.Health.Healthy {
			ins.Healthy = false
		}
		if msg.RemoteConfigStatus != nil {
			ins.EffectiveConfigHash = hex.EncodeToString(msg.RemoteConfigStatus.LastRemoteConfigHash)
		}

		isFresh := s.registry.BindInstance(uid, workloadID, ins)
		// A new binding supersedes any pending grace timer.
		s.grace.Cancel(workloadID)

		// Upsert the workload row BEFORE emitting events — workload_events
		// has a FK to workloads(id) so the parent must exist first.
		s.upsertWorkloadFromDescription(uid, workloadID, fp, attrs, msg)

		if isFresh {
			s.emitEvent(models.WorkloadEvent{
				WorkloadID:  workloadID,
				InstanceUID: uid,
				PodName:     ins.PodName,
				EventType:   "connected",
				Version:     ins.Version,
				OccurredAt:  time.Now().UTC(),
			})
		} else if prevVersion != "" && prevVersion != version {
			s.emitEvent(models.WorkloadEvent{
				WorkloadID:  workloadID,
				InstanceUID: uid,
				PodName:     ins.PodName,
				EventType:   "version_changed",
				Version:     version,
				PrevVersion: prevVersion,
				OccurredAt:  time.Now().UTC(),
			})
		}

		// Only ask for available_components when the capability bit is set
		// and the agent hasn't already populated the list.
		if msg.Capabilities&reportsAvailableComponentsCap != 0 {
			if wl, err := s.getWorkload(workloadID); err == nil && wl.AvailableComponents == nil {
				requestComponents = true
			}
		}
	} else {
		wl, ok := s.registry.LookupWorkload(uid)
		if !ok {
			// Unknown UID with no AgentDescription — either a fresh agent
			// that hasn't identified yet, or an existing agent reconnecting
			// after we lost registry state (server restart with ephemeral
			// DB). Ask for a full state so we can bootstrap the workload;
			// OpAMP agents won't resend AgentDescription on their own.
			return &protobufs.ServerToAgent{
				InstanceUid: msg.InstanceUid,
				Flags:       uint64(protobufs.ServerToAgentFlags_ServerToAgentFlags_ReportFullState),
			}
		}
		workloadID = wl
		s.registry.UpdateInstance(uid, func(i *Instance) {
			if msg.Health != nil {
				i.Healthy = msg.Health.Healthy
			}
			if msg.RemoteConfigStatus != nil {
				i.EffectiveConfigHash = hex.EncodeToString(msg.RemoteConfigStatus.LastRemoteConfigHash)
			}
		})
	}

	// Aggregated status + broadcast + conditional auto-push.
	if s.store != nil {
		if wl, err := s.store.GetWorkload(workloadID); err == nil {
			wl.Status = s.registry.AggregatedStatus(workloadID)
			wl.LastSeenAt = time.Now().UTC()
			if err := s.store.UpsertWorkload(wl); err != nil {
				log.Printf("Failed to upsert workload %s: %v", workloadID, err)
			}
			if s.notifier != nil {
				connected := s.registry.Count(workloadID)
				drifted := s.countDrift(workloadID, wl.ActiveConfigHash)
				s.notifier.BroadcastWorkloadUpdate(wl, connected, drifted)
			}

			// Auto-push (P.2): only when this specific instance diverges
			// from the workload's pinned active config.
			if wl.ActiveConfigHash != "" && wl.ActiveConfigID != nil {
				for _, i := range s.registry.Instances(workloadID) {
					if i.InstanceUID != uid {
						continue
					}
					if i.EffectiveConfigHash != "" && i.EffectiveConfigHash != wl.ActiveConfigHash {
						//nolint:gosec // auto-push is server-initiated and must outlive the OpAMP message context
						go s.triggerAutoPush(context.Background(), *wl.ActiveConfigID, workloadID, uid)
					}
				}
			}
		}
	}

	// RemoteConfigStatus bookkeeping (keeps the audit trail in workload_configs
	// + auto-rollback on FAILED).
	if s.store != nil && msg.RemoteConfigStatus != nil {
		s.handleRemoteConfigStatus(workloadID, uid, msg.RemoteConfigStatus)
	}

	reply := &protobufs.ServerToAgent{InstanceUid: msg.InstanceUid}
	if requestComponents {
		reply.Flags = uint64(protobufs.ServerToAgentFlags_ServerToAgentFlags_ReportAvailableComponents)
	}
	return reply
}

// getWorkload is a nil-safe wrapper around Store.GetWorkload.
func (s *Server) getWorkload(id string) (models.Workload, error) {
	if s.store == nil {
		return models.Workload{}, fmt.Errorf("no store")
	}
	return s.store.GetWorkload(id)
}

// upsertWorkloadFromDescription materializes the workload row from the live
// attributes, merging with DB state so we don't clobber fields managed
// elsewhere (active_config_id, retention_until, remote_config_status snap).
func (s *Server) upsertWorkloadFromDescription(_, workloadID string, fp FingerprintResult, attrs map[string]string, msg *protobufs.AgentToServer) {
	if s.store == nil {
		return
	}
	var w models.Workload
	if prev, err := s.store.GetWorkload(workloadID); err == nil {
		w = prev
	}
	w.ID = workloadID
	w.FingerprintSource = fp.Source
	w.FingerprintKeys = models.FingerprintKeys(fp.Keys)
	if svc := attrs["service.name"]; svc != "" {
		w.DisplayName = svc
	}
	if v := attrs["service.version"]; v != "" {
		w.Version = v
	}
	w.Type = classifyAgent(attrs)
	w.Status = s.registry.AggregatedStatus(workloadID)
	w.LastSeenAt = time.Now().UTC()
	w.AcceptsRemoteConfig = msg.Capabilities&acceptsRemoteConfigCap != 0

	// Rebuild labels from scratch to match the current attribute set. Skip
	// keys already projected into dedicated columns. Preserve reserved trusted
	// selector labels only from existing DB state; never accept them from
	// self-reported OpAMP attributes.
	trustedLabels := models.Labels{}
	for k, v := range w.Labels {
		if strings.HasPrefix(k, models.TrustedSelectorLabelPrefix) {
			trustedLabels[k] = v
		}
	}
	w.Labels = models.Labels{}
	for k, v := range trustedLabels {
		w.Labels[k] = v
	}
	for k, v := range attrs {
		switch k {
		case "service.name", "service.version":
			continue
		}
		if strings.HasPrefix(k, models.TrustedSelectorLabelPrefix) {
			continue
		}
		w.Labels[k] = v
	}

	// Resurrection: a live message clears any retention deadline.
	w.RetentionUntil = nil

	if cfgID := s.persistEffectiveConfig(workloadID, w.DisplayName, msg.EffectiveConfig); cfgID != "" {
		w.ActiveConfigID = &cfgID
	}

	if ac := flattenAvailableComponents(msg.AvailableComponents); ac != nil {
		w.AvailableComponents = ac
	}

	if w.Type == "" {
		return
	}
	if err := s.store.UpsertWorkload(w); err != nil {
		log.Printf("Failed to upsert workload %s: %v", workloadID, err)
		return
	}
	// The UPSERT already wrote retention_until=NULL via the w.RetentionUntil=nil
	// assignment above, but an explicit clear is cheap and guards against
	// future schema changes that might stop propagating NULL through COALESCE.
	if err := s.store.ClearWorkloadRetention(workloadID); err != nil {
		log.Printf("clear retention %s: %v", workloadID, err)
	}
}

// countDrift returns how many live instances have an effective config hash
// that differs from the workload's pinned active hash.
func (s *Server) countDrift(workloadID, activeHash string) int {
	if activeHash == "" {
		return 0
	}
	n := 0
	for _, i := range s.registry.Instances(workloadID) {
		if i.EffectiveConfigHash != "" && i.EffectiveConfigHash != activeHash {
			n++
		}
	}
	return n
}

// triggerAutoPush re-pushes the workload's pinned config to a single instance
// that has reported a divergent effective hash. Runs as a goroutine launched
// from onMessage, so all errors are logged (no channel to propagate them).
func (s *Server) triggerAutoPush(_ context.Context, configID, workloadID, instanceUID string) {
	if s.store == nil {
		return
	}
	cfg, err := s.store.GetConfig(configID)
	if err != nil {
		log.Printf("auto-push: cannot load config %s: %v", configID, err)
		return
	}
	if err := s.pushFn(workloadID, []byte(cfg.Content), instanceUID); err != nil {
		log.Printf("auto-push to workload=%s instance=%s failed: %v", workloadID, instanceUID, err)
	}
}

// emitEvent persists a WorkloadEvent and broadcasts it. Store failures are
// logged — events are best-effort.
func (s *Server) emitEvent(e models.WorkloadEvent) {
	if s.store == nil {
		return
	}
	id, err := s.store.InsertWorkloadEvent(e)
	if err != nil {
		log.Printf("workload_events insert: %v", err)
		return
	}
	e.ID = id
	if s.notifier != nil {
		s.notifier.BroadcastWorkloadEvent(e)
	}
}

// flattenAvailableComponents converts the OpAMP nested representation
// (category -> ComponentDetails{SubComponentMap: type -> ComponentDetails})
// into a flat map of category -> sorted list of component type names.
// Returns nil if the input is empty (e.g. heartbeat).
func flattenAvailableComponents(ac *protobufs.AvailableComponents) *models.AvailableComponents {
	if ac == nil || len(ac.Components) == 0 {
		return nil
	}
	out := &models.AvailableComponents{
		Components: make(map[string][]string, len(ac.Components)),
		Hash:       hex.EncodeToString(ac.Hash),
	}
	for category, details := range ac.Components {
		if details == nil {
			continue
		}
		names := make([]string, 0, len(details.SubComponentMap))
		for name := range details.SubComponentMap {
			names = append(names, name)
		}
		sort.Strings(names)
		out.Components[category] = names
	}
	return out
}

func (s *Server) onConnectionClose(conn types.Connection) {
	s.mu.Lock()
	uid, ok := s.connToUID[conn]
	if !ok {
		// Connection was never registered (e.g. closed before first
		// message). Return without touching registry state.
		s.mu.Unlock()
		return
	}
	delete(s.conns, uid)
	delete(s.connToUID, conn)
	s.mu.Unlock()

	// Capture pod_name for the disconnected event BEFORE unbinding — the
	// pod name is only known from the registry entry.
	var podName string
	if wl, found := s.registry.LookupWorkload(uid); found {
		for _, i := range s.registry.Instances(wl) {
			if i.InstanceUID == uid {
				podName = i.PodName
				break
			}
		}
	}
	workloadID := s.registry.UnbindInstance(uid)
	if workloadID == "" {
		return
	}

	s.emitEvent(models.WorkloadEvent{
		WorkloadID:  workloadID,
		InstanceUID: uid,
		PodName:     podName,
		EventType:   "disconnected",
		OccurredAt:  time.Now().UTC(),
	})

	if s.registry.Count(workloadID) == 0 {
		s.grace.Schedule(workloadID, func() {
			// Re-check under the real clock: a rolling restart could have
			// rebound an instance during the grace window.
			if s.registry.Count(workloadID) > 0 {
				return
			}
			if s.store == nil {
				return
			}
			until := time.Now().UTC().Add(s.retention)
			if err := s.store.MarkWorkloadDisconnected(workloadID, until); err != nil {
				log.Printf("mark disconnected %s: %v", workloadID, err)
				return
			}
			if s.notifier != nil {
				if wl, err := s.store.GetWorkload(workloadID); err == nil {
					s.notifier.BroadcastWorkloadUpdate(wl, 0, 0)
				}
			}
		})
	}
}

// persistEffectiveConfig stores the YAML config reported by the agent
// (deduplicated by content hash) and returns the resulting config ID.
// Returns empty if the message carries no effective config (typical for
// heartbeats).
func (s *Server) persistEffectiveConfig(workloadID, displayName string, effective *protobufs.EffectiveConfig) string {
	if effective == nil || effective.ConfigMap == nil || len(effective.ConfigMap.ConfigMap) == 0 {
		return ""
	}

	// Collectors typically report a single file under the empty key "".
	// Concatenate deterministically if multiple files are present.
	keys := make([]string, 0, len(effective.ConfigMap.ConfigMap))
	for k := range effective.ConfigMap.ConfigMap {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var buf []byte
	for _, k := range keys {
		buf = append(buf, effective.ConfigMap.ConfigMap[k].Body...)
	}
	if len(buf) == 0 {
		return ""
	}

	sum := sha256.Sum256(buf)
	configID := hex.EncodeToString(sum[:])

	if _, err := s.store.GetConfig(configID); err != nil {
		shortFallback := workloadID
		if len(shortFallback) > 8 {
			shortFallback = shortFallback[:8]
		}
		name := fmt.Sprintf("%s-reported-%s", fallback(displayName, shortFallback), configID[:8])
		cfg := models.Config{
			ID:        configID,
			Name:      name,
			Content:   string(buf),
			CreatedAt: time.Now().UTC(),
			CreatedBy: "agent-reported",
		}
		if err := s.store.CreateConfig(cfg); err != nil {
			log.Printf("Failed to persist effective config %s: %v", configID[:8], err)
			return ""
		}
	}

	return configID
}

func fallback(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

// classifyAgent decides whether a workload is an OTel Collector or an SDK,
// based on identifying attributes. The order of checks is significant:
// otelcol.version is authoritative when present; os.description containing
// "otelcol" catches collectors that omit it; telemetry.sdk.language is
// authoritative for SDKs; the service.name fallback preserves backward
// compatibility with agents that report none of the above.
func classifyAgent(attrs map[string]string) string {
	if attrs["otelcol.version"] != "" {
		return "collector"
	}
	if strings.Contains(strings.ToLower(attrs["os.description"]), "otelcol") {
		return "collector"
	}
	if attrs["telemetry.sdk.language"] != "" {
		return "sdk"
	}
	if isCollectorName(attrs["service.name"]) {
		return "collector"
	}
	return "sdk"
}

// isCollectorName returns true if the service.name indicates an OTel Collector.
// Collectors typically report as "otelcol", "otelcol-contrib", "otelcol-custom",
// or "io.opentelemetry.collector".
func isCollectorName(name string) bool {
	n := strings.ToLower(name)
	return strings.HasPrefix(n, "otelcol") ||
		strings.Contains(n, "opentelemetry-collector") ||
		strings.Contains(n, "opentelemetry.collector")
}
