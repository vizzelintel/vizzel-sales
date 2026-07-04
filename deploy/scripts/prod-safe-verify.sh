#!/usr/bin/env bash
# Safe prod verification — no changes to smartAssetTracking / nginx unless --reload-nginx.
#
# Checks:
#   - Asset tracking URLs still OK (app + api)
#   - Sales prod + staging healthy, DB not on host ports
#   - Prod DB row counts, CORS prod-only
#
# Usage:
#   ./prod-safe-verify.sh
#   ./prod-safe-verify.sh --reload-nginx   # only if nginx -t passes
set -euo pipefail

DIR="$(cd "$(dirname "$0")" && pwd)"
BACKEND="${SALE_BACKEND:-/opt/sale-app/backend}"
DEPLOY="$BACKEND/deploy"
RELOAD_NGINX=false
FAIL=0

for arg in "$@"; do
  [[ "$arg" == --reload-nginx ]] && RELOAD_NGINX=true
done

log() { echo "[$(date -Iseconds)] $*"; }
fail() { log "FAIL $*"; FAIL=1; }
ok() { log "OK   $*"; }

check_url() {
  local name="$1" url="$2" expect="${3:-200}"
  local code
  code="$(curl -fsS -o /dev/null -w '%{http_code}' -m 15 "$url" 2>/dev/null || echo 000)"
  if [[ "$code" == "$expect" ]]; then ok "$name ($code) $url"
  else fail "$name ($code, want $expect) $url"; fi
}

log "=== 1/5 Other projects (must stay healthy) ==="
check_url "asset-tracking FE" "https://app.vizzeltrack.com/"
check_url "asset-tracking API" "https://api.vizzeltrack.com/health"

log "=== 2/5 Sales isolation ==="
for c in sales-postgres sales-postgres-staging; do
  if docker port "$c" 2>/dev/null | grep -q .; then
    fail "$c exposes host port: $(docker port "$c")"
  else
    ok "$c no host port"
  fi
done

for c in mysql-db-prod redis-prod nest-backend-prod next-frontend-prod; do
  st="$(docker inspect "$c" --format '{{.State.Status}}' 2>/dev/null || echo missing)"
  [[ "$st" == "running" ]] && ok "$c running" || fail "$c status=$st"
done

log "=== 3/5 Sales prod + staging ==="
for c in sales-api sales-api-staging; do
  st="$(docker inspect "$c" --format '{{.State.Health.Status}}' 2>/dev/null || echo missing)"
  [[ "$st" == "healthy" ]] && ok "$c healthy" || fail "$c health=$st"
done

check_url "sale FE" "https://sale.vizzeltrack.com/"
check_url "sale API" "https://sale-api.vizzeltrack.com/health"
check_url "staging FE" "https://staging-sale.vizzeltrack.com/"
check_url "staging API" "https://staging-sale-api.vizzeltrack.com/health"

log "=== 4/5 Prod DB + env ==="
# shellcheck disable=SC1090
set -a && source "$DEPLOY/.env.prod" && set +a
docker exec sales-postgres psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" -tAc \
  "SELECT 'users='||count(*) FROM users UNION ALL SELECT 'projects='||count(*) FROM projects;" \
  | while read -r line; do ok "prod DB $line"; done

cors="$(grep '^CORS_ORIGINS=' "$DEPLOY/.env.production" | cut -d= -f2-)"
[[ "$cors" == *"staging"* ]] && fail "prod CORS includes staging: $cors" || ok "prod CORS prod-only"

log "=== 5/5 nginx config ==="
if docker exec nginx-proxy nginx -t 2>&1 | grep -q "successful"; then
  ok "nginx -t"
  if [[ "$RELOAD_NGINX" == true ]]; then
    docker exec nginx-proxy nginx -s reload
    ok "nginx reload"
  fi
else
  fail "nginx -t"
fi

log "=== Manual Console (prod) ==="
log "LINE LIFF → https://sale.vizzeltrack.com/"
log "Lark webhook → https://sale-api.vizzeltrack.com/api/v1/webhook/lark"
log "Lark token → grep LARK_EVENT_VERIFY_TOKEN $DEPLOY/.env.production"

if [[ "$FAIL" -ne 0 ]]; then
  log "=== RESULT: FAILED ==="
  exit 1
fi
log "=== RESULT: ALL CHECKS PASSED ==="
