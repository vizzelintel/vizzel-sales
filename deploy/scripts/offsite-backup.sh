#!/usr/bin/env bash
# Optional offsite sync for ~/backups/sales (VS-021).
# Configure ONE of: OFFSITE_RCLONE_REMOTE or OFFSITE_RSYNC_TARGET
#
# Example crontab (after 02:00 local backup):
#   30 2 * * * /opt/sale-app/scripts/offsite-backup.sh >> /home/boss/backups/sales/offsite.log 2>&1
#
# rclone: rclone config create vizzel-backup s3 ... (or drive, etc.)
# export OFFSITE_RCLONE_REMOTE=vizzel-backup:vizzel-sales-backups
set -euo pipefail

BACKUP_ROOT="${BACKUP_ROOT:-/home/boss/backups/sales}"
RCLONE="${OFFSITE_RCLONE_REMOTE:-}"
RSYNC_TARGET="${OFFSITE_RSYNC_TARGET:-}"

log() { echo "[$(date -Iseconds)] $*"; }

if [[ -z "$RCLONE" && -z "$RSYNC_TARGET" ]]; then
  log "SKIP offsite: set OFFSITE_RCLONE_REMOTE or OFFSITE_RSYNC_TARGET"
  exit 0
fi

latest="$(find "$BACKUP_ROOT" -maxdepth 1 -type d -name '20*' | sort | tail -1)"
if [[ -z "$latest" ]]; then
  log "ERROR: no backup directory under $BACKUP_ROOT"
  exit 1
fi

if [[ -n "$RCLONE" ]]; then
  log "rclone sync $latest → $RCLONE/$(basename "$latest")"
  rclone sync "$latest" "${RCLONE}/$(basename "$latest")" --transfers 4
  log "OK rclone"
elif [[ -n "$RSYNC_TARGET" ]]; then
  log "rsync $latest → $RSYNC_TARGET/"
  rsync -az --delete "$latest/" "${RSYNC_TARGET}/$(basename "$latest")/"
  log "OK rsync"
fi
