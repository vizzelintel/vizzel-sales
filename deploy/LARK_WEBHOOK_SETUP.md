# Lark Webhook — Staging & Prod

Webhook handler พร้อมบน server (`LARK_WEBHOOK_ENABLED=true`).

## Staging — ตั้งใน Lark Developer Console (VS-016)

1. เปิด https://open.larksuite.com/app → App ของ Vizzel Sales
2. **Events & Callbacks**
3. **Request URL:** `https://staging-sale-api.vizzeltrack.com/api/v1/webhook/lark`
4. **Verification Token:** ต้องตรงกับค่าใน server:
   ```bash
   grep LARK_EVENT_VERIFY_TOKEN /opt/sale-app/backend/deploy/.env.staging
   ```
5. Subscribe: **Bitable — Record changed** (`drive.file.bitable_record_changed_v1` หรือ v2)
6. กด **Save** → Lark ส่ง `url_verification` — server ตอบ `{"challenge":"..."}` อัตโนมัติ

### ทดบน server (ไม่ต้องรอ Console)

```bash
/opt/sale-app/scripts/staging-smoke.sh
# PASS Lark url_verification + bitable_record_changed mock
```

### ทด manual

```bash
TOKEN=$(grep LARK_EVENT_VERIFY_TOKEN /opt/sale-app/backend/deploy/.env.staging | cut -d= -f2)
curl -sS -X POST https://staging-sale-api.vizzeltrack.com/api/v1/webhook/lark \
  -H 'Content-Type: application/json' \
  -d "{\"type\":\"url_verification\",\"challenge\":\"ping\",\"token\":\"$TOKEN\"}"
```

## Production (หลัง go-live)

- URL: `https://sale-api.vizzeltrack.com/api/v1/webhook/lark`
- Token: จาก `deploy/.env.production` (**คนละค่ากับ staging** — สร้างโดย `prod-prep.sh`)

## Encrypt Key (optional)

ถ้าเปิด Encrypt ใน Lark Console → copy **Encrypt Key** ใส่ `LARK_ENCRYPT_KEY` ใน env แล้ว recreate `sales-api`

## Checklist VS-016

- [ ] Request URL ถูกต้อง (staging หรือ prod)
- [ ] Verification Token = `LARK_EVENT_VERIFY_TOKEN` บน server
- [ ] Subscribe Bitable record changed
- [ ] กด Save สำเร็จ (ไม่มี error ใน Console)
- [ ] แก้ row ใน Lark Bitable → ดู log `docker logs sales-api-staging --tail 50`
