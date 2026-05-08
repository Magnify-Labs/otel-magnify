#!/usr/bin/env bash
# Stub that sleeps long enough to hit the configured timeout, then would
# exit successfully if not killed. Used to assert the timeout branch.
set -euo pipefail
cat >/dev/null
sleep 5
exit 0
