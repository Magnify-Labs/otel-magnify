#!/usr/bin/env bash
# Stub emitting two distinct confmap errors so the parser must split them.
set -euo pipefail
cat >/dev/null
cat >&2 <<'EOF'
Error: invalid configuration: exporters::otlphttp/primary: endpoint must be a non-empty string

Error: invalid configuration: service::pipelines::traces::receivers: references unknown receiver "ghost"
EOF
exit 1
