# 本地模型付费权益与设备授权 v1

本协议由 uzApi 服务端实现，供后续 24Hbutler 集成。本次没有修改 24Hbutler。

自定义 API 每次请求的在线计次契约见 [自定义 API 配额](CUSTOM_API_QUOTA.md)。它复用本文的账户权益及版本；设备的 5 分钟签名授权不代表一次自定义 API 请求已获准，也不代替每日账本。

兑换码授予账户权益；设备必须证明持有私钥，才能获得 5 分钟签名授权。客户端显示权益状态和实际执行本地模型接入应分开处理。只读取 `local_model_access_enabled` 不能保护执行入口。

## 安全边界

- 服务端每次挑战、登记、签发和续签都读取数据库中的最新账户状态与权益；所有相关操作与兑换、撤销通过同一用户行锁串行化。
- 授权绑定账户、设备公钥指纹、用途、环境、权益版本和账户凭据版本。挑战含 256 位随机数，60 秒有效，只存哈希，只能在指定操作中消费一次。签名验证、消费挑战、设备变更和审计在同一事务内提交。
- 普通用户 API 只接受登录 access token，账户 ID 从认证上下文取得；管理员 API Key 和推理 API Key 不能替代用户登录。设备 ID、账户 ID、布尔值和客户端机器指纹均不作为持有私钥的证明。
- 默认最多 2 台活跃设备，可配置 1–10 台。每账户每分钟最多创建 30 个挑战，同时最多 5 个未消费且未过期挑战；24 小时最多登记 5 个新设备。消费挑战或撤销设备不会重置这些限额。请求体最大 16 KiB。
- 账户权益撤销会递增权益版本，撤销全部设备，作废未使用挑战。单设备撤销会作废该公钥的待使用挑战。所有副本必须连接同一数据库并使用一致的签名配置。
- 撤销立即阻止新的签发和续签。已签发的离线授权无法被服务端主动收回，遵守协议的客户端最多继续使用到 `exp`（5 分钟）。网络异常或续签失败不得延长授权。
- **无法保证纯本地功能不被破解。** 用户控制的程序可以被修改，跳过全部本地检查；软件私钥也可能被复制。此方案保护服务端发证、降低授权复制与账号共享，不是硬件证明或 App 完整性证明。未来可接入平台证明并在服务端验证结果；强制不可绕过的功能必须保留服务端参与的执行环节。

## 部署

新增迁移 `150_local_model_access.sql`（权益）和 `151_local_model_device_licenses.sql`（撤销、设备、挑战和审计）。使用项目启动迁移机制；已经应用的迁移不能修改。发布前备份数据库，回退应用时保留新增列和表。

```yaml
local_model_access:
  signing_seed: ""             # 标准 base64 编码的 32 字节 Ed25519 seed
  issuer: ""                   # 环境唯一的 HTTPS URL，例如 https://api.example.com
  max_devices: 2
  previous_public_keys: []      # 旧 Ed25519 公钥 x，无填充 base64url
```

同名环境变量为 `LOCAL_MODEL_ACCESS_SIGNING_SEED`、`LOCAL_MODEL_ACCESS_ISSUER`、`LOCAL_MODEL_ACCESS_MAX_DEVICES`、`LOCAL_MODEL_ACCESS_PREVIOUS_PUBLIC_KEYS`（逗号分隔）。仓库的 Compose 模板已传入这些变量。

通过 `openssl rand -base64 32` 生成独立密钥，保存在部署密钥管理系统中；不要复用 `JWT_SECRET`，不要放入 App、日志、代码或兑换码中。空 seed 禁用发证相关接口（503），账户权益查询、设备查询和撤销仍可使用。配置了错误的 seed、issuer、设备上限或旧公钥时，服务初始化失败，不降级为无签名授权。Issuer 自动去掉末尾 `/`，不得含账号密码、查询参数或 fragment。

轮换时先记录旧 JWKS 的 `x`，将新 seed 与旧公钥列表一致地发布到全部副本。每个 `kid` 是原始 Ed25519 公钥的 SHA-256，无填充 base64url。旧公钥仅用于验证；新票据用新密钥签发。最后一个副本切换后至少等待 5 分钟，再移除旧公钥。若密钥泄露，移除该公钥会阻止其续签；离线客户端接受旧密钥的风险取决于其公钥更新与信任策略，不能承诺即时失效。

`local_model_license_events` 记录设备登记、签发、续签、设备撤销和权益撤销，不记录私钥、挑战明文、证明签名或授权 JWT。运营需制定审计归档策略，至少保留最近 24 小时的 `device_registered` 记录供限额判断。过期超过一小时的挑战在该账户下次请求挑战时清理；闲置账户的旧挑战不会自动全局清理。

## HTTP 接口

