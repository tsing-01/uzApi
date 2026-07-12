#!/usr/bin/env bash

set -u

BASE_URL="${BASE_URL:-https://api.uzapi.org}"
ADMIN_EMAIL="${ADMIN_EMAIL:-admin@uzapi.org}"
ADMIN_PASSWORD="${ADMIN_PASSWORD:-admin123}"
ADMIN_TOKEN="${ADMIN_TOKEN:-}"
ADMIN_API_KEY="${ADMIN_API_KEY:-}"

ACCESS_TOKEN="$ADMIN_TOKEN"
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

print_response() {
  local response_file="$1"
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

print(json.dumps(payload, ensure_ascii=False, indent=2)[:5000])
PY
}

request_json() {
  local name="$1"
  local method="$2"
  local path="$3"
  local body="${4:-}"
  local expect="${5:-200}"
  local response_file
  local status

  response_file="$(mktemp)"
  RESPONSE_FILES+=("$response_file")

  print_header "$name"
  printf '%s %s%s\n' "$method" "$BASE_URL" "$path"

  if [ -n "$ADMIN_API_KEY" ]; then
    if [ -n "$body" ]; then
      status="$(curl -sS -o "$response_file" -w '%{http_code}' \
        -X "$method" "$BASE_URL$path" \
        -H 'Content-Type: application/json' \
        -H "x-api-key: $ADMIN_API_KEY" \
        -d "$body")"
    else
      status="$(curl -sS -o "$response_file" -w '%{http_code}' \
        -X "$method" "$BASE_URL$path" \
        -H "x-api-key: $ADMIN_API_KEY")"
    fi
  elif [ -n "$ACCESS_TOKEN" ]; then
    if [ -n "$body" ]; then
      status="$(curl -sS -o "$response_file" -w '%{http_code}' \
        -X "$method" "$BASE_URL$path" \
        -H 'Content-Type: application/json' \
        -H "Authorization: Bearer $ACCESS_TOKEN" \
        -d "$body")"
    else
      status="$(curl -sS -o "$response_file" -w '%{http_code}' \
        -X "$method" "$BASE_URL$path" \
        -H "Authorization: Bearer $ACCESS_TOKEN")"
    fi
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
  print_response "$response_file"

  if [ "$status" != "$expect" ]; then
    printf 'EXPECT HTTP %s, GOT HTTP %s\n' "$expect" "$status"
    FAILURES=$((FAILURES + 1))
  fi

  LAST_RESPONSE_FILE="$response_file"
}

print_header "测试参数"
printf 'BASE_URL=%s\n' "$BASE_URL"
if [ -n "$ADMIN_API_KEY" ]; then
  printf 'AUTH=x-api-key\n'
elif [ -n "$ACCESS_TOKEN" ]; then
  printf 'AUTH=provided ADMIN_TOKEN\n'
else
  printf 'AUTH=login ADMIN_EMAIL=%s\n' "$ADMIN_EMAIL"
fi

request_json "健康检查" "GET" "/health" "" "200"

if [ -z "$ADMIN_API_KEY" ] && [ -z "$ACCESS_TOKEN" ]; then
  LOGIN_BODY="$(printf '{"email":"%s","password":"%s"}' "$ADMIN_EMAIL" "$ADMIN_PASSWORD")"
  request_json "管理员登录获取 token" "POST" "/api/v1/auth/login" "$LOGIN_BODY" "200"

  ACCESS_TOKEN="$(extract_json_field "$LAST_RESPONSE_FILE" "data.access_token")"
  if [ -z "$ACCESS_TOKEN" ]; then
    ACCESS_TOKEN="$(extract_json_field "$LAST_RESPONSE_FILE" "access_token")"
  fi

  if [ -z "$ACCESS_TOKEN" ]; then
    printf '\n未能从登录响应解析 access_token。请确认 ADMIN_EMAIL/ADMIN_PASSWORD 是管理员账号。\n'
    exit 1
  fi
fi

request_json "管理员身份校验 /auth/me" "GET" "/api/v1/auth/me" "" "200"
request_json "Admin 仪表盘统计" "GET" "/api/v1/admin/dashboard/stats" "" "200"
request_json "Admin 用户列表" "GET" "/api/v1/admin/users?page=1&page_size=10" "" "200"
request_json "Admin 分组列表" "GET" "/api/v1/admin/groups?page=1&page_size=10" "" "200"
request_json "Admin 系统设置" "GET" "/api/v1/admin/settings" "" "200"
request_json "Admin 系统版本" "GET" "/api/v1/admin/system/version" "" "200"

print_header "测试结果"
if [ "$FAILURES" -eq 0 ]; then
  printf 'PASS: admin curl smoke test 通过。\n'
else
  printf 'FAIL: admin curl smoke test 发现 %s 个 HTTP 状态不符合预期。\n' "$FAILURES"
  exit 1
fi
