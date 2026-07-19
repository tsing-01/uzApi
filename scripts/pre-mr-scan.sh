#!/usr/bin/env bash
#
# pre-mr-scan.sh —— 提 MR / push 前在本地复现 GitHub Actions 的检查，尽早发现
# 格式、lint、类型和测试问题，避免把红叉推到 CI 上。
#
# 对齐的 workflow：
#   .github/workflows/backend-ci.yml   (test + golangci-lint)
#   .github/workflows/security-scan.yml (govulncheck + pnpm audit)
#
# 用法：
#   scripts/pre-mr-scan.sh                 # 跑全部检查
#   scripts/pre-mr-scan.sh --backend       # 只跑后端
#   scripts/pre-mr-scan.sh --frontend      # 只跑前端
#   scripts/pre-mr-scan.sh --fix           # 自动修复 gofmt / eslint 后再检查
#   scripts/pre-mr-scan.sh --skip-integration  # 跳过需要 DB/Redis 的集成测试
#   scripts/pre-mr-scan.sh --security      # 额外跑 govulncheck / pnpm audit（较慢）
#
set -uo pipefail

# ---- 与 CI 保持一致的版本号（改 CI 时同步这里）----------------------------
GOLANGCI_VERSION="v2.9.0"      # backend-ci.yml: golangci-lint-action version
REQUIRED_GO="go1.26.5"        # backend-ci.yml: Verify Go version
LINT_TIMEOUT="30m"            # backend-ci.yml: --timeout=30m
# ---------------------------------------------------------------------------

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BACKEND="$ROOT/backend"
FRONTEND="$ROOT/frontend"

# 颜色（非 TTY 时自动关闭）
if [ -t 1 ]; then
  RED=$'\033[31m'; GRN=$'\033[32m'; YLW=$'\033[33m'; CYN=$'\033[36m'; BLD=$'\033[1m'; RST=$'\033[0m'
else
  RED=""; GRN=""; YLW=""; CYN=""; BLD=""; RST=""
fi

RUN_BACKEND=1
RUN_FRONTEND=1
RUN_SECURITY=0
RUN_INTEGRATION=1
DO_FIX=0

for arg in "$@"; do
  case "$arg" in
    --backend)          RUN_FRONTEND=0 ;;
    --frontend)         RUN_BACKEND=0 ;;
    --security)         RUN_SECURITY=1 ;;
    --skip-integration) RUN_INTEGRATION=0 ;;
    --fix)              DO_FIX=1 ;;
    -h|--help)
      sed -n '2,25p' "$0" | sed 's/^# \{0,1\}//'
      exit 0 ;;
    *) echo "${RED}未知参数: $arg${RST}"; exit 2 ;;
  esac
done

FAILED_STEPS=()
step() { printf '\n%s==>%s %s%s%s\n' "$CYN" "$RST" "$BLD" "$1" "$RST"; }
ok()   { printf '%s  ✓ %s%s\n' "$GRN" "$1" "$RST"; }
bad()  { printf '%s  ✗ %s%s\n' "$RED" "$1" "$RST"; FAILED_STEPS+=("$1"); }

# 解析 golangci-lint 可执行文件：优先 PATH，其次 GOBIN/GOPATH/bin，缺失则安装。
resolve_golangci() {
  local want="${GOLANGCI_VERSION#v}"
  local candidates=()
  command -v golangci-lint >/dev/null 2>&1 && candidates+=("$(command -v golangci-lint)")
  local gobin; gobin="$(go env GOBIN 2>/dev/null)"
  [ -n "$gobin" ] && candidates+=("$gobin/golangci-lint")
  candidates+=("$(go env GOPATH 2>/dev/null)/bin/golangci-lint")

  for c in "${candidates[@]}"; do
    if [ -x "$c" ] && "$c" version 2>/dev/null | grep -q "$want"; then
      echo "$c"; return 0
    fi
  done

  printf '%s  golangci-lint %s 未安装，正在 go install...%s\n' "$YLW" "$GOLANGCI_VERSION" "$RST" >&2
  if ! go install "github.com/golangci/golangci-lint/v2/cmd/golangci-lint@${GOLANGCI_VERSION}" >&2; then
    return 1
  fi
  local installed="${gobin:-$(go env GOPATH)/bin}/golangci-lint"
  [ -x "$installed" ] && echo "$installed" && return 0
  return 1
}

