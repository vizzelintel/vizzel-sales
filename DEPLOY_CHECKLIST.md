# Self-host deploy checklist

See also: [docs/SELF_HOST.md](docs/SELF_HOST.md)

## Before deploy

- [ ] Load test on smartAssetTracking completed
- [ ] Staging smoke tests pass (A7)
- [ ] `JWT_SECRET` set (not equal to `LINE_CHANNEL_SECRET`)
- [ ] `DATABASE_URL` points to **sales-postgres** (not Supabase / MySQL)
- [ ] `FILE_STORAGE_PATH=/data/docs` volume mounted
- [ ] CORS includes frontend origin(s)
- [ ] LINE / Lark webhooks point to staging first, then prod

## Database

```bash
# Fresh install
MIGRATE_MODE=fresh DATABASE_URL=... bash scripts/migrate.sh

# From Supabase: pg_dump → pg_restore, then verify row counts
```

## Server (DevOps)

- [ ] Clone repo to `/opt/vizzel-sales`
- [ ] Copy `deploy/.env.production.example` → `deploy/.env.production` (mode 600)
- [ ] Connect `nginx-proxy` to `smartassettracking_app_network`
- [ ] Mount `deploy/nginx/*.conf` or include in nginx config
- [ ] DNS: `sale-api.vizzeltrack.com`, `staging-sale-api.vizzeltrack.com`
- [ ] `docker compose -f deploy/docker-compose.prod.yml up -d --build`

## Verify

```bash
curl -s https://staging-sale-api.vizzeltrack.com/health
curl -s https://sale-api.vizzeltrack.com/health
# Expect: {"service":"vizzel-backend","status":"ok","db":"ok"}
```

## Rollback

1. `docker compose -f deploy/docker-compose.prod.yml down`
2. Remove nginx sale-* conf → `nginx -t && reload`
3. Restore DB from latest `pg_dump` if needed

## Legacy Fly.io

Fly config archived at [docs/legacy/fly.toml](docs/legacy/fly.toml). Decommission after 7 stable days on self-host.
