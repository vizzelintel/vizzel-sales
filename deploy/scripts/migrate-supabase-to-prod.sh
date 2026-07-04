#!/usr/bin/env bash
# One-shot: pg_dump Supabase → restore into self-hosted prod Postgres.
#
# Prereq: deploy/.env.supabase with:
#   SUPABASE_DB_URL=postgresql://postgres.[ref]:[pass]@aws-0-....pooler.supabase.com:6543/postgres
# Optional (legacy doc download until re-upload):
#   SUPABASE_URL=https://[ref].supabase.co
#   SUPABASE_SERVICE_KEY=...
#
# Usage:
#   ./migrate-supabase-to-prod.sh
#   ./migrate-supabase-to-prod.sh --data-only   # default after fresh schema
set -euo pipefail

DIR="$(cd "$(dirname "$0")" && pwd)"
BACKEND="${SALE_BACKEND:-/opt/sale-app/backend}"
if [[ -d "$DIR/../../migrations" && -f "$DIR/../../go.mod" ]]; then
  BACKEND="$(cd "$DIR/../.." && pwd)"
fi
DEPLOY="$BACKEND/deploy"
SUPABASE_ENV="${SUPABASE_ENV:-$DEPLOY/.env.supabase}"
DUMP="/tmp/vizzel_sales_supabase_$(date +%Y%m%d_%H%M%S).dump"
DATA_ONLY=true

for arg in "$@"; do
  [[ "$arg" == "--full" ]] && DATA_ONLY=false
done

if [[ ! -f "$SUPABASE_ENV" ]]; then
  echo "ERROR: missing $SUPABASE_ENV" >&2
  echo "Create from Supabase Dashboard → Project Settings → Database → Connection string (URI)" >&2
  exit 1
fi

# shellcheck disable=SC1090
set -a && source "$DEPLOY/.env.prod" && source "$SUPABASE_ENV" && set +a

if [[ -z "${SUPABASE_DB_URL:-}" ]]; then
  echo "ERROR: SUPABASE_DB_URL not set in $SUPABASE_ENV" >&2
  exit 1
fi

PROD_URL="postgres://${POSTGRES_USER}:${POSTGRES_PASSWORD}@sales-postgres:5432/${POSTGRES_DB}?sslmode=disable"

echo "=== 1/4 Ensure prod Postgres is up ==="
cd "$BACKEND"
docker compose --env-file deploy/.env.prod -f deploy/docker-compose.prod.yml up -d sales-postgres
for i in $(seq 1 30); do
  if docker exec sales-postgres pg_isready -U "$POSTGRES_USER" -d "$POSTGRES_DB" >/dev/null 2>&1; then
    break
  fi
  sleep 2
done

echo "=== 2/4 Fresh schema on prod (if empty) ==="
count="$(docker exec sales-postgres psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" -tAc "SELECT count(*) FROM information_schema.tables WHERE table_schema='public' AND table_name='users';" 2>/dev/null || echo 0)"
if [[ "${count// /}" == "0" ]]; then
  docker exec -i sales-postgres psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" -v ON_ERROR_STOP=1 \
    < "$BACKEND/migrations/000_initial_schema.sql"
  echo "Fresh schema applied."
else
  echo "Schema exists (users table present) — skip 000"
fi

echo "=== 3/4 pg_dump from Supabase ==="
docker run --rm -e PGCONNECT_TIMEOUT=15 postgres:17-alpine \
  pg_dump "$SUPABASE_DB_URL" --format=custom --no-owner \
  > "$DUMP"
echo "Dump: $DUMP ($(du -h "$DUMP" | cut -f1))"

echo "=== 4/4 pg_restore into prod ==="
if [[ "$DATA_ONLY" == true ]]; then
  docker run --rm -i \
    --network vizzel_sales_internal \
    -v "$DUMP:/dump/in.dump:ro" \
    postgres:16-alpine \
    pg_restore -d "$PROD_URL" --data-only --no-owner --disable-triggers /dump/in.dump \
    || echo "WARN: pg_restore exited non-zero (often OK for duplicate constraints — verify counts)"
else
  docker run --rm -i \
    --network vizzel_sales_internal \
    -v "$DUMP:/dump/in.dump:ro" \
    postgres:16-alpine \
    pg_restore -d "$PROD_URL" --no-owner --clean --if-exists /dump/in.dump
fi

echo "=== Row counts ==="
docker exec sales-postgres psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" -c \
  "SELECT 'users' t, count(*) FROM users UNION ALL SELECT 'companies', count(*) FROM companies UNION ALL SELECT 'projects', count(*) FROM projects UNION ALL SELECT 'documents', count(*) FROM documents;"

echo "=== Done ==="
echo "Legacy Supabase Storage files are NOT in pg_dump — re-upload critical docs or set SUPABASE_URL + SUPABASE_SERVICE_KEY in .env.production"
