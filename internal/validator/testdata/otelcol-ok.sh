#!/usr/bin/env bash
# Stub for `otelcol validate --config /dev/stdin` that always succeeds.
# Drains stdin so callers writing into a closed pipe don't observe SIGPIPE.
set -euo pipefail
cat >/dev/null
exit 0
