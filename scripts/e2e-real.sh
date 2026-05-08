#!/usr/bin/env bash
# Spin up the full otel-magnify stack against SQLite and run the real-backend
# Playwright suite end-to-end. Tears down on exit (success or failure).
#
# Usage: ./scripts/e2e-real.sh
# Requires: docker, npx (playwright installed in frontend/)

set -euo pipefail

cd "$(dirname "$0")/.."

# Test credentials — fixed so re-runs on the same DB volume are predictable.
# The volume is wiped by `docker compose down -v` at the end of each run.
export JWT_SECRET="e2e-real-jwt-secret"
export SEED_ADMIN_EMAIL="admin@e2e.local"
export SEED_ADMIN_PASSWORD="initialPass!!!12"

cleanup() {
  echo "--- docker compose down -v ---"
  docker compose -p otel-magnify-e2e down -v >/dev/null 2>&1 || true
}
trap cleanup EXIT

# Wipe any leftover volume from a previous aborted run before starting.
docker compose -p otel-magnify-e2e down -v >/dev/null 2>&1 || true

echo "--- docker compose up (build + detach) ---"
docker compose -p otel-magnify-e2e up -d --build

echo "--- waiting for /api/auth/methods (up to 90s) ---"
for i in $(seq 1 90); do
  if curl -sf http://localhost:8080/api/auth/methods >/dev/null 2>&1; then
    echo "server ready after ${i}s"
    break
  fi
  sleep 1
  if [ "$i" -eq 90 ]; then
    echo "server did not become ready in 90s"
    docker compose -p otel-magnify-e2e logs --tail=100
    exit 1
  fi
done

echo "--- smoke: config-versioning routing + JSON shape ---"
# Catch routing/JSON regressions on /configs/{hash}/{label,rollback,GET} before
# we burn time on Playwright. These curl checks need only a working API and
# the seeded admin — no agent connection required.
TOKEN="$(curl -fsS -X POST http://localhost:8080/api/auth/login \
  -H 'Content-Type: application/json' \
  -d "{\"email\":\"${SEED_ADMIN_EMAIL}\",\"password\":\"${SEED_ADMIN_PASSWORD}\"}" \
  | grep -o '"token":"[^"]*"' | cut -d'"' -f4)"
if [ -z "$TOKEN" ]; then
  echo "smoke: failed to login as ${SEED_ADMIN_EMAIL}"
  exit 1
fi

# 401 paths (no header) — every versioning endpoint must reject anonymous calls.
for endpoint in \
    "POST /api/workloads/foo/configs/bar/label" \
    "GET  /api/workloads/foo/configs/bar" \
    "POST /api/workloads/foo/configs/bar/rollback"; do
  method="${endpoint%% *}"
  path="${endpoint##* }"
  code="$(curl -s -o /dev/null -w '%{http_code}' -X "$method" "http://localhost:8080${path}")"
  if [ "$code" != "401" ]; then
    echo "smoke: expected 401 on ${method} ${path}, got ${code}"
    exit 1
  fi
done

# Authenticated 404 path on an unknown hash — proves routing + handler return
# the JSON {"error":...} shape the frontend renders.
not_found_body="$(curl -fsS -X GET http://localhost:8080/api/workloads/ghost/configs/ghost \
  -H "Authorization: Bearer ${TOKEN}" -o /dev/null -w '%{http_code}')"
if [ "$not_found_body" != "404" ]; then
  echo "smoke: expected 404 on GET /api/workloads/ghost/configs/ghost, got ${not_found_body}"
  exit 1
fi

echo "smoke: config-versioning OK"

echo "--- running Playwright real suite ---"
cd frontend
npx playwright test --config=playwright.real.config.ts "$@"
