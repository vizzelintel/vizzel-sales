#!/usr/bin/env bash
# Uptime check for sales API (staging required; prod optional until go-live).
# Cron example: */5 * * * * /opt/sale-app/scripts/sales-uptime-check.sh >> /home/boss/backups/sales/uptime.log 2>&1
set -euo pipefail

STAGING_API="${STAGING_API:-https://staging-sale-api.vizzeltrack.com/health}"
PROD_API="${PROD_API:-https://sale-api.vizzeltrack.com/health}"
REQUIRE_PROD="${REQUIRE_PROD:-false}"

log() { echo "[$(date -Iseconds)] $*"; }
fail=0

check_url() {
  local name="$1" url="$2" required="$3"
  local body code
  body="$(curl -fsS -m 15 "$url" 2>&1)" && code=0 || code=$?
  if [[ $code -eq 0 && "$body" == *'"status":"ok"'* && "$body" == *'"db":"ok"'* ]]; then
    log "OK   $name $url"
    return 0
  fi
  if [[ "$required" == true ]]; then
    log "FAIL $name $url — ${body:-HTTP error}"
    fail=1
  else
    log "WARN $name $url — prod not up yet (${body:-HTTP error})"
  fi
}

check_url "staging" "$STAGING_API" true
check_url "prod" "$PROD_API" "$REQUIRE_PROD"

exit "$fail"
