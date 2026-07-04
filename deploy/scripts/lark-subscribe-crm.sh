#!/usr/bin/env bash
# Subscribe CRM Bitable for drive.file.bitable_record_changed events (after bot added to Base).
set -euo pipefail
ENV_FILE="${ENV_FILE:-/opt/sale-app/backend/deploy/.env.staging}"
# shellcheck disable=SC1090
set -a && source "$ENV_FILE" && set +a

python3 << 'PY'
import json, os, urllib.request, urllib.error

app_id = os.environ["LARK_APP_ID"]
app_secret = os.environ["LARK_APP_SECRET"]
wiki = os.environ.get("LARK_WIKI_NODE_TOKEN") or os.environ.get("LARK_BASE_APP_TOKEN", "")
app_token = os.environ.get("LARK_BASE_APP_TOKEN", "")

req = urllib.request.Request(
    "https://open.larksuite.com/open-apis/auth/v3/tenant_access_token/internal",
    data=json.dumps({"app_id": app_id, "app_secret": app_secret}).encode(),
    headers={"Content-Type": "application/json"}, method="POST")
with urllib.request.urlopen(req, timeout=30) as r:
    token = json.load(r)["tenant_access_token"]

def bitable_app_token(candidate: str) -> str:
    url = f"https://open.larksuite.com/open-apis/bitable/v1/apps/{candidate}"
    req = urllib.request.Request(url, headers={"Authorization": f"Bearer {token}"})
    with urllib.request.urlopen(req, timeout=30) as r:
        body = json.load(r)
    if body.get("code") != 0:
        raise RuntimeError(f"bitable app get: {body}")
    real = body["data"]["app"]["app_token"]
    print(f"CRM: {body['data']['app'].get('name')} app_token={real}")
    return real

real = bitable_app_token(app_token)
url = f"https://open.larksuite.com/open-apis/drive/v1/files/{real}/subscribe?file_type=bitable"
req = urllib.request.Request(url, data=b"{}", headers={"Authorization": f"Bearer {token}", "Content-Type": "application/json"}, method="POST")
try:
    with urllib.request.urlopen(req, timeout=30) as r:
        print("subscribe OK:", json.dumps(json.load(r), ensure_ascii=False))
except urllib.error.HTTPError as e:
    err = e.read().decode()
    print(f"subscribe FAILED HTTP {e.code}: {err}")
    if "1069603" in err or "forbidden" in err.lower():
        print("Hint: bot needs Manage on Base + scope docs:event:subscribe (or drive:drive) published.")
    raise SystemExit(1)
PY
