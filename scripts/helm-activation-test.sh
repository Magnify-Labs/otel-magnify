#!/usr/bin/env bash

set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
chart_path="$repo_root/helm/otel-magnify"
rendered_file="$(mktemp)"
error_file="$(mktemp)"

helm_activation_cleanup() {
  rm -f "$rendered_file" "$error_file"
}
trap helm_activation_cleanup EXIT

helm_activation_fail() {
  echo "helm activation test: $*" >&2
  exit 1
}

helm_activation_assert_contains() {
  local expected="$1"
  if ! grep -Fq -- "$expected" "$rendered_file"; then
    helm_activation_fail "rendered chart does not contain: $expected"
  fi
}

helm template magnify "$chart_path" \
  --namespace observability \
  --set database.existingSecret=magnify-db \
  --set auth.existingSecret=magnify-auth \
  --set auth.seedAdmin.enabled=true \
  --set auth.seedAdmin.existingSecret=magnify-bootstrap \
  >"$rendered_file"

for expected in \
  "name: DB_DSN" \
  "name: magnify-db" \
  "key: \"db-dsn\"" \
  "name: JWT_SECRET" \
  "name: magnify-auth" \
  "key: \"jwt-secret\"" \
  "name: SEED_ADMIN_EMAIL" \
  "name: SEED_ADMIN_PASSWORD" \
  "name: magnify-bootstrap" \
  "key: \"seed-admin-email\"" \
  "key: \"seed-admin-password\""; do
  helm_activation_assert_contains "$expected"
done

if grep -Fq "kind: Secret" "$rendered_file"; then
  helm_activation_fail "reference-only install unexpectedly rendered a Secret containing inline values"
fi

if helm template magnify "$chart_path" \
  --set database.existingSecret=magnify-db \
  >"$rendered_file" 2>"$error_file"; then
  helm_activation_fail "chart rendered without a JWT Secret reference or legacy inline JWT value"
fi
if ! grep -Fq "set auth.existingSecret or jwtSecret to provide JWT_SECRET" "$error_file"; then
  helm_activation_fail "missing JWT validation did not return the documented error"
fi

if helm template magnify "$chart_path" \
  --set database.existingSecret=magnify-db \
  --set auth.existingSecret=magnify-auth \
  --set auth.seedAdmin.enabled=true \
  >"$rendered_file" 2>"$error_file"; then
  helm_activation_fail "chart rendered with admin seed enabled but no bootstrap Secret"
fi
if ! grep -Fq "set auth.seedAdmin.existingSecret when auth.seedAdmin.enabled=true" "$error_file"; then
  helm_activation_fail "missing seed Secret validation did not return the documented error"
fi

echo "helm activation test: OK"
