#!/usr/bin/env bash

set -euo pipefail

readonly ACTIVATION_API_URL="${ACTIVATION_API_URL:-http://127.0.0.1:8080}"
readonly ACTIVATION_TIMEOUT_SECONDS="${ACTIVATION_TIMEOUT_SECONDS:-900}"
readonly ACTIVATION_WORKLOAD_NAME="otelcol-activation-demo"

activation_completed=false
activation_project_name=""
activation_temporary_directory=""

activation_require_command() {
  local command_name="$1"

  if ! command -v "${command_name}" >/dev/null 2>&1; then
    echo "required command not found: ${command_name}" >&2
    exit 1
  fi
}

activation_wait_for_readiness() {
  local deadline="$1"
  local readiness_body

  until readiness_body="$(curl --fail --silent --show-error "${ACTIVATION_API_URL}/readyz" 2>/dev/null)" \
    && [[ "${readiness_body}" == "ready" ]]; do
    if ((SECONDS >= deadline)); then
      echo "timed out waiting for the exact API readiness response" >&2
      return 1
    fi
    sleep 2
  done
}

activation_wait_for_workload() {
  local cookie_jar="$1"
  local response_file="$2"
  local deadline="$3"

  until curl --fail --silent --show-error \
    --cookie "${cookie_jar}" \
    --output "${response_file}" \
    "${ACTIVATION_API_URL}/api/workloads" \
    && jq -e --arg name "${ACTIVATION_WORKLOAD_NAME}" \
      '.[] | select(.display_name == $name and .type == "collector" and .status == "connected" and .accepts_remote_config == true)' \
      "${response_file}" >/dev/null; do
    if ((SECONDS >= deadline)); then
      echo "timed out waiting for the remote-config workload" >&2
      return 1
    fi
    sleep 2
  done
}

activation_wait_for_applied_config() {
  local cookie_jar="$1"
  local workload_id="$2"
  local response_file="$3"
  local deadline="$4"

  until curl --fail --silent --show-error \
    --cookie "${cookie_jar}" \
    --output "${response_file}" \
    "${ACTIVATION_API_URL}/api/workloads/${workload_id}/configs" \
    && jq -e '.[0].status == "applied"' "${response_file}" >/dev/null; do
    if ((SECONDS >= deadline)); then
      echo "timed out waiting for the governed config to be applied" >&2
      return 1
    fi
    sleep 2
  done
}

activation_cleanup() {
  local exit_code=$?
  set +e

  if [[ -n "${activation_project_name}" ]]; then
    local -a cleanup_compose=(docker compose --project-name "${activation_project_name}" --profile activation)
    if [[ "${activation_completed}" != true ]]; then
      "${cleanup_compose[@]}" ps >&2
      "${cleanup_compose[@]}" logs --no-color --tail=200 >&2
    fi
    "${cleanup_compose[@]}" down --volumes --remove-orphans >/dev/null 2>&1
  fi
  if [[ -n "${activation_temporary_directory}" ]]; then
    rm -rf "${activation_temporary_directory}"
  fi
  return "${exit_code}"
}

