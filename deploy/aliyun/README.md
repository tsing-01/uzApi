# uzApi 阿里云 ECS 部署说明

这套配置的目标是：你 push 到 `master` 后，GitHub Actions 自动构建检查，通过后把当前仓库同步到阿里云 ECS，并执行 Docker Compose 部署。

## 1. 阿里云侧准备

推荐先用一台 ECS 单机跑通：

- 地域：香港 / 新加坡 / 日本 / 美国优先，访问 OpenAI 上游更稳。
- 系统：Ubuntu 22.04 LTS 或 Alibaba Cloud Linux。
- 规格：起步 2C4G，正式建议 4C8G。
- 安全组：开放 80、443；SSH 22 只允许你自己的 IP。
- 域名：先把 `api.yourdomain.com` A 记录解析到 ECS 公网 IP。没有域名时先用 `DOMAIN=:80` 测试。

首次登录 ECS 后执行：

```bash
sudo bash scripts/aliyun-bootstrap.sh
```

## 2. 生产环境变量

在 ECS 的项目目录里创建：

```bash
cp deploy/aliyun/.env.production.example deploy/aliyun/.env.production
vi deploy/aliyun/.env.production
```

至少必须修改：

- `DOMAIN`：有域名填 `api.yourdomain.com`，无域名先填 `:80`
- `DATABASE_PASSWORD`
- `REDIS_PASSWORD`
- `ADMIN_EMAIL`
- `ADMIN_PASSWORD`
- `JWT_SECRET`
- `TOTP_ENCRYPTION_KEY`

生成密钥：

```bash
openssl rand -hex 32
```

你的 OpenAI API Key 通常在后台账号/渠道里配置，不建议写进 `.env.production`。

## 3. 手动部署一次

```bash
./scripts/aliyun-deploy.sh
```

检查：

```bash
docker compose --env-file deploy/aliyun/.env.production -f deploy/aliyun/docker-compose.yml ps
curl -i http://127.0.0.1/health
```

如果 `DOMAIN=api.yourdomain.com` 且 DNS 已指向 ECS，Caddy 会自动申请 HTTPS 证书。

## 4. GitHub Actions 自动部署

在 GitHub 仓库 `Settings -> Secrets and variables -> Actions -> New repository secret` 添加：

| Secret | 示例 | 说明 |
| --- | --- | --- |
| `ALIYUN_HOST` | `47.xx.xx.xx` | ECS 公网 IP 或域名 |
| `ALIYUN_USER` | `root` | SSH 用户，默认 root |
| `ALIYUN_PORT` | `22` | SSH 端口，默认 22 |
| `ALIYUN_SSH_KEY` | 私钥全文 | 能登录 ECS 的私钥 |
| `ALIYUN_APP_DIR` | `/opt/uzapi` | ECS 上项目部署目录，默认 `/opt/uzapi` |

随后 push 到 `master`：

```bash
git push origin master
```

workflow 会：

1. Docker build 检查前后端能否完整构建。
2. 把仓库快照同步到 ECS。
3. 在 ECS 上执行 `scripts/aliyun-deploy.sh`。
4. 重建并重启服务。
5. 等待 `/health` 通过。

## 5. 数据备份

如果使用 compose 内置 PostgreSQL：

```bash
./scripts/backup-db.sh
```

备份会保存到：

```text
deploy/aliyun/backups/uzapi-YYYYMMDD-HHMMSS.sql.gz
```

恢复示例：

```bash
gunzip -c deploy/aliyun/backups/uzapi-xxxx.sql.gz \
  | docker compose --env-file deploy/aliyun/.env.production -f deploy/aliyun/docker-compose.yml exec -T postgres \
      psql -U uzapi -d uzapi
```

## 6. 使用 RDS / Tair

跑通后建议把数据库迁到阿里云 RDS PostgreSQL，把 Redis 换成 Tair/Redis。

`.env.production` 改：

```env
DATABASE_HOST=<rds-endpoint>
DATABASE_PORT=5432
DATABASE_USER=<rds-user>
DATABASE_PASSWORD=<rds-password>
DATABASE_DBNAME=uzapi
DATABASE_SSLMODE=disable

REDIS_HOST=<redis-endpoint>
REDIS_PORT=6379
REDIS_PASSWORD=<redis-password>
```

compose 里本地 `postgres` / `redis` 仍会启动但不会被应用使用；后续可以再删掉这两个服务。
