#!/usr/bin/env bash

set -euo pipefail

umask 077

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)"
candidate_context_input="${POSTGRES_LIFECYCLE_CANDIDATE_CONTEXT:-$repo_root}"

if ! command -v openssl >/dev/null 2>&1; then
  echo "postgres lifecycle test failed: required command is unavailable: openssl" >&2
  exit 1
fi

run_id="$(openssl rand -hex 8)"
if [[ ! "$run_id" =~ ^[a-f0-9]{16}$ ]]; then
  echo "postgres lifecycle test failed: unable to generate a safe run identifier" >&2
  exit 1
fi

resource_prefix="otel-magnify-pg-lifecycle-${run_id}"
network_name="${resource_prefix}-network"
postgres16_container="${resource_prefix}-postgres16"
baseline16_container="${resource_prefix}-baseline16"
postgres18_target_container="${resource_prefix}-postgres18-target"
baseline18_target_container="${resource_prefix}-baseline18-target"
candidate_container="${resource_prefix}-candidate"
postgres18_restore_container="${resource_prefix}-postgres18-restore"
baseline18_restore_container="${resource_prefix}-baseline18-restore"
client_container="${resource_prefix}-client"
postgres16_volume="${resource_prefix}-postgres16-data"
postgres18_target_volume="${resource_prefix}-postgres18-target-data"
postgres18_restore_volume="${resource_prefix}-postgres18-restore-data"
candidate_image="otel-magnify-postgres-lifecycle:${run_id}"
temporary_directory="${TMPDIR:-/tmp}/${resource_prefix}"
postgres16_dump="${temporary_directory}/postgres16.dump"
pre_upgrade_dump="${temporary_directory}/postgres18-pre-upgrade.dump"

containers=(
  "$client_container"
  "$candidate_container"
  "$baseline18_restore_container"
  "$baseline18_target_container"
  "$baseline16_container"
  "$postgres18_restore_container"
  "$postgres18_target_container"
  "$postgres16_container"
)
volumes=(
  "$postgres18_restore_volume"
  "$postgres18_target_volume"
  "$postgres16_volume"
)
cleanup_started=0
cleanup_signal_exit_code=0
docker_resources_may_exist=0

postgres_lifecycle_resource_list_contains() {
  local resource_list="$1"
  local resource_name="$2"

  [[ $'\n'"${resource_list}"$'\n' == *$'\n'"${resource_name}"$'\n'* ]]
}

