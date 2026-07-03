# Server review — self-host branches

Branches for DevOps to verify against [vizzel_sales_selfhost_todo.md](SELF_HOST.md) requirements.

| Repo | Branch | Scope |
|---|---|---|
| `vizzel-sales` | **`feat/self-host-postgres`** | A1–A4 (backend) |
| `vizzel-sales-frontend` | **`feat/production-urls`** | A5 (frontend) |

## Backend checklist (A1–A4)

- [ ] `DATABASE_URL` in `config/database.go`, `.env.example`, README
- [ ] `migrations/000_initial_schema.sql` + `scripts/migrate.sh`
- [ ] `docker-compose.dev.yml` for local Postgres
- [ ] `internal/storage/` LocalFileStore + `GET /api/v1/documents/:id/download`
- [ ] `JWT_SECRET` separate from `LINE_CHANNEL_SECRET` (`config/secrets.go`)
- [ ] CORS whitelist: staging-sale, sale, github.io (transition)
- [ ] `/health` returns DB status
- [ ] `deploy/docker-compose.{prod,staging}.yml`
- [ ] `deploy/nginx/sale-api.conf`, `staging-sale-api.conf`
- [ ] `deploy/.env.production.example`
- [ ] CI: `.github/workflows/test.yml`, `deploy-staging.yml`, `deploy-prod.yml`
- [ ] `fly.toml` moved to `docs/legacy/fly.toml`

## Frontend checklist (A5)

- [ ] `js/config.js` + `config/config.{production,staging,local,legacy}.js`
- [ ] `index.html` + `upload.html` load shared config
- [ ] Staging API → `staging-sale-api.vizzeltrack.com`
- [ ] Production API → `sale-api.vizzeltrack.com`
- [ ] Document view uses JWT download endpoint (not public `/files/`)
- [ ] CI rsync: `/opt/vizzel-sales-frontend-staging/`, `/opt/vizzel-sales-frontend/`

## Server deploy (B0–B7) — not in repo

After load test, on `103.142.150.226`:

1. Clone branches to `/opt/vizzel-sales` and `/opt/vizzel-sales-staging`
2. Create `.env.production` / `.env.staging` from `deploy/.env.production.example`
3. Connect `nginx-proxy` to `smartassettracking_app_network`
4. Mount `deploy/nginx/*.conf`, add static roots for frontend paths
5. DNS + SSL for `staging-sale*`, then `sale*`
6. Run migrations, `docker compose up -d --build`
7. A6: LINE LIFF + webhooks, A7: smoke tests

## Quick verify on server

```bash
# After deploy
curl -s https://staging-sale-api.vizzeltrack.com/health
curl -s https://sale-api.vizzeltrack.com/health
# Expected: {"service":"vizzel-backend","status":"ok","db":"ok"}
```
