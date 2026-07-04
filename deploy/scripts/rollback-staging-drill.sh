#!/usr/bin/env bash
# Rollback drill for staging sales stack (non-destructive by default).
# Usage:
#   ./rollback-staging-drill.sh           # dry-run: print steps + verify backups exist
#   ./rollback-staging-drill.sh --execute # stop staging stack + reload nginx (recovery: up -d --build)
set -euo pipefail

EXECUTE=false
for arg in "$@"; do
  [[ "$arg" == --execute ]] && EXECUTE=true
done

BACKUP_ROOT="${BACKUP_ROOT:-/home/boss/backups/sales}"
SMART="/opt/smartAssetTracking"
SALE_BACKEND="/opt/sale-app/backend"

log() { echo "[$(date -Iseconds)] $*"; }

latest_backup() {
  find "$BACKUP_ROOT" -maxdepth 1 -type d -name '20*' 2>/dev/null | sort | tail -1
}

log "=== Rollback drill (staging sales) ==="
log "Backup root: $BACKUP_ROOT"
lb="$(latest_backup || true)"
if [[ -n "$lb" ]]; then
  log "Latest backup dir: $lb"
else
  log "WARN no dated backup under $BACKUP_ROOT"
fi

if [[ -f "$SMART/nginx/conf.d/staging-sale-api.conf" ]]; then
  log "OK   nginx conf staging-sale-api.conf present"
else
  log "FAIL missing staging-sale-api.conf"
  exit 1
fi

if docker inspect sales-api-staging >/dev/null 2>&1; then
  log "OK   container sales-api-staging exists ($(docker inspect sales-api-staging --format '{{.State.Status}}'))"
else
  log "WARN sales-api-staging not running"
fi

if [[ "$EXECUTE" != true ]]; then
  log "DRY-RUN — would run:"
  log "  1. cd $SALE_BACKEND && docker compose --env-file deploy/.env -f deploy/docker-compose.staging.yml down"
  log "  2. docker exec nginx-proxy nginx -t && docker exec nginx-proxy nginx -s reload"
  log "  3. (optional) pg_restore from $lb"
  log "Recovery: cd $SALE_BACKEND && docker compose --env-file deploy/.env -f deploy/docker-compose.staging.yml up -d --build"
  log "Re-run with --execute to perform steps 1–2"
  exit 0
fi

log "EXECUTE — stopping staging stack"
cd "$SALE_BACKEND"
docker compose --env-file deploy/.env -f deploy/docker-compose.staging.yml down

log "Reload nginx"
docker exec nginx-proxy nginx -t
docker exec nginx-proxy nginx -s reload

log "DONE — staging stopped. Recovery command above."
