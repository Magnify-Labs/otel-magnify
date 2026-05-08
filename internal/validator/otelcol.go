package validator

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

// OtelcolValidator wraps the upstream `otelcol` binary's `validate` subcommand.
// Compared to the in-process light validator (Validate), this catches per-component
// schema errors — invalid keys inside a receiver/processor/exporter block, type
// mismatches, missing required fields — that the light parser cannot detect
// because it does not know each component's option schema.
//
// Spawned under the same UID as the server process. Stdin streams the candidate
// configuration; stderr carries diagnostics. We do not setuid: the server
// already runs as a low-privilege user (Dockerfile creates 'magnify' uid 10001),
// and the validate subcommand performs no I/O beyond reading the config.
type OtelcolValidator struct {
	BinaryPath string
	Timeout    time.Duration
}

// DefaultOtelcolValidateTimeout is the wall-clock deadline applied when the
// caller did not set OtelcolValidator.Timeout. Picked generously: a cold-start
// otelcol on a small VM takes ~1s, so 10s leaves margin for slow CI hosts
// without making a misconfigured binary hang the request.
const DefaultOtelcolValidateTimeout = 10 * time.Second

// Validate spawns `<BinaryPath> validate --config /dev/stdin`, writes content
// to the child's stdin, and parses stderr. Returns Valid: true on exit code 0,
// otherwise a Result populated with parsed Errors.
func (v *OtelcolValidator) Validate(ctx context.Context, content []byte) Result {
	if v == nil || v.BinaryPath == "" {
		return Result{Errors: []Error{{
			Code:    "validator_unconfigured",
			Message: "otelcol binary path is not configured (set BINARY_OTELCOL)",
		}}}
	}

	timeout := v.Timeout
	if timeout <= 0 {
		timeout = DefaultOtelcolValidateTimeout
	}
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(cctx, v.BinaryPath, "validate", "--config", "/dev/stdin")
	cmd.Stdin = bytes.NewReader(content)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	// WaitDelay bounds how long Wait() blocks for I/O goroutines after the
	// process is killed. Without it, a child that exec'd a grandchild (e.g.
	// `bash -c 'sleep N'`) would keep stderr open via the grandchild and
	// stall Run() until that grandchild exits — defeating the timeout.
	cmd.WaitDelay = 500 * time.Millisecond

	runErr := cmd.Run()

	// Distinguish timeout from other failures: cctx.Err() is set by
	// CommandContext-on-deadline regardless of whether Run returned the
	// killed-process error or a clean exit.
	if errors.Is(cctx.Err(), context.DeadlineExceeded) {
		return Result{Errors: []Error{{
			Code:    "validator_timeout",
			Message: fmt.Sprintf("otelcol validate timed out after %s", timeout),
		}}}
	}

	if runErr == nil {
		return Result{Valid: true}
	}

	// Process never started (binary missing, permission denied, ENOENT).
	// ProcessState stays nil in that branch; treat as a server-side
	// configuration error rather than a validation diagnostic so the operator
	// can fix the deployment instead of hunting the user's YAML.
	var pathErr *fs.PathError
	if cmd.ProcessState == nil || errors.As(runErr, &pathErr) {
		cause := runErr.Error()
		if pathErr != nil {
			cause = pathErr.Err.Error()
		}
		return Result{Errors: []Error{{
			Code:    "validator_unavailable",
			Message: fmt.Sprintf("cannot run otelcol binary at %q: %s", v.BinaryPath, cause),
		}}}
	}

	diagnostics := parseOtelcolStderr(stderr.String())
	if len(diagnostics) == 0 {
		// Non-zero exit with no parseable diagnostic — keep the raw stderr
		// (trimmed) so the operator at least sees something actionable.
		raw := strings.TrimSpace(stderr.String())
		if raw == "" {
			raw = runErr.Error()
		}
		diagnostics = []Error{{Code: "otelcol_validate", Message: raw}}
	}
	return Result{Errors: diagnostics}
}

// pathSegment matches the `section::name` and `section::name::sub` forms that
// the upstream confmap error formatter uses to locate offending nodes.
// Examples it captures: "receivers::otlp", "exporters::otlphttp/primary",
// "service::pipelines::traces::receivers". The leading section is one or more
// lowercase letters; subsequent segments allow alphanumerics, dot, slash, dash,
// and underscore so we don't drop named instances ("otlp/secondary") or hostnames.
var confmapPathRE = regexp.MustCompile(`\b([a-z]+(?:::[A-Za-z0-9_./-]+)+)\b`)

// parseOtelcolStderr converts otelcol's multi-line error output into
// validator.Error entries. The general shape is:
//
//	Error: invalid configuration: receivers::otlp: 1 error(s) decoding:
//
//	* '' has invalid keys: bogus_field
//
// The first "Error:" line becomes the parent diagnostic; subsequent "* "
// bullet lines are appended as sub-points and parsed for an inner path.
// Each bullet emitted before any "Error:" anchor becomes its own diagnostic
// (defensive: handles cases where the upstream format changes).
func parseOtelcolStderr(stderr string) []Error {
	if strings.TrimSpace(stderr) == "" {
		return nil
	}

	var errs []Error
	var current *Error
	flush := func() {
		if current != nil {
			errs = append(errs, *current)
			current = nil
		}
	}

	for _, raw := range strings.Split(stderr, "\n") {
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" {
			continue
		}
		if strings.HasPrefix(trimmed, "Error:") {
			flush()
			msg := strings.TrimSpace(strings.TrimPrefix(trimmed, "Error:"))
			current = &Error{
				Code:    "otelcol_validate",
				Message: msg,
				Path:    extractConfmapPath(msg),
			}
			continue
		}
		if strings.HasPrefix(trimmed, "*") {
			bullet := strings.TrimSpace(strings.TrimLeft(trimmed, "*"))
			if bullet == "" {
				continue
			}
			if current == nil {
				errs = append(errs, Error{
					Code:    "otelcol_validate",
					Message: bullet,
					Path:    extractConfmapPath(bullet),
				})
				continue
			}
			current.Message += "\n  * " + bullet
			// Refine path if the parent didn't already carry one and the
			// bullet exposes a deeper location.
			if current.Path == "" {
				current.Path = extractConfmapPath(bullet)
			}
		}
	}
	flush()
	return errs
}

// extractConfmapPath converts a confmap-style "::"-separated path to the
// dotted form used by the rest of the validator package, so frontends can
// render every diagnostic with a uniform path style.
func extractConfmapPath(s string) string {
	m := confmapPathRE.FindString(s)
	if m == "" {
		return ""
	}
	return strings.ReplaceAll(m, "::", ".")
}
