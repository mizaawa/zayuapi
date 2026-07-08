# zayuapi - AI API 网关平台

<div align="center">

[![Go](https://img.shields.io/badge/Go-1.25.7-00ADD8.svg)](https://golang.org/)
[![Vue](https://img.shields.io/badge/Vue-3.4+-4FC08D.svg)](https://vuejs.org/)
[![PostgreSQL](https://img.shields.io/badge/PostgreSQL-15+-336791.svg)](https://www.postgresql.org/)
[![Redis](https://img.shields.io/badge/Redis-7+-DC382D.svg)](https://redis.io/)
[![Docker](https://img.shields.io/badge/Docker-Ready-2496ED.svg)](https://www.docker.com/)

**基于订阅配额分发的 AI API 网关平台**

**mizaawa 定制版本 | 新增视频按秒计费 | 移除请求头限制**

</div>

## 📋 目录

- [关于本版本](#关于本版本)
- [核心特性](#核心特性)
- [快速开始](#快速开始)
- [部署方式](#部署方式)
  - [方式一：Docker Compose（推荐）](#方式一docker-compose推荐)
  - [方式二：脚本安装](#方式二脚本安装)
  - [方式三：从源码构建](#方式三从源码构建)
- [功能说明](#功能说明)
- [配置说明](#配置说明)
- [升级指南](#升级指南)
- [常见问题](#常见问题)
- [技术栈](#技术栈)
- [许可证](#许可证)

## 关于本版本

本项目基于 [Wei-Shaw/sub2api](https://github.com/Wei-Shaw/sub2api) 官方版本进行定制增强。

### 🎯 主要改进

#### ✨ 新增功能

**1. 视频按秒计费模式**
- 在渠道定价管理中新增"视频（按秒）"计费选项
- 支持阶梯定价：根据视频秒数范围设置不同单价
- 自动从请求体中读取 `seconds` 参数进行计费
- 示例：5秒视频 × 3元/秒 = 扣费15元

#### 🔓 移除限制

**2. 请求头覆写全面开放**
- 官方版本仅允许 Claude/OpenAI 平台使用
- **现已支持所有平台**的 API Key 账号类型
- 移除了大部分请求头黑名单限制
- 仅保留最基本的 HTTP 传输控制头（`content-length`、`transfer-encoding`、`connection`）

#### 🔄 数据库兼容性

- 完全兼容官方版本数据库结构
- 可无缝切换回官方版本
- 迁移文件采用 `ALTER TABLE ... ADD COLUMN IF NOT EXISTS` 确保安全性

## 核心特性

- **多账号管理** - 支持多种上游账号类型（OAuth、API Key）
- **API Key 分发** - 为用户生成和管理 API Keys
- **精确计费** - Token 级别的使用追踪和费用计算
- **智能调度** - 智能账号选择与会话保持
- **并发控制** - 用户级和账号级并发限制
- **速率限制** - 可配置的请求和 Token 速率限制
- **内置支付系统** - 支持支付宝、微信支付、Stripe 等多种支付方式
- **管理后台** - Web 界面进行监控和管理
- **外部系统集成** - 通过 iframe 嵌入外部系统扩展管理后台

## 快速开始

### 系统要求

- Docker 20.10+ 和 Docker Compose v2+（Docker 部署）
- 或 PostgreSQL 15+ 和 Redis 7+（手动部署）

### 一键部署

```bash
# 创建部署目录
mkdir -p zayuapi && cd zayuapi

# 下载部署脚本
curl -sSL https://raw.githubusercontent.com/mizaawa/zayuapi/main/deploy/docker-deploy.sh | bash

# 启动服务
docker compose up -d

# 查看日志
docker compose logs -f sub2api
```

部署完成后，打开浏览器访问：`http://YOUR_SERVER_IP:8080`

如果使用自动生成的管理员密码，可通过日志查看：
```bash
docker compose logs sub2api | grep "admin password"
```

## 部署方式

### 方式一：Docker Compose（推荐）

使用 Docker Compose 部署，包含 PostgreSQL 和 Redis 容器。

#### 快速部署

```bash
# 创建部署目录
mkdir -p zayuapi && cd zayuapi

# 下载并运行部署脚本
curl -sSL https://raw.githubusercontent.com/mizaawa/zayuapi/main/deploy/docker-deploy.sh | bash

# 启动服务
docker compose up -d

# 查看日志
docker compose logs -f sub2api
```

**脚本会自动完成以下操作**：
- 下载 `docker-compose.yml` 和 `.env.example`
- 生成安全凭据（JWT_SECRET、TOTP_ENCRYPTION_KEY、POSTGRES_PASSWORD）
- 创建 `.env` 配置文件
- 创建数据目录
- 显示生成的凭据供参考

#### 手动部署

```bash
# 1. 克隆仓库
git clone https://github.com/mizaawa/zayuapi.git
cd zayuapi/deploy

# 2. 复制环境配置
cp .env.example .env

# 3. 编辑配置（必须设置密码）
nano .env
```

**必需配置项（.env 文件）**：

```bash
# PostgreSQL 密码（必填）
POSTGRES_PASSWORD=your_secure_password_here

# JWT 密钥（推荐设置 - 保持用户登录状态）
JWT_SECRET=your_jwt_secret_here

# TOTP 加密密钥（推荐设置 - 保持 2FA 配置）
TOTP_ENCRYPTION_KEY=your_totp_key_here

# 可选：管理员账号
ADMIN_EMAIL=admin@example.com
ADMIN_PASSWORD=your_admin_password

# 可选：自定义端口
SERVER_PORT=8080
```

**生成安全密钥**：

```bash
# 生成 JWT_SECRET
openssl rand -hex 32

# 生成 TOTP_ENCRYPTION_KEY
openssl rand -hex 32

# 生成 POSTGRES_PASSWORD
openssl rand -hex 32
```

```bash
# 4. 创建数据目录（本地版本）
mkdir -p data postgres_data redis_data

# 5. 启动所有服务
# 选项 A：本地目录版本（推荐 - 便于迁移）
docker compose -f docker-compose.local.yml up -d

# 选项 B：命名卷版本（简单设置）
docker compose up -d

# 6. 检查状态
docker compose ps

# 7. 查看日志
docker compose logs -f sub2api
```

#### 部署版本对比

| 版本 | 数据存储 | 迁移难度 | 适用场景 |
|------|---------|----------|----------|
| **docker-compose.local.yml** | 本地目录 | ✅ 简单（tar 整个目录） | 生产环境、频繁备份 |
| **docker-compose.yml** | 命名卷 | ⚠️ 需要 docker 命令 | 简单设置 |

**推荐**：使用 `docker-compose.local.yml`（脚本默认部署），便于数据管理。

#### 访问服务

打开浏览器访问：`http://YOUR_SERVER_IP:8080`

如果使用自动生成的管理员密码，可在日志中查找：
```bash
docker compose logs sub2api | grep "admin password"
```

#### 升级

```bash
# 拉取最新镜像并重新创建容器
docker compose pull
docker compose up -d
```

#### 数据迁移（本地目录版本）

使用 `docker-compose.local.yml` 时，迁移非常简单：

```bash
# 源服务器
docker compose down
cd ..
tar czf zayuapi-backup.tar.gz zayuapi/

# 传输到新服务器
scp zayuapi-backup.tar.gz user@new-server:/path/

# 新服务器
tar xzf zayuapi-backup.tar.gz
cd zayuapi/
docker compose up -d
```

#### 常用命令

```bash
# 停止所有服务
docker compose down

# 重启服务
docker compose restart

# 查看所有日志
docker compose logs -f

# 清除所有数据（谨慎！）
docker compose down
rm -rf data/ postgres_data/ redis_data/
```

---

### 方式二：脚本安装

一键安装脚本，从 GitHub Releases 下载预编译二进制。

#### 系统要求

- Linux 服务器（amd64 或 arm64）
- PostgreSQL 15+（已安装并运行）
- Redis 7+（已安装并运行）
- Root 权限

#### 安装步骤

```bash
curl -sSL https://raw.githubusercontent.com/mizaawa/zayuapi/main/deploy/install.sh | sudo bash
```

脚本会自动：
1. 检测系统架构
2. 下载最新版本
3. 安装到 `/opt/sub2api`
4. 创建 systemd 服务
5. 配置系统用户和权限

#### 安装后操作

```bash
# 1. 启动服务
sudo systemctl start sub2api

# 2. 设置开机自启
sudo systemctl enable sub2api

# 3. 打开设置向导
# http://YOUR_SERVER_IP:8080
```

设置向导将引导您完成：
- 数据库配置
- Redis 配置
- 管理员账号创建

#### 升级

可以直接在**管理后台**点击左上角的**检查更新**按钮进行升级。

Web 界面会自动：
- 检查新版本
- 一键下载并应用更新
- 支持回滚

或使用命令行升级：
```bash
curl -sSL https://raw.githubusercontent.com/mizaawa/zayuapi/main/deploy/install.sh | sudo bash
```

#### 常用命令

```bash
# 查看状态
sudo systemctl status sub2api

# 查看日志
sudo journalctl -u sub2api -f

# 重启服务
sudo systemctl restart sub2api

# 卸载
curl -sSL https://raw.githubusercontent.com/mizaawa/zayuapi/main/deploy/install.sh | sudo bash -s -- uninstall -y
```

---

### 方式三：从源码构建

适合开发或自定义需求。

#### 系统要求

- Go 1.21+
- Node.js 18+
- PostgreSQL 15+
- Redis 7+

#### 构建步骤

```bash
# 1. 克隆仓库
git clone https://github.com/mizaawa/zayuapi.git
cd zayuapi

# 2. 安装 pnpm
npm install -g pnpm

# 3. 构建前端
cd frontend
pnpm install
pnpm run build
# 输出到 ../backend/internal/web/dist/

# 4. 构建后端（包含嵌入式前端）
cd ../backend
VERSION="$(./scripts/resolve-version.sh)"
go build -tags embed -ldflags="-X main.Version=${VERSION}" -o sub2api ./cmd/server

# 5. 创建配置文件
cp ../deploy/config.example.yaml ./config.yaml

# 6. 编辑配置
nano config.yaml
```

> **注意**：`-tags embed` 标志会将前端嵌入到二进制文件中。没有此标志，二进制文件将无法提供前端 UI。

**关键配置（config.yaml）**：

```yaml
server:
  host: "0.0.0.0"
  port: 8080
  mode: "release"

database:
  host: "localhost"
  port: 5432
  user: "postgres"
  password: "your_password"
  dbname: "sub2api"

redis:
  host: "localhost"
  port: 6379
  password: ""

jwt:
  secret: "change-this-to-a-secure-random-string"
  expire_hour: 24

default:
  user_concurrency: 5
  user_balance: 0
  api_key_prefix: "sk-"
  rate_multiplier: 1.0
```

**安全相关配置**（可选）：

```yaml
security:
  url_allowlist:
    enabled: false                      # 禁用 URL 白名单检查
    allow_insecure_http: false          # 仅允许 HTTPS（生产环境推荐）
    allow_private_hosts: false          # 禁止私有/本地 IP 地址

cors:
  allowed_origins: ["*"]                # CORS 允许来源

turnstile:
  required: false                       # 是否需要 Turnstile 验证
```

#### ⚠️ 重要：创建管理员账号

初始管理员账号**仅通过设置向导创建**（首次运行时在 `http://<host>:8080` 提供）。`config.yaml` 中的 `default.admin_email` / `default.admin_password` 字段**不会**用于创建账号。

因为上面第 5 步预先创建了 `config.yaml`，首次运行时会**跳过设置向导**：服务器检测到已有配置，直接进入正常模式，此时 `users` 表为空，首次登录会失败并显示 `invalid email or password`。

**两种创建管理员账号的方式**：

1. **推荐 - 让向导生成 config.yaml**：跳过第 5 步（不执行 `cp` 命令）。直接启动 `./sub2api`；设置向导会在 `http://localhost:8080` 引导您完成数据库、Redis 和管理员账号设置，然后为您生成 `config.yaml`。

2. **如果已创建 config.yaml**：临时移开它以触发向导，完成后再恢复：
   ```bash
   mv config.yaml config.yaml.bak
   ./sub2api        # 向导在 http://localhost:8080 运行并生成新的 config.yaml
   # 完成向导后停止服务器（Ctrl+C），然后恢复配置：
   mv config.yaml.bak config.yaml
   ./sub2api        # 以正常模式重启并使用刚创建的管理员登录
   ```

```bash
# 6. 运行应用
./sub2api
```

#### 开发模式

```bash
# 后端（热重载）
cd backend
go run ./cmd/server

# 前端（热重载）- 新终端
cd frontend
pnpm run dev
```

#### 代码生成

编辑 `backend/ent/schema` 后，需要重新生成 Ent + Wire：

```bash
cd backend
go generate ./ent
go generate ./cmd/server
```

---

## 功能说明

### 视频按秒计费

#### 使用场景
- 视频生成服务（如 Sora、Runway）
- 视频处理 API
- 视频分析服务
- 任何需要按时长计费的服务

#### 配置步骤

1. 登录管理后台
2. 进入"渠道管理" → 选择渠道 → "模型定价"
3. 点击"添加定价规则"
4. 在"计费模式"下拉框中选择"视频（按秒）"
5. 设置默认每秒价格
6. （可选）添加阶梯定价

#### 阶梯定价示例

| 秒数范围 | 每秒价格 |
|---------|---------|
| 0-10秒  | $0.10   |
| 10-30秒 | $0.08   |
| 30秒以上 | $0.05   |

#### API 使用

请求体中必须包含 `seconds` 字段：

```json
{
  "model": "video-model",
  "seconds": 25
}
```

**计费示例**：
- 5秒视频 × $0.10/秒 = $0.50
- 25秒视频 × $0.08/秒 = $2.00（命中第二档）
- 60秒视频 × $0.05/秒 = $3.00（命中第三档）

---

### 请求头覆写

#### 使用场景
- 自定义 User-Agent
- 添加自定义认证头
- 修改 Content-Type
- 注入追踪头（如 X-Request-ID）

#### 配置步骤

1. 进入"账号管理"
2. 选择任意 API Key 类型账号
3. 在"请求头覆写"部分启用
4. 添加需要覆写的请求头

#### 支持的账号类型

- ✅ **所有平台**的 API Key 账号
- ✅ Anthropic / OpenAI / Google / 其他所有平台

#### 限制说明

为保证 HTTP 协议正常工作，以下请求头仍不可覆写：
- `content-length`
- `transfer-encoding`
- `connection`

其他所有请求头均可自由覆写。

---

## 配置说明

### 环境变量配置（Docker）

在 `.env` 文件中配置：

```bash
# 数据库配置
POSTGRES_PASSWORD=your_secure_password

# JWT 配置
JWT_SECRET=your_jwt_secret
JWT_EXPIRE_HOUR=24

# TOTP 加密
TOTP_ENCRYPTION_KEY=your_totp_key

# 管理员账号（可选）
ADMIN_EMAIL=admin@example.com
ADMIN_PASSWORD=your_admin_password

# 服务器配置
SERVER_PORT=8080
SERVER_HOST=0.0.0.0
```

### 配置文件（二进制部署）

在 `config.yaml` 中配置，详细选项请参考 `deploy/config.example.yaml`。

---

## 升级指南

### 从官方版本升级

1. **备份数据库**：
   ```bash
   pg_dump -U postgres sub2api > backup.sql
   ```

2. **停止官方版本服务**：
   ```bash
   docker compose down
   # 或
   sudo systemctl stop sub2api
   ```

3. **部署 mizaawa 版本**（使用上述任一方式）

4. 系统会自动运行数据库迁移，添加新字段

### 切换回官方版本

完全兼容！直接切换即可，新增字段不会影响官方版本运行。

```bash
# Docker 方式
docker compose down
# 修改 docker-compose.yml 中的镜像为官方版本
# image: ghcr.io/wei-shaw/sub2api:latest
docker compose up -d

# 二进制方式
# 下载官方版本替换即可
```

---

## 常见问题

### Q: 如何从官方版本迁移？

A: 直接使用 mizaawa 版本的镜像或二进制替换即可，数据库会自动迁移。

### Q: 视频计费的 seconds 参数必须是整数吗？

A: 是的，目前只支持整数秒数。

### Q: 可以同时使用多种计费模式吗？

A: 可以，每个模型定价规则可以独立设置计费模式。

### Q: 请求头覆写对所有模型生效吗？

A: 是的，在账号级别配置，对该账号的所有请求生效。

### Q: 数据会丢失吗？

A: 不会，所有改动都是向后兼容的。可以随时切换回官方版本。

### Q: Nginx 反向代理需要特殊配置吗？

A: 需要启用下划线请求头支持：

```nginx
http {
    underscores_in_headers on;
    
    server {
        listen 80;
        server_name your-domain.com;
        
        location / {
            proxy_pass http://localhost:8080;
            proxy_set_header Host $host;
            proxy_set_header X-Real-IP $remote_addr;
            proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
            proxy_set_header X-Forwarded-Proto $scheme;
        }
    }
}
```

---

## 技术栈

| 组件 | 技术 |
|------|-----|
| 后端 | Go 1.25.7, Gin, Ent |
| 前端 | Vue 3.4+, Vite 5+, TailwindCSS |
| 数据库 | PostgreSQL 15+ |
| 缓存/队列 | Redis 7+ |

---

## 项目结构

```
zayuapi/
├── backend/                  # Go 后端服务
│   ├── cmd/server/           # 应用入口
│   ├── internal/             # 内部模块
│   │   ├── config/           # 配置
│   │   ├── model/            # 数据模型
│   │   ├── service/          # 业务逻辑
│   │   ├── handler/          # HTTP 处理器
│   │   └── gateway/          # API 网关核心
│   └── migrations/           # 数据库迁移
│
├── frontend/                 # Vue 3 前端
│   └── src/
│       ├── api/              # API 调用
│       ├── stores/           # 状态管理
│       ├── views/            # 页面组件
│       └── components/       # 可复用组件
│
└── deploy/                   # 部署文件
    ├── docker-compose.yml    # Docker Compose 配置
    ├── .env.example          # 环境变量示例
    └── config.example.yaml   # 完整配置示例
```

---

## 简单模式

简单模式专为个人开发者或内部团队设计，无需完整的 SaaS 功能即可快速访问。

- 启用：设置环境变量 `RUN_MODE=simple`
- 区别：隐藏 SaaS 相关功能并跳过计费流程
- 安全提示：生产环境必须同时设置 `SIMPLE_MODE_CONFIRM=true` 才能启动

---

## 许可证

本项目基于 [GNU Lesser General Public License v3.0](LICENSE)（或更高版本）开源。

Copyright (c) 2026 Wesley Liddick

---

## 相关链接

- **本仓库**：https://github.com/mizaawa/zayuapi
- **原项目**：https://github.com/Wei-Shaw/sub2api
- **问题反馈**：https://github.com/mizaawa/zayuapi/issues

---

<div align="center">

**如果觉得这个项目有用，请给个 Star！⭐**

</div>