#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
COMPOSE_FILE="$ROOT_DIR/deploy/aliyun/docker-compose.yml"
ENV_FILE="$ROOT_DIR/deploy/aliyun/.env.production"
BACKUP_DIR="$ROOT_DIR/deploy/aliyun/backups"
TS="$(date +%Y%m%d-%H%M%S)"

if [ ! -f "$ENV_FILE" ]; then
  echo "ERROR: $ENV_FILE does not exist." >&2
  exit 1
fi

set -a
# shellcheck disable=SC1090
. "$ENV_FILE"
set +a

mkdir -p "$BACKUP_DIR"
OUT="$BACKUP_DIR/uzapi-${TS}.sql.gz"

echo "==> Creating database backup: $OUT"
docker compose --env-file "$ENV_FILE" -f "$COMPOSE_FILE" exec -T postgres \
  pg_dump -U "${DATABASE_USER:-uzapi}" -d "${DATABASE_DBNAME:-uzapi}" \
  | gzip -9 > "$OUT"

echo "Backup complete: $OUT"
