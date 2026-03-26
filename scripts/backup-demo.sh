#!/usr/bin/env bash
# backup-demo.sh — Daily backup of the Kratos Postgres database on the demo VM.
#
# Usage:
#   ./scripts/backup-demo.sh
#
# Configuration (environment variables):
#   BACKUP_DIR          Directory where backup files are stored.
#                       Default: /backups
#   RETAIN_DAYS         Number of daily backups to keep before pruning.
#                       Default: 7
#   POSTGRES_CONTAINER  Name of the running Postgres Docker container.
#                       Default: demo-postgres
#   POSTGRES_USER       Postgres superuser for pg_dump.
#                       Default: kratos
#   POSTGRES_DB         Database name to dump.
#                       Default: kratos
#   RSYNC_TARGET        Optional rsync destination for remote copy, e.g.
#                       u123456@u123456.your-storagebox.de::backups/vibewarden
#                       When empty, the rsync step is skipped.
#   RSYNC_SSH_KEY       Path to the SSH private key used by rsync.
#                       Only consulted when RSYNC_TARGET is set.
#                       Default: ~/.ssh/id_ed25519
#
# The script is designed to be run as a daily cron job. See
# examples/demo-app/RECOVERY.md for full setup instructions.

set -euo pipefail

# --------------------------------------------------------------------------
# Configuration
# --------------------------------------------------------------------------
BACKUP_DIR="${BACKUP_DIR:-/backups}"
RETAIN_DAYS="${RETAIN_DAYS:-7}"
POSTGRES_CONTAINER="${POSTGRES_CONTAINER:-demo-postgres}"
POSTGRES_USER="${POSTGRES_USER:-kratos}"
POSTGRES_DB="${POSTGRES_DB:-kratos}"
RSYNC_TARGET="${RSYNC_TARGET:-}"
RSYNC_SSH_KEY="${RSYNC_SSH_KEY:-${HOME}/.ssh/id_ed25519}"

# --------------------------------------------------------------------------
# Helpers
# --------------------------------------------------------------------------
log() {
    echo "[backup-demo] $(date -u '+%Y-%m-%dT%H:%M:%SZ') $*"
}

die() {
    log "ERROR: $*" >&2
    exit 1
}

# --------------------------------------------------------------------------
# Validate environment
# --------------------------------------------------------------------------
command -v docker >/dev/null 2>&1 || die "docker not found in PATH"

if ! docker inspect --type container "$POSTGRES_CONTAINER" >/dev/null 2>&1; then
    die "container '$POSTGRES_CONTAINER' is not running"
fi

# --------------------------------------------------------------------------
# Prepare backup directory
# --------------------------------------------------------------------------
mkdir -p "$BACKUP_DIR"

TIMESTAMP="$(date -u '+%Y-%m-%d')"
BACKUP_FILE="${BACKUP_DIR}/kratos-backup-${TIMESTAMP}.sql.gz"

# --------------------------------------------------------------------------
# Run pg_dump inside the Postgres container, compress on the fly
# --------------------------------------------------------------------------
log "Starting pg_dump of database '$POSTGRES_DB' from container '$POSTGRES_CONTAINER'..."

docker exec "$POSTGRES_CONTAINER" \
    pg_dump -U "$POSTGRES_USER" "$POSTGRES_DB" \
    | gzip -9 > "$BACKUP_FILE"

BACKUP_SIZE="$(du -sh "$BACKUP_FILE" | cut -f1)"
log "Backup written: $BACKUP_FILE ($BACKUP_SIZE)"

# --------------------------------------------------------------------------
# Prune old backups (keep last RETAIN_DAYS daily files)
# --------------------------------------------------------------------------
log "Pruning backups older than ${RETAIN_DAYS} days..."
find "$BACKUP_DIR" -maxdepth 1 -name 'kratos-backup-*.sql.gz' \
    -mtime "+${RETAIN_DAYS}" -delete -print \
    | while read -r pruned; do
        log "Deleted old backup: $pruned"
    done

# --------------------------------------------------------------------------
# Optional: rsync to remote storage (Hetzner Storage Box or any rsync target)
# --------------------------------------------------------------------------
if [[ -n "$RSYNC_TARGET" ]]; then
    log "Rsyncing $BACKUP_DIR to $RSYNC_TARGET ..."

    SSH_OPTS="-o StrictHostKeyChecking=no -o BatchMode=yes"
    if [[ -f "$RSYNC_SSH_KEY" ]]; then
        SSH_OPTS="$SSH_OPTS -i $RSYNC_SSH_KEY"
    fi

    rsync -az --delete \
        -e "ssh $SSH_OPTS" \
        "${BACKUP_DIR}/" \
        "$RSYNC_TARGET"

    log "Rsync complete."
else
    log "RSYNC_TARGET not set — skipping remote copy."
fi

log "Backup finished successfully."
