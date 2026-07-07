#!/usr/bin/env bash
# Sync latest ~/backups/sales/* to offsite (Cloudflare R2 via rclone, or rsync target).
#
# Setup R2 (one time):
#   cp /home/boss/backups/sales/offsite.env.example /home/boss/backups/sales/offsite.env
#   chmod 600 offsite.env   # fill R2_* values from Cloudflare dashboard
#   /opt/sale-app/scripts/offsite-backup.sh
#
# Cron (after 02:00 local backup):
#   30 2 * * * /opt/sale-app/scripts/offsite-backup.sh >> /home/boss/backups/sales/offsite.log 2>&1
set -euo pipefail

BACKUP_ROOT="${BACKUP_ROOT:-/home/boss/backups/sales}"
ENV_FILE="${OFFSITE_ENV:-/home/boss/backups/sales/offsite.env}"
RCLONE="${OFFSITE_RCLONE_REMOTE:-}"
RSYNC_TARGET="${OFFSITE_RSYNC_TARGET:-}"

log() { echo "[$(date -Iseconds)] $*"; }

if [[ -f "$ENV_FILE" ]]; then
  # shellcheck disable=SC1090
  set -a && source "$ENV_FILE" && set +a
  RCLONE="${OFFSITE_RCLONE_REMOTE:-$RCLONE}"
  RSYNC_TARGET="${OFFSITE_RSYNC_TARGET:-$RSYNC_TARGET}"
fi

r2_remote_spec() {
  [[ -n "${R2_ACCESS_KEY_ID:-}" && -n "${R2_SECRET_ACCESS_KEY:-}" && -n "${R2_BUCKET:-}" && -n "${R2_ACCOUNT_ID:-}" ]] || return 1
  local endpoint="${R2_ENDPOINT:-https://${R2_ACCOUNT_ID}.r2.cloudflarestorage.com}"
  printf ':s3,provider=Cloudflare,access_key_id=%s,secret_access_key=%s,endpoint=%s:%s' \
    "$R2_ACCESS_KEY_ID" "$R2_SECRET_ACCESS_KEY" "$endpoint" "$R2_BUCKET"
}

latest="$(find "$BACKUP_ROOT" -maxdepth 1 -type d -name '20*' | sort | tail -1)"
if [[ -z "$latest" ]]; then
  log "ERROR: no backup directory under $BACKUP_ROOT — run sales-backup.sh first"
  exit 1
fi

dest_name="$(basename "$latest")"

if [[ -n "$RCLONE" ]]; then
  log "rclone sync $latest → $RCLONE/$dest_name"
  rclone sync "$latest" "${RCLONE}/${dest_name}" --transfers 4
  log "OK rclone → $RCLONE"
elif spec="$(r2_remote_spec)"; then
  prefix="${R2_PREFIX:-vizzel-sales-backups}"
  log "rclone sync $latest → R2://${R2_BUCKET}/${prefix}/${dest_name}"
  rclone sync "$latest" "${spec}/${prefix}/${dest_name}" --transfers 4
  log "OK Cloudflare R2"
elif [[ -n "$RSYNC_TARGET" ]]; then
  mkdir -p "$RSYNC_TARGET"
  log "rsync $latest → $RSYNC_TARGET/$dest_name/"
  rsync -az "$latest/" "${RSYNC_TARGET}/${dest_name}/"
  log "OK rsync → $RSYNC_TARGET"
else
  log "ERROR: configure offsite.env (R2_* or OFFSITE_RCLONE_REMOTE or OFFSITE_RSYNC_TARGET)"
  exit 1
fi
