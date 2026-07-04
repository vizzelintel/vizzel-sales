#!/usr/bin/env bash
# Prepare production env files (best practice: separate secrets from staging).
#
# Usage:
#   /opt/sale-app/scripts/prod-prep.sh           # create if missing
#   /opt/sale-app/scripts/prod-prep.sh --force   # regenerate secrets (destructive)
#   /opt/sale-app/scripts/prod-prep.sh --validate # compose config only
set -euo pipefail

DEPLOY="/opt/sale-app/backend/deploy"
STAGING_ENV="${DEPLOY}/.env.staging"
ENV_PROD="${DEPLOY}/.env.prod"
ENV_APP="${DEPLOY}/.env.production"
FORCE=false
VALIDATE_ONLY=false

for arg in "$@"; do
  case "$arg" in
    --force) FORCE=true ;;
    --validate) VALIDATE_ONLY=true ;;
  esac
done

rand_hex() { openssl rand -hex 32; }
rand_hex_16() { openssl rand -hex 16; }

read_staging() {
  local key="$1"
  grep -E "^${key}=" "$STAGING_ENV" 2>/dev/null | head -1 | cut -d= -f2- || true
}

if [[ "$VALIDATE_ONLY" == true ]]; then
  echo "=== Validating prod compose config ==="
  cd /opt/sale-app/backend
  docker compose --env-file deploy/.env.prod -f deploy/docker-compose.prod.yml config >/dev/null
  echo "OK  docker compose config valid"
  exit 0
fi

if [[ -f "$ENV_PROD" && "$FORCE" != true ]]; then
  echo "Exists: $ENV_PROD (use --force to regenerate)"
else
  pw="$(rand_hex)"
  cat > "$ENV_PROD" <<EOF
POSTGRES_USER=vizzel_sales
POSTGRES_PASSWORD=${pw}
POSTGRES_DB=vizzel_sales
EOF
  chmod 600 "$ENV_PROD"
  echo "Created $ENV_PROD"
fi

if [[ -f "$ENV_APP" && "$FORCE" != true ]]; then
  echo "Exists: $ENV_APP (use --force to regenerate)"
else
  if [[ ! -f "$STAGING_ENV" ]]; then
    echo "ERROR: missing $STAGING_ENV" >&2
    exit 1
  fi

  jwt="$(rand_hex)"
  otp="$(rand_hex_16)"
  lark_token="$(rand_hex_16)"

  cat > "$ENV_APP" <<EOF
JWT_SECRET=${jwt}
EMAIL_OTP_SECRET=${otp}
LINE_CHANNEL_SECRET=$(read_staging LINE_CHANNEL_SECRET)
LIFF_ID=$(read_staging LIFF_ID)
CORS_ORIGINS=https://sale.vizzeltrack.com

LARK_APP_ID=$(read_staging LARK_APP_ID)
LARK_APP_SECRET=$(read_staging LARK_APP_SECRET)
LARK_BASE_APP_TOKEN=$(read_staging LARK_BASE_APP_TOKEN)
LARK_TABLE_ID=$(read_staging LARK_TABLE_ID)
LARK_DEV_TASKS_TABLE_ID=$(read_staging LARK_DEV_TASKS_TABLE_ID)
LARK_NOTIFY_ENABLED=false
LARK_WEBHOOK_ENABLED=true
LARK_EVENT_VERIFY_TOKEN=${lark_token}

NOTIFY_ENABLED=true
NOTIFY_EXTRA_EMAILS=

FILE_STORAGE_DRIVER=local
FILE_STORAGE_PATH=/data/docs
GIN_MODE=release
PORT=8080

SMTP_HOST=$(read_staging SMTP_HOST)
SMTP_PORT=$(read_staging SMTP_PORT)
SMTP_USERNAME=$(read_staging SMTP_USERNAME)
SMTP_PASSWORD=$(read_staging SMTP_PASSWORD)
SMTP_FROM_EMAIL=$(read_staging SMTP_FROM_EMAIL)
SMTP_FROM_NAME=$(read_staging SMTP_FROM_NAME)
EOF
  chmod 600 "$ENV_APP"
  echo "Created $ENV_APP (prod CORS + new JWT/LARK verify token)"
  echo "NOTE: Set NOTIFY_EXTRA_EMAILS and LARK_NOTIFY_* before go-live."
  echo "NOTE: Lark Console prod webhook must use LARK_EVENT_VERIFY_TOKEN from $ENV_APP"
fi

echo
echo "=== Validate compose ==="
cd /opt/sale-app/backend
docker compose --env-file deploy/.env.prod -f deploy/docker-compose.prod.yml config >/dev/null
echo "OK  prod compose config valid"
echo
echo "Go-live (after load test):"
echo "  cd /opt/sale-app/backend"
echo "  docker compose --env-file deploy/.env.prod -f deploy/docker-compose.prod.yml up -d --build"
