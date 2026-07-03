#!/usr/bin/env bash
# Apply SQL migrations in order. Safe to re-run (uses IF NOT EXISTS where possible).
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
MIGRATIONS_DIR="${MIGRATIONS_DIR:-$ROOT/migrations}"

DATABASE_URL="${DATABASE_URL:-${SUPABASE_DB_URL:-}}"
if [[ -z "$DATABASE_URL" ]]; then
  echo "ERROR: set DATABASE_URL (or legacy SUPABASE_DB_URL)" >&2
  exit 1
fi

shopt -s nullglob
files=("$MIGRATIONS_DIR"/[0-9][0-9][0-9]_*.sql)
if [[ ${#files[@]} -eq 0 ]]; then
  echo "No migration files in $MIGRATIONS_DIR" >&2
  exit 1
fi

IFS=$'\n' sorted=($(sort <<<"${files[*]}"))
unset IFS

echo "Database: ${DATABASE_URL%%@*}@***"

if [[ "${MIGRATE_MODE:-all}" == "fresh" ]]; then
  echo "==> FRESH install: 000_initial_schema.sql only"
  psql "$DATABASE_URL" -v ON_ERROR_STOP=1 -f "$MIGRATIONS_DIR/000_initial_schema.sql"
  echo "Fresh schema applied."
  exit 0
fi

for f in "${sorted[@]}"; do
  echo "==> $(basename "$f")"
  psql "$DATABASE_URL" -v ON_ERROR_STOP=1 -f "$f"
done

echo "Migrations complete."
