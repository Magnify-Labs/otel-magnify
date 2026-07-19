// Package config loads the server's runtime settings from environment variables.
package config

import (
	"os"
	"strconv"
	"time"
)

// Config groups the server's runtime settings: DB, listen addresses, JWT secret, CORS, and workload lifecycle tuning.
type Config struct {
	DBDSN             string // PostgreSQL connection string
	DBMaxOpenConns    int
	DBMaxIdleConns    int
	DBConnMaxIdleTime time.Duration
	DBConnMaxLifetime time.Duration
	ListenAddr        string // e.g. ":8080"
	OpAMPAddr         string // e.g. ":4320"
	OpAMPSharedSecret string // bearer token required by OpAMP clients; empty = disabled for local/dev
	JWTSecret         string
	CORSOrigins       string // comma-separated allowed origins
	MinAgentVersion   string // minimum required agent version; empty = disabled
	WebhookURL        string // HTTP endpoint to notify on alert fire; empty = disabled

	// Workload lifecycle tuning.
	WorkloadRetention       time.Duration // how long disconnected workloads linger before archival
	WorkloadDisconnectGrace time.Duration // grace period before marking a workload disconnected
	WorkloadJanitorInterval time.Duration // tick interval of the workload janitor loop
	WorkloadEventRetention  time.Duration // how long workload events are kept before purge
}

// Load reads the configuration from environment variables, applying default values for missing or invalid entries.
func Load() Config {
	return Config{
		DBDSN:                   getenv("DB_DSN", ""),
		DBMaxOpenConns:          positiveInt(getenv("DB_MAX_OPEN_CONNS", "40"), 40),
		DBMaxIdleConns:          positiveInt(getenv("DB_MAX_IDLE_CONNS", "10"), 10),
		DBConnMaxIdleTime:       dbConnMaxIdleTime(getenv("DB_CONN_MAX_IDLE_TIME_SECONDS", "300")),
		DBConnMaxLifetime:       dbConnMaxLifetime(getenv("DB_CONN_MAX_LIFETIME_SECONDS", "1800")),
		ListenAddr:              getenv("LISTEN_ADDR", ":8080"),
		OpAMPAddr:               getenv("OPAMP_ADDR", ":4320"),
		OpAMPSharedSecret:       getenv("OPAMP_SHARED_SECRET", ""),
		JWTSecret:               getenv("JWT_SECRET", ""),
		CORSOrigins:             getenv("CORS_ORIGINS", "http://localhost:5173"),
		MinAgentVersion:         getenv("MIN_AGENT_VERSION", ""),
		WebhookURL:              getenv("WEBHOOK_URL", ""),
		WorkloadRetention:       days(getenv("WORKLOAD_RETENTION_DAYS", "30")),
		WorkloadDisconnectGrace: seconds(getenv("WORKLOAD_DISCONNECT_GRACE_SECONDS", "120")),
		WorkloadJanitorInterval: seconds(getenv("WORKLOAD_JANITOR_INTERVAL_SECONDS", "300")),
		WorkloadEventRetention:  days(getenv("WORKLOAD_EVENT_RETENTION_DAYS", "30")),
	}
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// days parses a positive integer day count, falling back to 30 days on
// invalid or non-positive input. Keeping the fallback matches the default
// passed by Load so a malformed env var never produces a zero duration.
func days(s string) time.Duration {
	n, err := strconv.Atoi(s)
	if err != nil || n <= 0 {
		n = 30
	}
	return time.Duration(n) * 24 * time.Hour
}

// seconds parses a positive integer second count, falling back to 1s on
// invalid or non-positive input. The 1s floor avoids a zero-interval
// ticker if the operator misconfigures the janitor period.
func seconds(s string) time.Duration {
	n, err := strconv.Atoi(s)
	if err != nil || n <= 0 {
		n = 1
	}
	return time.Duration(n) * time.Second
}

func dbConnMaxLifetime(s string) time.Duration {
	return time.Duration(positiveInt(s, 1800)) * time.Second
}

func dbConnMaxIdleTime(s string) time.Duration {
	return time.Duration(positiveInt(s, 300)) * time.Second
}

func positiveInt(s string, fallback int) int {
	n, err := strconv.Atoi(s)
	if err != nil || n <= 0 {
		return fallback
	}
	return n
}
