# Vizzel Sales CRM - Production Deploy Checklist

Single-environment deployment (production only).

## 1) GitHub

- [ ] `main` branch is green and reviewed.
- [ ] No secrets in tracked files (`.env`, keys, tokens).
- [ ] Frontend repo has GitHub Pages deploy workflow enabled.
- [ ] Backend repo has latest commit pushed before Fly deploy.

## 2) Supabase

- [ ] `SUPABASE_DB_URL` is set in Fly secrets.
- [ ] `SUPABASE_URL` is set in Fly secrets.
- [ ] `SUPABASE_SERVICE_KEY` is set in Fly secrets.
- [ ] Required migrations are applied in order (`001` through `011` in `migrations/`).
- [ ] `go test ./...` passes locally before deploy.
- [ ] DB connectivity check passes from backend (`/health` + startup logs).

## 3) Fly.io (Backend)

- [ ] App name: `vizzel-sales-api`.
- [ ] API base URL is `https://vizzel-sales-api.fly.dev/api/v1`.
- [ ] Secrets are set:
  - [ ] `SUPABASE_DB_URL`
  - [ ] `SUPABASE_URL`
  - [ ] `SUPABASE_SERVICE_KEY`
  - [ ] `LARK_APP_ID`
  - [ ] `LARK_APP_SECRET`
  - [ ] `LARK_BASE_APP_TOKEN`
  - [ ] `LARK_TABLE_ID`
  - [ ] `LIFF_ID`
  - [ ] `LINE_CHANNEL_SECRET`
- [ ] Optional calendar secrets configured when calendar feature is used:
  - [ ] `GOOGLE_CALENDAR_CREDENTIALS_JSON`
  - [ ] `GOOGLE_CALENDAR_ID`
  - [ ] `SMTP_HOST`
  - [ ] `SMTP_PORT`
  - [ ] `SMTP_USERNAME`
  - [ ] `SMTP_PASSWORD`
  - [ ] `SMTP_FROM_EMAIL`
  - [ ] `SMTP_FROM_NAME`
  - [ ] `EMAIL_OTP_SECRET`
- [ ] CORS allows `https://vizzelintel.github.io`.
- [ ] `/health` returns `200`.

## 4) LIFF / LINE

- [ ] LIFF ID configured in frontend (`LIFF_ID` constant).
- [ ] Login redirect URI: `https://vizzelintel.github.io/vizzel-sales-frontend/`.
- [ ] Backend webhook URL set to `https://vizzel-sales-api.fly.dev/api/v1/webhook` (if webhook is used).
- [ ] LINE channel secret matches `LINE_CHANNEL_SECRET` on Fly.
- [ ] LINE channel is not in `developing` mode for public users (or all testers are explicitly added).

## 5) Lark

- [ ] `LARK_APP_ID` and `LARK_APP_SECRET` are valid.
- [ ] Lark app is added as **admin/member** of the Wiki space (required for wiki Bitable API).
- [ ] If Bitable is inside Wiki (URL `…/wiki/{token}?table=tbl…`):
  - [ ] `LARK_WIKI_NODE_TOKEN` = wiki token from URL (e.g. `Es4NwqP2OimAqZkUADOjVuSip4c`)
  - [ ] `LARK_TABLE_ID` = `table=` from URL (e.g. `tblhmbd8fUOB5GGv`)
- [ ] Or standalone base: `LARK_BASE_APP_TOKEN` = app token from `…/base/{token}` URL.
- [ ] `GET /api/v1/admin/lark-diagnose` → `config_ok`, `fields_ok`, `records_ok` all true.
- [ ] `POST /api/v1/admin/lark-sync-probe` → `{ "ok": true }` for a test project.
- [ ] Create/update sync works after project create and status change.
- [ ] Bitable table includes appointment + document columns (see `GET /api/v1/admin/lark-diagnose` → `recommended_columns`):
  - Dates (type **Date**): `วันพรีเซ็น`, `วัน Demo`, `วัน Site Survey`
  - Text: `รูปแบบ Present`, `หมายเหตุ Present`, `หมายเหตุ Demo`, `หมายเหตุ Site Survey`, `สรุปนัดหมาย`, `เอกสาร`
  - URL: `ลิงก์ Google Meet` (Support วางลิงก์ → webhook inbound → แอปแสดงปุ่ม Meet)
  - Long text (read-only จากแอป): `สรุปนัดหมาย` (รายการสูงสุด 10/ประเภท), `วิธีใช้ (Support)`
  - If date columns are **Text** instead, set Fly secret `LARK_DATE_FORMAT=text`
- [ ] Sync runs after: create project, นัดหมาย (present/demo/site survey), แนบ/ลบเอกสาร, เปลี่ยนสถานะ
- [ ] Admin bulk re-sync: `POST /api/v1/admin/sync-lark`
- [ ] **Lark → App webhook** (แก้ใน Lark แล้วแอปอัปเดต):
  - [ ] Fly secret `LARK_EVENT_VERIFY_TOKEN` = token เดียวกับใน Lark Developer Console
  - [ ] Fly secret `LARK_WEBHOOK_ENABLED=true`
  - [ ] Lark Events → Request URL `https://vizzel-sales-api.fly.dev/api/v1/webhook/lark`
  - [ ] Subscribe event `drive.file.bitable_record_changed_v1` (bitable record changed)
  - [ ] Bitable column `รายละเอียดเพิ่มเติม` (Text) exists
  - [ ] แก้รายละเอียดใน Lark → **ปิดแล้วเปิดโครงการในแอปอีกครั้ง** → ข้อความตรงกัน (แอปดึงจาก Lark อัตโนมัติทุก ~20 วินาทีต่อโครงการ)
  - [ ] ถ้า webhook ไม่มา: ดู Fly log ว่ามี `[LARK] webhook bitable` หรือไม่; ใช้ `POST /admin/lark-pull-inbound?project_id=...` (admin) ทดสอบดึงทันที

## 5b) Notifications

- [ ] Fly: `NOTIFY_ENABLED=true`
- [ ] Fly: `LARK_NOTIFY_CHAT_ID` = กลุ่ม Lark ที่มีสมาชิก Base + บอทแอป (scope `im:message`)
- [ ] Fly: `LARK_NOTIFY_ENABLED=true` (optional `NOTIFY_EXTRA_EMAILS` สำหรับอีเมลนอกแอป)
- [ ] SMTP secrets ครบ (ส่ง .ics ตอนนัดหมาย → ผู้ใช้ที่ยืนยันอีเมลในแอปแล้ว)
- [ ] สร้างโครงการใหม่ → ข้อความในกลุ่ม Lark; สร้างนัดหมาย → อีเมลแนบ invite.ics

## 6) Frontend URLs

- [ ] Frontend URL: `https://vizzelintel.github.io/vizzel-sales-frontend/`.
- [ ] Frontend API target: `https://vizzel-sales-api.fly.dev`.
- [ ] Upload page returns to the same frontend URL after success.

## 7) Smoke Test

- [ ] LINE login succeeds for existing user.
- [ ] New user returns `user_not_found` flow.
- [ ] Create project works (`status = register`).
- [ ] Upload `quotation_support` advances `present -> quotation`.
- [ ] Upload `tor_support` + `tor_dealer` advances to `tor` (dealer-owned project).
- [ ] Upload `contract` sets `status = contract` and clears auto reject timer.
- [ ] Only support/admin can upload `closing`.
- [ ] Lark sync logs show successful upsert.
- [ ] Unverified user is blocked with `email_not_verified`.
- [ ] OTP email send/verify flow works.
- [ ] Appointment status sends calendar invite mail (`.ics`) successfully.
