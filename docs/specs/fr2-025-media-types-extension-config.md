# 功能规格：媒体类型与扫描后缀可配置

> 状态：已交付@v0.23.0　·　关联 PRD：FR2-025　·　阶段：P2 `0.23.x`　·　分支：待定

## 1. 背景与目标

当前系统已有视频/图片内置后缀和每库自定义后缀，但模型只覆盖 `video` / `image`，内置项不可统一展示为全局规则，自定义 image 后缀在部分筛选路径中也未完全对齐。P2 要支持多媒体库分型、元数据解析、转码和缩略图任务，必须先把“媒体类型、后缀、中文解释、默认扫描策略”定义为统一配置。

目标：

- 建立全局媒体类型与扩展名规则 registry。
- 支持内置常见视频/图片格式、用户增删自定义规则、中文说明。
- 扫描、列表筛选、缩略图、转码、元数据解析共用同一类型判断口径。
- 为 FR2-052 的电影/剧集/家庭录像库分型提供可继承规则。

前置依赖：FR2-007（Space/索引）、FR2-017（迁移框架）、FR2-024（配置 registry）、FR2-040（审计核心）。FR2-052 只提供最小 `library_kind` 上下文，本规格消费该上下文但不要求 FR2-052 先实现完整命名规则。

## 2. 需求（要什么）

- 内置媒体类型首批至少包含 `video`、`image`；预留 `audio`、`subtitle`、`sidecar` 但不要求本期完整处理。
- 每个类型有中文名称、说明、默认扩展名、是否可扫描、是否可转码、是否可缩略图、是否可提取技术元数据。
- 用户可新增/禁用自定义扩展名规则；内置规则不能物理删除，但可按配置禁用扫描。
- 规则支持全局默认 + 每库覆盖；每库覆盖可按 FR2-052 的 `library_kind` 调整扫描策略。
- 扫描、`type=` 筛选、统计、媒体列表、缩略图/HLS 任务使用同一规则服务。
- 配置变更写审计事件，并触发后续增量扫描建议，不自动全库重扫。
- 范围内：规则模型、API、前端配置、扫描/筛选口径收敛、测试。
- 不做（范围外）：完整插件式媒体类型、音频播放器、字幕库管理、自动识别所有未知格式。

## 3. 设计（怎么做）

Schema：

- `media_type_rules`：`id`、`space_id`、`library_id`（可空，空=全局）、`type`、`extension`、`label`、`description`、`enabled`、`builtin`、`capabilities_json`、`created_at`、`updated_at`。
- 唯一约束拆成两类，避免 SQLite 中 `NULL` 破坏全局唯一性：全局规则 `UNIQUE(space_id,type,extension) WHERE library_id IS NULL`；每库规则 `UNIQUE(space_id,library_id,type,extension) WHERE library_id IS NOT NULL`。

服务：

- 新增规则解析服务，提供 `ResolveRules(spaceID, libraryID)` 与 `ClassifyExtension(spaceID, libraryID, ext)`。
- 内置规则由代码注册，启动时不强制写库；API 返回内置规则虚拟 ID。禁用或覆盖内置规则时写 override 记录，不物理删除内置定义。
- 旧 `media_extensions` 表通过迁移或兼容层转入新规则模型。

API：

- `GET /api/media-types`
- `POST /api/media-types/rules`
- `PUT /api/media-types/rules/:id`
- `DELETE /api/media-types/rules/:id`（仅删除自定义规则；内置项改为禁用覆盖）
- 旧 `/api/library/extensions` 保留兼容，内部映射到新规则服务。

前端：

- 媒体库/设置页展示类型、后缀、中文解释、能力标签。
- 自定义后缀输入必须标准化为不带点的小写扩展名。

## 4. 任务拆分

- [ ] 定义媒体类型规则模型与内置 registry。
- [ ] 实现规则解析服务，替换扫描和筛选中的分散后缀判断。
- [ ] 新增媒体类型规则 API。
- [ ] 前端增加媒体类型/扫描后缀配置界面。
- [ ] 迁移或兼容旧 `media_extensions` 数据。
- [ ] 接入审计事件与配置变更提示。
- [ ] 补单元测试：扩展名归一、内置/自定义覆盖、禁用规则、能力判断。
- [ ] 补集成测试：扫描、列表 `type=` 筛选、统计与自定义 image 后缀一致。
- [ ] 补 E2E：新增后缀、禁用内置扫描、刷新后仍生效。
- [ ] 文档同步：PRD 状态、ARCHITECTURE、API、CHANGELOG。

## 5. 验收标准

- 自定义 image/video 后缀在扫描、列表筛选、统计、缩略图入口中口径一致。
- 内置后缀可禁用扫描但不能被物理删除。
- 旧 `/api/library/extensions` 兼容接口仍可读写既有每库后缀配置。
- API 返回中文名称、说明与能力标签，前端可展示并编辑自定义规则。
- 跨 Space 规则隔离；Space A 规则不影响 Space B。
- 配置变更产生审计事件。
- `go test`、前端测试、Playwright 配置 E2E、`pnpm run quality` 全绿。

## 6. 风险 / 待定

- 已确认：本规格只为 `audio` 类型建模型预留，不把音频播放纳入验收。
- 已确认：旧 `media_extensions` 迁移到新表，并保留只读兼容一版。
