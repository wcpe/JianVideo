# 功能规格：多媒体库分型

> 状态：已审核接受　·　关联 PRD：FR2-052　·　阶段：P2 `0.23.x`　·　分支：待定

## 1. 背景与目标

当前媒体库只有来源类型 `local/smb` 和用户 label，无法表达电影、剧集、家庭录像等内容分型。FR2-031 的离线影视推断、FR2-025 的后缀规则、FR2-027 的扫描策略、FR2-047 的剧集连播都依赖稳定的库分型。P2 需要先把库分型落到 schema、API、UI 与扫描上下文。

目标：

- 为每个媒体库增加内容类型 `library_kind`。
- 不改变来源类型 `local/smb` 的语义。
- 让扫描、命名解析、离线推断和后缀规则能按库分型选择策略。

前置依赖：FR2-007（Space/索引）、FR2-017（迁移框架）、FR2-024（配置 registry）。本规格先落最小 `library_kind` 上下文；FR2-025 再消费该上下文扩展媒体类型规则；FR2-027 同时依赖二者。

## 2. 需求（要什么）

- 首批库分型：`movie`、`series`、`home_video`、`mixed`。
- 新增/编辑媒体库时可选择分型；旧库默认 `mixed`。
- 每种分型有中文名称、说明、默认命名规则提示和推荐扫描策略。
- 扫描上下文携带 `library_kind`，供 FR2-031、FR2-030、FR2-027 使用。
- `series` 支持目录/文件名中的季、集解析入口；本规格只提供上下文，不实现完整推断。
- API 与前端保持向后兼容，`type=local/smb` 不被改名。
- 范围内：schema、API、UI、扫描上下文、规则说明、测试。
- 不做（范围外）：完整合集/播放列表、自动连播、联网刮削、复杂命名模板语言。

## 3. 设计（怎么做）

Schema：

- `library_paths.library_kind`：字符串，默认 `mixed`。
- 可选 `library_profile_json`：保存后续分型配置；本期只存最小策略字段。

API：

- `GET/POST/PUT /api/library/paths` 返回和接收 `library_kind`。
- 新增 `GET /api/library/kinds` 返回内置分型说明。

扫描：

- `ScanLibrary` 读取库分型并传入 metadata/inference 上下文。
- 后缀规则解析在 FR2-025 落地后使用全局规则 + 每库覆盖；本规格只保证扫描上下文能传递 `library_kind`。

前端：

- 媒体库管理页新增分型选择和中文说明。
- 已有库列表展示来源类型与内容分型，避免混淆。

## 4. 任务拆分

- [ ] 增加 `library_kind` schema 与默认值迁移。
- [ ] 扩展库路径 API 与前端类型定义。
- [ ] 增加内置分型说明 API。
- [ ] 媒体库管理 UI 增加分型选择。
- [ ] 扫描服务向元数据/推断链路传递分型上下文。
- [ ] 补单元测试：分型校验、默认 mixed、非法值拒绝。
- [ ] 补集成测试：旧库迁移默认值、新增/编辑库分型持久化。
- [ ] 补 E2E：创建电影/剧集/家庭录像库并刷新展示。
- [ ] 文档同步：PRD 状态、ARCHITECTURE、API、CHANGELOG。

## 5. 验收标准

- 旧媒体库升级后 `library_kind=mixed`，不影响原扫描。
- 新增/编辑媒体库可以设置并回读 `movie/series/home_video/mixed`。
- 扫描上下文能在测试中证明携带正确 `library_kind`。
- 前端清楚区分来源类型与内容分型。
- `go test`、前端测试、Playwright 媒体库管理 E2E、`pnpm run quality` 全绿。

## 6. 风险 / 待定

- 已确认：默认分型为 `mixed`，避免旧库被错误套用影视规则。
- 已确认：本规格不做每库自定义命名规则模板语言，仅预留 profile。
