# 变更日志 (CHANGELOG)

## [1.0.0-mizaawa] - 2026-07-08

### 🎉 初始发布

基于 Wei-Shaw/sub2api 官方版本的首个定制版本。

### ✨ 新增功能

#### 视频按秒计费模式
- 在渠道定价管理中新增"视频（按秒）"计费选项
- 支持从请求体 `seconds` 字段读取秒数
- 支持默认按秒价格配置
- 支持阶梯定价（类似图片模式）
- 前后端完整实现，包括 UI 和 API

**技术实现**：
- 后端新增 `BillingModeVideo` 常量
- 新增 `calculateVideoCost` 计费函数
- `PricingInterval` 结构体新增 `video_per_sec_price` 字段
- 数据库迁移：`170_add_video_per_sec_price.sql`
- 前端新增视频模式 UI 和阶梯定价界面

#### 请求头覆写全面开放
- 移除了仅限 Claude/OpenAI 平台的限制
- 现在所有平台的 API Key 账号均可使用请求头覆写功能
- 简化黑名单，仅保留 HTTP 协议必需头：
  - `content-length`
  - `transfer-encoding`
  - `connection`

**技术实现**：
- 修改 `IsHeaderOverrideEligible` 函数，移除平台判断
- 大幅简化 `headerOverrideBlockedNames` 黑名单
- 保持向后兼容性

### 🔄 Docker 部署优化

- 所有 Docker 配置文件更新为 mizaawa 仓库镜像
- `docker-compose.yml` 镜像改为 `ghcr.io/mizaawa/zayuapi`
- `docker-compose.local.yml` 镜像改为 `ghcr.io/mizaawa/zayuapi`
- README 中的所有部署脚本链接更新

### 📚 文档改进

- 新增 `DEPLOYMENT.md` 完整部署指南
- 新增 `README_MIZAAWA.md` 定制版本说明
- README.md 开头添加 mizaawa 版本特性说明
- 更新所有仓库链接到 mizaawa/zayuapi

### 🔧 数据库改动

#### 新增字段

**pricing_intervals 表**：
- `video_per_sec_price NUMERIC(20, 10)` - 视频模式每秒价格

**迁移策略**：
- 使用 `ALTER TABLE ... ADD COLUMN IF NOT EXISTS` 确保安全
- 字段允许 NULL，不影响现有数据
- 完全兼容官方版本，可无缝切换

### 🎨 前端改动

#### 新增组件功能

**PricingEntryCard.vue**：
- 视频模式默认价格输入
- 视频阶梯定价界面
- `addVideoTier()` 函数

**IntervalRow.vue**：
- 支持视频按秒价格输入
- 秒数范围输入（minSeconds/maxSeconds）
- 动态标签显示

#### 国际化

**中文 (zh)**：
- `defaultVideoPrice` - 默认每秒价格
- `videoTiers` - 视频阶梯定价
- `secondsThreshold` - 秒数阈值
- `pricePerSecond` - 每秒价格
- `minSeconds` - 最小秒数
- `maxSeconds` - 最大秒数

**英文 (en)**：
- 对应的英文翻译

### 🔒 兼容性保证

- ✅ 数据库向后兼容官方版本
- ✅ 可无缝切换回官方版本
- ✅ 不破坏现有配置和数据
- ✅ API 完全兼容现有客户端

### 📝 技术债务

以下为已知的技术债务，将在后续版本改进：

1. **Docker 镜像构建**
   - 需要设置 GitHub Actions 自动构建
   - 需要推送到 GitHub Container Registry
   
2. **测试覆盖**
   - 需要为视频计费功能添加单元测试
   - 需要为请求头覆写添加集成测试

3. **文档完善**
   - 需要更新中文 README (README_CN.md)
   - 需要更新日文 README (README_JA.md)

### 🚀 升级指南

#### 从官方版本升级

```bash
# 1. 备份数据库
pg_dump -U postgres sub2api > backup.sql

# 2. 更新 Docker 镜像
# docker-compose.yml 中修改：
# image: ghcr.io/mizaawa/zayuapi:latest

# 3. 重启服务
docker compose pull
docker compose up -d

# 数据库会自动迁移！
```

#### 切换回官方版本

```bash
# 直接修改镜像即可，完全兼容
# docker-compose.yml 中修改：
# image: ghcr.io/wei-shaw/sub2api:latest

docker compose pull
docker compose up -d
```

### ⚠️ 重要提示

1. **请求头覆写安全性**
   - 移除限制后，建议在网关层进行额外安全过滤
   - 生产环境需评估安全风险

2. **视频计费**
   - 当前仅支持整数秒数
   - 请确保客户端传递正确的 `seconds` 字段

3. **数据库迁移**
   - 迁移文件可安全执行多次
   - 建议在升级前备份数据库

---

## 版本规范

本项目采用以下版本号规范：

- 格式：`<major>.<minor>.<patch>-mizaawa`
- 例如：`1.0.0-mizaawa`
- 保留 `-mizaawa` 后缀以区分官方版本

### 版本号说明

- **Major（主版本号）**：不兼容的 API 改动
- **Minor（次版本号）**：向后兼容的功能新增
- **Patch（修订号）**：向后兼容的问题修复

---

**完整改动清单**：[GitHub Compare](https://github.com/mizaawa/zayuapi/compare/upstream...main)