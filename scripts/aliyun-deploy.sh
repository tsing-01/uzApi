#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
COMPOSE_FILE="$ROOT_DIR/deploy/aliyun/docker-compose.yml"
ENV_FILE="$ROOT_DIR/deploy/aliyun/.env.production"
BACKUP_DIR="$ROOT_DIR/deploy/aliyun/backups"

cd "$ROOT_DIR"

if ! command -v docker >/dev/null 2>&1; then
  echo "ERROR: docker is not installed on this ECS host." >&2
  echo "Run scripts/aliyun-bootstrap.sh first." >&2
  exit 1
fi

if ! docker compose version >/dev/null 2>&1; then
  echo "ERROR: docker compose v2 is not available." >&2
  echo "Run scripts/aliyun-bootstrap.sh first." >&2
  exit 1
fi

if [ ! -f "$ENV_FILE" ]; then
  echo "ERROR: $ENV_FILE does not exist." >&2
  echo "Copy deploy/aliyun/.env.production.example to .env.production and fill real secrets on the server." >&2
  exit 1
fi

mkdir -p "$BACKUP_DIR"

echo "==> Pulling uzApi image"
docker compose --env-file "$ENV_FILE" -f "$COMPOSE_FILE" pull --quiet uzapi

echo "==> Starting uzApi"
docker compose --env-file "$ENV_FILE" -f "$COMPOSE_FILE" up -d --no-build --remove-orphans

echo "==> Waiting for health check"
for i in $(seq 1 60); do
  if docker compose --env-file "$ENV_FILE" -f "$COMPOSE_FILE" exec -T uzapi wget -q -T 3 -O /dev/null http://localhost:8080/health; then
    echo "uzApi is healthy."
    docker compose --env-file "$ENV_FILE" -f "$COMPOSE_FILE" ps
    exit 0
  fi
  sleep 2
done

echo "ERROR: uzApi did not become healthy in time. Recent logs:" >&2
docker compose --env-file "$ENV_FILE" -f "$COMPOSE_FILE" logs --tail=200 uzapi >&2
exit 1
