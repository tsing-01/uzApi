# uzApi Admin Reference

## Environment

```bash
export UZAPI_BASE_URL='https://your-uzapi-host'
export UZAPI_ADMIN_API_KEY='<admin api key>'
```

后台鉴权只使用 `x-api-key`。如果返回 `INVALID_ADMIN_KEY`，重新生成管理员 API Key。

## CLI

```bash
node ~/.codex/skills/uzapi-admin/scripts/uzapi-admin.js <command>
```

## Accounts

### 只读

```bash
node ~/.codex/skills/uzapi-admin/scripts/uzapi-admin.js accounts list --page-size 20
node ~/.codex/skills/uzapi-admin/scripts/uzapi-admin.js accounts list --search outlook --platform openai --type oauth --status active
node ~/.codex/skills/uzapi-admin/scripts/uzapi-admin.js accounts get 40
node ~/.codex/skills/uzapi-admin/scripts/uzapi-admin.js accounts usage 40
node ~/.codex/skills/uzapi-admin/scripts/uzapi-admin.js accounts stats 40 --days 30
node ~/.codex/skills/uzapi-admin/scripts/uzapi-admin.js accounts today-stats 40
node ~/.codex/skills/uzapi-admin/scripts/uzapi-admin.js accounts batch-today-stats --ids 40,39
node ~/.codex/skills/uzapi-admin/scripts/uzapi-admin.js accounts models 40
node ~/.codex/skills/uzapi-admin/scripts/uzapi-admin.js accounts temp-unschedulable 40
node ~/.codex/skills/uzapi-admin/scripts/uzapi-admin.js accounts antigravity-default-model-mapping
```

`accounts export` 会包含账号凭据和 token，建议写入文件，不要直接刷屏：

```bash
node ~/.codex/skills/uzapi-admin/scripts/uzapi-admin.js accounts export --ids 40,39 --file accounts-export.json
node ~/.codex/skills/uzapi-admin/scripts/uzapi-admin.js accounts export --platform openai --type oauth --include-proxies false --file accounts-export.json
```

### 单账号写入

```bash
node ~/.codex/skills/uzapi-admin/scripts/uzapi-admin.js accounts create --file account.json
node ~/.codex/skills/uzapi-admin/scripts/uzapi-admin.js accounts update 40 --json '{"concurrency":20}'
node ~/.codex/skills/uzapi-admin/scripts/uzapi-admin.js accounts set-status 40 active
node ~/.codex/skills/uzapi-admin/scripts/uzapi-admin.js accounts set-schedulable 40 true
node ~/.codex/skills/uzapi-admin/scripts/uzapi-admin.js accounts clear-error 40
node ~/.codex/skills/uzapi-admin/scripts/uzapi-admin.js accounts clear-rate-limit 40
node ~/.codex/skills/uzapi-admin/scripts/uzapi-admin.js accounts recover-state 40
node ~/.codex/skills/uzapi-admin/scripts/uzapi-admin.js accounts reset-quota 40
node ~/.codex/skills/uzapi-admin/scripts/uzapi-admin.js accounts refresh 40
node ~/.codex/skills/uzapi-admin/scripts/uzapi-admin.js accounts test 40
node ~/.codex/skills/uzapi-admin/scripts/uzapi-admin.js accounts sync-models 40
node ~/.codex/skills/uzapi-admin/scripts/uzapi-admin.js accounts apply-oauth 40 --file credentials.json
node ~/.codex/skills/uzapi-admin/scripts/uzapi-admin.js accounts reset-temp-unschedulable 40
```

### 删除与清理

删除前先列出目标账号名和 ID。

```bash
node ~/.codex/skills/uzapi-admin/scripts/uzapi-admin.js accounts delete 25
node ~/.codex/skills/uzapi-admin/scripts/uzapi-admin.js accounts keep-only --name 'target@example.com'
```

### 批量写入

```bash
node ~/.codex/skills/uzapi-admin/scripts/uzapi-admin.js accounts batch-create --file accounts.json
node ~/.codex/skills/uzapi-admin/scripts/uzapi-admin.js accounts batch-update-credentials --file payload.json
node ~/.codex/skills/uzapi-admin/scripts/uzapi-admin.js accounts bulk-update --ids 40,39 --json '{"concurrency":10,"priority":2}'
node ~/.codex/skills/uzapi-admin/scripts/uzapi-admin.js accounts batch-refresh --ids 40,39
node ~/.codex/skills/uzapi-admin/scripts/uzapi-admin.js accounts batch-clear-error --ids 40,39
```

