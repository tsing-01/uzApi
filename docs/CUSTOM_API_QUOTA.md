# 自定义 API 在线授权与每日计次 v1

本次只实现 uzApi 服务端，未修改 24Hbutler。按需求方确认，不安排 App 联调或交付测试环境凭据。接口定义见 [OpenAPI](CUSTOM_API_QUOTA.openapi.yaml)。

## 规则

状态正常的已登录账户直接获得每天 5 次试用，无需套餐。一次新批准对应一次准备发出的自定义上游 HTTP 请求；工具循环、轮询、上游失败后的重试都分别申请新 ID。账号的所有设备和会话共享账本。余额、并发数、账户模型、订阅、模型目录和配置保存不经过此接口。

每日按 `Asia/Shanghai` 00:00 切换；`quota.date` 为北京时间日期，其他时间字段为 UTC RFC 3339。`used` 是当天已批准总数，包含付费解锁期间的请求。解锁时 `remaining=null`；撤销后剩余为 `max(0,5-used)`。兑换、授予、撤销、换套餐和 Redis 清空均不重置账本。批准后失败、取消、崩溃不退次，也不提供重置或退款接口。

沿用 `users.local_model_access_unlocked_at` / `local_model_access_revoked_at` 判断权限；对外 `entitlement_version` 就是已有的 `local_model_access_version`，不新增并行权限标志。实际授予和撤销递增版本；重复已生效的管理操作不重复授予或消费。

## 接口

使用现有登录 Bearer access token 和 CSRF 规则。推理 API Key 不能代替登录，用户 ID 仅来自 JWT。所有授权相关响应设置 `Cache-Control: no-store` 和 HTTP `Date`，包括认证和 CSRF 失败。现有登录失效错误及 CSRF 错误仍按原流程处理。

兼容说明：既有登录/CSRF 中间件使用字符串 `code`（例如 `{"code":"UNAUTHORIZED","message":"Authorization header is required"}`）。新增额度业务错误、`ACCOUNT_DISABLED` 及 `CUSTOM_API_QUOTA_UNAVAILABLE` 使用数字 `code` 和独立 `reason`，包括 JWT 验证之后的账户数据库核验失败。

| 方法与路径 | 用途 |
| --- | --- |
| `GET /api/v1/local-model-access` | 当前权限、版本和当天账本；不扣调用次数 |
| `POST /api/v1/local-model-access/requests` | 持久化一次批准或日额度拒绝决定 |
| `POST /api/v1/redeem` | 沿用专用 `local_model_access` 码兑换 |
| `GET /api/v1/auth/me` | 保留布尔权限，同时返回 `entitlement_version` |
| `POST /api/v1/admin/users/:id/local-model-access/grant` | 管理员授予；`{"reason":"已核验购买"}` |
| `POST /api/v1/admin/users/:id/local-model-access/revoke` | 管理员撤销；`{"reason":"退款"}` |

查询状态成功示例：

```json
{"code":0,"message":"success","data":{"contract_version":1,"local_model_access_enabled":false,"entitlement_version":1,"server_time":"2026-09-12T04:00:00Z","quota":{"date":"2026-09-12","timezone":"Asia/Shanghai","daily_limit":5,"used":2,"remaining":3,"resets_at":"2026-09-12T16:00:00Z"}}}
```

授权请求只接受一个字段，JSON 请求体最多 1 KiB，不接受查询参数、重复字段、尾随 JSON、控制字段或服务商数据。UUID 必须为标准 36 字符 UUID v4（大小写归一为小写）：

```json
{"request_id":"5941742f-37dc-4666-9f97-9e3921274b42"}
```

HTTP 200 成功数据包含上面的状态字段以及 `request_id`、`allowed:true`、`replayed`、`authorized_at`、`expires_at`。批准有效期为 `min(authorized_at+30s, resets_at)`，用于限定开始上游请求的时间，不要求流式响应在 30 秒内完成。只有数据库事务提交成功后才返回批准。