除公开公钥外，用户接口均使用 `Authorization: Bearer <用户登录 access_token>`。每个接口返回项目标准封装：`{"code":0,"message":"success","data":...}`。以下请求体和响应示例省略该封装。响应包含 `Cache-Control: no-store`。

| 方法与路径 | 功能 |
| --- | --- |
| `POST /api/v1/redeem` | `{"code":"兑换码"}`，兑换 `local_model_access` 权益 |
| `GET /api/v1/integration/entitlements` | 当前账户权益，仅供展示 |
| `GET /api/v1/local-model-access/keys` | 公开 JWKS、issuer、audience，无需登录 |
| `POST /api/v1/local-model-access/challenges` | 请求 register、issue 或 renew 挑战 |
| `POST /api/v1/local-model-access/devices` | 证明设备私钥持有并登记 |
| `GET /api/v1/local-model-access/devices` | 当前账户的设备（包括已撤销设备） |
| `DELETE /api/v1/local-model-access/devices/:device_id` | 撤销自己的设备，重复撤销幂等 |
| `POST /api/v1/local-model-access/licenses` | 以新挑战签发授权 |
| `POST /api/v1/local-model-access/licenses/renew` | 以新挑战及现有授权续签 |
| `POST /api/v1/admin/users/:id/local-model-access/revoke` | 管理员撤销账户权益，必须提供 `reason` |

