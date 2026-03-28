#!/usr/bin/env sh
# seed-secrets.sh — populate OpenBao with demo secrets for the VibeWarden secrets plugin.
#
# Requires: BAO_ADDR and BAO_TOKEN set in the environment.
# Idempotent: overwrites existing secrets (dev mode only).

set -eu

echo "Waiting for OpenBao to be ready..."
until bao status >/dev/null 2>&1; do
  sleep 1
done

echo "Enabling KV v2 secrets engine at secret/ ..."
# In dev mode the 'secret/' engine is already mounted, but enable is idempotent.
bao secrets enable -path=secret -version=2 kv 2>/dev/null || true

echo "Seeding demo secrets..."

# Example API key — injected as X-Demo-Api-Key header by the secrets plugin
bao kv put secret/demo/api-key \
  token="vw-demo-api-key-2026"

# Example app config — written to .env file by the secrets plugin
bao kv put secret/demo/app-config \
  database_url="postgres://demo:demo1234@postgres:5432/demo?sslmode=disable" \
  session_secret="super-secret-session-key-for-demo"

echo "Verifying seeded secrets..."
bao kv get secret/demo/api-key
bao kv get secret/demo/app-config

echo "Done — OpenBao secrets seeded successfully."