**重试响应中的权限、版本、日期、used 和批准时间是原决定的快照**；`server_time` 为本次校验时刻。重试不会把旧快照改成当前状态或延长有效期。需要当前账本时调用 GET。日额度拒绝也返回原 ID 的拒绝及快照；跨日或兑换后重新发起操作必须使用新 ID。

| HTTP | reason | 含义 |
| --- | --- | --- |
| 400 | `CUSTOM_API_INVALID_REQUEST` | JSON / UUID / 控制字段无效，不扣次 |
| 401 | 原有认证 reason | 沿用登录续期，恢复同次授权保留 ID |
| 403 | `ACCOUNT_DISABLED` | 账户不可用，包括重放检查 |
| 403 | `CUSTOM_API_AUTHORIZATION_REVOKED` | 原批准版本失效，`allowed:false` |
| 410 | `CUSTOM_API_AUTHORIZATION_EXPIRED` | 原批准到期，`allowed:false`，不重复扣次 |
| 429 | `CUSTOM_API_DAILY_LIMIT_EXCEEDED` | 已记录日额度拒绝，包含原 ID、`allowed:false` 和 quota |
| 429 | `RATE_LIMITED` | 基础防滥用，含 `Retry-After` 秒数，不保存批准决定 |
| 503 | `CUSTOM_API_QUOTA_UNAVAILABLE` | 数据库、权限核验或提交结果不确定；无批准和推测余量 |

基础防滥用固定为每账户每自然分钟 120 次，状态查询、申请和幂等重试共享；使用数据库时间与持久化计数，付费账户也适用，和每日 5 次是两个不同限制。不会影响账户模型的原有调用链。超过限额不继续写入幂等记录；下一分钟可用原 ID 重试。

## 兑换与管理

现有管理后台继续生成 `local_model_access` 类型的 128 位随机兑换码，沿用有效期、作废、查询和反猜码流程。兑换成功附加 `local_model_access_enabled:true` 和 `entitlement_version`；客户端随后刷新 GET 状态，无需重新登录。

同一码只能被一个账户消费。已解锁账户兑换另一张未使用码返回 `LOCAL_MODEL_ACCESS_ALREADY_UNLOCKED`，事务回滚，新码保留；同一已使用码重试返回现有 `REDEEM_CODE_USED`，不会误报“未消费”。所有其他类型码保留原行为，专用码不改变余额、并发、套餐或当天 `used`。

新增管理员 grant API 复用现有管理认证，revoke API 保留。两者要求 1–500 字节非空原因。`local_model_license_events` 记录操作者、目标账户、原因、前后权限及版本、时间；兑换记录使用 `entitlement_redeemed` 事件关联 `redeem_code_id`，不记录完整码。管理来源由 `entitlement_granted` / `entitlement_revoked` 及 actor 表达。审计写失败则权限和兑换一同回滚。

## 一致性、故障与保留

所有副本连接同一 PostgreSQL 主库。以 `users` 行的 `FOR UPDATE` 锁排列授权、兑换、管理和账户禁用；取得锁后才读取 `clock_timestamp()`，避免排队跨午夜仍扣前一天。每日行主键为 `(user_id,quota_date)`；决定主键为 `(user_id,request_id)`，不包含日期。计数、决定和防滥用状态在同一事务写入。

幂等记录保留至少 7 天；新 ID 写入时按当前账户清理超过 7 天的记录，每次最多 500 条。旧 ID 保留时不会重新计次，成功记录每次仍检查账户、权限版本和到期时间。清理后未命中记录会按新请求重新判断并扣次，不会免费放行。账本按日自然换行，不靠定时归零。

数据库错误和提交确认丢失统一返回 503；客户端只能用原 ID 重试。实际已提交的返回原决定，未提交的允许完整执行一次。请求的 5 秒服务端上下文覆盖 JWT 的数据库核验和事务锁等待；错误不包含 SQL、堆栈或凭据。

