# Vizzel Sales Backend

Production backend for Vizzel Sales CRM.

## Production Endpoints

- Frontend: `https://vizzelintel.github.io/vizzel-sales-frontend/`
- Backend API base: `https://vizzel-sales-api.fly.dev/api/v1`

## Required Environment Variables

- `SUPABASE_DB_URL`
- `SUPABASE_URL`
- `SUPABASE_SERVICE_KEY`
- `LARK_APP_ID`
- `LARK_APP_SECRET`
- `LARK_BASE_APP_TOKEN`
- `LARK_TABLE_ID`
- `LIFF_ID`
- `LINE_CHANNEL_SECRET`

Optional for Calendar integration:

- `GOOGLE_CALENDAR_CREDENTIALS_JSON`
- `GOOGLE_CALENDAR_ID`
- `SMTP_HOST`
- `SMTP_PORT`
- `SMTP_USERNAME`
- `SMTP_PASSWORD`
- `SMTP_FROM_EMAIL`
- `SMTP_FROM_NAME`
- `EMAIL_OTP_SECRET`

## Email Verification + Calendar Mail

- New users must verify email with OTP before using business endpoints.
- OTP endpoints:
  - `POST /api/v1/auth/email/send-otp`
  - `POST /api/v1/auth/email/verify-otp`
- Appointment status updates now also attempt email calendar invite (`.ics`) so users on Google/Outlook can import events.

Use `.env.example` as the canonical template for production setup.

## Run Locally

```bash
go mod tidy
go run main.go
```

Health check: `GET /health`