# 功能规格：批量操作扩展

> 状态：开发中　·　关联 PRD：FR-91　·　分支：feature/fr-91-batch-ops

## 1. 背景与目标

FR-69 已交付列表多选基建（ctrl 点选 / shift 连选 / 全选 / 反选 / 复选框模式）与右键菜单，但右键菜单当前仅支持批量删除。本特性在既有多选基建上扩展三类常用批量操作，提升整理与导出效率，属第八期（P8）界面体验打磨。

## 2. 需求（要什么）

- **批量加相册**：右键菜单「加入相册」→ 选相册弹窗 → 对选中集每个 id 调用单项端点 `addAlbumItem` 加入；去重幂等、单项失败不中断整批，完成后 toast 显示成功/跳过计数。
- **批量打标签**：右键菜单「打标签」→ 选/建标签弹窗 → 对每个 id 调用单项端点 `addMediaTag`；完成后 toast 显示成功/失败计数。
- **批量打包下载**：右键菜单「打包下载」→ 新增后端 zip 流端点，将选中媒体的原文件流式打包为 zip 附件下载；`smb://` 路径项跳过、文件名按 RFC 5987 编码。
- 范围内：扩展 `MediaContextMenu` 三项菜单；两个宿主（TimelineView / DirectoryBrowser）透传新回调；两页（TimelinePage / BrowsePage）复用同一编排；新增后端 zip 端点与批量查询。
- 不做（范围外）：批量移动 / 批量重命名 / 批量改元数据；不预留多尺寸 / 插画等无关物；加相册与打标签不新增后端端点（纯前端循环复用 FR-40/FR-41 单项端点）。

## 3. 设计（怎么做）

### 后端（净新端点）

- 新增 `GET /api/library/media/batch-download?ids=1,2,3`：用 Go 标准库 `archive/zip` 将选中媒体原文件流式写入响应体，**边写边 flush**，不一次性读入内存。
- 用 `c.Request.Context()` 控制取消（无整体 `client.Timeout`，规避自更新被 30s 超时掐断的教训）。
- `smb://` 路径项跳过（与 `serveDownload` 对 smb 返 400 一致），记入跳过计数（响应头 `X-Skipped` 提示）。
- 上限：选中数量 ≤ 500、总大小 ≤ 5 GiB，超限返回 400。
- `library.Service` 新增 `GetMediaFilesByIDs(ids)`：单次查询批量取媒体记录（排除软删），无 N+1。

### 前端

- `MediaContextMenu` 增加「加入相册」「打标签」「打包下载」三项与对应可选回调。
- 新增 hook `useBatchActions`：封装选相册 / 选标签弹窗状态、循环调用单项端点、toast 计数、触发 zip 下载（fetch 带 Bearer token、blob 触发浏览器附件下载）；返回触发函数与弹窗组件 props。
- 新增组件 `BatchActionsModals`：渲染选相册弹窗与选/建标签弹窗。
- 两个宿主透传三回调到父页面；两页各调用一次 `useBatchActions` 完成编排。

## 4. 任务拆分

- [x] 后端 `GetMediaFilesByIDs` service 方法 + 单测
- [x] 后端 `BatchDownloadMediaFiles` handler（zip 流式、smb 跳过、上限）+ 单测
- [x] 前端 `MediaContextMenu` 三项菜单 + 回调
- [x] 前端 `useBatchActions` + `BatchActionsModals` + vitest
- [x] 两宿主透传回调、两页编排接入
- [x] 文档同步：PRD 状态、API、CHANGELOG

## 5. 验收标准

- 加相册：选中 N 项→「加入相册」→选相册→成员数增 N（幂等去重）；toast 显示成功/跳过计数。
- 打标签：选中 N 项→「打标签」→每项含该标签；toast 显示成功/失败计数。
- 打包下载：选中 N 项→收到 attachment zip、解压得 N 个原文件、文件名不乱码、`smb://` 项被跳过并提示。
- 后端 zip 端点 handler/service 单测：空 ids no-op、含无效 id 跳过、smb 项跳过、超限拒绝。
- 前端 vitest 覆盖三个菜单项触发与 toast 计数。
- 真机（需用户确认）：大文件/多文件 zip 边写边 flush 不 OOM、不被超时掐断。单元测试不替代此项。

## 6. 风险 / 待定

- 前端 zip 下载经 `fetch` 取 blob 再触发，blob 在浏览器侧；后端流式输出满足「不一次性读入内存」，浏览器侧 blob 为常见可接受权衡。
- 超大批量 zip 真机内存/超时表现需真机复验。
