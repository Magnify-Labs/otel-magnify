package api

import (
	"database/sql"
	"net/http"
)

type databaseStatsProvider interface {
	Stats() sql.DBStats
}

type databaseStatsResponse struct {
	MaxOpenConnections int   `json:"max_open_connections"`
	OpenConnections    int   `json:"open_connections"`
	InUse              int   `json:"in_use"`
	Idle               int   `json:"idle"`
	WaitCount          int64 `json:"wait_count"`
	WaitDurationMS     int64 `json:"wait_duration_ms"`
	MaxIdleClosed      int64 `json:"max_idle_closed"`
	MaxIdleTimeClosed  int64 `json:"max_idle_time_closed"`
	MaxLifetimeClosed  int64 `json:"max_lifetime_closed"`
}

func (a *API) handleDatabaseStats(w http.ResponseWriter, _ *http.Request) {
	if isNilDatabaseStore(a.db) {
		respondError(w, http.StatusServiceUnavailable, "database statistics unavailable")
		return
	}

	provider, ok := a.db.(databaseStatsProvider)
	if !ok {
		respondError(w, http.StatusServiceUnavailable, "database statistics unavailable")
		return
	}

	stats := provider.Stats()
	respondJSON(w, http.StatusOK, databaseStatsResponse{
		MaxOpenConnections: stats.MaxOpenConnections,
		OpenConnections:    stats.OpenConnections,
		InUse:              stats.InUse,
		Idle:               stats.Idle,
		WaitCount:          stats.WaitCount,
		WaitDurationMS:     stats.WaitDuration.Milliseconds(),
		MaxIdleClosed:      stats.MaxIdleClosed,
		MaxIdleTimeClosed:  stats.MaxIdleTimeClosed,
		MaxLifetimeClosed:  stats.MaxLifetimeClosed,
	})
}
