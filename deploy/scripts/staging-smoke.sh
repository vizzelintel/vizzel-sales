#!/usr/bin/env bash
# A7 staging smoke checks — run after deploy or env change.
# Usage: /opt/sale-app/scripts/staging-smoke.sh
set -euo pipefail

API="${API:-https://staging-sale-api.vizzeltrack.com}"
FE="${FE:-https://staging-sale.vizzeltrack.com}"
ENV_STAGING="${ENV_STAGING:-/opt/sale-app/backend/deploy/.env.staging}"

pass=0
fail=0

check() {
  local name="$1"
  shift
  if "$@"; then
    echo "PASS  $name"
    pass=$((pass + 1))
  else
    echo "FAIL  $name"
    fail=$((fail + 1))
  fi
}

is_healthy() {
  [[ "$(docker inspect sales-api-staging --format '{{.State.Health.Status}}' 2>/dev/null || echo missing)" == "healthy" ]]
}

health_ok() {
  local body
  body="$(curl -sf "$API/health")"
  [[ "$body" == *'"status":"ok"'* && "$body" == *'"db":"ok"'* ]]
}

http_code() {
  curl -s -o /dev/null -w '%{http_code}' "$1"
}

echo "=== Vizzel Sales staging smoke ==="
echo "API=$API  FE=$FE"
echo

check "docker sales-api-staging healthy" is_healthy
check "GET /health → status ok + db ok" health_ok
check "frontend HTTPS 200" test "$(http_code "$FE/")" = "200"
check "/files/ not public (404)" test "$(http_code "$API/files/x")" = "404"
check "/uploads/ not public (404)" test "$(http_code "$API/uploads/x")" = "404"

LARK_EVENT_VERIFY_TOKEN=""
if [[ -f "$ENV_STAGING" ]]; then
  LARK_EVENT_VERIFY_TOKEN="$(grep -E '^LARK_EVENT_VERIFY_TOKEN=' "$ENV_STAGING" | head -1 | cut -d= -f2- | tr -d '\r')"
fi

lark_verify_ok() {
  local resp
  resp="$(curl -sf -X POST "$API/api/v1/webhook/lark" \
    -H 'Content-Type: application/json' \
    -d "{\"type\":\"url_verification\",\"challenge\":\"smoke\",\"token\":\"$LARK_EVENT_VERIFY_TOKEN\"}")"
  [[ "$resp" == *'"challenge":"smoke"'* ]]
}

lark_inbound_ok() {
  local resp
  resp="$(curl -sf -X POST "$API/api/v1/webhook/lark" \
    -H 'Content-Type: application/json' \
    -d "{\"token\":\"$LARK_EVENT_VERIFY_TOKEN\",\"type\":\"drive.file.bitable_record_changed_v1\",\"header\":{\"event_type\":\"drive.file.bitable_record_changed_v1\",\"token\":\"$LARK_EVENT_VERIFY_TOKEN\"},\"event\":{\"table_id\":\"tbltest\",\"action_list\":[{\"action\":\"update\",\"record_id\":\"recsmoke\"}]}}")"
  [[ "$resp" == *'"ok":true'* ]]
}

if [[ -n "$LARK_EVENT_VERIFY_TOKEN" ]]; then
  check "Lark url_verification" lark_verify_ok
  check "Lark bitable_record_changed mock → 200" lark_inbound_ok
else
  echo "SKIP  Lark webhook tests (no LARK_EVENT_VERIFY_TOKEN)"
fi

echo
echo "=== Result: $pass passed, $fail failed ==="
[[ "$fail" -eq 0 ]]
