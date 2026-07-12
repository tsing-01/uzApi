#!/usr/bin/env bash

set -u

BASE_URL="${BASE_URL:-https://api.uzapi.org}"
EMAIL="${EMAIL:-api-curl-test-$(date +%s)@test.local}"
PASSWORD="${PASSWORD:-CurlTest123456}"
VERIFY_CODE="${VERIFY_CODE:-123456}"
USERNAME="${USERNAME:-curl-test-updated-$(date +%s)}"
AVATAR_URL="${AVATAR_URL:-https://example.com/avatar.png}"

ACCESS_TOKEN=""
REFRESH_TOKEN=""
FAILURES=0
RESPONSE_FILES=()

cleanup() {
  if [ "${#RESPONSE_FILES[@]}" -gt 0 ]; then
    rm -f "${RESPONSE_FILES[@]}"
  fi
}

trap cleanup EXIT

print_header() {
  printf '\n==== %s ====\n' "$1"
}

extract_json_field() {
  local file="$1"
  local field="$2"
  python3 - "$file" "$field" <<'PY'
import json
import sys

path, field = sys.argv[1], sys.argv[2]
try:
    with open(path, "r", encoding="utf-8") as f:
        payload = json.load(f)
except Exception:
    sys.exit(0)

value = payload
for part in field.split("."):
    if isinstance(value, dict):
        value = value.get(part)
    else:
        value = None
        break

if value is not None:
    print(value)
PY
}

request_json() {
  local name="$1"
  local method="$2"
  local path="$3"
  local body="${4:-}"
  local auth="${5:-}"
  local expect="${6:-200}"
  local response_file
  local status

  response_file="$(mktemp)"
  RESPONSE_FILES+=("$response_file")

  print_header "$name"
  printf '%s %s%s\n' "$method" "$BASE_URL" "$path"

  if [ -n "$auth" ]; then
    status="$(curl -sS -o "$response_file" -w '%{http_code}' \
      -X "$method" "$BASE_URL$path" \
      -H 'Content-Type: application/json' \
      -H "Authorization: Bearer $auth" \
      -d "$body")"
  elif [ -n "$body" ]; then
    status="$(curl -sS -o "$response_file" -w '%{http_code}' \
      -X "$method" "$BASE_URL$path" \
      -H 'Content-Type: application/json' \
      -d "$body")"
  else
    status="$(curl -sS -o "$response_file" -w '%{http_code}' \
      -X "$method" "$BASE_URL$path")"
  fi

  printf 'HTTP %s\n' "$status"
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
    print(raw[:2000].decode("utf-8", errors="replace"))
    sys.exit(0)

print(json.dumps(payload, ensure_ascii=False, indent=2)[:4000])
PY

  if [ "$status" != "$expect" ]; then
    printf 'EXPECT HTTP %s, GOT HTTP %s\n' "$expect" "$status"
    FAILURES=$((FAILURES + 1))
  fi

  LAST_RESPONSE_FILE="$response_file"
}

print_header "测试参数"
printf 'BASE_URL=%s\nEMAIL=%s\nUSERNAME=%s\n' "$BASE_URL" "$EMAIL" "$USERNAME"

request_json "健康检查" "GET" "/health" "" "" "200"

REGISTER_BODY="$(printf '{"email":"%s","password":"%s","verify_code":"%s"}' "$EMAIL" "$PASSWORD" "$VERIFY_CODE")"
request_json "注册" "POST" "/api/v1/auth/register" "$REGISTER_BODY" "" "200"

if [ "$FAILURES" -gt 0 ]; then
  cat <<'MSG'

注册失败时常见原因：
- 服务未启动或 BASE_URL 不正确。
- 注册功能在后台设置中关闭。
- 邮箱验证码开启但 VERIFY_CODE 不存在或已失效。
- 后端依赖的 Postgres/Redis 不可用。
MSG
fi

LOGIN_BODY="$(printf '{"email":"%s","password":"%s"}' "$EMAIL" "$PASSWORD")"
request_json "登录获取 token" "POST" "/api/v1/auth/login" "$LOGIN_BODY" "" "200"
ACCESS_TOKEN="$(extract_json_field "$LAST_RESPONSE_FILE" "data.access_token")"
REFRESH_TOKEN="$(extract_json_field "$LAST_RESPONSE_FILE" "data.refresh_token")"

if [ -z "$ACCESS_TOKEN" ]; then
  ACCESS_TOKEN="$(extract_json_field "$LAST_RESPONSE_FILE" "access_token")"
fi
if [ -z "$REFRESH_TOKEN" ]; then
  REFRESH_TOKEN="$(extract_json_field "$LAST_RESPONSE_FILE" "refresh_token")"
fi

if [ -z "$ACCESS_TOKEN" ] || [ -z "$REFRESH_TOKEN" ]; then
  printf '\n未能从登录响应中解析 access_token / refresh_token，后续认证接口将跳过。\n'
  exit 1
fi

REFRESH_BODY="$(printf '{"refresh_token":"%s"}' "$REFRESH_TOKEN")"
request_json "刷新 token" "POST" "/api/v1/auth/refresh" "$REFRESH_BODY" "" "200"
NEW_ACCESS_TOKEN="$(extract_json_field "$LAST_RESPONSE_FILE" "data.access_token")"
if [ -n "$NEW_ACCESS_TOKEN" ]; then
  ACCESS_TOKEN="$NEW_ACCESS_TOKEN"
fi

request_json "登录校验 / 当前用户" "GET" "/api/v1/auth/me" "" "$ACCESS_TOKEN" "200"
request_json "获取用户资料" "GET" "/api/v1/user/profile" "" "$ACCESS_TOKEN" "200"

UPDATE_BODY="$(printf '{"username":"%s","avatar_url":"%s","balance_notify_enabled":true,"balance_notify_threshold":1}' "$USERNAME" "$AVATAR_URL")"
request_json "更新用户信息" "PUT" "/api/v1/user" "$UPDATE_BODY" "$ACCESS_TOKEN" "200"
request_json "更新后再次获取用户资料" "GET" "/api/v1/user/profile" "" "$ACCESS_TOKEN" "200"

print_header "测试结果"
if [ "$FAILURES" -eq 0 ]; then
  printf 'PASS: curl 链路测试通过。\n'
else
  printf 'FAIL: curl 链路测试发现 %s 个 HTTP 状态不符合预期。\n' "$FAILURES"
  exit 1
fi