postgres_lifecycle_cleanup() {
  local exit_code=$?
  local cleanup_failed=0
  local resource_list
  local resource

  trap - EXIT INT TERM
  if ((cleanup_signal_exit_code != 0)); then
    exit_code="$cleanup_signal_exit_code"
  fi
  if ((cleanup_started == 0)); then
    cleanup_started=1

    if ((docker_resources_may_exist != 0)); then
      if ! command -v docker >/dev/null 2>&1; then
        cleanup_failed=1
      else
        if resource_list="$(docker container ls --all --format '{{.Names}}' 2>/dev/null)"; then
          for resource in "${containers[@]}"; do
            if postgres_lifecycle_resource_list_contains "$resource_list" "$resource"; then
              docker container rm --force --volumes "$resource" >/dev/null 2>&1 || cleanup_failed=1
            fi
          done
          if resource_list="$(docker container ls --all --format '{{.Names}}' 2>/dev/null)"; then
            for resource in "${containers[@]}"; do
              if postgres_lifecycle_resource_list_contains "$resource_list" "$resource"; then
                cleanup_failed=1
              fi
            done
          else
            cleanup_failed=1
          fi
        else
          cleanup_failed=1
        fi

        if resource_list="$(docker network ls --format '{{.Name}}' 2>/dev/null)"; then
          if postgres_lifecycle_resource_list_contains "$resource_list" "$network_name"; then
            docker network rm "$network_name" >/dev/null 2>&1 || cleanup_failed=1
          fi
          if resource_list="$(docker network ls --format '{{.Name}}' 2>/dev/null)"; then
            if postgres_lifecycle_resource_list_contains "$resource_list" "$network_name"; then
              cleanup_failed=1
            fi
          else
            cleanup_failed=1
          fi
        else
          cleanup_failed=1
        fi

        if resource_list="$(docker volume ls --format '{{.Name}}' 2>/dev/null)"; then
          for resource in "${volumes[@]}"; do
            if postgres_lifecycle_resource_list_contains "$resource_list" "$resource"; then
              docker volume rm "$resource" >/dev/null 2>&1 || cleanup_failed=1
            fi
          done
          if resource_list="$(docker volume ls --format '{{.Name}}' 2>/dev/null)"; then
            for resource in "${volumes[@]}"; do
              if postgres_lifecycle_resource_list_contains "$resource_list" "$resource"; then
                cleanup_failed=1
              fi
            done
          else
            cleanup_failed=1
          fi
        else
          cleanup_failed=1
        fi

        if resource_list="$(docker image ls --format '{{.Repository}}:{{.Tag}}' 2>/dev/null)"; then
          if postgres_lifecycle_resource_list_contains "$resource_list" "$candidate_image"; then
            docker image rm --force "$candidate_image" >/dev/null 2>&1 || cleanup_failed=1
          fi
          if resource_list="$(docker image ls --format '{{.Repository}}:{{.Tag}}' 2>/dev/null)"; then
            if postgres_lifecycle_resource_list_contains "$resource_list" "$candidate_image"; then
              cleanup_failed=1
            fi
          else
            cleanup_failed=1
          fi
        else
          cleanup_failed=1
        fi
      fi
    fi

    if [[ -e "$temporary_directory" ]]; then
      rm -rf -- "$temporary_directory" || cleanup_failed=1
    fi
  fi

  if ((cleanup_failed != 0)); then
    echo "postgres lifecycle test cleanup failed: one or more private resources remain" >&2
    if ((exit_code == 0)); then
      exit_code=1
    fi
  fi
  exit "$exit_code"
}
trap postgres_lifecycle_cleanup EXIT
trap 'cleanup_signal_exit_code=130; postgres_lifecycle_cleanup' INT
trap 'cleanup_signal_exit_code=143; postgres_lifecycle_cleanup' TERM

postgres_lifecycle_fail() {
  echo "postgres lifecycle test failed: $*" >&2
  exit 1
}

postgres_lifecycle_require_command() {
  local command_name="$1"

  if ! command -v "$command_name" >/dev/null 2>&1; then
    postgres_lifecycle_fail "required command is unavailable: ${command_name}"
  fi
}

postgres_lifecycle_wait_for_health() {
  local container_name="$1"
  local description="$2"
  local attempt
  local health_status
  local running

  for ((attempt = 0; attempt < 120; attempt++)); do
    if ! health_status="$(docker container inspect --format '{{if .State.Health}}{{.State.Health.Status}}{{else}}missing{{end}}' "$container_name" 2>/dev/null)"; then
      postgres_lifecycle_fail "${description} container disappeared"
    fi
    if [[ "$health_status" == "healthy" ]]; then
      return
    fi
    if ! running="$(docker container inspect --format '{{.State.Running}}' "$container_name" 2>/dev/null)"; then
      postgres_lifecycle_fail "${description} state is unavailable"
    fi
    if [[ "$running" != "true" ]]; then
      postgres_lifecycle_fail "${description} stopped before becoming healthy"
    fi
    sleep 1
  done

  postgres_lifecycle_fail "${description} did not become healthy"
}

postgres_lifecycle_wait_for_readyz() {
  local container_name="$1"
  local description="$2"
  local attempt
  local response
  local running

  for ((attempt = 0; attempt < 60; attempt++)); do
    if response="$(docker container exec "$container_name" \
      wget -qO- http://127.0.0.1:8080/readyz 2>/dev/null)"; then
      if [[ "$response" == "ready" ]]; then
        return
      fi
    fi
    if ! running="$(docker container inspect --format '{{.State.Running}}' "$container_name" 2>/dev/null)"; then
      postgres_lifecycle_fail "${description} state is unavailable"
    fi
    if [[ "$running" != "true" ]]; then
      postgres_lifecycle_fail "${description} stopped before readiness"
    fi
    sleep 1
  done

  postgres_lifecycle_fail "${description} did not return the real /readyz response"
}

