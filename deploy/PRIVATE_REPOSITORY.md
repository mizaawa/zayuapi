# 私人仓库部署与发布

代码与 Release：<https://github.com/mizaawa/zayuapi>。镜像：`ghcr.io/mizaawa/zayuapi`。程序、Compose 服务、数据库、数据目录和二进制文件继续使用 `sub2api` 名称，便于迁移已有部署。主页 Home 的 GitHub 开源按钮仍指向 `Wei-Shaw/sub2api`。

`deploy/.env` 随私库部署配置一并签入，GitHub 令牌值保持为空。在仓库克隆的 `deploy` 目录运行 `bash start-private.sh` 前，通过外部 `GHCR_TOKEN` 环境变量提供镜像令牌，脚本会登录 GHCR、拉取镜像并启动 Compose。其他部署路径可设置 `SUB2API_ENV_FILE`、`SUB2API_COMPOSE_FILE`。应用启动后，在「系统设置 → 功能开关 → 拓展功能」填写 GitHub 通行令牌。GoReleaser 发布归档排除该环境文件。

## Token 配置

| 用途 | 凭据和最小权限 | 配置位置 |
| --- | --- | --- |
| 后台 Release API、更新与回退资源 | fine-grained PAT，仓库 `Contents: read`，或 classic PAT，`repo` | 系统设置中的 GitHub 通行令牌 |
| 克隆、拉取私人代码、安装脚本下载 | fine-grained PAT，仓库 `Contents: read`，或 classic PAT，`repo` | HTTPS Git 凭据和外部 `UPDATE_GITHUB_TOKEN` 环境变量 |
| 拉取私人 GHCR 镜像 | classic PAT，`read:packages` 或 `write:packages`，账号须有包读取权限 | 外部 `GHCR_TOKEN` 环境变量，经 `docker login ghcr.io --password-stdin` |
| Actions 发布 Release 与 GHCR | GitHub 自动提供的 `GITHUB_TOKEN`；workflow 已声明 `contents: write`、`packages: write` | 无需额外 PAT |

令牌原文不再随代码保存。后台令牌保存在系统设置中，接口只返回是否已配置；替换和清除后，后续请求立即使用最新配置，无需重启。GHCR 登录账号为镜像令牌所属账号。

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
# 编辑 .env：配置可选的管理员账号，不要将令牌提交到仓库。
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

后台版本检查、systemd 二进制更新、版本列表及回退均读取 `mizaawa/zayuapi` 的 Release，并使用系统设置中的 GitHub 通行令牌。归档和 checksums 通过受认证的 GitHub Release Asset API 获取，下载重定向不携带 token。首次迁移后填写令牌，并强制刷新版本检查以清除旧缓存。

Docker 更新与回退通过 Compose 镜像执行。修改 `.env` 中的 `SUB2API_IMAGE=ghcr.io/mizaawa/zayuapi:0.2.108`，随后拉取并重建应用容器。二进制更新不能替代镜像更新，重建容器会恢复镜像内程序。

systemd 运行中的后台更新同样读取系统设置，不再读取环境文件中的令牌。单独执行安装、升级或回退脚本时，需在调用前从外部导出只读 `UPDATE_GITHUB_TOKEN`，并通过 `sudo --preserve-env=UPDATE_GITHUB_TOKEN bash deploy/install.sh` 保留环境变量。环境变量不会作为后台令牌的隐式替代。

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
