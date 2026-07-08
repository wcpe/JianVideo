# 功能规格：配置分层边界

> 状态：已交付@v0.23.0　·　关联 PRD：FR2-024　·　阶段：P2 `0.23.x`　·　分支：`codex/fr2-024-config-boundary`

## 1. 背景与目标

当前系统已有两类配置来源：启动期 `config`/环境变量，以及运行期 SQLite `settings`。历史迭代中，`settings.SetMany` 可以写任意 key，前端设置页、工具路径、代理、扫描周期、调试日志等能力也在逐步扩展。P2 后续会新增任务队列、缓存、工具下载、转码、审计、媒体类型、库分型等配置，如果不先定义边界，会继续产生“哪些可热改、哪些必须重启、哪些敏感不可回显”的漂移。

目标：

- 建立配置分层边界表，明确启动固定项、运行期可改项、敏感项、派生只读项。
- 建立 typed registry，限制运行期设置只能写入已登记 key。
- 保持现有 `/api/settings` 兼容，同时为新配置提供校验、默认值、脱敏和审计事件接入点。
- 为 FR2-022、FR2-025、FR2-027、FR2-037、FR2-048、FR2-056 等后续配置提供统一落点。

## 2. 需求（要什么）

- 配置分类：
  - 启动固定项：监听地址/端口、数据库路径、JWT 密钥、SMB 主密码、数据目录等，启动后不可通过 Web 修改。
  - 运行期可改项：扫描周期、工具路径、代理、转码偏好、缓存保留策略、任务并发上限、媒体类型规则等。
  - 敏感项：密钥、密码、带凭据代理 URL 等，只允许写入或存在性展示，不允许明文回显。
  - 派生只读项：系统信息、工具版本、硬件能力、磁盘占用等，只能从系统状态接口读取。
- 新增设置 registry：包含 key、中文名称、分类、值类型、默认值、校验函数、是否敏感、是否允许热应用、消费模块。
- `GET /api/settings` 默认继续返回现有 map，新增结构化元数据接口供前端设置页渲染分组与校验提示。
- `PUT /api/settings` 仅允许写 registry 中 `runtime` 类 key；未知 key 返回 `400`，不得静默落库。
- 保存配置后触发对应 apply hook，失败时返回明确错误；不得出现“落库成功但运行期未生效且无提示”。
- apply hook 拆分为保存前 `Validate` 与提交后 `Apply`：可热应用的 key 必须先通过运行态校验；不可热应用的 key 必须标记为 `HotApply=false` 并返回“需重启”提示，不得假装已即时生效。
- 配置变更写 FR2-040 审计事件，敏感值只记录脱敏摘要。
- 文档列出所有配置边界表，并说明新增 key 的登记流程。
- 范围内：后端 registry、设置 API 兼容与增强、前端设置页按分类展示、审计接入点、文档边界表。
- 不做（范围外）：完整角色权限、外部配置中心、分布式动态配置、编辑环境变量文件、加密存储所有 settings。

## 3. 设计（怎么做）

后端以 `internal/settings` 为运行期设置真源，新增 registry 层：

- `SettingDefinition`：`Key`、`Label`、`Description`、`Layer`、`ValueType`、`DefaultValue`、`Sensitive`、`HotApply`、`Consumer`、`Validate`。
- `Layer` 枚举：`startup`、`runtime`、`readonly`。只有 `runtime` 可经 `PUT /api/settings` 写入；敏感性由 `Sensitive` 单独表达，不与分层混用。
- `ValueType` 枚举：`string`、`int`、`bool`、`json`、`url`、`path`、`enum`。
- `SetMany` 先按 registry 校验全部 key，全部通过后单事务写入；任何一项非法则整体失败。
- apply hook 仍由装配层注入，按 key 调度到扫描、转码、代理、日志等消费模块。保存前执行 `Validate`，事务提交后执行 `Apply`；若 `Apply` 失败，API 必须返回明确错误并记录审计/日志，且该 key 不得被标记为已热应用。

API 增强：

- 保留 `GET /api/settings` 和 `PUT /api/settings` 的 map 兼容结构。
- 新增 `GET /api/settings/definitions`，返回前端可展示的配置定义、分组、默认值和脱敏状态。
- `GET /api/system/env` 继续只读展示启动固定项，敏感项不回显。

前端：

- 设置页按分组渲染运行期可改项，启动固定项以只读提示进入系统状态/环境页。
- 对 enum、bool、path、url、int 等类型使用匹配控件，不允许自由写未知 key。
- 保存失败时展示服务端中文错误，不吞掉校验错误。

审计：

- 保存成功后为每个变更 key 通过审计 `Recorder` 接口写 `settings.updated` 事件；FR2-040 合入后升级为同事务真实审计事件。
- `before`/`after` 对敏感值只存 `已设置/未设置` 或 hash 摘要，不存明文。
- FR2-040 未落地前只能验证 `Recorder` 接口调用与脱敏输入，不能宣称真实审计查询完成。

## 4. 任务拆分

- [x] 梳理现有设置 key，并建立配置边界表。
- [x] 在 `internal/settings` 增加 registry、类型校验与未知 key 拒绝。
- [x] 保持 `GET/PUT /api/settings` 兼容，同时新增 definitions 接口。
- [x] 为现有 apply hook 补全写前校验与事务后应用顺序。
- [x] 前端设置页改为基于 definitions 渲染与校验。
- [x] 接入配置变更审计 `Recorder` 接口，FR2-040 合入后追加真实审计查询集成验收。
- [x] 补单元测试：未知 key、类型错误、敏感脱敏、批量原子失败、默认值。
- [x] 补集成测试：保存扫描周期/代理/工具路径后热应用，启动固定项不可写。
- [x] 补 E2E：设置页展示分组、保存合法项、非法项有错误提示。
- [x] 文档同步：PRD 状态、ARCHITECTURE、API、CHANGELOG。

## 5. 验收标准

- `PUT /api/settings` 写未知 key 返回 `400`，数据库不产生脏 key。
- 已有合法 key 行为保持兼容：扫描周期、工具路径、代理、调试日志保存后仍能即时生效。
- `GET /api/settings/definitions` 能返回前端所需分组、类型、默认值、敏感标记和说明。
- 启动固定项和敏感项不可通过通用设置接口修改。
- 敏感值不会在 API 响应、日志、审计事件中明文出现。
- 保存成功的热应用 key 必须有测试证明数据库值与运行态一致；不可热应用 key 必须有“需重启”提示。
- 配置变更会调用审计 `Recorder` 并传入脱敏后的 before/after；真实审计事件查询在 FR2-040 合入后核销。
- `go test`、前端测试、Playwright 设置页 E2E、`pnpm run quality` 全绿。
- Go 单二进制 serve 构建后的前端，设置页真实保存与刷新回读通过。

## 6. 风险 / 待定

- 已确认：typed registry 与未知 key 拒绝按 ADR-0061 落地，改变 ADR-0029 中“任意 key upsert”的设置模型。
- 已确认：不保留未知 key 写入兼容窗口，旧客户端写未知 key 直接失败，避免配置继续漂移。
- 已确认：敏感运行期项本规格只做脱敏与不回显，不新增加密体系。
- 本规格的真实审计查询验收依赖 FR2-040；若与 FR2-040 同批实现，必须在 foundation 批整体验收时核销。