postgres_lifecycle_start_postgres() {
  local image="$1"
  local container_name="$2"
  local volume_name="$3"
  local password="$4"
  local volume_mount="$5"
  local pgdata="${6:-}"
  local description="$7"
  local docker_arguments=(
    run
    --detach
    --name "$container_name"
    --network "$network_name"
    --env POSTGRES_DB
    --env POSTGRES_USER
    --env POSTGRES_PASSWORD
    --health-cmd 'pg_isready -U magnify -d magnify'
    --health-interval 1s
    --health-timeout 3s
    --health-retries 60
    --health-start-period 2s
    --volume "${volume_name}:${volume_mount}"
  )

  export POSTGRES_PASSWORD="$password"
  if [[ -n "$pgdata" ]]; then
    export PGDATA="$pgdata"
    docker_arguments+=(--env PGDATA)
  fi

  if ! docker "${docker_arguments[@]}" "$image" >/dev/null 2>&1; then
    unset POSTGRES_PASSWORD PGDATA || true
    postgres_lifecycle_fail "unable to start ${description}"
  fi
  unset POSTGRES_PASSWORD PGDATA || true

  postgres_lifecycle_wait_for_health "$container_name" "$description"
}

postgres_lifecycle_start_application() {
  local image="$1"
  local container_name="$2"
  local database_host="$3"
  local database_password="$4"
  local jwt_secret="$5"
  local description="$6"

  export DB_DSN="postgres://${POSTGRES_USER}:${database_password}@${database_host}:5432/${POSTGRES_DB}?sslmode=disable"
  export DB_DRIVER="pgx"
  export JWT_SECRET="$jwt_secret"
  export LISTEN_ADDR=":8080"
  export OPAMP_ADDR=":4320"
  export WORKLOAD_JANITOR_INTERVAL_SECONDS="86400"

  if ! docker run --detach \
    --name "$container_name" \
    --network "$network_name" \
    --env DB_DSN \
    --env DB_DRIVER \
    --env JWT_SECRET \
    --env LISTEN_ADDR \
    --env OPAMP_ADDR \
    --env WORKLOAD_JANITOR_INTERVAL_SECONDS \
    --health-interval 1s \
    --health-timeout 3s \
    --health-retries 60 \
    --health-start-period 2s \
    "$image" >/dev/null 2>&1; then
    unset DB_DSN DB_DRIVER JWT_SECRET LISTEN_ADDR OPAMP_ADDR WORKLOAD_JANITOR_INTERVAL_SECONDS
    postgres_lifecycle_fail "unable to start ${description}"
  fi

  unset DB_DSN DB_DRIVER JWT_SECRET LISTEN_ADDR OPAMP_ADDR WORKLOAD_JANITOR_INTERVAL_SECONDS
}

postgres_lifecycle_stop_application() {
  local container_name="$1"
  local description="$2"
  local running

  if ! docker container stop --time 30 "$container_name" >/dev/null 2>&1; then
    postgres_lifecycle_fail "unable to stop ${description}"
  fi
  if ! running="$(docker container inspect --format '{{.State.Running}}' "$container_name" 2>/dev/null)"; then
    postgres_lifecycle_fail "unable to inspect stopped ${description}"
  fi
  if [[ "$running" != "false" ]]; then
    postgres_lifecycle_fail "${description} is still writing"
  fi
}

postgres_lifecycle_run_sql() {
  local container_name="$1"
  local sql_file="$2"
  local description="$3"

  if ! docker container exec --interactive "$container_name" \
    psql \
      --no-psqlrc \
      --set ON_ERROR_STOP=1 \
      --quiet \
      --username "$POSTGRES_USER" \
      --dbname "$POSTGRES_DB" \
      <"$sql_file" >/dev/null 2>&1; then
    postgres_lifecycle_fail "$description"
  fi
}

