# 私人仓库部署与发布

代码与 Release：<https://github.com/mizaawa/zayuapi>。镜像：`ghcr.io/mizaawa/zayuapi`。程序、Compose 服务、数据库、数据目录和二进制文件继续使用 `sub2api` 名称，便于迁移已有部署。主页 Home 的 GitHub 开源按钮仍指向 `Wei-Shaw/sub2api`。

`deploy/.env` 随私库部署配置一并签入，并含 Release API 与 GHCR 凭据。在仓库克隆的 `deploy` 目录运行 `bash start-private.sh`，脚本会用配置中的 PAT 登录 GHCR、拉取镜像并启动 Compose。使用同一 classic PAT 需同时有 `repo` 和 `read:packages` 权限。其他部署路径可设置 `SUB2API_ENV_FILE`、`SUB2API_COMPOSE_FILE`，单独镜像 token 可通过 `GHCR_TOKEN` 环境变量提供。GoReleaser 发布归档排除该环境文件。

## Token 配置

| 用途 | 凭据和最小权限 | 配置位置 |
| --- | --- | --- |
| 克隆、拉取私人代码、后台 Release API 与资源 | classic PAT，`repo` | HTTPS Git 凭据和 `UPDATE_GITHUB_TOKEN` |
| 拉取私人 GHCR 镜像 | classic PAT，`read:packages` 或 `write:packages`，账号须有包读取权限 | `deploy/.env` 中的同一 PAT 经 `docker login ghcr.io --password-stdin` |
| Actions 发布 Release 与 GHCR | GitHub 自动提供的 `GITHUB_TOKEN`；workflow 已声明 `contents: write`、`packages: write` | 无需额外 PAT |

当前服务器部署使用同一个能够访问本仓库和 GHCR 的 classic PAT。这个 PAT 随私库的 `deploy/.env` 一并保存，并配置为仓库的 `UPDATE_GITHUB_TOKEN` Actions Secret。GHCR 登录账号为 token 所属账号。

```bash
read -rsp 'GHCR token: ' GHCR_TOKEN; echo
printf '%s' "$GHCR_TOKEN" | docker login ghcr.io -u mizaawa --password-stdin
unset GHCR_TOKEN
```

GitHub Packages 首次发布后应保持 **Private**，包须关联本仓库，并允许本仓库 Actions 写入。仓库权限与包权限可能分别管理；如果登录成功但拉取报 denied，请检查包的读取权限。

## 新部署

先完成 Git 认证和 GHCR 登录，再运行：

```bash
git clone https://github.com/mizaawa/zayuapi.git
mkdir -p sub2api-deploy && cd sub2api-deploy
bash ../zayuapi/deploy/docker-deploy.sh
# 编辑 .env：配置 UPDATE_GITHUB_TOKEN 及可选的管理员账号。
docker compose config -q
docker compose pull
docker compose up -d
docker compose logs -f sub2api
```

准备脚本优先读取相邻 checkout 内的部署文件。单独运行脚本时，远程获取使用 GitHub Contents API，需要导出 `UPDATE_GITHUB_TOKEN`。私人仓库不能使用匿名 `raw.githubusercontent.com` 一行安装命令。

已有部署先备份数据库、`.env` 和 Compose 文件，再修改应用镜像为 `${SUB2API_IMAGE:-ghcr.io/mizaawa/zayuapi:latest}`，保留原数据库、Redis、JWT 与 TOTP 密钥。仅更新应用容器：

```bash
docker compose pull sub2api
docker compose up -d --no-deps sub2api
```

## 更新与回退

后台版本检查、systemd 二进制更新、版本列表及回退均读取 `mizaawa/zayuapi` 的 Release。归档和 checksums 通过受认证的 GitHub Release Asset API 获取，下载重定向不携带 token。首次迁移后强制刷新版本检查以清除旧缓存。

Docker 更新与回退通过 Compose 镜像执行。修改 `.env` 中的 `SUB2API_IMAGE=ghcr.io/mizaawa/zayuapi:0.2.108`，随后拉取并重建应用容器。二进制更新不能替代镜像更新，重建容器会恢复镜像内程序。

systemd 持续更新使用 root 管理的环境文件：

```bash
sudo install -m 600 -o root -g root /dev/null /etc/sub2api/update.env
sudoedit /etc/sub2api/update.env
# 写入 UPDATE_GITHUB_TOKEN=你的只读token
sudo systemctl restart sub2api
```

执行安装、升级或回退脚本时，需要在调用前导出同一个只读 token，并通过 `sudo --preserve-env=UPDATE_GITHUB_TOKEN bash deploy/install.sh` 保留环境变量。`GITHUB_TOKEN` 和 `GH_TOKEN` 不会作为后台更新 token 的隐式替代。

## 自动发布

推送新版本标签会触发 `Release` workflow，也可在 Actions 手动选择已有标签。默认使用完整构建，发布 Linux/macOS/Windows 二进制、`checksums.txt`、Linux amd64/arm64 镜像及多架构 `latest`、版本、主次版本标签。二进制与归档继续命名为 `sub2api`，与更新器一致。

```bash
git tag -a v0.2.109 -m 'Sub2API 0.2.109' -m 'Describe the changes here.'
git push origin main
git push origin v0.2.109
```

手动勾选 `simple_release` 会仅发布 amd64 镜像，不提供后台二进制更新所需资源；需要完整更新能力时保持关闭。Docker Hub 与 Telegram 仅在显式开启相应仓库变量并配置 secrets 后使用，私人部署无需配置。

历史提交已保留。历史标签不重新推送，避免触发旧版本发布及覆盖 `latest`。GitHub Actions 日志、原仓库已有 Release 文件和仓库 secrets 不属于 Git 内容，不会随代码复制；本仓库由首个完整 Release 开始建立自己的更新历史。

## 外部依赖

模型价格更新仍使用独立的公共 `Wei-Shaw/model-price-repo`，失败时读取仓库随附的本地价格数据。它不是应用代码或 Release 仓库，也无需私人仓库 token。支付服务、OAuth、AI 供应商和 TLS 指纹收集站点继续使用对应服务的接口。

内部 Go module/import 路径保留 `github.com/Wei-Shaw/sub2api`，这些引用解析本仓库的本地包，不会在运行时下载或调用上游代码。更改它们需要重生成代码且不影响私人发布和部署。
