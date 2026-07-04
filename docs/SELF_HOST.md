# Self-host PostgreSQL + local file storage

## Git flow

| Branch | Purpose |
|--------|---------|
| `main` | stable; merges via PR |
| `feat/self-host-postgres` | self-host migration (this work) |
| `feat/production-urls` | frontend URLs (vizzel-sales-frontend repo) |

**Release tags (prod deploy):** `sale-v1.0.0`, `sale-v1.0.1`, …

```bash
# 1. Feature branch from main
git checkout main && git pull
git checkout -b feat/self-host-postgres

# 2. Push + open PR → main
git push -u origin feat/self-host-postgres

# 3. After merge, tag for production deploy
git checkout main && git pull
git tag sale-v1.0.0
git push origin sale-v1.0.0
```

## Local development

```bash
cp .env.example .env
docker compose -f docker-compose.dev.yml up -d postgres
MIGRATE_MODE=fresh DATABASE_URL=postgres://vizzel:vizzel_dev@127.0.0.1:5433/vizzel_sales?sslmode=disable bash scripts/migrate.sh
go run .
```

Or full stack:

```bash
docker compose -f docker-compose.dev.yml up -d --build
```

## Environment variables

See `.env.example` and `deploy/.env.production.example`.

Required for self-host:
- `DATABASE_URL` — PostgreSQL connection string
- `JWT_SECRET` — API token signing (separate from LINE secret)
- `LINE_CHANNEL_SECRET` — LINE webhook signature
- `FILE_STORAGE_PATH` — local document storage (default `/data/docs`)

## Migrations

| Scenario | Command |
|----------|---------|
| **Fresh database** | `MIGRATE_MODE=fresh DATABASE_URL=... bash scripts/migrate.sh` |
| **Existing Supabase** | run `001`–`014` only (skip `000`) or pg_dump/restore |

## Deploy

See `DEPLOY_CHECKLIST.md` and `deploy/docker-compose.prod.yml`.

**Important:** `StartAutoRejectCron()` runs in-process — do not scale `sales-api` to >1 replica until cron is extracted.