postgres_lifecycle_dump_database() {
  local database_host="$1"
  local database_password="$2"
  local dump_file="$3"
  local description="$4"
  local digest_output
  local dump_digest

  export PGPASSWORD="$database_password"
  if ! docker run --rm \
    --name "$client_container" \
    --network "$network_name" \
    --env PGPASSWORD \
    postgres:18-alpine \
    pg_dump \
      --host "$database_host" \
      --username "$POSTGRES_USER" \
      --dbname "$POSTGRES_DB" \
      --format custom \
      --no-password \
      >"$dump_file" 2>/dev/null; then
    unset PGPASSWORD
    postgres_lifecycle_fail "unable to create ${description}"
  fi
  unset PGPASSWORD

  if [[ ! -s "$dump_file" ]]; then
    postgres_lifecycle_fail "${description} is empty"
  fi
  if ! docker run --rm \
    --name "$client_container" \
    --interactive \
    postgres:18-alpine \
    pg_restore --list \
      <"$dump_file" >/dev/null 2>&1; then
    postgres_lifecycle_fail "${description} is not a valid custom-format dump"
  fi

  if ! digest_output="$("${sha256_command[@]}" "$dump_file" 2>/dev/null)"; then
    postgres_lifecycle_fail "unable to hash ${description}"
  fi
  dump_digest="${digest_output%% *}"
  if [[ ! "$dump_digest" =~ ^[a-f0-9]{64}$ ]]; then
    postgres_lifecycle_fail "${description} SHA-256 output is invalid"
  fi
}

postgres_lifecycle_restore_database() {
  local database_host="$1"
  local database_password="$2"
  local dump_file="$3"
  local description="$4"

  export PGPASSWORD="$database_password"
  if ! docker run --rm \
    --name "$client_container" \
    --interactive \
    --network "$network_name" \
    --env PGPASSWORD \
    postgres:18-alpine \
    pg_restore \
      --host "$database_host" \
      --username "$POSTGRES_USER" \
      --dbname "$POSTGRES_DB" \
      --no-password \
      --no-owner \
      --no-privileges \
      --exit-on-error \
      --single-transaction \
      <"$dump_file" >/dev/null 2>&1; then
    unset PGPASSWORD
    postgres_lifecycle_fail "unable to restore ${description}"
  fi
  unset PGPASSWORD
}

for required_command in docker cosign openssl; do
  postgres_lifecycle_require_command "$required_command"
done

if ! docker info >/dev/null 2>&1; then
  postgres_lifecycle_fail "Docker daemon is unavailable"
fi
if ! docker buildx version >/dev/null 2>&1; then
  postgres_lifecycle_fail "Docker Buildx is unavailable"
fi

if command -v sha256sum >/dev/null 2>&1; then
  sha256_command=(sha256sum)
elif command -v shasum >/dev/null 2>&1; then
  sha256_command=(shasum -a 256)
else
  postgres_lifecycle_fail "a SHA-256 tool is unavailable"
fi

if [[ ! -d "$candidate_context_input" ]]; then
  postgres_lifecycle_fail "candidate context is not a directory"
fi
if ! candidate_context="$(cd "$candidate_context_input" && pwd -P)"; then
  postgres_lifecycle_fail "candidate context cannot be resolved"
fi
for required_path in Dockerfile go.mod cmd/server internal pkg frontend/package-lock.json; do
  if [[ ! -e "${candidate_context}/${required_path}" ]]; then
    postgres_lifecycle_fail "candidate context is missing a required project path"
  fi
done

fixture_directory="${repo_root}/tests/postgres-lifecycle"
seed_fixture="${fixture_directory}/seed-v0.7.1.sql"
inject_fixture="${fixture_directory}/inject-legacy-status.sql"
assert_legacy_fixture="${fixture_directory}/assert-legacy-v25.sql"
assert_baseline_fixture="${fixture_directory}/assert-v0.7.1-sanitized.sql"
assert_current_fixture="${fixture_directory}/assert-current.sql"
for fixture_file in \
  "$seed_fixture" \
  "$inject_fixture" \
  "$assert_legacy_fixture" \
  "$assert_baseline_fixture" \
  "$assert_current_fixture"; do
  if [[ ! -f "$fixture_file" ]]; then
    postgres_lifecycle_fail "required SQL fixture is unavailable"
  fi
done

baseline_repository="ghcr.io/magnify-labs/otel-magnify"
baseline_tag="${baseline_repository}:0.7.1"
if ! baseline_digest="$(docker buildx imagetools inspect \
  --format '{{.Manifest.Digest}}' \
  "$baseline_tag" 2>/dev/null)"; then
  postgres_lifecycle_fail "unable to resolve the v0.7.1 baseline artifact"
fi
if [[ ! "$baseline_digest" =~ ^sha256:[a-f0-9]{64}$ ]]; then
  postgres_lifecycle_fail "resolved v0.7.1 baseline digest is invalid"
fi
baseline_reference="${baseline_repository}@${baseline_digest}"