# ============================ Backend ======================================
if [ "$RUN_BACKEND" -eq 1 ]; then
  step "后端：Go 版本校验（要求 ${REQUIRED_GO}）"
  if go version | grep -q "$REQUIRED_GO"; then
    ok "$(go version | awk '{print $3}')"
  else
    bad "Go 版本不是 ${REQUIRED_GO}（当前 $(go version | awk '{print $3}')），CI 会拒绝"
  fi

  if [ "$DO_FIX" -eq 1 ]; then
    step "后端：gofmt -w 自动格式化"
    ( cd "$BACKEND" && gofmt -w . ) && ok "已格式化"
  fi

  step "后端：gofmt 格式检查"
  unformatted="$(cd "$BACKEND" && gofmt -l . 2>/dev/null)"
  if [ -z "$unformatted" ]; then
    ok "格式正确"
  else
    bad "以下文件未按 gofmt 格式化（可用 --fix 修复）："
    printf '      %s\n' $unformatted
  fi

  step "后端：go vet"
  if ( cd "$BACKEND" && go vet ./... ); then ok "go vet 通过"; else bad "go vet 报错"; fi

  step "后端：golangci-lint $GOLANGCI_VERSION"
  if LINT_BIN="$(resolve_golangci)"; then
    if ( cd "$BACKEND" && "$LINT_BIN" run --timeout="$LINT_TIMEOUT" ./... ); then
      ok "golangci-lint 0 issues"
    else
      bad "golangci-lint 有问题"
    fi
  else
    bad "无法获取 golangci-lint $GOLANGCI_VERSION"
  fi

  step "后端：单元测试 (make test-unit)"
  if ( cd "$BACKEND" && make test-unit ); then ok "单元测试通过"; else bad "单元测试失败"; fi

  if [ "$RUN_INTEGRATION" -eq 1 ]; then
    step "后端：集成测试 (make test-integration，需要 Postgres + Redis)"
    if ( cd "$BACKEND" && make test-integration ); then ok "集成测试通过"; else bad "集成测试失败"; fi
  else
    printf '%s  – 跳过集成测试（--skip-integration）%s\n' "$YLW" "$RST"
  fi
fi

# ============================ Frontend =====================================
if [ "$RUN_FRONTEND" -eq 1 ]; then
  if [ "$DO_FIX" -eq 1 ]; then
    step "前端：eslint --fix"
    ( cd "$FRONTEND" && pnpm run lint ) && ok "已修复"
  fi

  step "前端：lint + typecheck + 关键 vitest (make test-frontend)"
  if make -C "$ROOT" test-frontend; then ok "前端检查通过"; else bad "前端检查失败"; fi
fi

# ============================ Security（可选）================================
if [ "$RUN_SECURITY" -eq 1 ]; then
  step "安全：govulncheck"
  if command -v govulncheck >/dev/null 2>&1 || go install golang.org/x/vuln/cmd/govulncheck@latest; then
    GOVULN="$(command -v govulncheck || echo "$(go env GOPATH)/bin/govulncheck")"
    if ( cd "$BACKEND" && "$GOVULN" ./... ); then ok "govulncheck 无漏洞"; else bad "govulncheck 发现漏洞"; fi
  else
    bad "govulncheck 安装失败"
  fi

  step "安全：pnpm audit + 例外校验（对齐 security-scan.yml）"
  audit_json="$(mktemp)"
  ( cd "$FRONTEND" && pnpm audit --prod --audit-level=high --json > "$audit_json" 2>/dev/null || true )
  if python3 "$ROOT/tools/check_pnpm_audit_exceptions.py" --audit "$audit_json" --exceptions "$ROOT/.github/audit-exceptions.yml"; then
    ok "pnpm audit 例外校验通过"
  else
    bad "pnpm audit 发现未豁免/已过期的高危依赖（见 .github/audit-exceptions.yml）"
  fi
  rm -f "$audit_json"
fi

# ============================ 汇总 =========================================
printf '\n%s────────────────────────────────────────%s\n' "$BLD" "$RST"
if [ "${#FAILED_STEPS[@]}" -eq 0 ]; then
  printf '%s%s✓ 全部检查通过，可以提 MR%s\n' "$BLD" "$GRN" "$RST"
  exit 0
else
  printf '%s%s✗ %d 项检查未通过：%s\n' "$BLD" "$RED" "${#FAILED_STEPS[@]}" "$RST"
  for s in "${FAILED_STEPS[@]}"; do printf '%s  - %s%s\n' "$RED" "$s" "$RST"; done
  exit 1
fi
