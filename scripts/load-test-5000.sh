#!/usr/bin/env bash
# Run the destructive-to-capacity, but isolated, 5,000 collector OpAMP scenario.

set -euo pipefail

cd "$(dirname "$0")/.."

if [ "${LOAD_TEST_CONFIRM:-}" != "5000" ]; then
  echo "set LOAD_TEST_CONFIRM=5000 to run the 5,000 collector load test" >&2
  exit 2
fi

require_env() {
  local name="$1"
  if [ -z "${!name:-}" ]; then
    echo "${name} must be set" >&2
    exit 2
  fi
}

require_command() {
  local name="$1"
  if ! command -v "$name" >/dev/null 2>&1; then
    echo "required command not found: ${name}" >&2
    exit 2
  fi
}

require_env "DB_DSN"
require_env "JWT_SECRET"
require_env "OPAMP_SHARED_SECRET"
require_command "docker"
require_command "jq"

export DB_DSN
export JWT_SECRET
export OPAMP_SHARED_SECRET

project_name="otel-magnify-load-5000-$$"
output_dir="${LOAD_TEST_OUTPUT_DIR:-$(mktemp -d "${TMPDIR:-/tmp}/otel-magnify-load-5000.XXXXXX")}"
load_test_ramp="${LOAD_TEST_RAMP:-5m}"
load_test_hold="${LOAD_TEST_HOLD:-10m}"

mkdir -p "$output_dir"

cleanup() {
  # This project name is unique to this invocation. Deliberately omit -v so
  # neither this command nor a naming mistake can remove a production volume.
  docker compose -p "$project_name" down --remove-orphans >/dev/null 2>&1 || true
}
trap cleanup EXIT

load_test_compose() {
  # Keep the benchmark entirely inside its unique network so its fixed service
  # ports cannot collide with a developer's local Compose or production stack.
  printf '%s\n' \
    'services:' \
    '  otel-magnify:' \
    '    ports: !override []' \
    '  postgres:' \
    '    ports: !override []' \
    | docker compose -p "$project_name" -f docker-compose.yml -f - "$@"
}

echo "load-test artifacts: ${output_dir}"
echo "starting isolated Compose project: ${project_name}"
load_test_compose up -d --build

echo "waiting for /healthz (up to 120 seconds)"
for attempt in $(seq 1 120); do
  if load_test_compose exec -T otel-magnify \
    wget -q -O /dev/null http://127.0.0.1:8080/healthz; then
    break
  fi
  if [ "$attempt" -eq 120 ]; then
    load_test_compose logs --no-color >"${output_dir}/compose.log" || true
    echo "server did not become healthy within 120 seconds" >&2
    exit 1
  fi
  sleep 1
done

set +e
docker run --rm \
  --network "${project_name}_default" \
  -v "$PWD:/app:ro" \
  -w /app \
  -e OPAMP_SHARED_SECRET \
  golang:1.25.12 \
  go run ./cmd/opamp-load \
  --endpoint "ws://otel-magnify:4320/v1/opamp" \
  --collectors 5000 \
  --ramp "$load_test_ramp" \
  --hold "$load_test_hold" \
  | tee "${output_dir}/summary.json"
client_status="${PIPESTATUS[0]}"
set -e

container_ids=()
while IFS= read -r container_id; do
  if [ -n "$container_id" ]; then
    container_ids+=("$container_id")
  fi
done < <(load_test_compose ps -q)
if [ "${#container_ids[@]}" -gt 0 ]; then
  docker stats --no-stream "${container_ids[@]}" >"${output_dir}/docker-stats.txt"
fi

load_test_compose exec -T postgres \
  psql -U magnify -d magnify \
  -c "SELECT state, count(*) FROM pg_stat_activity WHERE datname = current_database() AND pid <> pg_backend_pid() GROUP BY state ORDER BY state;" \
  >"${output_dir}/pg-stat-activity.txt"
load_test_compose logs --no-color otel-magnify \
  | grep -Ei "error|failed|panic" >"${output_dir}/opamp-errors.txt" || true

if ! jq -e \
  '.attempted == 5000 and .connected == 5000 and .failed == 0 and .disconnected == 5000' \
  "${output_dir}/summary.json" >/dev/null; then
  echo "load test summary did not meet the 5,000 connected collector target" >&2
  exit 1
fi
if [ "$client_status" -ne 0 ]; then
  echo "opamp-load exited with status ${client_status}" >&2
  exit "$client_status"
fi

echo "5,000 collector load test completed; artifacts: ${output_dir}"