管理员撤销使用现有管理员认证；用户 JWT 只有数据库角色为 admin 才可操作。管理命令见 [管理员 CLI 文档](../skills/uzapi-admin/references/admin-cli.md#撤销本地模型权益)。

兑换码每个只能使用一次，本类型 `value=0`，不支持 `group_id` 和非零 `validity_days`。权益无固定到期时间，可由管理员撤销；码的过期时间仅限制兑换。已有有效权益时兑换新码返回 409，不消费新码；撤销后可兑换新码重新开通，旧设备公钥保持撤销状态，需生成新密钥登记。用户资料更新接口无法自行赋予或清除这些服务端字段。

## 设备登记与证明

设备生成 P-256 密钥对。优先采用系统硬件支持的不可导出私钥；本协议本身不验证硬件属性。提交公开 JWK，不得发送 `d`：

```json
{"action":"register","public_key":{"kty":"EC","crv":"P-256","x":"32字节无填充base64url","y":"32字节无填充base64url"}}
```

`x`、`y` 必须严格为曲线上坐标的 32 字节大端值。服务端按照 [RFC 7638](https://www.rfc-editor.org/rfc/rfc7638) 对 `{"crv":"P-256","kty":"EC","x":"...","y":"..."}` 计算 SHA-256 指纹，编码为无填充 base64url。

响应：

```json
{"challenge":"64个十六进制字符","message":"待签名消息","expires_at":"RFC3339时间"}
```

对 `message` 的原始 UTF-8 字节进行 ECDSA P-256 SHA-256 签名。签名编码为 64 字节 `r || s`（各 32 字节大端补零），再无填充 base64url 编码为 86 个字符；不是 ASN.1 DER。[Node.js 签名 API](https://nodejs.org/api/crypto.html#cryptosignalgorithm-data-key-callback) 示例：

```js
import { generateKeyPairSync, sign } from 'node:crypto';
const { privateKey, publicKey } = generateKeyPairSync('ec', { namedCurve: 'prime256v1' });
const jwk = publicKey.export({ format: 'jwk' });
// 发送 jwk 请求 register 挑战，得到 challengeResponse。
const signature = sign('sha256', Buffer.from(challengeResponse.message, 'utf8'), {
  key: privateKey, dsaEncoding: 'ieee-p1363'
}).toString('base64url');
// POST /devices: { name, challenge: challengeResponse.challenge, signature }
```

这是带域分离和用途绑定的自定义 v1 挑战协议，不是 DPoP。客户端应从固定 HTTPS 服务取挑战，并核对上下文后签名，不能把设备密钥暴露为供网页或不可信插件任意签名的接口。消息逐行如下（LF 分隔，空字段仍保留；最后一项为空时消息自然以 LF 结束，不额外追加换行）：

```text
UZAPI-DEVICE-PROOF-V1
{issuer}
POST
{/api/v1/local-model-access/devices 或 /licenses 或 /licenses/renew 的完整路径}
{user_id 十进制}
{device_id；register 时为空}
{公钥指纹}
{grant_version 十进制}
{token_version 十进制}
{challenge}
{上一个授权 JWT 原始字符串的 SHA-256 小写十六进制；register/issue 时为空}
```

`token_version` 来源于服务端当前凭据版本，不作为客户端计数器。客户端原样签名，不经数字精度转换或 JSON 再序列化。消息中的账户、issuer、操作、设备指纹必须与本次调用意图一致。

登记请求：

```json
{"name":"我的电脑","challenge":"挑战值","signature":"证明签名"}
```

返回设备 `id`、`name`、`public_key`、`key_thumbprint`、`created_at`、`last_issued_at`、`revoked_at`。名称限制 1–100 UTF-8 字节，不得包含换行或 NUL。同一有效公钥重新以新挑战登记返回原设备；已经消费的挑战不能再次提交。

## 签发和续签

首次签发先请求 `{"action":"issue","device_id":"已登记设备ID"}`，设备签名返回的消息，再 `POST /licenses`：

```json
{"challenge":"挑战值","signature":"证明签名"}
```

响应：

```json
{"license":"签名JWT","expires_at":"RFC3339时间","renew_after":"RFC3339时间","device_id":"设备ID"}
```

客户端在 `renew_after`（签发后 2 分钟）开始续签。先请求 `{"action":"renew","device_id":"设备ID","license":"当前JWT"}`，签名新消息，再 `POST /licenses/renew`：

```json
{"challenge":"新挑战值","signature":"新证明签名","license":"与请求挑战时完全相同的JWT"}
```

续签要求旧授权仍有效，且账户、设备、密钥、权益版本和凭据版本都匹配。其他设备的授权、重新编码的字符串、过期或篡改的票据均不能续签。网络重试如果已经消费挑战，应重新取挑战；不要反复使用旧证明。授权已到期时，可在重新确认用户登录有效后走 `issue` 流程，不获得离线宽限。

## 客户端验签与执行要求（后续 24Hbutler 实现）

JWT header 固定 `alg=EdDSA`、`typ=uzapi-local-model-license+jwt`，`kid` 只能选自可信服务的公钥集合。服务端签名密钥独立于登录 JWT。客户端不得依据 JWT 自带的任意 URL 下载公钥；忽略不认识的算法并不安全，应直接拒绝。先验证签名，再使用 payload：

- `iss`：与预置环境一致；`aud`：`24hbutler.local-model-access`。
- `sub`：当前登录账户 ID 的字符串；`device_id`：当前登记设备 ID；`device_key_thumbprint`：本机设备公钥指纹。
- `feature`：`local_model_access`；`grant_version`：权益版本；`token_version`：字符串形式的凭据版本。
- `iat`、`nbf`、`exp`：Unix 秒，全部必须存在。票据尚未生效或过期必须拒绝，`exp - iat` 必须在 0–300 秒之间且大于 0。`jti` 为随机 32 字符 ID。

设备私钥操作和授权检查应放在受保护的主进程/原生服务中，并覆盖每个本地模型执行入口、后台任务和已有会话。切换账户、注销时清除授权；确认仍持有本机设备密钥。不要允许渲染进程通过 IPC 写入 `unlocked=true` 或任意指定授权账户。私钥优先用系统安全存储，避免以明文文件导出。

应使用可信服务时间与单调时钟限制本次票据剩余时长，不因系统时间回拨而延长授权。每次续签重新校验返回票据；401 重新登录，403 停止受限功能，429 退避，503/网络失败仅可使用仍在有效期内的原票据，到期即停。客户端无法仅靠离线的 `grant_version` 判断服务器是否已经撤销，应靠短期到期和联网续签收敛。

## 主要错误

| HTTP | 代码 | 含义 |
| --- | --- | --- |
| 400 | `LOCAL_DEVICE_PROOF_INVALID` | 无效、过期、跨用途或已消费证明 |
| 400 | `LOCAL_LICENSE_INVALID` | 无效、过期或与本次续签不匹配的授权 |
| 403 | `LOCAL_MODEL_ACCESS_REQUIRED` | 没有有效权益或账户状态无效 |
| 403 | `LOCAL_DEVICE_REVOKED` | 设备公钥已撤销，不能复活 |
| 404 | `LOCAL_DEVICE_NOT_FOUND` | 设备不属于当前账户或不存在 |
| 409 | `LOCAL_DEVICE_LIMIT` | 活跃设备数量达到上限 |
| 429 | `LOCAL_LICENSE_RATE_LIMIT` | 挑战或登记频率超限 |
| 503 | `LOCAL_LICENSE_DISABLED` | 签名功能未配置 |

## 验证

测试覆盖兑换原子性、重复兑换、JWT 用户隔离、越权撤销、请求体限制、公私钥字段、签名/算法/用途/版本篡改、公钥轮换、证明重放、跨账户/设备使用、续签绑定、设备名额并发、撤销与签发并发、事务回滚、凭据变更和两层限额。

```bash
cd backend
go test ./internal/service ./internal/handler ./internal/config
go test -tags=integration ./internal/repository -run 'TestLocalDevice|TestLocalModelAccess' -count=1
```

集成测试需项目指定的 PostgreSQL/Redis Docker 镜像，使用临时数据库，不应指向生产数据库。