activation_main() {
  local command_name
  for command_name in curl docker jq openssl; do
    activation_require_command "${command_name}"
  done

  local repo_root
  repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
  cd "${repo_root}"

  activation_project_name="otel-magnify-activation-$$"
  local -a compose=(docker compose --project-name "${activation_project_name}" --profile activation)
  local temporary_directory
  temporary_directory="$(mktemp -d)"
  chmod 700 "${temporary_directory}"
  activation_temporary_directory="${temporary_directory}"
  local cookie_jar="${temporary_directory}/cookies"
  local login_response="${temporary_directory}/login.json"
  local response_file="${temporary_directory}/response.json"
  local anonymous_docker_config="${temporary_directory}/docker-config"
  mkdir -m 700 "${anonymous_docker_config}"

  export JWT_SECRET
  JWT_SECRET="$(openssl rand -hex 32)"
  export POSTGRES_PASSWORD
  POSTGRES_PASSWORD="$(openssl rand -hex 24)"
  export SEED_ADMIN_EMAIL="activation-admin@example.invalid"
  export SEED_ADMIN_PASSWORD
  SEED_ADMIN_PASSWORD="$(openssl rand -base64 24 | tr -d '\n')"

  local started_at
  started_at="$(date +%s)"
  trap activation_cleanup EXIT

  if ! "${compose[@]}" config --services | grep -Fxq activation-agent; then
    echo "the Compose activation profile must define activation-agent" >&2
    return 1
  fi

  if [[ -n "${OTEL_MAGNIFY_IMAGE:-}" ]]; then
    DOCKER_CONFIG="${anonymous_docker_config}" docker pull "${OTEL_MAGNIFY_IMAGE}" >/dev/null
    "${compose[@]}" build activation-agent
    "${compose[@]}" up --detach --no-build postgres otel-magnify activation-agent
  else
    "${compose[@]}" up --detach --build postgres otel-magnify activation-agent
  fi

  local deadline=$((SECONDS + ACTIVATION_TIMEOUT_SECONDS))
  activation_wait_for_readiness "${deadline}"

  local postgres_version
  postgres_version="$("${compose[@]}" exec --no-TTY postgres \
    psql --username magnify --dbname magnify --no-align --tuples-only \
    --command 'SHOW server_version')"
  local postgres_major
  if [[ "${postgres_version}" =~ ^([0-9]+)(\.|$) ]]; then
    postgres_major="${BASH_REMATCH[1]}"
  else
    echo "could not parse PostgreSQL server version: ${postgres_version}" >&2
    return 1
  fi
  if [[ "${postgres_major}" != "18" ]]; then
    echo "PostgreSQL 18 is required, got ${postgres_version}" >&2
    return 1
  fi

  curl --fail --silent --show-error \
    --output "${response_file}" \
    "${ACTIVATION_API_URL}/api/features"
  jq -e '.features == {
    "config_safety.approvals": true,
    "config_safety.policy_preview": true
  }' "${response_file}" >/dev/null

  jq --null-input \
    --arg email "${SEED_ADMIN_EMAIL}" \
    --arg password "${SEED_ADMIN_PASSWORD}" \
    '{email: $email, password: $password}' \
    | curl --fail --silent --show-error \
      --cookie-jar "${cookie_jar}" \
      --header 'Content-Type: application/json' \
      --data-binary @- \
      --output "${login_response}" \
      "${ACTIVATION_API_URL}/api/auth/login"
  chmod 600 "${cookie_jar}" "${login_response}"
  jq -e '.token | type == "string" and length > 0' "${login_response}" >/dev/null

  activation_wait_for_workload "${cookie_jar}" "${response_file}" "${deadline}"
  local workload_id
  workload_id="$(jq -er --arg name "${ACTIVATION_WORKLOAD_NAME}" '.[] | select(.display_name == $name) | .id' "${response_file}")"

  local draft_yaml
  draft_yaml=$'receivers:\n  otlp:\n    protocols:\n      grpc: {}\nprocessors:\n  batch: {}\nexporters:\n  debug: {}\nservice:\n  pipelines:\n    traces:\n      receivers: [otlp]\n      processors: [batch]\n      exporters: [debug]\n'

  jq --null-input \
    --arg draft_yaml "${draft_yaml}" \
    '{draft_yaml: $draft_yaml, target_group: "activation", target_env: "dev", comment: "Activation smoke test request"}' \
    | curl --fail --silent --show-error \
      --cookie "${cookie_jar}" \
      --header 'Content-Type: application/json' \
      --data-binary @- \
      --output "${response_file}" \
      "${ACTIVATION_API_URL}/api/workloads/${workload_id}/config/approvals"
  local approval_id
  approval_id="$(jq -er 'select(.status == "pending") | .id' "${response_file}")"

  jq --null-input '{comment: "Activation smoke test approval"}' \
    | curl --fail --silent --show-error \
      --cookie "${cookie_jar}" \
      --header 'Content-Type: application/json' \
      --data-binary @- \
      --output "${response_file}" \
      "${ACTIVATION_API_URL}/api/workloads/${workload_id}/config/approvals/${approval_id}/approve"
  jq -e '.status == "approved"' "${response_file}" >/dev/null

  jq --null-input '{comment: "Activation smoke test governed push"}' \
    | curl --fail --silent --show-error \
      --cookie "${cookie_jar}" \
      --header 'Content-Type: application/json' \
      --data-binary @- \
      --output "${response_file}" \
      "${ACTIVATION_API_URL}/api/workloads/${workload_id}/config/approvals/${approval_id}/push"
  jq -e '.status == "pushed"' "${response_file}" >/dev/null

  activation_wait_for_applied_config "${cookie_jar}" "${workload_id}" "${response_file}" "${deadline}"

  local elapsed_seconds=$(( $(date +%s) - started_at ))
  if ((elapsed_seconds >= ACTIVATION_TIMEOUT_SECONDS)); then
    echo "activation exceeded ${ACTIVATION_TIMEOUT_SECONDS} seconds" >&2
    return 1
  fi

  activation_completed=true
  echo "activation smoke: OK"
  echo "health=ready features=approvals+policy_preview login=ok workload=connected governed_push=applied"
  echo "postgres_version=${postgres_version}"
  echo "activation_seconds=${elapsed_seconds}"
}

activation_main "$@"
