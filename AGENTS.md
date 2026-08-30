# Agent Guide

## Project

uzApi is an AI API gateway for distributing and managing subscription quotas. The repository contains a Go backend, a Vue 3 admin frontend, deployment assets, and Codex skills used for operational administration.

## Layout

- `backend/`: Go service, Gin handlers, Ent schema, migrations, integration tests, and server entrypoint.
- `frontend/`: Vue 3 + Vite admin UI.
- `deploy/`: Docker, systemd, local compose, and deployment templates.
- `docs/`: user-facing integration and payment documentation (git-ignored via `docs/*`).
- `skills/uzapi-admin/`: Codex admin skill and CLI for uzApi admin API operations.

## Common Commands

Backend:

```bash
cd backend
make build
make test-unit
go test ./...
```

Frontend:

```bash
cd frontend
pnpm install
pnpm lint:check
pnpm typecheck
pnpm build
pnpm test:run
```

Local deployment helpers live in `deploy/`, especially `docker-compose.local.yml` and `deploy/Makefile`.

## 提交前必须做代码检查

**每次提交前都要跑检查，不要把红叉推到 CI 上。** 两层防线：

1. **pre-commit hook（自动，快）** —— `git config core.hooksPath` 已指向 `scripts/hooks`，提交时自动对**暂存文件**跑 gofmt、golangci-lint（相关包）、eslint。首次 clone 后如果没生效，执行：

   ```bash
   git config core.hooksPath scripts/hooks
   ```

   只有在明确知道后果时才用 `git commit --no-verify` 跳过。

2. **`scripts/pre-mr-scan.sh`（手动，全量）** —— 在本地复现 GitHub Actions 的检查，push / 提 MR 前跑一次：

   ```bash
   scripts/pre-mr-scan.sh                    # 全部检查
   scripts/pre-mr-scan.sh --skip-integration # 跳过需要 DB/Redis 的集成测试
   scripts/pre-mr-scan.sh --security         # 额外跑 govulncheck / pnpm audit（较慢）
   scripts/pre-mr-scan.sh --fix              # 自动修复 gofmt / eslint 后再检查
   ```

改动落在哪一侧，至少要跑对应的那一侧：

- 后端：`gofmt -l`、`golangci-lint run`、`go build ./...`、`go test ./...`（带 `//go:build unit` 标签的用例需要 `go test -tags=unit ./...`）。
- 前端：`pnpm lint:check`、`pnpm typecheck`、`pnpm test:run`、`pnpm build`。

改了依赖或 Go 版本时，额外跑一次 `scripts/pre-mr-scan.sh --security`，对齐 `security-scan.yml`。

## 版本与安全扫描

- **Go 版本是多处 pin 的，改一处必须全改**：`backend/go.mod`、`backend/Dockerfile`、根 `Dockerfile`、`deploy/Dockerfile`、`scripts/pre-mr-scan.sh` 的 `REQUIRED_GO`、以及 `.github/workflows/` 下 `backend-ci.yml`(2 处)、`security-scan.yml`、`release.yml` 的 `go version | grep`。漏改任意一处 CI 都会红。
- **govulncheck**：标准库漏洞通常靠升级 Go 补丁版解决（把上面所有位置一起抬到 `Fixed in` 的版本）。
- **pnpm audit**：high/critical 漏洞要么升级依赖（传递依赖用 `frontend/pnpm-workspace.yaml` 的 `overrides` 顶版本），要么在 `.github/audit-exceptions.yml` 里登记例外，字段 `package/advisory/severity/reason/mitigation/expires_on/owner` 必填且 `expires_on` 不能过期，否则 `tools/check_pnpm_audit_exceptions.py` 会直接失败。优先升级，例外是兜底。

## Database Migrations

`backend/migrations/NNN_description.sql`，前向迁移、幂等（`IF NOT EXISTS`），服务启动时自动执行。**已应用的迁移文件不可修改**（有 SHA256 校验），要改就加新文件。含 `CREATE INDEX CONCURRENTLY` 的迁移必须用 `_notx.sql` 后缀。

改 `backend/ent/schema/` 后要重新生成 ORM 代码，并**同时**手写对应迁移（ent 代码生成不会产出迁移）：

```bash
cd backend && make generate   # go generate ./ent + ./cmd/server (wire)
```

依赖注入用 google/wire：新增 service / repository / handler 后要在对应 `wire.go` 的 ProviderSet 注册，再重新生成 `cmd/server/wire_gen.go`。

> 注意：`go generate ./cmd/server` 在 go.sum 缺少 wire CLI 依赖时会失败（`missing go.sum entry ... github.com/google/subcommands`）。这时装独立二进制再跑，避免为工具链改 go.mod：
>
> ```bash
> go install github.com/google/wire/cmd/wire@latest
> "$(go env GOPATH)/bin/wire" ./cmd/server
> ```

## Runtime Data

Do not commit local runtime data or secrets. In particular:

- `deploy/postgres_data_local/` is a local PostgreSQL data directory.
- `deploy/data_local/`, local `.env` files, logs, and generated configs may contain machine-specific or sensitive values.
- Use `deploy/.env.example` and `deploy/config.example.yaml` as shareable templates.

## Admin Operations

Use `skills/uzapi-admin` for admin API tasks instead of ad hoc requests:

```bash
export UZAPI_BASE_URL='https://your-uzapi-host'
export UZAPI_ADMIN_API_KEY='<admin api key>'
node ~/.codex/skills/uzapi-admin/scripts/uzapi-admin.js accounts list
```

For destructive or bulk admin changes, inspect the target IDs and names first, perform the write, then run a read command to verify the result.

## Development Notes

- Prefer existing backend response helpers and service patterns when adding handlers.
- Keep frontend changes aligned with existing Vue, Pinia, router, and component conventions.
- Avoid broad refactors while fixing focused issues.
- Keep generated files, local databases, build outputs, and secret-bearing configs out of commits.
