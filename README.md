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

Use `.env.example` as the canonical template for production setup.

## Run Locally

```bash
go mod tidy
go run main.go
```

Health check: `GET /health`