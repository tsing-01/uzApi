# uzApi 阿里云 ECS 部署说明

这套配置的目标是：你 push 到 `master` 后，GitHub Actions 构建并推送 Docker 镜像到 GHCR，再把部署文件同步到阿里云 ECS，由 ECS 拉取镜像并执行 Docker Compose 部署。

## 1. 阿里云侧准备

推荐先用一台 ECS 单机跑通：

- 地域：香港 / 新加坡 / 日本 / 美国优先，访问 OpenAI 上游更稳。
- 系统：Ubuntu 22.04 LTS 或 Alibaba Cloud Linux。
- 规格：起步 2C4G，正式建议 4C8G。
- 安全组：开放 80、443；SSH 22 只允许你自己的 IP。
- 域名：先把 `api.uzapi.org` A 记录解析到 ECS 公网 IP。没有域名时先用 `DOMAIN=:80` 测试。

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

- `DOMAIN`：有域名填 `api.uzapi.org`，无域名先填 `:80`
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

`APP_IMAGE` / `APP_IMAGE_TAG` 在 GitHub Actions 自动部署时会被当前 commit 的 GHCR 镜像覆盖。手动部署时如果使用私有镜像仓库，需要先在 ECS 上执行对应的 `docker login`。

## 3. 手动部署一次

```bash
./scripts/aliyun-deploy.sh
```

检查：

```bash
docker compose --env-file deploy/aliyun/.env.production -f deploy/aliyun/docker-compose.yml ps
curl -i http://127.0.0.1/health
```

如果 `DOMAIN=api.uzapi.org` 且 DNS 已指向 ECS，Caddy 会自动申请 HTTPS 证书。

## 4. GitHub Actions 自动部署

在 GitHub 仓库 `Settings -> Secrets and variables -> Actions -> New repository secret` 添加：

| Secret | 示例 | 说明 |
| --- | --- | --- |
| `ALIYUN_HOST` | `47.xx.xx.xx` | ECS 公网 IP 或域名 |
| `ALIYUN_USER` | `root` | SSH 用户，默认 root |
| `ALIYUN_PORT` | `22` | SSH 端口，默认 22 |
| `ALIYUN_SSH_KEY` | 私钥全文 | 能登录 ECS 的私钥 |
| `ALIYUN_APP_DIR` | `/opt/uzapi` | ECS 上项目部署目录，默认 `/opt/uzapi` |
| `GHCR_USERNAME` | `your-github-user` | 可选，私有 GHCR 包拉取用户名 |
| `GHCR_TOKEN` | `ghp_xxx` | 可选，私有 GHCR 包拉取 token，需 `read:packages` |

随后 push 到 `master`：

```bash
git push origin master
```

workflow 会：

1. 在 GitHub Actions runner 上构建 Docker 镜像。
2. 推送镜像到 GHCR。标签为当前 commit SHA 和 `aliyun-latest`。
3. 只把 `deploy/aliyun` 和必要脚本同步到 ECS。
4. 在 ECS 上执行 `scripts/aliyun-deploy.sh`。
5. ECS 拉取指定镜像并重启服务，不在 ECS 上 build。
6. 等待 `/health` 通过。

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
