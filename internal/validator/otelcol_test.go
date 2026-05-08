package validator

import (
	"context"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// stubBinary returns the absolute path of a testdata/*.sh stub.
// We resolve from the test source file (not os.Getwd) so the helper works
// when tests are run from any working directory.
func stubBinary(t *testing.T, name string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("stub binaries are POSIX shell scripts")
	}
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Join(filepath.Dir(file), "testdata", name)
}

func TestOtelcolValidator_OK(t *testing.T) {
	v := &OtelcolValidator{BinaryPath: stubBinary(t, "otelcol-ok.sh"), Timeout: 2 * time.Second}
	r := v.Validate(context.Background(), []byte("receivers: {otlp: {}}\n"))
	if !r.Valid {
		t.Fatalf("expected Valid=true, got %+v", r)
	}
	if len(r.Errors) != 0 {
		t.Fatalf("expected no errors, got %+v", r.Errors)
	}
}

func TestOtelcolValidator_SingleConfmapError(t *testing.T) {
	v := &OtelcolValidator{BinaryPath: stubBinary(t, "otelcol-invalid-component.sh"), Timeout: 2 * time.Second}
	r := v.Validate(context.Background(), []byte("receivers: {otlp: {bogus_field: 1}}\n"))
	if r.Valid {
		t.Fatalf("expected Valid=false, got %+v", r)
	}
	if len(r.Errors) != 1 {
		t.Fatalf("expected 1 diagnostic, got %d: %+v", len(r.Errors), r.Errors)
	}
	got := r.Errors[0]
	if got.Code != "otelcol_validate" {
		t.Errorf("Code = %q, want otelcol_validate", got.Code)
	}
	if !strings.Contains(got.Message, "invalid keys: bogus_field") {
		t.Errorf("Message missing bullet detail: %q", got.Message)
	}
	// Path is dotted form of the confmap "::" notation.
	if got.Path != "receivers.otlp" {
		t.Errorf("Path = %q, want receivers.otlp", got.Path)
	}
}

func TestOtelcolValidator_MultipleErrors(t *testing.T) {
	v := &OtelcolValidator{BinaryPath: stubBinary(t, "otelcol-multi-error.sh"), Timeout: 2 * time.Second}
	r := v.Validate(context.Background(), []byte("anything"))
	if r.Valid {
		t.Fatal("expected Valid=false")
	}
	if len(r.Errors) != 2 {
		t.Fatalf("expected 2 diagnostics, got %d: %+v", len(r.Errors), r.Errors)
	}
	if r.Errors[0].Path != "exporters.otlphttp/primary" {
		t.Errorf("first Path = %q, want exporters.otlphttp/primary", r.Errors[0].Path)
	}
	if r.Errors[1].Path != "service.pipelines.traces.receivers" {
		t.Errorf("second Path = %q, want service.pipelines.traces.receivers", r.Errors[1].Path)
	}
}

func TestOtelcolValidator_UnparseableStderrFallsBack(t *testing.T) {
	v := &OtelcolValidator{BinaryPath: stubBinary(t, "otelcol-unparseable.sh"), Timeout: 2 * time.Second}
	r := v.Validate(context.Background(), []byte("anything"))
	if r.Valid {
		t.Fatal("expected Valid=false")
	}
	if len(r.Errors) != 1 || r.Errors[0].Code != "otelcol_validate" {
		t.Fatalf("expected single otelcol_validate fallback, got %+v", r.Errors)
	}
	if !strings.Contains(r.Errors[0].Message, "panic: runtime error") {
		t.Errorf("fallback should preserve raw stderr, got %q", r.Errors[0].Message)
	}
}

func TestOtelcolValidator_Timeout(t *testing.T) {
	if testing.Short() {
		t.Skip("timeout test sleeps; skipped under -short")
	}
	v := &OtelcolValidator{BinaryPath: stubBinary(t, "otelcol-slow.sh"), Timeout: 250 * time.Millisecond}
	start := time.Now()
	r := v.Validate(context.Background(), []byte("anything"))
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Errorf("Validate did not honor timeout (took %s)", elapsed)
	}
	if r.Valid {
		t.Fatal("expected Valid=false on timeout")
	}
	if len(r.Errors) != 1 || r.Errors[0].Code != "validator_timeout" {
		t.Fatalf("expected validator_timeout error, got %+v", r.Errors)
	}
}

func TestOtelcolValidator_Unconfigured(t *testing.T) {
	v := &OtelcolValidator{}
	r := v.Validate(context.Background(), []byte("x"))
	if r.Valid {
		t.Fatal("expected Valid=false")
	}
	if len(r.Errors) != 1 || r.Errors[0].Code != "validator_unconfigured" {
		t.Fatalf("expected validator_unconfigured, got %+v", r.Errors)
	}
}

func TestOtelcolValidator_BinaryMissing(t *testing.T) {
	v := &OtelcolValidator{BinaryPath: "/nonexistent/path/to/otelcol", Timeout: time.Second}
	r := v.Validate(context.Background(), []byte("x"))
	if r.Valid {
		t.Fatal("expected Valid=false")
	}
	if len(r.Errors) != 1 || r.Errors[0].Code != "validator_unavailable" {
		t.Fatalf("expected validator_unavailable, got %+v", r.Errors)
	}
}

func TestParseOtelcolStderr_BulletsWithoutAnchor(t *testing.T) {
	// Defensive: if the upstream format ever drops the "Error:" anchor, every
	// bullet should still surface as its own diagnostic.
	in := "* receivers::otlp: bogus_field\n* exporters::logging: not registered\n"
	got := parseOtelcolStderr(in)
	if len(got) != 2 {
		t.Fatalf("expected 2, got %+v", got)
	}
	if got[0].Path != "receivers.otlp" || got[1].Path != "exporters.logging" {
		t.Errorf("paths = %q,%q", got[0].Path, got[1].Path)
	}
}
