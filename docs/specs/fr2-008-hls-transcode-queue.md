# 功能规格：视频 HLS 预览与转码任务队列

> 状态：已验收　·　关联 PRD：FR2-008　·　阶段：P2 `0.23.x`　·　分支：`feature/p2-fr2-008-complete`

## 1. 背景与目标

系统已有 HLS 产物读写、`master.m3u8`、播放页 HLS 探测和旧预生成队列，但转码仍是局部队列，缺少统一优先级、重试、取消、Space、缓存资产登记和队列监控。P2 要把 HLS 预览与转码队列接入 FR2-037，并确保失败可重试、状态可查、产物可重建。

目标：

- 将 HLS 预览/预转码任务接入统一任务队列。
- 统一 HLS 产物登记到 FR2-048 缓存资产模型。
- 保持播放直连优先，HLS 作为预览/兼容路径。

前置依赖：FR2-007（Space/索引）、FR2-017（迁移框架）、FR2-037（任务队列）、FR2-040（审计核心）、FR2-048（缓存资产模型）。

## 2. 需求（要什么）

- 视频入库或用户触发后可创建 HLS 预览任务。
- 任务支持优先级、失败重试、取消、进度、错误摘要。
- HLS 产物按 Space/media/profile 管理，写缓存资产登记。
- 播放协商优先直连；不能直连或用户选择 HLS 时走已生成 HLS；未生成时返回任务状态或触发生成。
- 旧 `/api/transcode/tasks` 兼容，内部映射到统一任务中心。
- 旧 `/api/play/hls/:id/master(.m3u8)` 与 variant URL 保持兼容，映射到默认 HLS profile。
- 范围内：HLS 任务化、API 兼容、缓存登记、队列监控、测试素材。
- 不做（范围外）：完整多码率策略（FR2-026）、硬件选择面板（FR2-056）、字幕/音轨高级选择。

## 3. 设计（怎么做）

任务：

- `transcode.hls.preview`：生成单档或预览 HLS。
- payload：`media_id`、`profile_id`、`priority`、`force_rebuild`。
- progress 根据 ffmpeg 输出或已生成分片数更新。

产物：

- HLS 目录结构包含 Space/media/profile，避免不同配置互相覆盖。
- 生成成功后登记 `cache_assets(kind=hls)`。
- 默认 profile 继续服务现有 `/api/play/hls/:id/master(.m3u8)` 探测路径；新 profile 通过显式状态/协商接口访问。
- 重建前不直接 `RemoveAll` 整个 media 目录，应通过缓存服务安全清理。

API：

- 保留 `POST /api/transcode/tasks`，返回统一 `task_id`。
- 保留旧 HLS 播放 URL，内部按默认 profile 定位新 Space/media/profile 目录。
- 新增或扩展 `GET /api/play/:id/hls-status` 返回 HLS 可用性与任务。

## 4. 任务拆分

- [x] 定义 HLS preview profile 与任务 payload。
- [x] 将旧预生成队列适配到统一任务中心。
- [x] 生成 HLS 时登记缓存资产。
- [x] 播放协商接入 HLS 状态查询。
- [x] 保留旧转码任务 API 兼容层。
- [x] 补单元测试：ffmpeg 参数、任务 payload、错误映射。
- [x] 补集成测试：入队、生成、失败重试、取消、缓存登记。
- [x] 补 E2E：转码页入队、任务进度、播放页 HLS fallback。
- [x] 文档同步：PRD 状态、ARCHITECTURE、API、CHANGELOG。

## 5. 验收标准

- HLS 预览任务通过统一任务 API 可见，可取消、失败可重试。
- HLS 生成成功后有缓存资产记录，清理后可重建。
- 播放仍直连优先，HLS fallback 不破坏现有可播路径。
- 旧 `/api/play/hls/:id/master(.m3u8)` 在默认 profile 下仍可用。
- 旧转码任务接口兼容现有前端。
- `go test`、转码集成测试、Playwright 播放/任务 E2E、`pnpm run quality` 全绿。
- Go 单二进制 serve 后，用测试视频生成 HLS 并播放实跑通过。

## 6. 风险 / 待定

- 已确认：HLS preview 先按需/手动触发，避免大库首次扫描产生高成本任务洪峰。
- 多码率 ladder 由 FR2-026 定义，本规格只保证队列与单档预览路径。
