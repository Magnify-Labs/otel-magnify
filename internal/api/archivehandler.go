package api

import (
	"database/sql"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/magnify-labs/otel-magnify/internal/audit"
)

// handleArchiveWorkload hides a stale workload from the default inventory
// immediately. An administrator can later hard-delete it via
// DELETE /api/workloads/{id}.
func (a *API) handleArchiveWorkload(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	wl, err := a.db.GetWorkload(id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			respondError(w, http.StatusNotFound, "workload not found")
			return
		}
		respondError(w, 500, "failed to load workload")
		return
	}
	if wl.Status != "disconnected" {
		respondJSON(w, http.StatusConflict, map[string]string{"error": "only disconnected workloads can be archived", "code": "workload_not_disconnected"})
		return
	}
	if err := a.db.ArchiveWorkload(id, time.Now().UTC()); err != nil {
		respondError(w, 500, "failed to archive workload")
		return
	}
	if err := audit.Emit(r.Context(), a.audit, "workload.archive", "workload", id, ""); err != nil {
		respondAuditUnavailable(w, sideEffectApplied)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
