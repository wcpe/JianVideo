# 功能规格：分享增强（P5）

> 状态：开发中（首切后端 + 二切前端 UI 已落地；accessed 采样仍待）　·　关联 PRD：FR2-055　·　阶段：P5 `0.26.x`　·　背景：历史 [share-links](share-links.md)、[share-enhance](share-enhance.md)（FR-43/FR-78，不可直接当 v2 排期）　·　前置：FR2-010 成员守卫已落地

## 0. 首切范围（本 PR 只做这些）

| 切片 | 内容 | 首切 |
|------|------|------|
| A | 模型 `allow_download`（默认 true）+ 迁移 | ✅ |
| B | 创建 API 接收 `allow_download`；公开 download 在 false 时 403/404 统一口径 | ✅ |
| C | 公开路径成本门禁：集成测试探针断言访问 share 期间不入队 `transcode`/`hls`/`export`/AI；缩略图缺失不 Enqueue | ✅ |
| D | 审计 `share.created` / `share.revoked`（accessed 采样二切） | ✅ |
| E | 文档：API/CHANGELOG | ✅ |
| F | 前端创建弹窗禁下载 + 公开页隐藏下载 | ✅ 二切 |

**现状**：密码/过期/限次/`space_id`/CreateShare Space 校验/editor 写守卫已有；缺 `allow_download` 与成本门禁自动化断言。

## 1. 背景与目标

代码已具备：分享 token、过期、密码、访问限次、Space 字段、公开只读原文件流（不经转码管线）。P5 要在 **v2 Space 多用户** 下把分享增强收口为产品契约，并堵死匿名高成本路径。
目标：

- 密码 / 有效期 / **禁下载** / 限次（已有则对齐 API 与 UI）。
- 创建与访问均强制 Space 范围；成员权限见 FR2-010。
- **匿名分享不得**触发 HLS/ABR 转码、AI 推理、批量重建、导出队列等高成本任务。

## 2. 需求（要什么）

### 2.1 范围内

- 分享模型扩展（若缺）：
  - `allow_download`（默认 true；false 时公开 download 端点 403/404 统一口径）。
  - 保持 `password_hash` / `max_uses` / `used_count` / `expires_at` / `space_id`。
- 创建分享：校验资源属于当前 Space；调用方需 editor+。
- 公开访问：
  - 继续免登；密码与限次语义与现网一致（实际拉资源才计次）。
  - **禁止**路由转发到 `/api/play/*/hls*`、转码入队、thumbnail 批量重建、AI、export。
  - 允许：元信息、原文件 stream/raw、（若 allow_download）download、已有静态缩略图文件读取（不触发生成队列则允许；若会入队则改为占位图）。
- 管理：列表/撤销 Space 过滤；审计 `share.created/revoked/accessed`（accessed 可采样）。
- 前端：创建弹窗含密码、过期、限次、禁下载；公开页尊重禁下载。

### 2.2 不做

- 可写分享、评论、邮件发送。
- 按访客身份的细粒度 ACL。
- 公开路径启用 ffmpeg 转码「方便播放」。

## 3. 设计（怎么做）

- 路由层：公开 share 组与鉴权 API 完全分离；集成测试断言调用转码 service 次数为 0。
- 缩略图：仅 `os.Stat` 已存在缓存；缺失返回占位，不 `Enqueue`。
- 权限：`CreateShare` 走 SpaceRole；公开路径只信 token 范围。

## 4. 任务拆分

- [x] 模型 `allow_download` + 迁移
- [x] 公开 download 门禁 + CreateShare 字段
- [x] 公开缩略图仅已有缓存（不入队）+ 单测
- [x] 审计 created/revoked
- [x] 文档同步
- [x] 二切 UI：创建弹窗「允许下载」开关；公开页按 `allow_download` 隐藏下载
- [ ] 二切：accessed 采样
## 5. 验收标准

- 自动化：公开分享访问期间 `transcode`/`hls` 任务计数不增加。
- `allow_download=false` 时 download 失败且 UI 无下载按钮。
- 非 Space 成员不能对他人资源创建分享。
- 密码错误与过期对外文案不区分敏感细节（与现网一致）。

## 6. 风险 / 待定

- 部分格式仅转码可播：产品文案引导下载（若允许）或「仅支持直连格式在线播」。
