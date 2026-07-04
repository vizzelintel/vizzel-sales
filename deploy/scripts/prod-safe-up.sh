#!/usr/bin/env bash
# Idempotent prod stack update — touches ONLY vizzel_sales_prod compose project.
# Does NOT stop/restart mysql, nest, next, nginx (except optional reload via verify).
#
# Usage:
#   ./prod-safe-up.sh           # up prod API if needed
#   ./prod-safe-up.sh --build   # rebuild prod image
set -euo pipefail

DIR="$(cd "$(dirname "$0")" && pwd)"
BACKEND="${SALE_BACKEND:-/opt/sale-app/backend}"
DEPLOY="$BACKEND/deploy"
BUILD=false

for arg in "$@"; do
  [[ "$arg" == --build ]] && BUILD=true
done

log() { echo "[$(date -Iseconds)] $*"; }

prod_compose_project() {
  if [[ -n "${SALES_PROD_COMPOSE_PROJECT:-}" ]]; then
    echo "$SALES_PROD_COMPOSE_PROJECT"
    return
  fi
  if docker inspect sales-api >/dev/null 2>&1; then
    docker inspect sales-api --format '{{index .Config.Labels "com.docker.compose.project"}}'
  else
    echo "vizzel_sales_prod"
  fi
}

PROJECT="$(prod_compose_project)"
log "Compose project: $PROJECT"

log "Pre-check (other projects)..."
"$DIR/prod-safe-verify.sh" || { log "Abort: pre-check failed"; exit 1; }

log "Sync prod frontend static (no delete of unrelated paths)..."
rsync -a /opt/sale-app/frontend-staging/ /opt/sale-app/frontend/

log "Ensure prod Lark env from staging (wiki, pull-on-view, base token)..."
STAGING="$DEPLOY/.env.staging"
PROD="$DEPLOY/.env.production"
for k in LARK_WIKI_NODE_TOKEN LARK_PULL_ON_VIEW LARK_COL_DETAIL_NOTE LARK_BASE_APP_TOKEN; do
  val="$(grep -E "^${k}=" "$STAGING" 2>/dev/null | head -1 | cut -d= -f2- || true)"
  [[ -z "$val" ]] && continue
  if grep -qE "^${k}=" "$PROD" 2>/dev/null; then
    sed -i "s|^${k}=.*|${k}=${val}|" "$PROD"
  else
    echo "${k}=${val}" >> "$PROD"
  fi
done
chmod 600 "$PROD"

cd "$BACKEND"
if docker inspect sales-api >/dev/null 2>&1; then
  log "Recreate sales-api only (postgres unchanged — safe for other VM projects)"
  if [[ "$BUILD" == true ]]; then
    docker compose -p "$PROJECT" --env-file deploy/.env.prod -f deploy/docker-compose.prod.yml up -d --build --force-recreate --no-deps sales-api
  else
    docker compose -p "$PROJECT" --env-file deploy/.env.prod -f deploy/docker-compose.prod.yml up -d --force-recreate --no-deps sales-api
  fi
else
  log "First prod start (postgres + api)"
  if [[ "$BUILD" == true ]]; then
    docker compose -p "$PROJECT" --env-file deploy/.env.prod -f deploy/docker-compose.prod.yml up -d --build
  else
    docker compose -p "$PROJECT" --env-file deploy/.env.prod -f deploy/docker-compose.prod.yml up -d
  fi
fi

log "Wait sales-api healthy..."
for i in $(seq 1 40); do
  st="$(docker inspect sales-api --format '{{.State.Health.Status}}' 2>/dev/null || echo missing)"
  [[ "$st" == "healthy" ]] && break
  sleep 3
done

ENV_FILE="$PROD" "$DIR/lark-subscribe-crm.sh" || log "WARN lark subscribe"

log "Post-check..."
REQUIRE_PROD=true "$DIR/sales-uptime-check.sh"
"$DIR/prod-safe-verify.sh"

log "Done — prod updated; asset tracking untouched"
