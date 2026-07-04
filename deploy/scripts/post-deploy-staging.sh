#!/usr/bin/env bash
# Run after staging API deploy: subscribe Lark CRM + smoke.
set -euo pipefail
DIR="$(cd "$(dirname "$0")" && pwd)"
"$DIR/lark-subscribe-crm.sh"
"$DIR/staging-smoke.sh"
