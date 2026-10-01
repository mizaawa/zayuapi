# Sub2API 部署

<p align="center">
  <a href="README.md"><img src="https://img.shields.io/badge/English-README-0969da" alt="English"></a>
  <a href="README_CN.md"><img src="https://img.shields.io/badge/%E7%AE%80%E4%BD%93%E4%B8%AD%E6%96%87-README-0969da" alt="简体中文"></a>
  <a href="README_JA.md"><img src="https://img.shields.io/badge/%E6%97%A5%E6%9C%AC%E8%AA%9E-README-0969da" alt="日本語"></a>
</p>

本仓库由 [mizaawa/zayuapi](https://github.com/mizaawa/zayuapi) 维护。部署镜像、安装脚本以及后台的一键更新和版本回退均使用此仓库的 Release。

这是私人仓库。部署 PAT 已写入私有仓库的 `deploy/.env`：

```bash
git clone https://github.com/mizaawa/zayuapi.git
cd zayuapi
bash deploy/start-private.sh
```

启动脚本会读取 `.env`，登录 GHCR，拉取镜像并启动 Compose。详细配置及首次发布见 [私人仓库部署](deploy/PRIVATE_REPOSITORY.md)。

## Docker Compose 部署

前置条件：Docker Engine 20.10+ 与 Docker Compose v2+。

### 一键准备

```bash
git clone https://github.com/mizaawa/zayuapi.git
cd zayuapi/deploy
bash start-private.sh
```

脚本读取私库中的 `.env` 和 Compose 文件，登录 GHCR 并启动服务，数据保存在本地 `data`、`postgres_data`、`redis_data` 目录。

容器健康后访问 `http://服务器IP:8080`。如果 `.env` 未设置 `ADMIN_PASSWORD`，请从应用日志中查看首次生成的管理员密码。

### 手动部署

```bash
git clone https://github.com/mizaawa/zayuapi.git
cd zayuapi/deploy
cp .env.example .env
chmod 600 .env
mkdir -p data postgres_data redis_data
docker compose -f docker-compose.local.yml up -d
```

生产环境启动前，请在 `.env` 中设置强随机值 `POSTGRES_PASSWORD`、`JWT_SECRET` 和 `TOTP_ENCRYPTION_KEY`；可按需设置 `ADMIN_EMAIL`、`ADMIN_PASSWORD`、`SERVER_PORT`。

### 从上游部署迁移并保留数据

当前这种本地目录版 Compose 会把应用数据保存在 `./data`，PostgreSQL 保存在 `./postgres_data`，Redis 保存在 `./redis_data`。必须在**原来的部署目录**执行以下步骤，不要新建第二个目录，也不要再次运行 `docker-deploy.sh`，否则可能覆盖 `.env` 并生成新密钥。

```bash
cd /path/to/sub2api-deploy
umask 077
STAMP=$(date +%Y%m%d-%H%M%S)
mkdir -p "backups/$STAMP"
cp .env docker-compose.yml "backups/$STAMP/"
docker compose exec -T postgres sh -ec 'pg_dump -U "$POSTGRES_USER" -d "$POSTGRES_DB" -Fc' > "backups/$STAMP/sub2api.dump"

# 只替换 sub2api 应用镜像，postgres 和 redis 保持不变
sed -i 's#image: weishaw/sub2api:latest#image: ${SUB2API_IMAGE:-ghcr.io/mizaawa/zayuapi:latest}#' docker-compose.yml
printf '\nSUB2API_IMAGE=ghcr.io/mizaawa/zayuapi:latest\n' >> .env

docker compose config -q
docker compose pull sub2api
docker compose up -d --no-deps sub2api
docker compose ps
```

该流程只重建 `sub2api`，不会删除或重建数据库、Redis 数据目录。请保留原 `.env` 中的 `POSTGRES_PASSWORD`、`JWT_SECRET`、`TOTP_ENCRYPTION_KEY` 以及代理和供应商配置。

### 更新与回退

Docker 部署更新到最新已发布镜像：

```bash
docker compose -f docker-compose.local.yml pull
docker compose -f docker-compose.local.yml up -d
```

若要固定或回退 Docker 镜像版本，在 `.env` 中设置 `SUB2API_IMAGE=ghcr.io/mizaawa/zayuapi:<版本号>` 后运行相同命令。管理后台的一键更新、回退均从 `mizaawa/zayuapi` 的 GitHub Releases 获取版本，需先在本仓库发布带二进制资源的 Release。

容器部署请通过 Compose 更新镜像；后台的二进制替换适用于 systemd 安装，容器重建会重新使用镜像内的程序。完整 Release 同时发布 Linux/macOS/Windows 二进制和 `linux/amd64`、`linux/arm64` 镜像；Simple Release 只有 amd64 镜像，不能用于后台二进制更新。

### 常用命令

```bash
docker compose -f docker-compose.local.yml ps
docker compose -f docker-compose.local.yml logs -f sub2api
docker compose -f docker-compose.local.yml restart
docker compose -f docker-compose.local.yml down
```
