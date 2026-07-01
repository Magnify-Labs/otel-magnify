package api

import (
	"encoding/json"
	"log"
	"net/http"
	"sync"

	"github.com/gorilla/websocket"

	"github.com/magnify-labs/otel-magnify/pkg/models"
)

var upgrader = websocket.Upgrader{
	// Allow all origins — restrict in production via a proper CheckOrigin
	CheckOrigin: func(_ *http.Request) bool { return true },
}

type wsClient struct {
	conn *websocket.Conn
	send chan []byte
}

// Hub manages WebSocket clients and fans out broadcast messages to all of them.
type Hub struct {
	clients    map[*wsClient]bool
	broadcast  chan []byte
	register   chan *wsClient
	unregister chan *wsClient
	mu         sync.Mutex
	done       chan struct{}
}

// NewHub returns a Hub with initialized channels and client registry; call Run in a goroutine to start fan-out.
func NewHub() *Hub {
	return &Hub{
		clients:    make(map[*wsClient]bool),
		broadcast:  make(chan []byte, 256),
		register:   make(chan *wsClient),
		unregister: make(chan *wsClient),
		done:       make(chan struct{}),
	}
}

// Run is the central event loop; it must be called in its own goroutine.
func (h *Hub) Run() {
	for {
		select {
		case client := <-h.register:
			h.mu.Lock()
			h.clients[client] = true
			h.mu.Unlock()
		case client := <-h.unregister:
			h.mu.Lock()
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				close(client.send)
			}
			h.mu.Unlock()
		case msg := <-h.broadcast:
			h.mu.Lock()
			for client := range h.clients {
				select {
				case client.send <- msg:
				default:
					// Slow client: drop and disconnect
					delete(h.clients, client)
					close(client.send)
				}
			}
			h.mu.Unlock()
		case <-h.done:
			return
		}
	}
}

// Stop signals Run to exit.
func (h *Hub) Stop() {
	close(h.done)
}

// BroadcastConfigStatus fans out a remote config status update.
func (h *Hub) BroadcastConfigStatus(workloadID string, status models.RemoteConfigStatus) {
	status = status.Sanitized()
	event := map[string]any{
		"type":        "workload_config_status",
		"workload_id": workloadID,
		"status":      status,
	}
	data, err := json.Marshal(event)
	if err != nil {
		log.Printf("marshal config status: %v", err)
		return
	}
	h.broadcast <- data
}

// BroadcastAutoRollback fans out an automatic rollback notification.
func (h *Hub) BroadcastAutoRollback(workloadID, fromHash, toHash, reason, targetKind string) {
	event := map[string]any{
		"type":        "auto_rollback_applied",
		"workload_id": workloadID,
		"from_hash":   fromHash,
		"to_hash":     toHash,
		"reason":      models.SanitizeRemoteConfigErrorMessage(reason),
		"target_kind": targetKind,
	}
	data, err := json.Marshal(event)
	if err != nil {
		log.Printf("marshal auto rollback: %v", err)
		return
	}
	h.broadcast <- data
}

// BroadcastWorkloadUpdate fans out a workload state change plus live-instance counts.
func (h *Hub) BroadcastWorkloadUpdate(w models.Workload, connectedInstances, driftedInstances int) {
	if w.RemoteConfigStatus != nil {
		status := w.RemoteConfigStatus.Sanitized()
		w.RemoteConfigStatus = &status
	}
	event := map[string]any{
		"type":                     "workload_update",
		"workload":                 w,
		"connected_instance_count": connectedInstances,
		"drifted_instance_count":   driftedInstances,
	}
	data, err := json.Marshal(event)
	if err != nil {
		log.Printf("marshal workload update: %v", err)
		return
	}
	h.broadcast <- data
}

// BroadcastWorkloadEvent fans out a single append-only workload event.
func (h *Hub) BroadcastWorkloadEvent(ev models.WorkloadEvent) {
	event := map[string]any{
		"type":  "workload_event",
		"event": ev,
	}
	data, err := json.Marshal(event)
	if err != nil {
		log.Printf("marshal workload event: %v", err)
		return
	}
	h.broadcast <- data
}

// BroadcastAlertUpdate fans out an alert state change to all connected clients.
func (h *Hub) BroadcastAlertUpdate(alert models.Alert) {
	event := map[string]any{
		"type":  "alert_update",
		"alert": alert,
	}
	data, err := json.Marshal(event)
	if err != nil {
		log.Printf("marshal alert update: %v", err)
		return
	}
	h.broadcast <- data
}

// HandleWS upgrades an HTTP connection to WebSocket and registers the client.
func (h *Hub) HandleWS(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("ws upgrade: %v", err)
		return
	}

	client := &wsClient{
		conn: conn,
		send: make(chan []byte, 256),
	}
	h.register <- client

	go client.writePump()
	go client.readPump(h)
}

// writePump drains the send channel and writes messages to the WebSocket.
func (c *wsClient) writePump() {
	//nolint:errcheck // deferred cleanup; connection is being torn down regardless
	defer c.conn.Close()
	for msg := range c.send {
		if err := c.conn.WriteMessage(websocket.TextMessage, msg); err != nil {
			return
		}
	}
}

// readPump consumes incoming frames so the connection stays healthy and triggers
// unregistration when the client disconnects.
func (c *wsClient) readPump(h *Hub) {
	defer func() {
		h.unregister <- c
		//nolint:errcheck,gosec // deferred cleanup; connection is being torn down regardless
		c.conn.Close()
	}()
	for {
		if _, _, err := c.conn.ReadMessage(); err != nil {
			break
		}
	}
}
