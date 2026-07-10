package config

import (
	"os"
	"testing"
	"time"
)

func TestLoadWorkloadDefaults(t *testing.T) {
	for _, k := range []string{
		"WORKLOAD_RETENTION_DAYS",
		"WORKLOAD_DISCONNECT_GRACE_SECONDS",
		"WORKLOAD_JANITOR_INTERVAL_SECONDS",
		"WORKLOAD_EVENT_RETENTION_DAYS",
	} {
		os.Unsetenv(k)
	}
	c := Load()
	if c.WorkloadRetention != 30*24*time.Hour {
		t.Errorf("WorkloadRetention default wrong: %v", c.WorkloadRetention)
	}
	if c.WorkloadDisconnectGrace != 120*time.Second {
		t.Errorf("WorkloadDisconnectGrace default wrong: %v", c.WorkloadDisconnectGrace)
	}
	if c.WorkloadJanitorInterval != 300*time.Second {
		t.Errorf("WorkloadJanitorInterval default wrong: %v", c.WorkloadJanitorInterval)
	}
	if c.WorkloadEventRetention != 30*24*time.Hour {
		t.Errorf("WorkloadEventRetention default wrong: %v", c.WorkloadEventRetention)
	}
}

func TestLoadWorkloadOverrides(t *testing.T) {
	t.Setenv("WORKLOAD_RETENTION_DAYS", "7")
	t.Setenv("WORKLOAD_DISCONNECT_GRACE_SECONDS", "30")
	t.Setenv("WORKLOAD_JANITOR_INTERVAL_SECONDS", "60")
	t.Setenv("WORKLOAD_EVENT_RETENTION_DAYS", "14")
	c := Load()
	if c.WorkloadRetention != 7*24*time.Hour {
		t.Errorf("got %v", c.WorkloadRetention)
	}
	if c.WorkloadDisconnectGrace != 30*time.Second {
		t.Errorf("got %v", c.WorkloadDisconnectGrace)
	}
	if c.WorkloadJanitorInterval != 60*time.Second {
		t.Errorf("got %v", c.WorkloadJanitorInterval)
	}
	if c.WorkloadEventRetention != 14*24*time.Hour {
		t.Errorf("got %v", c.WorkloadEventRetention)
	}
}

func TestLoadInvalidValuesFallBackToDefault(t *testing.T) {
	t.Setenv("WORKLOAD_RETENTION_DAYS", "not-a-number")
	c := Load()
	if c.WorkloadRetention != 30*24*time.Hour {
		t.Errorf("got %v", c.WorkloadRetention)
	}
}

func TestLoadOpAMPSharedSecret(t *testing.T) {
	t.Setenv("OPAMP_SHARED_SECRET", "opamp-psk")

	c := Load()

	if c.OpAMPSharedSecret != "opamp-psk" {
		t.Fatalf("OpAMPSharedSecret = %q, want %q", c.OpAMPSharedSecret, "opamp-psk")
	}
}

func TestLoadDatabasePoolDefaults(t *testing.T) {
	for _, key := range []string{
		"DB_MAX_OPEN_CONNS",
		"DB_MAX_IDLE_CONNS",
		"DB_CONN_MAX_LIFETIME_SECONDS",
	} {
		t.Setenv(key, "")
	}

	c := Load()
	if c.DBMaxOpenConns != 40 {
		t.Fatalf("DBMaxOpenConns = %d, want 40", c.DBMaxOpenConns)
	}
	if c.DBMaxIdleConns != 10 {
		t.Fatalf("DBMaxIdleConns = %d, want 10", c.DBMaxIdleConns)
	}
	if c.DBConnMaxLifetime != 30*time.Minute {
		t.Fatalf("DBConnMaxLifetime = %v, want %v", c.DBConnMaxLifetime, 30*time.Minute)
	}
}

func TestLoadDatabasePoolLifetimeInvalidValuesFallBackToDefault(t *testing.T) {
	for _, value := range []string{"not-a-number", "0", "-1"} {
		t.Run(value, func(t *testing.T) {
			t.Setenv("DB_CONN_MAX_LIFETIME_SECONDS", value)

			c := Load()
			if c.DBConnMaxLifetime != 30*time.Minute {
				t.Fatalf("DBConnMaxLifetime = %v, want %v", c.DBConnMaxLifetime, 30*time.Minute)
			}
		})
	}
}