`bulk-update` 可覆盖页面“批量更新”的字段，payload 由后台表单字段决定，例如 `base_url`、`model_mapping`、`group_ids`、`proxy_id`、`concurrency`、`priority`、`rate_multiplier`、`status`、`compact_mode` 等。更新前先用 `accounts get <id>` 确认字段名。

### 导入

通用后台导入：

```bash
node ~/.codex/skills/uzapi-admin/scripts/uzapi-admin.js accounts import-data --file accounts-export.json
node ~/.codex/skills/uzapi-admin/scripts/uzapi-admin.js accounts import-codex-session --file payload.json
```

CRS 同步：

```bash
node ~/.codex/skills/uzapi-admin/scripts/uzapi-admin.js accounts crs-preview --file payload.json
node ~/.codex/skills/uzapi-admin/scripts/uzapi-admin.js accounts crs-sync --file payload.json
```

旧版 JSON 导入仍可用，会把模板账号的配置复制给导入账号：

```bash
node ~/.codex/skills/uzapi-admin/scripts/uzapi-admin.js accounts import-json \
  --file /path/accounts.json \
  --template-name 'template@example.com' \
  --dry-run
```

复制字段：

- `concurrency`
- `priority`
- `group_ids`
- `credentials.model_mapping`

## Groups And Proxies

```bash
node ~/.codex/skills/uzapi-admin/scripts/uzapi-admin.js groups all
node ~/.codex/skills/uzapi-admin/scripts/uzapi-admin.js proxies all
```

## Error Rules And TLS Profiles

对应账号页顶部“错误透传规则”和“TLS 指纹模板”。

```bash
node ~/.codex/skills/uzapi-admin/scripts/uzapi-admin.js error-rules list
node ~/.codex/skills/uzapi-admin/scripts/uzapi-admin.js error-rules get 1
node ~/.codex/skills/uzapi-admin/scripts/uzapi-admin.js error-rules create --file rule.json
node ~/.codex/skills/uzapi-admin/scripts/uzapi-admin.js error-rules update 1 --json '{"enabled":true}'
node ~/.codex/skills/uzapi-admin/scripts/uzapi-admin.js error-rules toggle 1 false
node ~/.codex/skills/uzapi-admin/scripts/uzapi-admin.js error-rules delete 1

node ~/.codex/skills/uzapi-admin/scripts/uzapi-admin.js tls-profiles list
node ~/.codex/skills/uzapi-admin/scripts/uzapi-admin.js tls-profiles get 1
node ~/.codex/skills/uzapi-admin/scripts/uzapi-admin.js tls-profiles create --file profile.json
node ~/.codex/skills/uzapi-admin/scripts/uzapi-admin.js tls-profiles update 1 --file profile.json
node ~/.codex/skills/uzapi-admin/scripts/uzapi-admin.js tls-profiles delete 1
```

## Raw Admin API

未封装或新版本后台接口可用 `api` 直通。路径可写 `/admin/...` 或 `/api/v1/admin/...`。

```bash
node ~/.codex/skills/uzapi-admin/scripts/uzapi-admin.js api GET /admin/groups/all
node ~/.codex/skills/uzapi-admin/scripts/uzapi-admin.js api POST /admin/accounts/bulk-update \
  --json '{"account_ids":[40],"concurrency":10}'
```

## Confirmed Admin Endpoints

