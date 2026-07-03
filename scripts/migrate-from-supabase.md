# Migrate data from Supabase to self-hosted PostgreSQL

## 1. Export from Supabase

```bash
pg_dump "$SUPABASE_DB_URL" \
  --format=custom \
  --no-owner \
  --file=vizzel_sales_supabase.dump
```

## 2. Prepare target database

```bash
MIGRATE_MODE=fresh DATABASE_URL="postgres://..." bash scripts/migrate.sh
```

## 3. Restore (schema may overlap — prefer data-only if 000 already applied)

```bash
# Option A: full restore into empty DB created from Supabase dump
pg_restore -d "$DATABASE_URL" --no-owner --role=vizzel_sales vizzel_sales_supabase.dump

# Option B: data-only after 000 schema
pg_restore -d "$DATABASE_URL" --data-only --no-owner vizzel_sales_supabase.dump
```

## 4. Verify

- Row counts: `users`, `projects`, `documents`, `companies`
- Sample login + document download
- Re-upload test file → stored under `FILE_STORAGE_PATH` (Supabase URLs remain legacy until re-upload)

## 5. File storage

Supabase Storage files are **not** in pg_dump. Export separately or re-upload critical documents after cutover.