事务配置应保留 PostgreSQL 持久提交保证（`fsync=on`、`synchronous_commit=on`）。只从可写主库批准；落后副本不能通过 `FOR UPDATE`。主从切换或灾难恢复若可能丢失已确认流水，先暂停授权，核对记录再开放；不把旧备份的余量直接当成当前账本。

`custom_api_authorization` 日志仅含账户 ID、结果 reason、是否重放和处理耗时，可统计批准、日额度拒绝、限流、撤销、到期、存储故障和重放比例；HTTP 请求日志覆盖认证失败。日志不记录授权体、登录令牌、Cookie、CSRF、兑换码或上游内容。幂等表保留各次批准与日额度拒绝，管理操作沿用既有审计表。

## 迁移、上线与回退

启动自动执行增量迁移 `152_custom_api_daily_quota.sql`：新增每日账本、幂等决定及审计扩展列。150/151 的已应用文件不变，无历史权限回填或覆盖。已有解锁账户保留权限，服务端新账本从接入日起计算，不接收无法验证的客户端历史计数。

先部署后端，再由 App 团队接入。旧 App 不调用新接口，因此单独上线后端不会改变旧 App 的本地计次行为。原有 5 分钟设备签名授权继续工作，但不能代替每次自定义 API 的在线计次。

回退二进制时保留 152 的表和审计列、既有账本及决定，不执行清零或 DROP。已经依赖新 API 的 App 遇到旧服务返回不支持或错误时必须暂停自定义调用，账户模型继续走旧链路。修复前进发布优先于反向迁移。

## 自动化验证

```bash
GOTOOLCHAIN=go1.26.6 scripts/pre-mr-scan.sh --backend --skip-integration
cd backend
GOTOOLCHAIN=go1.26.6 go test ./...
GOTOOLCHAIN=go1.26.6 go test -tags=unit ./...
GOTOOLCHAIN=go1.26.6 go test -tags=integration ./internal/repository -run 'CustomAPI|LocalModel|LocalDevice' -count=1 -v
```

数据库测试由仓库 harness 自动建立隔离 PostgreSQL / Redis；必须启动 Docker 并确认不是“skipping integration tests”。CI 使用 PostgreSQL 18 / Redis 8。本机可用镜像为 PostgreSQL 17 / Redis 7，使用临时 Go overlay 替换测试镜像名，不修改迁移、业务代码或生产数据库。

用例覆盖：两服务实例的 100 个不同 ID 严格 5 个批准；同 ID 并发 100 次只扣一次；跨账户隔离；午夜及真实行锁等待后的换日；成功和拒绝跨日重放；付费请求超过 5 次继续累计；授予撤销保留用量；120/min 独立限流；先写后故障回滚；提交确认丢失重试；关闭数据库拒绝放行；Redis 清空后重新构建服务仍读取原账本；7 天保留和清理后重新扣次；并发兑换、重复使用码、审计失败回滚；登录、CSRF、角色、字段、大小、UUID 和错误响应检查。已有设备授权与兑换回归用例一起运行。

2026-09-12 本机验证结果：后端 pre-MR 扫描（gofmt、vet、golangci-lint、unit）全通过；`go test ./...`、`go build ./...` 全通过；使用上述镜像 overlay 的 `go test -tags=integration ./... -count=1` 全通过。100 个不同 ID 得到 5 次批准、100 个相同 ID 只增加 1 次用量；跨日、事务回滚、丢失提交确认、撤销和保留期用例通过。OpenAPI YAML 解析及内部引用检查通过。

## 能保证的边界

服务端账本不会因为 App 清本地数据、换设备、修改电脑时间或正常并发而重置或超发。uzApi 不接收用户自带的 Key、地址、模型或内容，也不代理第三方请求。

直连第三方的改版 App 可以跳过本接口；服务器也无法验证一个 ID 对应几次真实上游调用。正常 App 必须维护每次在途操作的“未发送/已开始”状态，同 ID 最多启动一次上游；响应丢失后的重放仅恢复尚未发送的那次操作。这里不承诺彻底防破解，不新增客户端证明体系或托管网关。
