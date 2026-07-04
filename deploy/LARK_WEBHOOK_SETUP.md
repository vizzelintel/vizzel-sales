# Lark CRM — Staging & Prod

CRM อยู่ใน **Lark Wiki (JP tenant)** ไม่ใช่ `/base/` บน www.larksuite.com

- **เปิด CRM:** `https://ijp5bgs5n9x2.jp.larksuite.com/wiki/Es4NwqP2OimAqZkUADOjVuSip4c?table=tblhmbd8fUOB5GGv`
- **Wiki node token:** `Es4NwqP2OimAqZkUADOjVuSip4c` → `LARK_WIKI_NODE_TOKEN`
- **Bitable app_token:** `HBphba9bJalxLVsthJhjKTcxpCb` → `LARK_BASE_APP_TOKEN`

## Sync architecture (staging)

| ทิศทาง | วิธี | สถานะ |
|--------|------|--------|
| App → Lark | สร้างโครงการ/นัดหมาย | ✅ |
| Lark → App | **`LARK_PULL_ON_VIEW=true`** เปิดโครงการในแอป | ✅ (primary) |
| Lark → App | Webhook push (`drive.file.bitable_record_changed_v1`) | 🟡 handler OK; Lark ยังไม่ push จาก UI |

## Console (VS-016)

1. **Events & Callbacks** → Request URL: `https://staging-sale-api.vizzeltrack.com/api/v1/webhook/lark`
2. **Verification Token** = `LARK_EVENT_VERIFY_TOKEN` ใน `.env.staging` (copy จาก Console → server)
3. Subscribe event: **`drive.file.bitable_record_changed_v1`** (Tenant token)
4. **Encrypt Key:** ปิด (หรือใส่ `LARK_ENCRYPT_KEY` บน server)
5. **Add Application** (ไม่ใช่ Share user): `⋯ → More → Add Application` → **Vizzel Sales Dashboard** → **Manage**
6. **Permissions (publish แล้ว):** `wiki:node:read`, `docs:event:subscribe` หรือ `drive:drive`, `bitable:app`

## หลัง deploy / เปลี่ยน permission

```bash
/opt/sale-app/scripts/lark-subscribe-crm.sh      # ต้องได้ subscribe OK
/opt/sale-app/scripts/staging-smoke.sh           # 7/7 PASS
# หรือรวม:
/opt/sale-app/backend/deploy/scripts/post-deploy-staging.sh
```

## ทด Lark → App (pull — ใช้อยู่)

1. แก้ cell ใน CRM (หมายเหตุ / รายละเอียด)
2. เปิดโครงการใน https://staging-sale.vizzeltrack.com/
3. Log: `[LARK] inbound applied project=...`

## ทด webhook push (optional / D)

แก้ CRM แล้วดู log — ต้องมี **`ua="Go-http-client/1.1"`** ไม่ใช่ `curl`:

```bash
docker logs sales-api-staging -f 2>&1 | grep LARK
```

## Production

- URL: `https://sale-api.vizzeltrack.com/api/v1/webhook/lark`
- Token แยกจาก staging (`prod-prep.sh` สร้างใหม่)
- Subscribe + Add Application ทำซ้ำบน prod Base
