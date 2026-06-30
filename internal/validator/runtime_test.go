package validator

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestValidateWithRuntime_BinaryAbsentAddsSkippedWarning(t *testing.T) {
	result := ValidateWithRuntime(context.Background(), []byte(validMinimal), nil, RuntimeOptions{
		Enabled:    true,
		BinaryPath: filepath.Join(t.TempDir(), "missing-otelcol"),
		Timeout:    time.Second,
	})

	check := findCheck(t, result, "otelcol_runtime")
	if check.Status != "skipped" || len(check.Messages) == 0 || check.Messages[0].Code != "otelcol_not_found" || check.Messages[0].Severity != "warning" {
		t.Fatalf("unexpected runtime check: %+v", check)
	}
	if !result.Valid {
		t.Fatalf("runtime skip must not invalidate static-valid config: %+v", result)
	}
}

func TestValidateWithRuntime_CollectsVersionAndSucceeds(t *testing.T) {
	bin := writeFakeOtelcol(t, `
case "$1" in
  --version) echo "otelcol-contrib version 0.150.1"; exit 0 ;;
  validate)
    while [ "$#" -gt 0 ]; do
      if [ "$1" = "--config" ]; then shift; test -s "$1"; exit $?; fi
      shift
    done
    exit 2 ;;
esac
exit 9
`)

	result := ValidateWithRuntime(context.Background(), []byte(validMinimal), nil, RuntimeOptions{
		Enabled:       true,
		BinaryPath:    bin,
		Timeout:       time.Second,
		TargetVersion: "0.150.1",
	})

	check := findCheck(t, result, "otelcol_runtime")
	if check.Status != "passed" {
		t.Fatalf("runtime check status = %q, want passed: %+v", check.Status, check)
	}
	if check.Metadata["binary_path"] != bin || check.Metadata["binary_version"] != "0.150.1" || check.Metadata["binary_distribution"] != "otelcol-contrib" {
		t.Fatalf("runtime metadata missing binary proof: %+v", check.Metadata)
	}
	if check.Metadata["exit_code"] != float64(0) && check.Metadata["exit_code"] != 0 {
		t.Fatalf("exit_code metadata = %#v", check.Metadata["exit_code"])
	}
}

func TestValidateWithRuntime_RuntimeErrorFailsValidation(t *testing.T) {
	bin := writeFakeOtelcol(t, `
case "$1" in
  --version) echo "otelcol version 0.150.1"; exit 0 ;;
  validate) echo "bad receiver" >&2; exit 42 ;;
esac
exit 9
`)

	result := ValidateWithRuntime(context.Background(), []byte(validMinimal), nil, RuntimeOptions{
		Enabled:    true,
		BinaryPath: bin,
		Timeout:    time.Second,
	})

	check := findCheck(t, result, "otelcol_runtime")
	if result.Valid || check.Status != "failed" || len(check.Messages) == 0 || check.Messages[0].Code != "otelcol_validation_failed" {
		t.Fatalf("runtime failure not reflected: result=%+v check=%+v", result, check)
	}
	if !strings.Contains(check.Messages[0].Message, "bad receiver") {
		t.Fatalf("stderr not captured in message: %+v", check.Messages[0])
	}
}

func TestValidateWithRuntime_TimeoutIsNonBlockingWarning(t *testing.T) {
	bin := writeFakeOtelcol(t, `
case "$1" in
  --version) echo "otelcol version 0.150.1"; exit 0 ;;
  validate) sleep 2; exit 0 ;;
esac
exit 9
`)

	result := ValidateWithRuntime(context.Background(), []byte(validMinimal), nil, RuntimeOptions{
		Enabled:    true,
		BinaryPath: bin,
		Timeout:    25 * time.Millisecond,
	})

	check := findCheck(t, result, "otelcol_runtime")
	if !result.Valid || check.Status != "warning" || len(check.Messages) == 0 || check.Messages[0].Code != "otelcol_runtime_timeout" {
		t.Fatalf("timeout should be non-blocking warning: result=%+v check=%+v", result, check)
	}
}

func TestValidateWithRuntime_CleansTemporaryConfigFile(t *testing.T) {
	seenPath := filepath.Join(t.TempDir(), "seen-config-path")
	bin := writeFakeOtelcol(t, `
case "$1" in
  --version) echo "otelcol version 0.150.1"; exit 0 ;;
  validate)
    while [ "$#" -gt 0 ]; do
      if [ "$1" = "--config" ]; then shift; printf "%s" "$1" > "`+seenPath+`"; test -f "$1"; exit $?; fi
      shift
    done
    exit 2 ;;
esac
exit 9
`)

	result := ValidateWithRuntime(context.Background(), []byte(validMinimal), nil, RuntimeOptions{
		Enabled:    true,
		BinaryPath: bin,
		Timeout:    time.Second,
	})
	if !result.Valid {
		t.Fatalf("expected valid result: %+v", result)
	}
	configPathBytes, err := os.ReadFile(seenPath)
	if err != nil {
		t.Fatalf("fake otelcol did not record config path: %v", err)
	}
	if _, err := os.Stat(string(configPathBytes)); !os.IsNotExist(err) {
		t.Fatalf("temporary config file was not cleaned up, stat err=%v", err)
	}
}

func TestValidateWithRuntime_TargetVersionMismatchWarns(t *testing.T) {
	bin := writeFakeOtelcol(t, `
case "$1" in
  --version) echo "otelcol version 0.149.0"; exit 0 ;;
  validate) exit 0 ;;
esac
exit 9
`)

	result := ValidateWithRuntime(context.Background(), []byte(validMinimal), nil, RuntimeOptions{
		Enabled:       true,
		BinaryPath:    bin,
		Timeout:       time.Second,
		TargetVersion: "0.150.1",
	})

	check := findCheck(t, result, "otelcol_runtime")
	if check.Status != "warning" || !hasMessage(check, "otelcol_version_mismatch") {
		t.Fatalf("target mismatch warning missing: %+v", check)
	}
	if !result.Valid {
		t.Fatalf("version mismatch warning must not invalidate config: %+v", result)
	}
}

func writeFakeOtelcol(t *testing.T, body string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shell fake uses POSIX sh")
	}
	path := filepath.Join(t.TempDir(), "otelcol")
	content := "#!/bin/sh\n" + body
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatalf("write fake otelcol: %v", err)
	}
	return path
}

func findCheck(t *testing.T, result Result, id string) Check {
	t.Helper()
	for _, check := range result.Checks {
		if check.ID == id {
			return check
		}
	}
	t.Fatalf("check %q not found in %+v", id, result.Checks)
	return Check{}
}

func hasMessage(check Check, code string) bool {
	for _, msg := range check.Messages {
		if msg.Code == code {
			return true
		}
	}
	return false
}
