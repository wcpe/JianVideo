# 功能规格：列表多选 + 右键菜单 + 批量删除

> 状态：开发中　·　关联 PRD：FR-69（扩 FR-33/FR-25）　·　分支：feature/fr-69-multiselect

## 1. 背景与目标
时间轴（`TimelineView`）与目录浏览（`DirectoryBrowser`）目前对媒体的批量操作几乎为零：目录浏览有内部 Shift/Ctrl 多选但纯用于高亮、不向外暴露、无右键菜单、无 Ctrl+A/反选/复选框模式且不含目录；时间轴零选择能力；删除只有后端单条软删端点（FR-25）且前端两视图均无删除入口。本功能（P7，扩 FR-33 资源管理器视图与 FR-25 软删）让两视图都具备桌面级多选手势 + 右键常用菜单 + 批量删除（软删进回收站），并补齐后端批量软删端点。

## 2. 需求（要什么）
- 后端批量软删端点：`POST /api/library/media/batch-delete`，body `{"ids":[...]}`，在单事务内对多个 media_id 复用 FR-25 软删逻辑（仅置 `deleted_at`、不动磁盘）；返回成功软删条数。空 `ids` 视为 no-op（返回 0）、不存在/已软删的 id 跳过不报错。
- 可复用选择基建 hook `useMultiSelect`：封装 Ctrl 点选切换、Shift 连选区间、Ctrl+A 全选、反选、复选框模式开关（进入后每项显复选框、单击即切换）。暴露 `selectedIds` + 操作函数 + 模式状态给父组件消费。
- 目录浏览接入：`DirectoryBrowser` 用该 hook，选中集上抛父组件（`BrowsePage`）；右键菜单（Mantine `Menu`，`onContextMenu` 触发）含「删除选中」（≥1 选中时批量、未选中时删右键项）「全选」「反选」「进入/退出复选框模式」；选择仅作用于文件（目录不可选，保持现状）。
- 时间轴接入：`TimelineView` 从零接入同一 hook（卡片 Ctrl/Shift/Ctrl+A/反选/复选框选择）+ 右键菜单删除。
- 批量删除动作：右键「删除选中」→ Mantine 二次确认（复用 `ConfirmModal`）→ 调批量软删端点 → 成功后刷新列表 + 清空选择。
- 范围内：上述手势、右键菜单、批量软删端点与前端接入。
- 不做（范围外）：批量还原 / 批量彻底删除（回收站页 FR-25/26 自有口径，本次不动）；批量打标签 / 批量收藏（右键菜单仅放「删除选中」+ 选择辅助项，其余批量操作留后续）；目录项（文件夹）多选与批量删除目录（保持仅文件可选）；拖框选（rubber-band）。

## 3. 设计（怎么做）
模块：`internal/library`（服务）、`internal/api`（端点/路由）、`frontend`（hook/组件/页面/接口/mock）。依赖方向不变（web → library → db），无新依赖（Mantine `Menu`/`Checkbox`/`Modal` 已可用），无新架构决策、无新 ADR。批量端点契约与单条软删一致语义（幂等、软删不动盘），属 FR-25 既有决策的自然扩展。

### 后端
- 服务层 `internal/library/service.go` 新增 `BatchDeleteMediaFiles(ids []int64) (int64, error)`：在 `s.db.Transaction` 内对 `id IN (?) AND deleted_at IS NULL` 执行一条 `UPDATE deleted_at = now`，返回受影响行数。空 `ids` 直接返回 `(0, nil)` 不开事务。单条 SQL 批量更新天然原子，跳过不存在/已软删 id（不计入受影响行数、不报错）。
- 端点 `internal/api/handler.go` `BatchDeleteMediaFiles`：绑定 `{ids []int64}`，调服务，返回 `200 {"deleted": N}`；body 非法返回 400。路由 `internal/api/router.go` 注册 `lib.POST("/media/batch-delete", h.BatchDeleteMediaFiles)`（置于单条 DELETE 之后）。

