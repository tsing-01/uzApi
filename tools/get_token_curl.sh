#!/usr/bin/env bash

set -u

BASE_URL="${BASE_URL:-https://api.uzapi.org}"
EMAIL="${EMAIL:-}"
PASSWORD="${PASSWORD:-}"

if [ -z "$EMAIL" ] || [ -z "$PASSWORD" ]; then
  cat <<'MSG'
用法：
  EMAIL='你的邮箱' PASSWORD='你的密码' tools/get_token_curl.sh

可选：
  BASE_URL=https://api.uzapi.org EMAIL='你的邮箱' PASSWORD='你的密码' tools/get_token_curl.sh
MSG
  exit 2
fi

response_file="$(mktemp)"
cleanup() {
  rm -f "$response_file"
}
trap cleanup EXIT

status="$(curl -sS -o "$response_file" -w '%{http_code}' \
  -X POST "$BASE_URL/api/v1/auth/login" \
  -H 'Content-Type: application/json' \
  -d "$(printf '{"email":"%s","password":"%s"}' "$EMAIL" "$PASSWORD")")"

printf 'POST %s/api/v1/auth/login\n' "$BASE_URL"
printf 'HTTP %s\n\n' "$status"

python3 - "$response_file" <<'PY'
import json
import sys

path = sys.argv[1]
raw = open(path, "rb").read()
if not raw:
    print("(empty response)")
    sys.exit(0)

try:
    payload = json.loads(raw)
except Exception:
    print(raw.decode("utf-8", errors="replace"))
    sys.exit(0)

print(json.dumps(payload, ensure_ascii=False, indent=2))

data = payload.get("data") if isinstance(payload, dict) else None
if not isinstance(data, dict):
    data = payload if isinstance(payload, dict) else {}

access_token = data.get("access_token", "")
refresh_token = data.get("refresh_token", "")

print()
print("ACCESS_TOKEN=" + access_token)
print("REFRESH_TOKEN=" + refresh_token)
PY

if [ "$status" != "200" ]; then
  cat <<'MSG'

获取 token 失败。常见原因：
- 邮箱或密码错误。
- 账号不存在或被禁用。
- 后端开启了只允许管理员登录的模式。
- 登录入口启用了额外校验。
MSG
  exit 1
fi
