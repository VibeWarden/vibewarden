#!/usr/bin/env sh
# seed-users.sh — seeds demo identities into Ory Kratos on first boot.
#
# Usage:
#   KRATOS_ADMIN_URL=http://kratos:4434 ./seed-users.sh
#
# Environment variables:
#   KRATOS_ADMIN_URL   Base URL of the Kratos admin API (default: http://kratos:4434)
#
# The script is idempotent: it checks whether each identity already exists
# (by email lookup) before attempting to create it.  Exits 0 on success.

set -eu

KRATOS_ADMIN_URL="${KRATOS_ADMIN_URL:-http://kratos:4434}"

# ---------------------------------------------------------------------------
# wait_for_kratos — poll the admin health endpoint until Kratos is ready.
# ---------------------------------------------------------------------------
wait_for_kratos() {
  echo "[seed] Waiting for Kratos admin API at ${KRATOS_ADMIN_URL} ..."
  retries=30
  while [ "$retries" -gt 0 ]; do
    status=$(curl -s -o /dev/null -w "%{http_code}" "${KRATOS_ADMIN_URL}/admin/health/ready" 2>/dev/null || true)
    if [ "$status" = "200" ]; then
      echo "[seed] Kratos is ready."
      return 0
    fi
    retries=$((retries - 1))
    echo "[seed] Not ready yet (HTTP ${status}), retrying in 3s ... (${retries} retries left)"
    sleep 3
  done
  echo "[seed] ERROR: Kratos did not become ready in time." >&2
  exit 1
}

# ---------------------------------------------------------------------------
# identity_exists — returns 0 if an identity with the given email exists.
# ---------------------------------------------------------------------------
identity_exists() {
  email="$1"
  result=$(curl -s \
    "${KRATOS_ADMIN_URL}/admin/identities?credentials_identifier=${email}" \
    -H "Accept: application/json")
  # The endpoint returns a JSON array; a non-empty array means the user exists.
  count=$(printf '%s' "$result" | grep -c '"id"' || true)
  [ "$count" -gt 0 ]
}

# ---------------------------------------------------------------------------
# create_identity — creates an identity and sets its password.
# ---------------------------------------------------------------------------
create_identity() {
  email="$1"
  password="$2"

  if identity_exists "$email"; then
    echo "[seed] Identity ${email} already exists, skipping."
    return 0
  fi

  echo "[seed] Creating identity: ${email} ..."
  response=$(curl -s -w "\n%{http_code}" -X POST \
    "${KRATOS_ADMIN_URL}/admin/identities" \
    -H "Content-Type: application/json" \
    -d "{
      \"schema_id\": \"default\",
      \"traits\": {
        \"email\": \"${email}\"
      },
      \"credentials\": {
        \"password\": {
          \"config\": {
            \"password\": \"${password}\"
          }
        }
      },
      \"verifiable_addresses\": [
        {
          \"value\": \"${email}\",
          \"verified\": true,
          \"via\": \"email\",
          \"status\": \"completed\"
        }
      ]
    }")

  http_code=$(printf '%s' "$response" | tail -n1)
  body=$(printf '%s' "$response" | head -n -1)

  if [ "$http_code" = "201" ]; then
    echo "[seed] Created identity: ${email}"
  else
    echo "[seed] ERROR: Failed to create ${email} (HTTP ${http_code}): ${body}" >&2
    exit 1
  fi
}

# ---------------------------------------------------------------------------
# Main
# ---------------------------------------------------------------------------
wait_for_kratos

create_identity "demo@vibewarden.dev"  "demo1234"
create_identity "alice@vibewarden.dev" "alice1234"

echo "[seed] Seed complete."