### 前端
- 新增 `frontend/src/hooks/useMultiSelect.ts`：泛型按「有序 id 列表」驱动。状态 `selectedIds: Set<number>`、`anchorIndex`、`checkboxMode: boolean`。暴露 `handleItemClick(index, {ctrlKey, shiftKey, metaKey})`（复选框模式下任意单击=切换；否则迁移自 DirectoryBrowser 的 Shift 区间 / Ctrl 切换 / 单选）、`toggle(id)`、`selectAll()`、`invertSelection()`、`clear()`、`setCheckboxMode()`、`isSelected(id)`。纯逻辑、可穷举测试。
- `DirectoryBrowser`：移除内部 Shift/Ctrl 实现，改用 `useMultiSelect(sortedFiles.map(f => f.id))`；新增可选 props `onSelectionChange?(ids)`、`onBatchDelete?(ids)`、`onDeleteOne?(file)`；卡片加 `onContextMenu` 包裹 Mantine `Menu`，复选框模式下行内渲染 `Checkbox`。无回调时退化为纯高亮（不回归既有测试）。
- `TimelineView`：把全部已加载项展平为有序 id 列表 `flatIds`（按分组顺序），接同一 hook；卡片加选中态高亮 + 右键菜单 + 复选框；新增可选 props 同上。Ctrl+A/反选作用于全部已加载项（虚拟列表边界，见 §6）。
- `BrowsePage` / `TimelinePage`：持有选中集与「批量删除」流程——`ConfirmModal` 二次确认 → `libApi.batchDeleteMediaFiles(ids)` → 成功后刷新（browse 重载 / infinite reload）+ 清空选择 + 通知。
- `frontend/src/api/library.ts` 增 `batchDeleteMediaFiles(ids)`（real `POST /api/library/media/batch-delete` + mock：批量加入 `mockDeletedIds`）。`frontend/src/mocks/handlers.ts` 增对应 MSW handler。

## 4. 任务拆分
- [ ] 后端测试先行：批量软删（正常多条、空列表、含不存在/已软删 id、原子单事务）红→绿
- [ ] 后端服务 + 端点 + 路由实现
- [ ] 前端 hook 测试先行：ctrl/shift/ctrl+a/反选/复选框模式各路径穷举 红→绿
- [ ] `useMultiSelect` 实现 + DirectoryBrowser 迁移（既有测试不回归）
- [ ] 目录与时间轴右键菜单删除流程测试 + 接入实现
- [ ] 文档同步：PRD 状态、ARCHITECTURE、API、CHANGELOG

## 5. 验收标准
- 自动化：`internal/library` 与 `internal/api` 批量软删相关测试全绿（含空/非法 id、原子性）；前端 `useMultiSelect` 纯逻辑测试、两视图右键菜单删除流程测试全绿；`npx tsc --noEmit` 与 `npx vitest run` 全绿；后端 `go build ./... && go vet ./...` 通过。
- 批量软删端点：对 N 个有效 id 一次请求后，它们全部从常规列表消失、出现在回收站；磁盘源文件不动（软删语义，由不调用文件删除保证）。
- 【待真机验】目录浏览：Ctrl 点选 / Shift 连选 / Ctrl+A 全选 / 反选 / 复选框模式各手势在真实浏览器生效；右键「删除选中」二次确认后批量进回收站、列表刷新。
- 【待真机验】时间轴：同上各手势对全部已加载项生效（含跨日期分组的 Shift 区间与 Ctrl+A）；右键删除进回收站、列表刷新。

## 6. 风险 / 待定
- 虚拟列表（`useWindowVirtualizer` 分组渲染）下 Ctrl+A / Shift 区间的边界：选择口径锚定「全部已加载项」（`mediaFiles` 经分组展平后的有序集合），而非仅当前视口可见项——Ctrl+A 选中全部已加载、Shift 区间按展平顺序连选，与无限滚动「已加载多少就能选多少」一致；未加载（需滚动触发 loadMore）的项不在选择范围。该边界在 spec 与代码注释中写明。
- 右键菜单在「未选中任何项」时点某卡片：菜单「删除选中」退化为删该右键项（onDeleteOne），符合资源管理器直觉。
- 两视图单击语义不同（各自保留既有交互、不回归）：目录浏览沿用 FR-33 资源管理器式「单击=选中、双击=打开」；时间轴沿用画廊式「普通单击=打开预览/播放」，多选仅在带修饰键（Ctrl/Cmd/Shift）或复选框模式下触发。两者的 Ctrl/Shift/Ctrl+A/反选/复选框/右键菜单一致，差异仅在「无修饰键单击」的默认动作。
