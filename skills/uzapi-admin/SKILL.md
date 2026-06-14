---
name: uzapi-admin
description: Manage uzApi admin APIs for accounts, groups, proxies, error passthrough rules, TLS fingerprint profiles, imports, exports, batch updates, and raw administrator API calls. Use when the user mentions uzApi, admin API keys, account management, bulk account import/export, keeping or deleting accounts, refreshing accounts, clearing errors, CRS sync, or managing uzApi backend settings through the admin API.
---

# uzApi Admin

Use the bundled CLI instead of ad hoc `curl`.

```bash
export UZAPI_BASE_URL='https://your-uzapi-host'
export UZAPI_ADMIN_API_KEY='<admin api key>'

node ~/.codex/skills/uzapi-admin/scripts/uzapi-admin.js accounts list
```

For all commands and payload examples, read [references/admin-cli.md](references/admin-cli.md).

## Workflow

1. Reuse `UZAPI_BASE_URL` and `UZAPI_ADMIN_API_KEY` from the environment.
2. Run read-only commands first: `accounts list`, `accounts get <id>`, `groups all`, or `proxies all`.
3. Before destructive or bulk writes, print the target account names and IDs.
4. Execute the write command only after the target set is clear.
5. Run a follow-up read command to verify the result.

## Common Commands

```bash
node ~/.codex/skills/uzapi-admin/scripts/uzapi-admin.js accounts list --page-size 20
node ~/.codex/skills/uzapi-admin/scripts/uzapi-admin.js accounts get 40
node ~/.codex/skills/uzapi-admin/scripts/uzapi-admin.js accounts usage 40
node ~/.codex/skills/uzapi-admin/scripts/uzapi-admin.js accounts set-schedulable 40 true
node ~/.codex/skills/uzapi-admin/scripts/uzapi-admin.js accounts bulk-update --ids 40,39 --json '{"concurrency":10}'
node ~/.codex/skills/uzapi-admin/scripts/uzapi-admin.js error-rules list
node ~/.codex/skills/uzapi-admin/scripts/uzapi-admin.js tls-profiles list
```

## Safety Notes

- Authentication uses only `x-api-key`.
- If the API returns `INVALID_ADMIN_KEY`, ask the user to regenerate the admin API key.
- `accounts export` includes credentials and tokens. Prefer `--file` and avoid printing exports in chat.
- For uncertain or newly added backend APIs, use `api <METHOD> <admin-path>` after a read-only check.
