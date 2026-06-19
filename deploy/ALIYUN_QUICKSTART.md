# 阿里云一键部署快速清单

完整说明见：[`deploy/aliyun/README.md`](aliyun/README.md)。

## 最少要做的事

1. 买一台阿里云 ECS，安全组开放 `80`、`443`，SSH `22` 只允许你的 IP。
2. 把代码放到 GitHub 仓库，并配置 GitHub Actions Secrets：
   - `ALIYUN_HOST`
   - `ALIYUN_USER`，默认 `root`
   - `ALIYUN_PORT`，默认 `22`
   - `ALIYUN_SSH_KEY`
   - `ALIYUN_APP_DIR`，默认 `/opt/uzapi`
   - `GHCR_USERNAME` / `GHCR_TOKEN`，私有 GHCR 包才需要
3. 首次把仓库同步到 ECS 后，在 ECS 上执行：

```bash
sudo bash scripts/aliyun-bootstrap.sh
cp deploy/aliyun/.env.production.example deploy/aliyun/.env.production
vi deploy/aliyun/.env.production
./scripts/aliyun-deploy.sh
```

4. 以后直接：

```bash
git push origin master
```

GitHub Actions 会先构建并推送 Docker 镜像到 GHCR，成功后同步部署文件到阿里云 ECS，由 ECS 拉取镜像并重启服务。

## 注意

- `.env.production` 不会被提交，必须保留在 ECS 上。
- 私有 GHCR 包需要先在 ECS 上 `docker login ghcr.io`；公开包可直接拉取。
- 有域名时把 `DOMAIN=:80` 改成 `api.uzapi.org`，Caddy 会自动申请 HTTPS 证书。
- OpenAI API Key 通常在后台账号/渠道配置，不写进 `.env.production`。
