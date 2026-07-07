#!/usr/bin/env bash
# Prod UAT smoke — automated checks (LINE login in app is manual).
# Usage: /opt/sale-app/scripts/prod-smoke.sh
set -euo pipefail

API="${API:-https://sale-api.vizzeltrack.com}"
FE="${FE:-https://sale.vizzeltrack.com}"
ENV_PROD="${ENV_PROD:-/opt/sale-app/backend/deploy/.env.production}"
ENV_DB="${ENV_DB:-/opt/sale-app/backend/deploy/.env.prod}"

pass=0
fail=0
skip=0

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

skip_check() {
  echo "SKIP  $1 — $2"
  skip=$((skip + 1))
}

http_code() {
  curl -s -o /dev/null -w '%{http_code}' "$1"
}

echo "=== Vizzel Sales PROD smoke / UAT ==="
echo "API=$API  FE=$FE"
echo

check "docker sales-api healthy" \
  test "$(docker inspect sales-api --format '{{.State.Health.Status}}' 2>/dev/null || echo missing)" = "healthy"

health_ok() {
  local body
  body="$(curl -sf "$API/health")"
  [[ "$body" == *'"status":"ok"'* && "$body" == *'"db":"ok"'* ]]
}
check "GET /health → ok + db ok" health_ok

check "frontend HTTPS 200" test "$(http_code "$FE/")" = "200"

config_js_ok() {
  local js
  js="$(curl -sf "$FE/js/config.js")"
  [[ "$js" == *sale-api.vizzeltrack.com* ]]
}
check "config.js loads on prod FE" config_js_ok

check "/files/ not public (404)" test "$(http_code "$API/files/x")" = "404"
check "/uploads/ not public (404)" test "$(http_code "$API/uploads/x")" = "404"

# DB counts (migrated Supabase baseline)
if [[ -f "$ENV_DB" ]]; then
  # shellcheck disable=SC1090
  set -a && source "$ENV_DB" && set +a
  db_count() {
    docker exec -e PGPASSWORD="$POSTGRES_PASSWORD" sales-postgres \
      psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" -tAc "$1" 2>/dev/null | tr -d ' '
  }
  pc="$(db_count 'SELECT count(*) FROM projects')"
  uc="$(db_count 'SELECT count(*) FROM users')"
  cc="$(db_count 'SELECT count(*) FROM companies')"
  dc="$(db_count 'SELECT count(*) FROM documents')"
  check "DB projects >= 8 (got $pc)" test "${pc:-0}" -ge 8
  check "DB users >= 4 (got $uc)" test "${uc:-0}" -ge 4
  check "DB companies >= 2 (got $cc)" test "${cc:-0}" -ge 2
  check "DB documents >= 4 (got $dc)" test "${dc:-0}" -ge 4
else
  skip_check "DB counts" "no $ENV_DB"
fi

# Invite codes
invite_ok() {
  local r
  r="$(curl -sf "$API/api/v1/auth/validate-invite?code=$1")"
  [[ "$r" == *company_name* ]]
}
check "invite VIZZEL2025 valid" invite_ok VIZZEL2025
check "invite DLR-7961 valid" invite_ok DLR-7961

LARK_EVENT_VERIFY_TOKEN=""
if [[ -f "$ENV_PROD" ]]; then
  LARK_EVENT_VERIFY_TOKEN="$(grep -E '^LARK_EVENT_VERIFY_TOKEN=' "$ENV_PROD" | head -1 | cut -d= -f2- | tr -d '\r')"
fi

if [[ -n "$LARK_EVENT_VERIFY_TOKEN" ]]; then
  lark_verify_ok() {
    local resp
    resp="$(curl -sf -X POST "$API/api/v1/webhook/lark" \
      -H 'Content-Type: application/json' \
      -d "{\"type\":\"url_verification\",\"token\":\"$LARK_EVENT_VERIFY_TOKEN\",\"challenge\":\"prod-uat\"}")"
    [[ "$resp" == *'"challenge":"prod-uat"'* ]]
  }
  check "Lark url_verification (prod)" lark_verify_ok
