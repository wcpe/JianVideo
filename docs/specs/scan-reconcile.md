# 功能规格：增量/全量扫描 + 已删文件对账

> 状态：开发中　·　关联 PRD：FR-27　·　分支：feature/fr-27-reconcile

## 1. 背景与目标
当前扫描只做「索引新增文件」：遍历目录，对数据库尚无记录的媒体路径入库。一旦磁盘上的源文件被删除，数据库中的旧记录会一直残留，常规列表里出现「点不开的死链」。本功能（P2，FR-27）为扫描引入两种显式模式，并在全量扫描时对账已删文件：

- 增量更新：沿用现有行为，只处理新增/变更文件，速度快，适合频繁手动触发。
- 全量扫描：遍历全部文件并**对账**——数据库中属于该库、但源文件已不存在的记录标记为软删（`deleted_at`，进回收站，复用 FR-25），不物理删除、不动磁盘。

## 2. 需求（要什么）
- 扫描执行层区分两种模式：增量（incremental）与全量（full）。
- 全量扫描对账：以本次遍历到的「该库现存文件路径集合」为基准，库内**未软删**且不在该集合中的记录标记 `deleted_at`（软删进回收站）。
- 不误标仍存在的文件：本次遍历到的文件一律不软删；已软删的文件不重复处理。
- 扫描触发端点 `POST /api/library/scan/:id` 加 `mode` 查询参数（`full` / `incremental`），缺省为 `incremental`（向后兼容现有调用方）。
- 前端在存储库管理处为每个库提供「增量更新」「全量扫描」两个入口。
- 范围内：扫描执行层（`ScanLibraryWithType` / `indexMediaFiles` 内部 + 端点 `mode` 参数）、前端两入口。
- 不做（范围外，属 FR-29）：扫描触发的队列化、页眉常驻任务展示、多任务排队——不动触发/队列/页眉层。
- 不做：物理删除文件或数据库记录（对账只软删）。

## 3. 设计（怎么做）
- 扫描模式常量：`ScanModeIncremental = "incremental"`、`ScanModeFull = "full"`。
- `ScanLibraryWithType(libraryID, dirPath, dirType)` 增加 `mode` 形参，得 `ScanLibraryWithType(libraryID, dirPath, dirType, mode string)`；`ScanLibrary`（本地异步默认入口）与 `StartAsyncScan` 同步透传 `mode`。watcher 的 SMB 轮询调用按现状语义传 `incremental`。
- 增量与全量都先遍历得到现存路径集合并入库新文件（沿用 FR-31 的有界并发 `indexMediaFiles`，不改其并发结构）。差异仅在：全量模式在入库后追加一次对账。
- 对账（`reconcileDeleted`）：一条 `UPDATE media_files SET deleted_at = ? WHERE library_id = ? AND deleted_at IS NULL AND file_path NOT IN (现存集合)`，在事务/单语句内完成，避免逐条 N+1。现存集合统一为正斜杠，与入库口径一致。
- 对账仅对本地扫描启用（本地 `WalkDir` 能可靠枚举全集）；SMB 轮询为增量语义、不触发对账，避免远程列举不全时误删。
- 端点：`ScanLibrary` 处理器读取 `mode` 查询参数，校验后透传；非法值回退 `incremental`。
- 不引入新表、新字段（复用 `media_files.deleted_at`），无对外契约破坏（仅新增可选查询参数）。

## 4. 任务拆分
- [ ] 测试先行：扫描执行层全量对账软删 + 增量不软删 + 不误标现存文件
- [ ] 测试先行：端点 `mode=full` 触发对账、缺省增量
- [ ] 实现 `ScanLibraryWithType` 加 `mode` 参数与 `reconcileDeleted`
- [ ] 端点 `mode` 参数解析与透传
- [ ] 前端两入口（增量更新 / 全量扫描）+ mock/类型同步
- [ ] 文档同步：PRD 状态、API（端点 mode 参数）、CHANGELOG 未发布段

## 5. 验收标准
- 建文件 → 全量扫 → 删其一 → 全量扫：被删记录 `deleted_at` 非空（回收站可见、常规列表消失），仍在的文件 `deleted_at` 为空。集成测试覆盖（自动化）。
- 增量扫只新增不软删：删文件后增量扫，旧记录不被软删。集成测试覆盖。
- 端点 `POST /api/library/scan/:id?mode=full` 触发对账；无 `mode` 或非法值按增量执行。集成测试覆盖。
- 前端存储库管理处可见「增量更新」「全量扫描」两入口，分别调用对应模式。前端单测覆盖。
- 后端 `go build ./...`、受影响包 `go test`（含 `-race`）全绿；前端 `npm run build` + `npm run test` 全绿。

## 6. 风险 / 待定
- 对账依赖遍历得到完整现存集合：本地 `WalkDir` 在目录临时不可访问时会跳过该子树，可能把现存文件误判为缺失。缓解：遍历出错时（`err != nil`）整体放弃对账，不在不完整集合上软删。
- SMB/远程不做对账（列举不可靠），仅本地全量扫描对账。
