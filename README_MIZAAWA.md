# zayuapi - mizaawa 定制版本

> 基于 [Wei-Shaw/sub2api](https://github.com/Wei-Shaw/sub2api) 的增强定制版本

## 🎯 主要改进

### ✨ 新增功能

#### 1. 视频按秒计费模式
- **位置**：渠道管理 → 模型定价 → 计费模式 → "视频（按秒）"
- **功能**：
  - 从请求体 `seconds` 字段读取秒数
  - 支持默认单价和阶梯定价
  - 示例：5秒 × 3元/秒 = 扣费15元
  
**阶梯定价示例**：
```
0-10秒:   $0.10/秒
10-30秒:  $0.08/秒
30秒以上: $0.05/秒
```

**API 请求示例**：
```json
{
  "model": "video-model",
  "seconds": 25
}
```

### 🔓 移除限制

#### 2. 请求头覆写全面开放
- **原版限制**：仅 Claude/OpenAI 平台可用
- **现已支持**：所有平台的 API Key 账号
- **保留限制**：仅保留 HTTP 协议必需头（`content-length`、`transfer-encoding`、`connection`）

### 🔄 数据库兼容

- ✅ 完全兼容官方版本数据库
- ✅ 可无缝切换回官方版本
- ✅ 使用安全的增量迁移（`ALTER TABLE ... IF NOT EXISTS`）

## 📦 快速部署

### Docker Compose（推荐）

```bash
# 一键部署
mkdir -p zayuapi-deploy && cd zayuapi-deploy
curl -sSL https://raw.githubusercontent.com/mizaawa/zayuapi/main/deploy/docker-deploy.sh | bash
docker compose up -d
```

### 脚本安装

```bash
curl -sSL https://raw.githubusercontent.com/mizaawa/zayuapi/main/deploy/install.sh | sudo bash
```

### 手动部署

```bash
git clone https://github.com/mizaawa/zayuapi.git
cd zayuapi/deploy
cp .env.example .env
# 编辑 .env 设置密码
docker compose -f docker-compose.local.yml up -d
```

## 🔧 技术细节

### 后端改动

| 文件 | 改动内容 |
|------|---------|
| `backend/internal/service/channel.go` | 新增 `BillingModeVideo` 常量，`PricingInterval` 结构体新增 `VideoPerSecPrice` 字段 |
| `backend/internal/service/billing_service.go` | 新增 `calculateVideoCost` 函数，支持阶梯定价 |
| `backend/internal/service/model_pricing_resolver.go` | 新增 `applyVideoOverrides` 和 `filterValidIntervalsForVideo` |
| `backend/internal/service/account_header_override.go` | 移除平台限制，简化黑名单 |
| `backend/migrations/170_add_video_per_sec_price.sql` | 数据库迁移文件 |

### 前端改动

| 文件 | 改动内容 |
|------|---------|
| `frontend/src/constants/channel.ts` | 导出 `BILLING_MODE_VIDEO` |
| `frontend/src/components/admin/channel/PricingEntryCard.vue` | 新增视频模式 UI，包括阶梯定价 |
| `frontend/src/components/admin/channel/IntervalRow.vue` | 支持视频按秒价格输入 |
| `frontend/src/components/admin/channel/types.ts` | 更新接口定义 |
| `frontend/src/i18n/locales/zh/admin/channels.ts` | 中文翻译 |
| `frontend/src/i18n/locales/en/admin/channels.ts` | 英文翻译 |

### Docker 改动

| 文件 | 改动内容 |
|------|---------|
| `deploy/docker-compose.yml` | 镜像改为 `ghcr.io/mizaawa/zayuapi` |
| `deploy/docker-compose.local.yml` | 镜像改为 `ghcr.io/mizaawa/zayuapi` |
| `README.md` | 更新所有仓库引用 |

## 📚 完整文档

详细部署和使用指南请查看：[DEPLOYMENT.md](DEPLOYMENT.md)

## 🔄 从官方版本迁移

### 升级到 mizaawa 版本

```bash
# 1. 备份数据库
pg_dump -U postgres sub2api > backup.sql

# 2. 停止官方版本
docker compose down  # 或 systemctl stop sub2api

# 3. 使用 mizaawa 版本镜像启动
# 修改 docker-compose.yml:
# image: ghcr.io/mizaawa/zayuapi:latest

docker compose up -d

# 数据库会自动迁移！
```

### 切换回官方版本

```bash
# 完全兼容，直接切换镜像即可
docker compose down

# 修改 docker-compose.yml:
# image: ghcr.io/wei-shaw/sub2api:latest

docker compose up -d
```

## ⚠️ 重要说明

### 数据库兼容性

- ✅ 新增字段使用 `ADD COLUMN IF NOT EXISTS`，可安全执行多次
- ✅ 不修改任何现有字段，不影响官方版本
- ✅ 新增字段允许为 NULL，不破坏现有数据

### 安全性

- 请求头覆写移除了大部分黑名单限制
- 如有安全顾虑，可在 Nginx/网关层进行额外过滤
- 保留了 HTTP 协议必需的头部限制

## 🛠️ 开发

```bash
# 克隆仓库
git clone https://github.com/mizaawa/zayuapi.git
cd zayuapi

# 前端开发
cd frontend
pnpm install
pnpm run dev

# 后端开发
cd backend
go run ./cmd/server

# 构建
cd frontend && pnpm run build
cd ../backend && go build -tags embed -o sub2api ./cmd/server
```

## 📝 待办事项

- [ ] 构建并推送 Docker 镜像到 GitHub Container Registry
- [ ] 创建 GitHub Release
- [ ] 添加 CI/CD 工作流
- [ ] 更新中文和日文 README

## 📄 许可证

继承原项目许可证：[GNU Lesser General Public License v3.0](LICENSE)

## 🙏 致谢

感谢 [Wei-Shaw/sub2api](https://github.com/Wei-Shaw/sub2api) 提供优秀的开源项目基础。

---

**仓库地址**：https://github.com/mizaawa/zayuapi  
**原项目地址**：https://github.com/Wei-Shaw/sub2api