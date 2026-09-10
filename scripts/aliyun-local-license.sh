#!/usr/bin/env bash
# Initialize the production license signer without exposing or rotating secrets.
set +x
set -euo pipefail
umask 077

ENV_FILE="${1:?environment file is required}"
COMPOSE_FILE="${2:?compose file is required}"
LOCK_DIR="$ENV_FILE.license-lock"
TEMP_FILE=""
BACKUP_FILE=""

if [ ! -f "$ENV_FILE" ] || [ -L "$ENV_FILE" ]; then
  echo "ERROR: expected a regular production environment file." >&2
  exit 1
fi
if ! mkdir "$LOCK_DIR" 2>/dev/null; then
  echo "ERROR: production license configuration is locked; check for another deployment." >&2
  exit 1
fi
cleanup() {
  [ -z "$TEMP_FILE" ] || rm -f "$TEMP_FILE"
  rmdir "$LOCK_DIR"
}
trap cleanup EXIT

# Let Compose parse quoting/interpolation; never source or print the secret file.
RESOLVED_ENV="$(docker compose --env-file "$ENV_FILE" -f "$COMPOSE_FILE" config --environment)"
get_value() {
  printf '%s\n' "$RESOLVED_ENV" | sed -n "s/^$1=//p"
}
SIGNING_SEED="$(get_value LOCAL_MODEL_ACCESS_SIGNING_SEED)"
ISSUER="$(get_value LOCAL_MODEL_ACCESS_ISSUER)"
DOMAIN="$(get_value DOMAIN)"
NEEDS_WRITE=false

if [ -z "$ISSUER" ]; then
  if ! [[ "$DOMAIN" =~ ^[A-Za-z0-9]([A-Za-z0-9.-]*[A-Za-z0-9])?(:[0-9]+)?$ ]]; then
    echo "ERROR: set LOCAL_MODEL_ACCESS_ISSUER to the canonical HTTPS API URL (DOMAIN cannot be used)." >&2
    exit 1
  fi
  ISSUER="https://$DOMAIN"
  NEEDS_WRITE=true
fi
if ! [[ "$ISSUER" =~ ^https://[A-Za-z0-9]([A-Za-z0-9.-]*[A-Za-z0-9])?(:[0-9]+)?(/[^?#[:space:]@]*)?$ ]]; then
  echo "ERROR: LOCAL_MODEL_ACCESS_ISSUER must be an HTTPS URL without credentials, query or fragment." >&2
  exit 1
fi

if [ -z "$SIGNING_SEED" ]; then
  SIGNING_SEED="$(openssl rand -base64 32)"
  NEEDS_WRITE=true
fi
if ! [[ "$SIGNING_SEED" =~ ^[A-Za-z0-9+/]{43}=$ ]] ||
  [ "$(printf '%s' "$SIGNING_SEED" | openssl base64 -d -A | wc -c | tr -d ' ')" != 32 ]; then
  echo "ERROR: existing LOCAL_MODEL_ACCESS_SIGNING_SEED is invalid; refusing to replace it." >&2
  exit 1
fi

if [ "$NEEDS_WRITE" = true ]; then
  BACKUP_FILE="$(mktemp "$ENV_FILE.before-license.XXXXXX")"
  cat "$ENV_FILE" > "$BACKUP_FILE"
  TEMP_FILE="$(mktemp "$ENV_FILE.license.XXXXXX")"
  cat "$BACKUP_FILE" > "$TEMP_FILE"
  # Last definitions win in dotenv. Preserve all original settings and comments.
  printf '\n# Persistent local-model license signer, initialized by deployment.\nLOCAL_MODEL_ACCESS_SIGNING_SEED=%s\nLOCAL_MODEL_ACCESS_ISSUER=%s\n' \
    "$SIGNING_SEED" "$ISSUER" >> "$TEMP_FILE"
  if ! cmp -s "$ENV_FILE" "$BACKUP_FILE"; then
    echo "ERROR: production environment changed during initialization; retry deployment." >&2
    exit 1
  fi
  mv "$TEMP_FILE" "$ENV_FILE"
  TEMP_FILE=""
  echo "Local-model signer initialized; private environment backup retained on the server."
else
  echo "Local-model signer already configured; keeping the existing key."
fi
chmod 600 "$ENV_FILE"
