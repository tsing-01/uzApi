# Agent Guide

## Project

uzApi is an AI API gateway for distributing and managing subscription quotas. The repository contains a Go backend, a Vue 3 admin frontend, deployment assets, and Codex skills used for operational administration.

## Layout

- `backend/`: Go service, Gin handlers, Ent schema, migrations, integration tests, and server entrypoint.
- `frontend/`: Vue 3 + Vite admin UI.
- `deploy/`: Docker, systemd, local compose, and deployment templates.
- `docs/`: user-facing integration and payment documentation.
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
