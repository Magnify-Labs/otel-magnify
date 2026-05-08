#!/usr/bin/env bash
# Stub that exits non-zero with stderr that does not match the upstream format.
# Exercises the raw-stderr fallback branch.
set -euo pipefail
cat >/dev/null
echo "panic: runtime error: index out of range [5] with length 3" >&2
exit 2