if ! cosign verify \
  --certificate-oidc-issuer "https://token.actions.githubusercontent.com" \
  --certificate-identity "https://github.com/Magnify-Labs/otel-magnify/.github/workflows/release.yml@refs/tags/v0.7.1" \
  "$baseline_reference" >/dev/null 2>&1; then
  postgres_lifecycle_fail "v0.7.1 baseline signature verification failed"
fi
printf '%s\n' 'baseline_artifact_verified: ok'

if ! mkdir -m 700 "$temporary_directory"; then
  postgres_lifecycle_fail "unable to create the private temporary directory"
fi
if ! chmod 700 "$temporary_directory"; then
  postgres_lifecycle_fail "unable to secure the private temporary directory"
fi

postgres16_password="$(openssl rand -hex 32)"
postgres18_target_password="$(openssl rand -hex 32)"
postgres18_restore_password="$(openssl rand -hex 32)"
baseline16_jwt_secret="$(openssl rand -hex 32)"
baseline18_target_jwt_secret="$(openssl rand -hex 32)"
candidate_jwt_secret="$(openssl rand -hex 32)"
baseline18_restore_jwt_secret="$(openssl rand -hex 32)"
for random_secret in \
  "$postgres16_password" \
  "$postgres18_target_password" \
  "$postgres18_restore_password" \
  "$baseline16_jwt_secret" \
  "$baseline18_target_jwt_secret" \
  "$candidate_jwt_secret" \
  "$baseline18_restore_jwt_secret"; do
  if [[ ! "$random_secret" =~ ^[a-f0-9]{64}$ ]]; then
    postgres_lifecycle_fail "unable to generate a random hexadecimal secret"
  fi
done

docker_resources_may_exist=1
if ! docker network create "$network_name" >/dev/null 2>&1; then
  postgres_lifecycle_fail "unable to create the isolated Docker network"
fi
for volume_name in "${volumes[@]}"; do
  if ! docker volume create "$volume_name" >/dev/null 2>&1; then
    postgres_lifecycle_fail "unable to create an isolated PostgreSQL volume"
  fi
done

POSTGRES_DB="magnify"
POSTGRES_USER="magnify"
export POSTGRES_DB POSTGRES_USER

postgres_lifecycle_start_postgres \
  postgres:16-alpine \
  "$postgres16_container" \
  "$postgres16_volume" \
  "$postgres16_password" \
  /var/lib/postgresql/data \
  "" \
  "PostgreSQL 16 source"

postgres_lifecycle_start_application \
  "$baseline_reference" \
  "$baseline16_container" \
  "$postgres16_container" \
  "$postgres16_password" \
  "$baseline16_jwt_secret" \
  "v0.7.1 on PostgreSQL 16"
postgres_lifecycle_wait_for_health "$baseline16_container" "v0.7.1 on PostgreSQL 16"
postgres_lifecycle_run_sql "$postgres16_container" "$seed_fixture" "unable to seed the v0.7.1 fixture"
postgres_lifecycle_run_sql "$postgres16_container" "$inject_fixture" "unable to inject the PostgreSQL 16 legacy status"
postgres_lifecycle_run_sql "$postgres16_container" "$assert_legacy_fixture" "PostgreSQL 16 legacy fixture assertion failed"

postgres_lifecycle_stop_application "$baseline16_container" "v0.7.1 on PostgreSQL 16"
postgres_lifecycle_dump_database \
  "$postgres16_container" \
  "$postgres16_password" \
  "$postgres16_dump" \
  "PostgreSQL 16 source dump"

postgres_lifecycle_start_postgres \
  postgres:18-alpine \
  "$postgres18_target_container" \
  "$postgres18_target_volume" \
  "$postgres18_target_password" \
  /var/lib/postgresql \
  /var/lib/postgresql/18/docker \
  "PostgreSQL 18 target"
postgres_lifecycle_restore_database \
  "$postgres18_target_container" \
  "$postgres18_target_password" \
  "$postgres16_dump" \
  "PostgreSQL 16 dump into the PostgreSQL 18 target"
postgres_lifecycle_run_sql "$postgres18_target_container" "$assert_legacy_fixture" "PostgreSQL 18 pre-application legacy assertion failed"