- `GET /api/v1/admin/accounts`
- `GET /api/v1/admin/accounts/:id`
- `POST /api/v1/admin/accounts`
- `PUT /api/v1/admin/accounts/:id`
- `DELETE /api/v1/admin/accounts/:id`
- `POST /api/v1/admin/accounts/check-mixed-channel`
- `GET /api/v1/admin/accounts/:id/usage`
- `GET /api/v1/admin/accounts/:id/stats`
- `GET /api/v1/admin/accounts/:id/today-stats`
- `POST /api/v1/admin/accounts/today-stats/batch`
- `POST /api/v1/admin/accounts/:id/schedulable`
- `POST /api/v1/admin/accounts/:id/test`
- `POST /api/v1/admin/accounts/:id/refresh`
- `POST /api/v1/admin/accounts/:id/apply-oauth-credentials`
- `POST /api/v1/admin/accounts/:id/clear-error`
- `POST /api/v1/admin/accounts/:id/clear-rate-limit`
- `POST /api/v1/admin/accounts/:id/recover-state`
- `POST /api/v1/admin/accounts/:id/reset-quota`
- `GET /api/v1/admin/accounts/:id/temp-unschedulable`
- `DELETE /api/v1/admin/accounts/:id/temp-unschedulable`
- `GET /api/v1/admin/accounts/:id/models`
- `POST /api/v1/admin/accounts/:id/models/sync-upstream`
- `POST /api/v1/admin/accounts/batch`
- `POST /api/v1/admin/accounts/batch-update-credentials`
- `POST /api/v1/admin/accounts/bulk-update`
- `POST /api/v1/admin/accounts/batch-refresh`
- `POST /api/v1/admin/accounts/batch-clear-error`
- `GET /api/v1/admin/accounts/data`
- `POST /api/v1/admin/accounts/data`
- `POST /api/v1/admin/accounts/import/codex-session`
- `POST /api/v1/admin/accounts/sync/crs/preview`
- `POST /api/v1/admin/accounts/sync/crs`
- `GET /api/v1/admin/accounts/antigravity/default-model-mapping`
- `GET /api/v1/admin/groups/all`
- `GET /api/v1/admin/proxies/all`
- `GET /api/v1/admin/error-passthrough-rules`
- `GET /api/v1/admin/error-passthrough-rules/:id`
- `POST /api/v1/admin/error-passthrough-rules`
- `PUT /api/v1/admin/error-passthrough-rules/:id`
- `DELETE /api/v1/admin/error-passthrough-rules/:id`
- `GET /api/v1/admin/tls-fingerprint-profiles`
- `GET /api/v1/admin/tls-fingerprint-profiles/:id`
- `POST /api/v1/admin/tls-fingerprint-profiles`
- `PUT /api/v1/admin/tls-fingerprint-profiles/:id`
- `DELETE /api/v1/admin/tls-fingerprint-profiles/:id`

## Notes

- 线上写入前先只读核对目标集合。
- 导出结果包含敏感凭据，优先使用 `--file`。
- `PUT /admin/accounts/:id` 和 `bulk-update` 接受宽松请求体，字段名不确定时先用 `accounts get` 或后台页面确认。

## 本地模型接入付费兑换码

在管理员兑换码页面选择“本地模型接入”，或通过 CLI 生成：

```bash
node skills/uzapi-admin/scripts/uzapi-admin.js api GET '/redeem-codes?type=local_model_access&page_size=1'
node skills/uzapi-admin/scripts/uzapi-admin.js api POST /redeem-codes/generate --json '{"count":1,"type":"local_model_access","value":0,"expires_in_days":7}'
node skills/uzapi-admin/scripts/uzapi-admin.js api GET '/redeem-codes?type=local_model_access&page_size=1'
```

每个码只能兑换一次，为当前用户永久开通本地模型接入；不改变余额、并发数或订阅。
`value` 必须为 0（可省略），不可设置 `group_id` 或非零 `validity_days`。
`expires_at` / `expires_in_days` 仅控制兑换码使用期限，不是解锁权益的到期时间。
已解锁账户兑换新码返回 HTTP 409 / `LOCAL_MODEL_ACCESS_ALREADY_UNLOCKED`，新码仍未使用。
本类型由 `/redeem-codes/generate` 生成，不支持旧的固定码 `/create-and-redeem` 接口。

### 外部客户端集成（24Hbutler 后续接入）

以下为用户 API，使用登录取得的 `Authorization: Bearer <access_token>`，不接受管理员 API Key 或推理 API Key：

- `POST /api/v1/redeem`，请求 `{"code":"<兑换码>"}`。成功返回标准响应中的兑换记录，`data.type` 为 `local_model_access`，`data.status` 为 `used`。
- `GET /api/v1/integration/entitlements`，只读取 JWT 对应账户，不接受目标用户 ID；返回 `Cache-Control: no-store`。

未解锁的查询结果：

```json
{"code":0,"message":"success","data":{"user_id":123,"local_model_access_enabled":false,"local_model_access_unlocked_at":null}}
```

兑换成功后 `local_model_access_enabled` 为 `true`，`local_model_access_unlocked_at` 为 RFC3339 解锁时间。
`GET /api/v1/auth/me`、`GET /api/v1/user/profile` 和 `GET /api/v1/integration/me` 中的用户资料也包含这两个字段。
已有登录 token 无需重新登录即可查到解锁状态；禁用、删除账户和失效 token 无法查询。

客户端应在登录或切换账户、兑换成功及使用受限能力前重新向服务端检查权益。
401 应重新认证；其他查询失败应保留“待验证/不可用”状态，不能视为已解锁。
不要把客户端存储的布尔值或用户可编辑的个人资料作为授权依据。
本次 uzApi 提供权益记录与查询；24Hbutler 的具体入口和执行阶段鉴权需在后续接入中实现。