else
  skip_check "Lark webhook" "no LARK_EVENT_VERIFY_TOKEN"
fi

# Authenticated API (JWT) — simulates logged-in user without LINE
api_auth_ok() {
  [[ -f "$ENV_PROD" && -f "$ENV_DB" ]] || return 1
  local uid line_id name role token body n
  uid="$(db_count "SELECT id::text FROM users WHERE role='admin' ORDER BY created_at LIMIT 1")"
  line_id="$(db_count "SELECT line_id FROM users WHERE id='$uid'::uuid")"
  name="$(db_count "SELECT full_name FROM users WHERE id='$uid'::uuid")"
  role="admin"
  [[ -n "$uid" && -n "$line_id" ]] || return 1

  token="$(python3 - "$ENV_PROD" "$uid" "$line_id" "$name" "$role" <<'PY'
import sys
from datetime import datetime, timedelta, timezone
import jwt
env_path, uid, line_id, name, role = sys.argv[1:6]
env = {}
for line in open(env_path):
    line = line.strip()
    if not line or line.startswith("#") or "=" not in line:
        continue
    k, v = line.split("=", 1)
    env[k.strip()] = v.strip()
claims = {
    "sub": line_id, "uid": uid, "name": name, "role": role,
    "exp": int((datetime.now(timezone.utc) + timedelta(days=1)).timestamp()),
}
print(jwt.encode(claims, env["JWT_SECRET"], algorithm="HS256"))
PY
)"
  body="$(curl -sf -H "Authorization: Bearer $token" "$API/api/v1/projects?limit=100&scope=directory")"
  n="$(python3 -c "import json,sys; d=json.load(sys.stdin); print(d.get('total', len(d.get('items',[]))))" <<<"$body")"
  [[ "$n" -ge 8 ]]
}
check "GET /api/v1/projects (JWT admin) >= 8" api_auth_ok

doc_download_ok() {
  local doc_id file_url code
  doc_id="$(db_count "SELECT id::text FROM documents ORDER BY created_at LIMIT 1")"
  file_url="$(db_count "SELECT file_url FROM documents WHERE id='$doc_id'::uuid")"
  [[ -n "$doc_id" ]] || return 1

  local token uid line_id name role
  uid="$(db_count "SELECT id::text FROM users WHERE role='admin' ORDER BY created_at LIMIT 1")"
  line_id="$(db_count "SELECT line_id FROM users WHERE id='$uid'::uuid")"
  name="$(db_count "SELECT full_name FROM users WHERE id='$uid'::uuid")"
  role="admin"
  token="$(python3 - "$ENV_PROD" "$uid" "$line_id" "$name" "$role" <<'PY'
import sys
from datetime import datetime, timedelta, timezone
import jwt
env_path, uid, line_id, name, role = sys.argv[1:6]
env = {}
for line in open(env_path):
    line = line.strip()
    if not line or line.startswith("#") or "=" not in line: continue
    k, v = line.split("=", 1); env[k.strip()] = v.strip()
claims = {"sub": line_id, "uid": uid, "name": name, "role": role,
          "exp": int((datetime.now(timezone.utc) + timedelta(days=1)).timestamp())}
print(jwt.encode(claims, env["JWT_SECRET"], algorithm="HS256"))
PY
)"

  # Legacy Supabase URLs redirect; local files would stream
  code="$(curl -s -o /dev/null -w '%{http_code}' -L -H "Authorization: Bearer $token" \
    "$API/api/v1/documents/$doc_id/download")"
  [[ "$code" == "200" || "$code" == "302" || "$code" == "307" ]]
}
if [[ -f "$ENV_DB" ]]; then
  check "document download (JWT, legacy or local)" doc_download_ok
fi

echo
echo "=== Manual UAT (LINE app) ==="
echo "  1. Open https://sale.vizzeltrack.com/ in LINE"
echo "  2. Login with existing account"
echo "  3. Confirm 8 projects visible"
echo "  4. Open project → notes sync from Lark (pull-on-view)"
echo

echo "=== Result: $pass passed, $fail failed, $skip skipped ==="
[[ "$fail" -eq 0 ]]
