package api

import (
	"database/sql"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/magnify-labs/otel-magnify/internal/audit"
)

// handleArchiveWorkload marks a workload as archived immediately. An
// administrator can later hard-delete it via DELETE /api/workloads/{id}.
func (a *API) handleArchiveWorkload(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if _, err := a.db.GetWorkload(id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			respondError(w, http.StatusNotFound, "workload not found")
			return
		}
		respondError(w, 500, "failed to load workload")
		return
	}
	retentionUntil := time.Now().UTC().Add(a.workloadRetention)
	if err := a.db.MarkWorkloadDisconnected(id, retentionUntil); err != nil {
		respondError(w, 500, "failed to archive workload")
		return
	}
	if err := audit.Emit(r.Context(), a.audit, "workload.archive", "workload", id, ""); err != nil {
		respondAuditUnavailable(w, sideEffectApplied)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
