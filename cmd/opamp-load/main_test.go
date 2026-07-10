package main

import (
	"strings"
	"testing"
)

func TestConfigRejectsZeroCollectors(t *testing.T) {
	_, err := parseConfig([]string{"--collectors", "0"})
	if err == nil || !strings.Contains(err.Error(), "collectors") {
		t.Fatalf("error = %v", err)
	}
}

func TestConfigAcceptsFiveThousandCollectors(t *testing.T) {
	config, err := parseConfig([]string{"--collectors", "5000"})
	if err != nil {
		t.Fatalf("parseConfig() error = %v", err)
	}
	if config.collectors != 5000 {
		t.Fatalf("collectors = %d, want 5000", config.collectors)
	}
}
