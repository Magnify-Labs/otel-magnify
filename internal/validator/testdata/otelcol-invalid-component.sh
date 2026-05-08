#!/usr/bin/env bash
# Stub emitting a typical confmap "invalid keys" error for an unknown field.
set -euo pipefail
cat >/dev/null
cat >&2 <<'EOF'
Error: invalid configuration: receivers::otlp: 1 error(s) decoding:

* '' has invalid keys: bogus_field
EOF
exit 1