postgres_lifecycle_start_application \
  "$baseline_reference" \
  "$baseline18_target_container" \
  "$postgres18_target_container" \
  "$postgres18_target_password" \
  "$baseline18_target_jwt_secret" \
  "v0.7.1 on PostgreSQL 18"
postgres_lifecycle_wait_for_health "$baseline18_target_container" "v0.7.1 on PostgreSQL 18"
if ! server_version="$(docker container exec "$postgres18_target_container" \
  psql \
    --no-psqlrc \
    --tuples-only \
    --no-align \
    --username "$POSTGRES_USER" \
    --dbname "$POSTGRES_DB" \
    --command 'SHOW server_version' 2>/dev/null)"; then
  postgres_lifecycle_fail "unable to query the PostgreSQL 18 target version"
fi
if [[ "$server_version" != 18.* ]]; then
  postgres_lifecycle_fail "target database is not PostgreSQL 18"
fi
postgres_lifecycle_run_sql "$postgres18_target_container" "$assert_baseline_fixture" "v0.7.1 PostgreSQL 18 assertion failed"
printf '%s\n' 'postgres16_to_postgres18_same_app: ok'

postgres_lifecycle_stop_application "$baseline18_target_container" "v0.7.1 on PostgreSQL 18"
postgres_lifecycle_run_sql "$postgres18_target_container" "$inject_fixture" "unable to reinject the PostgreSQL 18 legacy status"
postgres_lifecycle_run_sql "$postgres18_target_container" "$assert_legacy_fixture" "PostgreSQL 18 pre-upgrade legacy assertion failed"
postgres_lifecycle_dump_database \
  "$postgres18_target_container" \
  "$postgres18_target_password" \
  "$pre_upgrade_dump" \
  "PostgreSQL 18 pre-upgrade dump"

if ! docker buildx build \
  --load \
  --tag "$candidate_image" \
  "$candidate_context" >/dev/null 2>&1; then
  postgres_lifecycle_fail "candidate image build failed"
fi

postgres_lifecycle_start_application \
  "$candidate_image" \
  "$candidate_container" \
  "$postgres18_target_container" \
  "$postgres18_target_password" \
  "$candidate_jwt_secret" \
  "candidate on PostgreSQL 18"
postgres_lifecycle_wait_for_readyz "$candidate_container" "candidate on PostgreSQL 18"
postgres_lifecycle_run_sql "$postgres18_target_container" "$assert_current_fixture" "candidate migration assertion failed"
printf '%s\n' 'postgres18_v0.7.1_to_candidate: ok'

if ! docker container restart "$candidate_container" >/dev/null 2>&1; then
  postgres_lifecycle_fail "unable to restart the candidate"
fi
postgres_lifecycle_wait_for_readyz "$candidate_container" "restarted candidate on PostgreSQL 18"
postgres_lifecycle_run_sql "$postgres18_target_container" "$assert_current_fixture" "candidate second-start assertion failed"
printf '%s\n' 'candidate_second_start: ok'

postgres_lifecycle_start_postgres \
  postgres:18-alpine \
  "$postgres18_restore_container" \
  "$postgres18_restore_volume" \
  "$postgres18_restore_password" \
  /var/lib/postgresql \
  /var/lib/postgresql/18/docker \
  "PostgreSQL 18 restore target"
postgres_lifecycle_restore_database \
  "$postgres18_restore_container" \
  "$postgres18_restore_password" \
  "$pre_upgrade_dump" \
  "pre-upgrade dump into the PostgreSQL 18 restore target"
postgres_lifecycle_run_sql "$postgres18_restore_container" "$assert_legacy_fixture" "restored pre-upgrade legacy assertion failed"

postgres_lifecycle_start_application \
  "$baseline_reference" \
  "$baseline18_restore_container" \
  "$postgres18_restore_container" \
  "$postgres18_restore_password" \
  "$baseline18_restore_jwt_secret" \
  "v0.7.1 on the PostgreSQL 18 restore target"
postgres_lifecycle_wait_for_health "$baseline18_restore_container" "v0.7.1 on the PostgreSQL 18 restore target"
postgres_lifecycle_run_sql "$postgres18_restore_container" "$assert_baseline_fixture" "restored v0.7.1 assertion failed"
printf '%s\n' 'pre_upgrade_restore_with_v0.7.1: ok'
