# 功能规格：存储与缓存管理

> 状态：已审核接受　·　关联 PRD：FR2-048　·　阶段：P2 `0.23.x`　·　分支：待定

## 1. 背景与目标

系统已有 `thumbnails/`、`image_cache/`、`hls/` 等可重建产物目录，但缺少统一缓存资产清单、占用统计、安全清理白名单、重建入口和审计。P2 的 HLS、分档缩略图、智能封面、代理产物都依赖同一“可重建数据”模型，否则容易误删原文件或让不同模块互相覆盖缓存。

目标：

- 区分可信源数据与可重建缓存数据。
- 建立缓存资产登记、统计、清理、重建和审计能力。
- 一键清理只作用于白名单缓存，不触碰原媒体、sidecar、审计和数据库。

前置依赖：FR2-007（Space/索引）、FR2-017（迁移框架）、FR2-037（任务队列）、FR2-040（审计核心）。FR2-008、FR2-026、FR2-028、FR2-059 必须消费本规格的缓存登记模型。

## 2. 需求（要什么）

- 缓存类型首批：`thumbnail`、`hls`、`image_proxy`、`cover`、`metadata_temp`。
- 记录缓存资产：Space、media、library、类型、profile/key、相对路径、大小、文件数、创建/访问时间、可重建状态。
- 提供按类型/Space/库/媒体统计占用。
- 清理操作走任务队列，支持预览影响范围、执行、进度、取消、审计。
- 所有删除必须经过安全路径白名单，禁止删除数据目录外文件，禁止删除原媒体。
- 清理后相关媒体可按需重建缩略图/HLS/封面。
- 范围内：缓存资产 model、盘点、统计、清理任务、安全白名单、基础 UI、测试。
- 不做（范围外）：自动复杂淘汰策略、跨磁盘迁移、云对象存储、原文件空间管理。

## 3. 设计（怎么做）

Schema：

- `cache_assets`：`id`、`space_id`、`library_id`、`media_id`、`kind`、`asset_level`（`file` / `directory`）、`profile_id`、`variant`、`cache_key`、`relative_path`、`size_bytes`、`file_count`、`rebuildable`、`created_at`、`accessed_at`、`missing_at`。
- HLS 缓存按 `space_id/media_id/profile_id/variant` 目录级登记，记录 aggregate `size_bytes` 与 `file_count`，不按每个 segment 写一行。
- 缩略图、封面、图片代理按文件级登记。
- 索引：`(space_id, kind)`、`(library_id, kind)`、`(media_id, kind)`、`(relative_path)`。

服务：

- 缓存登记 API 供缩略图、HLS、封面、图片代理模块调用。
- 盘点任务扫描白名单目录，补齐缺失登记并标记磁盘缺失。
- 清理任务先 dry-run 计算候选，再执行删除并写审计。
- `accessed_at` 不得在每个 HLS segment 请求上同步更新；如需访问时间，只能节流或批量刷新，避免 SQLite 写放大。

安全：

- 所有路径必须解析到数据目录下的白名单子目录。
- 删除前验证不是媒体库路径、不是数据库、不是 WAL/SHM、不是审计/备份目录。

API：

- `GET /api/storage/cache/summary`
- `GET /api/storage/cache/assets`
- `POST /api/storage/cache/inventory`
- `POST /api/storage/cache/clean`

## 4. 任务拆分

- [ ] 定义缓存资产模型、白名单与 repository。
- [ ] 接入缩略图、HLS、图片代理、封面缓存登记点。
- [ ] 实现缓存盘点和占用统计。
- [ ] 实现 dry-run 与清理任务。
- [ ] 前端增加缓存管理视图。
- [ ] 清理、盘点写审计事件。
- [ ] 补单元测试：路径白名单、统计聚合、dry-run。
- [ ] 补集成测试：生成缓存、盘点、清理、不删源文件、清理后可重建。
- [ ] 补 E2E：缓存管理页统计与清理流程。
- [ ] 补 Benchmark：大缓存资产表统计查询延迟。
- [ ] 文档同步：PRD 状态、ARCHITECTURE、API、CHANGELOG。

## 5. 验收标准

- 缓存统计按类型展示缩略图、HLS、图片代理、封面占用。
- HLS 以目录级资产统计，报告 `file_count` 和 aggregate `size_bytes`；不得为每个 segment 生成独立缓存行。
- 清理任务只删除白名单缓存路径，不删除原媒体、数据库、审计和备份。
- HLS segment 请求不会同步写 `accessed_at` 导致高频 SQLite 写入。
- 清理后访问缩略图/HLS/封面能触发重建或明确返回待生成状态。
- 清理动作、失败项和影响范围写审计事件。
- `go test`、集成测试、Playwright 缓存管理 E2E、Benchmark、`pnpm run quality` 全绿。
- Go 单二进制 serve 后，生成缓存、清理、重建实跑通过。

## 6. 风险 / 待定

- 已确认：本规格只做手动清理和盘点，自动保留策略后续增强。
- 缓存登记可能与已有磁盘文件不一致，首批必须提供盘点修复。
