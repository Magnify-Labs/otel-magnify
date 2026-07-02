package api

import (
	"encoding/csv"
	"net/http"
	"strconv"
	"time"

	"github.com/magnify-labs/otel-magnify/pkg/ext"
)

const (
	defaultAuditEventLimit = 50
	maxAuditEventLimit     = 500
)

type auditEventsResponse struct {
	Available bool              `json:"available"`
	Events    []ext.AuditRecord `json:"events"`
	Total     int               `json:"total"`
	Limit     int               `json:"limit"`
	Offset    int               `json:"offset"`
}

func (a *API) handleListAuditEvents(w http.ResponseWriter, r *http.Request) {
	filter, ok := parseAuditEventFilter(w, r)
	if !ok {
		return
	}
	page, available, ok := a.queryAuditEvents(w, r, filter)
	if !ok {
		return
	}
	respondJSON(w, http.StatusOK, auditEventsResponse{
		Available: available,
		Events:    nonNilAuditRecords(page.Events),
		Total:     page.Total,
		Limit:     filter.Limit,
		Offset:    filter.Offset,
	})
}

func (a *API) handleExportAuditEventsCSV(w http.ResponseWriter, r *http.Request) {
	filter, ok := parseAuditEventFilter(w, r)
	if !ok {
		return
	}
	page, _, ok := a.queryAuditEvents(w, r, filter)
	if !ok {
		return
	}

	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="audit-events.csv"`)
	w.WriteHeader(http.StatusOK)

	writer := csv.NewWriter(w)
	_ = writer.Write([]string{"id", "occurred_at", "action", "user_id", "email", "resource", "resource_id", "workload_id", "config_hash", "detail", "prev_hash", "event_hash", "immutable_ref"})
	for _, ev := range page.Events {
		_ = writer.Write(safeAuditCSVRow([]string{
			ev.ID,
			ev.OccurredAt.UTC().Format(time.RFC3339Nano),
			ev.Action,
			ev.UserID,
			ev.Email,
			ev.Resource,
			ev.ResourceID,
			ev.WorkloadID,
			ev.ConfigHash,
			ev.Detail,
			ev.PrevHash,
			ev.EventHash,
			ev.ImmutableRef,
		}))
	}
	writer.Flush()
}

func safeAuditCSVRow(row []string) []string {
	safe := make([]string, len(row))
	for i, cell := range row {
		safe[i] = safeAuditCSVCell(cell)
	}
	return safe
}

func safeAuditCSVCell(cell string) string {
	if cell == "" {
		return cell
	}
	switch cell[0] {
	case '=', '+', '-', '@', '	', '\r':
		return "'" + cell
	default:
		return cell
	}
}

func (a *API) queryAuditEvents(w http.ResponseWriter, r *http.Request, filter ext.AuditEventFilter) (ext.AuditEventPage, bool, bool) {
	querier, ok := a.audit.(ext.AuditEventQuerier)
	if !ok || querier == nil {
		return ext.AuditEventPage{Events: []ext.AuditRecord{}}, false, true
	}
	page, err := querier.ListAuditEvents(r.Context(), filter)
	if err != nil {
		respondError(w, http.StatusServiceUnavailable, "audit unavailable")
		return ext.AuditEventPage{}, true, false
	}
	page.Events = nonNilAuditRecords(page.Events)
	return page, true, true
}

func parseAuditEventFilter(w http.ResponseWriter, r *http.Request) (ext.AuditEventFilter, bool) {
	q := r.URL.Query()
	filter := ext.AuditEventFilter{
		User:       q.Get("user"),
		UserID:     q.Get("user_id"),
		Email:      q.Get("email"),
		Action:     q.Get("action"),
		ResourceID: q.Get("resource_id"),
		WorkloadID: q.Get("workload_id"),
		ConfigHash: q.Get("config_hash"),
		Limit:      defaultAuditEventLimit,
	}

	if raw := q.Get("limit"); raw != "" {
		limit, err := strconv.Atoi(raw)
		if err != nil || limit < 1 {
			respondError(w, http.StatusBadRequest, "invalid limit")
			return ext.AuditEventFilter{}, false
		}
		if limit > maxAuditEventLimit {
			limit = maxAuditEventLimit
		}
		filter.Limit = limit
	}
	if raw := q.Get("offset"); raw != "" {
		offset, err := strconv.Atoi(raw)
		if err != nil || offset < 0 {
			respondError(w, http.StatusBadRequest, "invalid offset")
			return ext.AuditEventFilter{}, false
		}
		filter.Offset = offset
	}
	if raw := q.Get("from"); raw != "" {
		from, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			respondError(w, http.StatusBadRequest, "invalid from")
			return ext.AuditEventFilter{}, false
		}
		filter.From = from
	}
	if raw := q.Get("to"); raw != "" {
		to, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			respondError(w, http.StatusBadRequest, "invalid to")
			return ext.AuditEventFilter{}, false
		}
		filter.To = to
	}
	return filter, true
}

func nonNilAuditRecords(events []ext.AuditRecord) []ext.AuditRecord {
	if events == nil {
		return []ext.AuditRecord{}
	}
	return events
}
