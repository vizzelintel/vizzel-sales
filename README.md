# Vizzel Sales Backend

Go API for Vizzel Sales CRM (LINE LIFF + Lark Bitable sync).

## Production URLs (self-host target)

| Env | Frontend | API |
|-----|----------|-----|
| Staging | `https://staging-sale.vizzeltrack.com` | `https://staging-sale-api.vizzeltrack.com` |
| Production | `https://sale.vizzeltrack.com` | `https://sale-api.vizzeltrack.com` |

Legacy: Fly.io + GitHub Pages (decommission after cutover).

## Quick start

```bash
cp .env.example .env
docker compose -f docker-compose.dev.yml up -d
MIGRATE_MODE=fresh DATABASE_URL=postgres://vizzel:vizzel_dev@127.0.0.1:5433/vizzel_sales?sslmode=disable bash scripts/migrate.sh
go run .
```

See **[docs/SELF_HOST.md](docs/SELF_HOST.md)** for git flow, migrations, and deploy.

## Required environment variables

- `DATABASE_URL` — PostgreSQL
- `JWT_SECRET` — API token signing (**separate from** `LINE_CHANNEL_SECRET`)
- `LINE_CHANNEL_SECRET` — LINE webhook signature
- `LIFF_ID`, Lark vars — see `.env.example`

File storage (self-host): `FILE_STORAGE_DRIVER=local`, `FILE_STORAGE_PATH=/data/docs`

## Document download (secure)

Uploads store an internal storage key. Clients download via:

`GET /api/v1/documents/:id/download` (JWT required)

## Email verification + calendar

- OTP: `POST /api/v1/auth/email/send-otp`, `verify-otp`
- Appointment updates send `.ics` email when SMTP is configured

## Tests

```bash
go test ./...
```

CI runs tests with Postgres service + fresh schema migration.

## Deploy checklist

[DEPLOY_CHECKLIST.md](DEPLOY_CHECKLIST.md)

**Note:** `StartAutoRejectCron()` runs in-process — scale API to 1 replica only until cron is extracted.
