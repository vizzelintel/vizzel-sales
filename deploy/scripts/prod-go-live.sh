#!/usr/bin/env bash
# Production go-live on 103.142.150.226
set -euo pipefail

DIR="$(cd "$(dirname "$0")" && pwd)"
BACKEND="${SALE_BACKEND:-/opt/sale-app/backend}"
# When invoked from repo: deploy/scripts → backend is parent of deploy
if [[ -f "$DIR/../.env.production.example" ]] || [[ -d "$DIR/../../migrations" && -f "$DIR/../../go.mod" ]]; then
  BACKEND="$(cd "$DIR/../.." && pwd)"
fi
DEPLOY="$BACKEND/deploy"
STAGING_ENV="$DEPLOY/.env.staging"
PROD_APP="$DEPLOY/.env.production"

log() { echo "[$(date -Iseconds)] $*"; }

merge_env_key() {
  local key="$1" from="$2" to="$3"
  local val
  val="$(grep -E "^${key}=" "$from" 2>/dev/null | head -1 | cut -d= -f2- || true)"
  [[ -z "$val" ]] && return 0
  if grep -qE "^${key}=" "$to" 2>/dev/null; then
    sed -i "s|^${key}=.*|${key}=${val}|" "$to"
  else
    echo "${key}=${val}" >> "$to"
  fi
}

log "=== Sync prod env keys from staging ==="
for k in LARK_WIKI_NODE_TOKEN LARK_PULL_ON_VIEW LARK_COL_DETAIL_NOTE; do
  merge_env_key "$k" "$STAGING_ENV" "$PROD_APP"
done
chmod 600 "$PROD_APP"

log "=== Sync frontend static ==="
rsync -a --delete /opt/sale-app/frontend-staging/ /opt/sale-app/frontend/

log "=== Supabase migrate (if .env.supabase exists) ==="
if [[ -f "$DEPLOY/.env.supabase" ]] && grep -qE '^SUPABASE_DB_URL=.+' "$DEPLOY/.env.supabase" 2>/dev/null; then
  "$DIR/migrate-supabase-to-prod.sh"
else
  log "SKIP migrate — create $DEPLOY/.env.supabase from .env.supabase.example"
  log "Applying fresh schema only..."
  cd "$BACKEND"
  docker compose --env-file deploy/.env.prod -f deploy/docker-compose.prod.yml up -d sales-postgres
  sleep 5
  # shellcheck disable=SC1090
  set -a && source "$DEPLOY/.env.prod" && set +a
  count="$(docker exec sales-postgres psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" -tAc "SELECT count(*) FROM information_schema.tables WHERE table_schema='public' AND table_name='users';" 2>/dev/null || echo 0)"
  if [[ "${count// /}" == "0" ]]; then
    docker exec -i sales-postgres psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" -v ON_ERROR_STOP=1 \
      < "$BACKEND/migrations/000_initial_schema.sql"
  fi
fi

log "=== Start prod API stack ==="
cd "$BACKEND"
docker compose --env-file deploy/.env.prod -f deploy/docker-compose.prod.yml up -d --build

log "=== Wait for sales-api healthy ==="
for i in $(seq 1 40); do
  st="$(docker inspect sales-api --format '{{.State.Health.Status}}' 2>/dev/null || echo missing)"
  [[ "$st" == "healthy" ]] && break
  sleep 3
done

log "=== Lark subscribe (prod CRM) ==="
ENV_FILE="$PROD_APP" "$DIR/lark-subscribe-crm.sh" || log "WARN lark subscribe — check bot Manage on Base"

log "=== Verify URLs ==="
curl -fsS "https://sale-api.vizzeltrack.com/health" | grep -q '"status":"ok"' && log "OK sale-api /health"
curl -fsS -o /dev/null "https://sale.vizzeltrack.com/" && log "OK sale frontend"

REQUIRE_PROD=true "$DIR/sales-uptime-check.sh" || true

log "=== Go-live done ==="
log "MANUAL: LINE Console → LIFF Endpoint https://sale.vizzeltrack.com/"
log "MANUAL: Lark Console → Request URL https://sale-api.vizzeltrack.com/api/v1/webhook/lark"
log "       Verification Token = grep LARK_EVENT_VERIFY_TOKEN $PROD_APP"
