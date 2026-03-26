#!/usr/bin/env bash
# deploy-demo.sh — Deploy or roll back the public demo on a remote VM.
#
# Usage:
#   ./scripts/deploy-demo.sh <ssh-target>            # deploy latest
#   ./scripts/deploy-demo.sh <ssh-target> --rollback # revert to the previous image tag
#
# Examples:
#   ./scripts/deploy-demo.sh root@challenge.vibewarden.dev
#   ./scripts/deploy-demo.sh root@challenge.vibewarden.dev --rollback
#
# The script assumes:
#   - The remote VM has Docker + Docker Compose v2 installed.
#   - The demo repo is checked out at DEMO_DIR (default: ~/vibewarden).
#   - A .env file already exists in examples/demo-app/ with production secrets.

set -euo pipefail

# --------------------------------------------------------------------------
# Configuration — override via env vars if needed.
# --------------------------------------------------------------------------
DEMO_DIR="${DEMO_DIR:-~/vibewarden}"
COMPOSE_FILE="examples/demo-app/docker-compose.prod.yml"

# --------------------------------------------------------------------------
# Argument parsing
# --------------------------------------------------------------------------
SSH_TARGET=""
ROLLBACK=false

usage() {
    echo "Usage: $0 <ssh-target> [--rollback]"
    echo ""
    echo "  ssh-target   SSH destination, e.g. root@challenge.vibewarden.dev"
    echo "  --rollback   Revert services to their previously pulled image tags"
    echo ""
    echo "Environment variables:"
    echo "  DEMO_DIR     Remote path to the vibewarden checkout (default: ~/vibewarden)"
    exit 1
}

for arg in "$@"; do
    case "$arg" in
        --rollback)
            ROLLBACK=true
            ;;
        --help|-h)
            usage
            ;;
        *)
            if [[ -z "$SSH_TARGET" ]]; then
                SSH_TARGET="$arg"
            else
                echo "error: unexpected argument: $arg" >&2
                usage
            fi
            ;;
    esac
done

if [[ -z "$SSH_TARGET" ]]; then
    echo "error: ssh-target is required" >&2
    usage
fi

# --------------------------------------------------------------------------
# Helpers
# --------------------------------------------------------------------------
log() {
    echo "[deploy-demo] $*"
}

remote() {
    # Run a command on the remote host via SSH.
    # Pass -T to suppress the "pseudo-terminal" warning when there's no tty.
    ssh -T "$SSH_TARGET" "$@"
}

# --------------------------------------------------------------------------
# Rollback
# --------------------------------------------------------------------------
rollback() {
    log "Rolling back to previous image tags on $SSH_TARGET ..."

    remote bash -s <<EOF
set -euo pipefail
cd ${DEMO_DIR}/${COMPOSE_FILE%/*}

# Compose file lives inside examples/demo-app/; change into that dir so
# relative paths in the file resolve correctly.
COMPOSE_DIR="${DEMO_DIR}/examples/demo-app"
cd "\$COMPOSE_DIR"

# Revert by tagging the previous images back and restarting.
# We track the previous digest in a local file written during the last deploy.
PREV_FILE=".previous-images"
if [[ ! -f "\$PREV_FILE" ]]; then
    echo "error: no previous-images file found — cannot roll back" >&2
    exit 1
fi

echo "==> Stopping current stack..."
docker compose -f docker-compose.prod.yml down --remove-orphans

echo "==> Restoring previous images..."
while IFS='=' read -r service image; do
    [[ -z "\$service" || -z "\$image" ]] && continue
    echo "  \$service => \$image"
    docker tag "\$image" "\$(docker compose -f docker-compose.prod.yml config --format json | \
        python3 -c "import sys,json; d=json.load(sys.stdin); print(d['services']['\$service']['image'])" 2>/dev/null || echo "\$image")"
done < "\$PREV_FILE"

echo "==> Starting stack with previous images..."
docker compose -f docker-compose.prod.yml up -d --remove-orphans

echo "==> Stack status after rollback:"
docker compose -f docker-compose.prod.yml ps
EOF

    log "Rollback complete."
}

# --------------------------------------------------------------------------
# Deploy
# --------------------------------------------------------------------------
deploy() {
    log "Deploying to $SSH_TARGET ..."

    remote bash -s <<EOF
set -euo pipefail

COMPOSE_DIR="${DEMO_DIR}/examples/demo-app"
cd "\$COMPOSE_DIR"

echo "==> Pulling latest code..."
cd "${DEMO_DIR}"
git fetch --all
git pull --ff-only

cd "\$COMPOSE_DIR"

echo "==> Saving current image digests for rollback..."
docker compose -f docker-compose.prod.yml ps --format json 2>/dev/null | \
    python3 -c "
import sys, json
lines = sys.stdin.read().strip()
if not lines:
    sys.exit(0)
try:
    services = json.loads(lines)
except json.JSONDecodeError:
    # older Compose emits one JSON object per line
    services = [json.loads(l) for l in lines.splitlines() if l.strip()]
if isinstance(services, dict):
    services = [services]
for svc in services:
    name = svc.get('Service', svc.get('Name', ''))
    image = svc.get('Image', '')
    if name and image:
        print(f'{name}={image}')
" > .previous-images || true

echo "==> Pulling latest images..."
docker compose -f docker-compose.prod.yml pull --quiet

echo "==> Starting updated stack..."
docker compose -f docker-compose.prod.yml up -d --remove-orphans

echo "==> Stack status after deployment:"
docker compose -f docker-compose.prod.yml ps
EOF

    log "Deployment to $SSH_TARGET complete."
}

# --------------------------------------------------------------------------
# Main
# --------------------------------------------------------------------------
if [[ "$ROLLBACK" == "true" ]]; then
    rollback
else
    deploy
fi
