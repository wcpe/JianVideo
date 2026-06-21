# 功能规格：文件名双模式编辑

> 状态：开发中　·　关联 PRD：FR-30　·　分支：feature/fr-30-rename

## 1. 背景与目标

媒体文件存在两类「名字」需求：一类是用户希望在系统内看到的友好展示名（如把
`绝命毒师.S01E01.mkv` 显示为「绝命毒师 第一集」），不应改动磁盘上的真实文件；另一类是
确实要把磁盘文件改名。本特性把两者拆为各自独立的修改路径，均需二次确认，避免误操作。
属于 P2 媒体管理增强。基础数据模型（`media_files.display_name` 字段）已由 foundation 提交备好，
本特性只补业务逻辑与 UI。

## 2. 需求（要什么）

- 显示名修改：仅更新数据库 `media_files.display_name`，不动磁盘文件名与路径；置空表示清除显示名。
- 真实文件名修改：磁盘改名，复用既有 `RenameMediaFile`（磁盘原子改名 + 更新 `file_path`/`file_name`/`format`）。
- 展示优先级：列表/卡片/详情展示名优先用 `display_name`，为空则回退 `file_name`。
- 两种修改在前端均弹出二次确认弹窗，确认后才执行。
- 范围内：本地与已索引媒体；显示名为自由文本（仅去首尾空白，可为空）。
- 不做（范围外）：批量改名、改名历史/撤销、按显示名搜索/排序（沿用现有按 `file_name` 的搜索）、SMB 远程文件磁盘改名（既有 `RenameMediaFile` 已拒绝）。

## 3. 设计（怎么做）

复用既有分层：`api`（HTTP）→ `library.Service`（业务）→ `db`（GORM）。无新架构决策，不写 ADR。

### 数据模型（foundation 已建，不改结构）

- `media_files.display_name`（string）：库内展示名，空则回退 `file_name`，不影响磁盘真实文件名。

### 后端服务（`internal/library/service.go` 新增方法）

- `UpdateDisplayName(id int64, displayName string) (*MediaFile, error)`：去首尾空白后仅更新
  `display_name` 列并回读返回；记录不存在返回 `gorm.ErrRecordNotFound`。
- 真实改名沿用既有 `RenameMediaFile`，不改动。

### 后端端点（`internal/api`）

- `PUT /api/library/media/:id/display-name`，体 `{"display_name": "..."}`，返回更新后的媒体对象（含 `display_name`）；
  空串表示清除显示名。`404` 记录不存在。
- 真实改名沿用既有 `PUT /api/library/media/:id/rename`，不改动。

### 前端（`frontend/src`）

- 类型：`MediaFile` 增 `display_name`。
- 展示：新增 `mediaDisplayName(file)` 工具，列表/卡片/详情统一优先取 `display_name`、回退 `file_name`；
  改造 `TimelineView`、`DirectoryBrowser`、`AlbumsPage`、`PlayPage` 的名字展示处。
- `api/library.ts`：补 `updateDisplayName` 的 real + mock 双实现与导出（沿用 `VITE_USE_MOCK` 切换）。
- `PlayPage` 标题区提供「改显示名」「改文件名」两个入口；各自弹出二次确认弹窗（复用 `ConfirmModal`，内嵌输入框），
  确认后调用对应 API 并刷新当前媒体。
- MSW（`mocks/handlers.ts`）：补 `PUT .../display-name` 与 `PUT .../rename` 处理，供组件测试。

## 4. 任务拆分

- [x] service 层 `UpdateDisplayName` + 单测（更新、清空、不存在）
- [x] handler + 路由 `PUT /api/library/media/:id/display-name` + 端点测试
- [x] 前端 `mediaDisplayName` 工具 + 单测；类型加 `display_name`
- [x] 前端 api 双实现 + MSW handler
- [x] PlayPage 双模式改名 UI + 二次确认 + 组件测试；各展示处回退逻辑
- [x] 文档同步：PRD 状态、API.md、ARCHITECTURE.md、CHANGELOG

## 5. 验收标准

- 改显示名：`media_files.display_name` 正确更新、磁盘文件名与 `file_path`/`file_name` 不变（service + handler 测试覆盖）。
- 改真实名：磁盘文件改名、`file_path`/`file_name` 更新（既有 `RenameMediaFile` 测试已覆盖，本特性不回归破坏）。
- 展示优先级：有 `display_name` 时展示 `display_name`，为空时展示 `file_name`（前端工具 + 组件测试覆盖）。
- 前端：改显示名与改文件名均经二次确认弹窗后才执行（组件 + MSW 测试覆盖）。
- 后端 `go test ./internal/library/... ./internal/api/...` 全绿；前端 `npm run build` 与 `npm run test` 全绿。

## 6. 风险 / 待定

- 显示名取最小规整：仅去首尾空白，允许为空（空即清除回退 `file_name`），不限字符集（自由文本）。
- 显示名不参与搜索/排序，避免牵动既有列表查询；范围外，后续如需再加。
