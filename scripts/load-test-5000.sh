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

local_postgres_dsn="postgres://magnify:magnify@postgres:5432/magnify?sslmode=disable"

# The application can only reach the PostgreSQL container from this isolated
# Compose project. Ignore inherited database settings from the caller.
export DB_DSN="$local_postgres_dsn"
export POSTGRES_PASSWORD="magnify"
export DB_MAX_OPEN_CONNS="40"
export DB_DSN
export JWT_SECRET
export OPAMP_SHARED_SECRET

project_name="otel-magnify-load-5000-$$"
output_dir="${LOAD_TEST_OUTPUT_DIR:-$(mktemp -d "${TMPDIR:-/tmp}/otel-magnify-load-5000.XXXXXX")}"
load_test_ramp="${LOAD_TEST_RAMP:-5m}"
load_test_hold="${LOAD_TEST_HOLD:-10m}"
ready_file="${output_dir}/ready.json"
summary_file="${output_dir}/summary.json"
client_stderr_file="${output_dir}/opamp-load.stderr"
opamp_errors_file="${output_dir}/opamp-errors.txt"
compose_log_file="${output_dir}/compose.log"
docker_stats_file="${output_dir}/docker-stats.txt"
postgres_activity_file="${output_dir}/pg-stat-activity.txt"
client_pid=""

mkdir -p "$output_dir"

# Reuse the caller's directory, but never let a previous run satisfy this
# invocation's readiness checks or appear as its evidence.
rm -f -- \
  "$ready_file" \
  "$summary_file" \
  "$client_stderr_file" \
  "$opamp_errors_file" \
  "$compose_log_file" \
  "$docker_stats_file" \
  "$postgres_activity_file"

cleanup() {
  if [ -n "$client_pid" ] && kill -0 "$client_pid" >/dev/null 2>&1; then
    kill -TERM "$client_pid" >/dev/null 2>&1 || true
    wait "$client_pid" >/dev/null 2>&1 || true
  fi

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
    '    environment:' \
    '      DB_DSN: postgres://magnify:magnify@postgres:5432/magnify?sslmode=disable' \
    '      DB_MAX_OPEN_CONNS: "40"' \
    '  postgres:' \
    '    ports: !override []' \
    | docker compose -p "$project_name" -f docker-compose.yml -f - "$@"
}

wait_for_ready() {
  local attempt
  for attempt in $(seq 1 900); do
    if [ -s "$ready_file" ]; then
      return 0
    fi
    if ! kill -0 "$client_pid" >/dev/null 2>&1; then
      return 1
    fi
    sleep 1
  done
  return 1
}

stop_client() {
  local client_status

  if [ -n "$client_pid" ] && kill -0 "$client_pid" >/dev/null 2>&1; then
    kill -TERM "$client_pid"
  fi
  if wait "$client_pid"; then
    client_pid=""
    return 0
  fi
  client_status=$?
  client_pid=""
  return "$client_status"
}

collect_opamp_errors() {
  local status

  if load_test_compose logs --no-color otel-magnify >"$compose_log_file" 2>&1; then
    :
  else
    status=$?
    echo "failed to collect Compose logs; see ${compose_log_file}" >&2
    return "$status"
  fi

  if grep -Ei "error|failed|panic" "$compose_log_file" >"$opamp_errors_file"; then
    return 0
  else
    status=$?
  fi
  if [ "$status" -eq 1 ]; then
    return 0
  fi

  echo "failed to filter Compose logs; see ${compose_log_file}" >&2
  return "$status"
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
    if ! load_test_compose logs --no-color >"$compose_log_file" 2>&1; then
      echo "failed to collect Compose logs; see ${compose_log_file}" >&2
      exit 1
    fi
    echo "server did not become healthy within 120 seconds" >&2
    exit 1
  fi
  sleep 1
done

docker run --rm \
  --network "${project_name}_default" \
  -v "$PWD:/app:ro" \
  -v "${output_dir}:/artifacts" \
  -w /app \
  -e OPAMP_SHARED_SECRET \
  golang:1.25.12 \
  go run ./cmd/opamp-load \
  --endpoint "ws://otel-magnify:4320/v1/opamp" \
  --collectors 5000 \
  --ramp "$load_test_ramp" \
  --hold "$load_test_hold" \
  --ready-file /artifacts/ready.json \
  >"$summary_file" 2>"$client_stderr_file" &
client_pid="$!"

if ! wait_for_ready; then
  if wait "$client_pid"; then
    client_status=0
  else
    client_status=$?
  fi
  client_pid=""
  echo "opamp-load did not reach the connection hold phase (status ${client_status})" >&2
  exit 1
fi

if ! jq -e \
  '.attempted == 5000 and .connected == 5000 and .failed == 0 and .cancelled == 0 and .disconnected == 0 and .stop_failed == 0 and .interrupted == false' \
  "$ready_file" >/dev/null; then
  echo "collectors did not all reach the hold phase successfully" >&2
  stop_client || true
  exit 1
fi

if ! kill -0 "$client_pid" >/dev/null 2>&1; then
  echo "opamp-load exited before hold-phase evidence could be captured" >&2
  stop_client || true
  exit 1
fi

container_ids=()
while IFS= read -r container_id; do
  if [ -n "$container_id" ]; then
    container_ids+=("$container_id")
  fi
done < <(load_test_compose ps -q)
if [ "${#container_ids[@]}" -gt 0 ]; then
  docker stats --no-stream "${container_ids[@]}" >"$docker_stats_file"
fi

load_test_compose exec -T postgres \
  psql -U magnify -d magnify \
  -X -A -t -F '|' \
  -c "SELECT count(*), 40 FROM pg_stat_activity WHERE datname = current_database() AND pid <> pg_backend_pid();" \
  >"$postgres_activity_file"
if ! collect_opamp_errors; then
  stop_client || true
  exit 1
fi

if ! awk -F '|' 'NF == 2 && $1 ~ /^[0-9]+$/ && $2 == "40" && $1 <= $2 { valid = 1 } END { exit !valid }' "$postgres_activity_file"; then
  echo "PostgreSQL connections exceeded the configured maximum of 40" >&2
  stop_client || true
  exit 1
fi

if [ -s "$opamp_errors_file" ]; then
  echo "application errors were recorded while collectors were held" >&2
  stop_client || true
  exit 1
fi

if wait "$client_pid"; then
  client_status=0
else
  client_status=$?
fi
client_pid=""

if ! jq -e \
  '.attempted == 5000 and .connected == 5000 and .failed == 0 and .cancelled == 0 and .disconnected == 5000 and .stop_failed == 0 and .interrupted == false' \
  "$summary_file" >/dev/null; then
  echo "load test summary did not meet the 5,000 connected collector target" >&2
  exit 1
fi
if [ "$client_status" -ne 0 ]; then
  echo "opamp-load exited with status ${client_status}" >&2
  exit "$client_status"
fi

echo "5,000 collector load test completed; artifacts: ${output_dir}"
