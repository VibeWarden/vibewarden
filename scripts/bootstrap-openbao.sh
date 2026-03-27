#!/usr/bin/env sh
# bootstrap-openbao.sh — initialises OpenBao for VibeWarden development.
#
# This script is designed to run as a Docker Compose init container
# (or directly from a developer machine). It:
#   1. Waits for OpenBao to be healthy (unsealed in dev mode).
#   2. Enables the KV v2 secret engine at "secret/".
#   3. Enables the database secret engine at "database/".
#   4. Creates a VibeWarden-specific policy (least-privilege).
#   5. Enables AppRole auth and creates a VibeWarden machine identity.
#   6. Prints the role_id and secret_id for use in vibewarden.yaml.
#
# Usage (Docker Compose):
#   Called automatically by the openbao-bootstrap service.
#
# Usage (manual):
#   BAO_ADDR=http://localhost:8200 BAO_TOKEN=vibewarden-dev ./scripts/bootstrap-openbao.sh
#
# Environment:
#   BAO_ADDR      — OpenBao server address (default: http://openbao:8200)
#   BAO_TOKEN     — Root token for bootstrapping (default: vibewarden-dev)
#   SKIP_DATABASE — Set to "true" to skip database engine setup

set -eu

BAO_ADDR="${BAO_ADDR:-http://openbao:8200}"
BAO_TOKEN="${BAO_TOKEN:-vibewarden-dev}"
SKIP_DATABASE="${SKIP_DATABASE:-false}"
MAX_RETRIES=30
RETRY_INTERVAL=2

log() { printf '[bootstrap-openbao] %s\n' "$1"; }

# ---------------------------------------------------------------------------
# 1. Wait for OpenBao to be healthy.
# ---------------------------------------------------------------------------
log "waiting for OpenBao at $BAO_ADDR ..."
i=0
while [ "$i" -lt "$MAX_RETRIES" ]; do
    status=$(curl -sf -o /dev/null -w '%{http_code}' "$BAO_ADDR/v1/sys/health" 2>/dev/null || true)
    # 200 = unsealed leader, 429 = standby — both are "reachable and ready".
    if [ "$status" = "200" ] || [ "$status" = "429" ]; then
        log "OpenBao is healthy (HTTP $status)"
        break
    fi
    i=$((i + 1))
    if [ "$i" -ge "$MAX_RETRIES" ]; then
        log "ERROR: OpenBao did not become healthy after $MAX_RETRIES attempts"
        exit 1
    fi
    log "OpenBao not ready (HTTP $status), retrying in ${RETRY_INTERVAL}s ... ($i/$MAX_RETRIES)"
    sleep "$RETRY_INTERVAL"
done

# Helper: call the OpenBao API with the root token.
bao() {
    method="$1"
    path="$2"
    data="${3:-}"
    if [ -n "$data" ]; then
        curl -sf -X "$method" \
            -H "X-Vault-Token: $BAO_TOKEN" \
            -H "Content-Type: application/json" \
            -d "$data" \
            "$BAO_ADDR$path"
    else
        curl -sf -X "$method" \
            -H "X-Vault-Token: $BAO_TOKEN" \
            "$BAO_ADDR$path"
    fi
}

# ---------------------------------------------------------------------------
# 2. Enable KV v2 secret engine at "secret/".
# ---------------------------------------------------------------------------
log "enabling KV v2 secret engine at secret/ ..."
bao POST /v1/sys/mounts/secret \
    '{"type":"kv","options":{"version":"2"}}' > /dev/null 2>&1 || \
    log "KV v2 already mounted at secret/ (continuing)"

# ---------------------------------------------------------------------------
# 3. Enable database secret engine at "database/" (optional).
# ---------------------------------------------------------------------------
if [ "$SKIP_DATABASE" != "true" ]; then
    log "enabling database secret engine at database/ ..."
    bao POST /v1/sys/mounts/database \
        '{"type":"database"}' > /dev/null 2>&1 || \
        log "database engine already mounted (continuing)"
fi

# ---------------------------------------------------------------------------
# 4. Create a VibeWarden-specific policy.
# ---------------------------------------------------------------------------
log "creating vibewarden policy ..."
bao PUT /v1/sys/policies/acl/vibewarden \
    '{
        "policy": "path \"secret/data/*\" { capabilities = [\"create\",\"read\",\"update\",\"delete\",\"list\"] } path \"secret/metadata/*\" { capabilities = [\"read\",\"list\",\"delete\"] } path \"database/creds/*\" { capabilities = [\"read\"] } path \"sys/leases/renew\" { capabilities = [\"update\"] } path \"sys/leases/revoke\" { capabilities = [\"update\"] }"
    }' > /dev/null

log "vibewarden policy created"

# ---------------------------------------------------------------------------
# 5. Enable AppRole auth method.
# ---------------------------------------------------------------------------
log "enabling AppRole auth method ..."
bao POST /v1/sys/auth/approle \
    '{"type":"approle"}' > /dev/null 2>&1 || \
    log "AppRole already enabled (continuing)"

# Create the vibewarden AppRole.
log "creating vibewarden AppRole ..."
bao POST /v1/auth/approle/role/vibewarden \
    '{
        "token_policies": ["vibewarden"],
        "token_ttl": "1h",
        "token_max_ttl": "24h",
        "secret_id_ttl": "0",
        "secret_id_num_uses": 0
    }' > /dev/null

# ---------------------------------------------------------------------------
# 6. Read and print role_id + secret_id.
# ---------------------------------------------------------------------------
log "reading role_id ..."
ROLE_ID=$(bao GET /v1/auth/approle/role/vibewarden/role-id | \
    sed 's/.*"role_id":"\([^"]*\)".*/\1/')

log "generating secret_id ..."
SECRET_ID=$(bao POST /v1/auth/approle/role/vibewarden/secret-id '{}' | \
    sed 's/.*"secret_id":"\([^"]*\)".*/\1/')

echo ""
echo "============================================================"
echo "  OpenBao bootstrap complete!"
echo "============================================================"
echo ""
echo "  Add these to your vibewarden.yaml or set as env vars:"
echo ""
echo "  secrets:"
echo "    enabled: true"
echo "    provider: openbao"
echo "    openbao:"
echo "      address: http://openbao:8200"
echo "      auth:"
echo "        method: approle"
echo "        role_id: $ROLE_ID"
echo "        secret_id: $SECRET_ID"
echo ""
echo "  Or set environment variables:"
echo "  OPENBAO_ROLE_ID=$ROLE_ID"
echo "  OPENBAO_SECRET_ID=$SECRET_ID"
echo ""
echo "  Dev token (dev mode only — revoke in production):"
echo "  OPENBAO_TOKEN=$BAO_TOKEN"
echo ""
echo "============================================================"

log "bootstrap complete"
