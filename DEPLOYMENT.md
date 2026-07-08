# zayuapi 部署指南（mizaawa 定制版本）

本文档详细说明 zayuapi（基于 sub2api 的定制版本）的部署流程和新增特性。

## 版本特性

### 相比官方版本的改进

1. **视频按秒计费模式**
   - 新增"视频（按秒）"计费选项
   - 支持阶梯定价（类似图片模式）
   - 从请求体 `seconds` 字段读取秒数并计费

2. **请求头覆写全面开放**
   - 移除了 Claude/OpenAI 平台限制
   - 所有 API Key 类型账号均可使用
   - 仅保留最基本的 HTTP 协议头限制

3. **数据库完全兼容**
   - 可无缝切换回官方版本
   - 使用 `ALTER TABLE ... ADD COLUMN IF NOT EXISTS` 确保安全升级

## 部署方式

### 方式一：Docker Compose（推荐）

#### 快速部署

```bash
# 创建部署目录
mkdir -p zayuapi-deploy && cd zayuapi-deploy

# 下载部署脚本
curl -sSL https://raw.githubusercontent.com/mizaawa/zayuapi/main/deploy/docker-deploy.sh | bash

# 启动服务
docker compose up -d

# 查看日志
docker compose logs -f sub2api
```

#### 手动部署

```bash
# 克隆仓库
git clone https://github.com/mizaawa/zayuapi.git
cd zayuapi/deploy

# 复制配置文件
cp .env.example .env

# 编辑配置（必须设置 POSTGRES_PASSWORD 等）
nano .env

# 启动服务
docker compose -f docker-compose.local.yml up -d
```

**重要配置项：**

```bash
# PostgreSQL 密码（必填）
POSTGRES_PASSWORD=your_secure_password

# JWT 密钥（推荐设置，保持用户登录状态）
JWT_SECRET=your_jwt_secret

# TOTP 加密密钥（推荐设置，保持 2FA 配置）
TOTP_ENCRYPTION_KEY=your_totp_key

# 管理员账号（可选）
ADMIN_EMAIL=admin@example.com
ADMIN_PASSWORD=your_admin_password

# 服务端口（可选）
SERVER_PORT=8080
```

**生成安全密钥：**

```bash
# JWT Secret
openssl rand -hex 32

# TOTP Encryption Key
openssl rand -hex 32

# PostgreSQL Password
openssl rand -hex 32
```

#### 访问服务

打开浏览器访问：`http://YOUR_SERVER_IP:8080`

如果使用自动生成的管理员密码，可通过日志查看：

```bash
docker compose logs sub2api | grep "admin password"
```

#### 升级

```bash
# 拉取最新镜像
docker compose pull

# 重新创建容器
docker compose up -d
```

#### 数据迁移

使用 `docker-compose.local.yml` 时，数据存储在本地目录，迁移非常简单：

```bash
# 源服务器
docker compose down
cd ..
tar czf zayuapi-complete.tar.gz zayuapi-deploy/

# 传输到新服务器
scp zayuapi-complete.tar.gz user@new-server:/path/

# 新服务器
tar xzf zayuapi-complete.tar.gz
cd zayuapi-deploy/
docker compose up -d
```

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
# 启动服务
sudo systemctl start sub2api

# 设置开机自启
sudo systemctl enable sub2api

# 访问设置向导
# http://YOUR_SERVER_IP:8080
```

设置向导将引导您完成：
- 数据库配置
- Redis 配置
- 管理员账号创建

#### 升级

可以直接在管理后台点击"检查更新"按钮进行升级，或使用命令：

```bash
# 重新运行安装脚本即可升级
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

### 方式三：从源码构建

适合开发或自定义需求。

#### 系统要求

- Go 1.21+
- Node.js 18+
- PostgreSQL 15+
- Redis 7+
- pnpm

#### 构建步骤

```bash
# 克隆仓库
git clone https://github.com/mizaawa/zayuapi.git
cd zayuapi

# 安装 pnpm
npm install -g pnpm

# 构建前端
cd frontend
pnpm install
pnpm run build

# 构建后端（包含嵌入式前端）
cd ../backend
VERSION="$(./scripts/resolve-version.sh)"
go build -tags embed -ldflags="-X main.Version=${VERSION}" -o sub2api ./cmd/server

# 创建配置文件
cp ../deploy/config.example.yaml ./config.yaml

# 编辑配置
nano config.yaml
```

**关键配置：**

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
```

**启动服务：**

```bash
./sub2api
```

**开发模式：**

```bash
# 后端热重载
cd backend
go run ./cmd/server

# 前端热重载（新终端）
cd frontend
pnpm run dev
```

## 新功能使用指南

### 1. 视频按秒计费

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

**计费示例：**
- 5秒视频 × $0.10/秒 = $0.50
- 25秒视频 × $0.08/秒 = $2.00（命中第二档）
- 60秒视频 × $0.05/秒 = $3.00（命中第三档）

### 2. 请求头覆写（已全面开放）

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

## 数据库迁移

### 从官方版本升级

1. 备份数据库：
```bash
pg_dump -U postgres sub2api > backup.sql
```

2. 停止官方版本服务

3. 部署 mizaawa 版本（使用上述任一方式）

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

## Nginx 反向代理配置

如果使用 Nginx 作为反向代理，需要添加以下配置以支持下划线请求头：

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

A: 不会，所有改动都是向后兼容的。

## 技术支持

如有问题，请访问：
- GitHub Issues: https://github.com/mizaawa/zayuapi/issues
- 原项目文档: https://github.com/Wei-Shaw/sub2api

## 许可证

本项目基于 [GNU Lesser General Public License v3.0](LICENSE) 开源。

---

**最后更新：2026-07-08**