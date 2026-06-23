# 功能规格：目录浏览聚合多库（单一虚拟根）

> 状态：已完成（待发版）　·　关联 PRD：FR-66　·　分支：feature/fr-66-directory

## 1. 背景与目标

目录浏览此前是单库视图：后端 `GET /api/library/browse` 必须带 `library_id`，前端写死取第一个库根目录（`paths[0]`）且用一次性锁只初始化一次。≥2 个存储库时只能看第一个库、无法切换，新增库不刷新。本 FR 让目录浏览顶层聚合所有存储库为单一虚拟根、逐库下钻，新增库即时反映（P7 界面与运维完善）。

## 2. 需求（要什么）

- 顶层虚拟根列出所有**启用**存储库作为顶层「目录」项，点击某库进入该库现有单库浏览树。
- 面包屑从虚拟根起，可从库内点回根。
- 去掉前端 `paths[0]` 写死与 `initialized` 一次性锁，新增库即时反映。
- 范围内：目录浏览页（`/browse`）的库聚合入口；后端聚合根能力。
- 不做（范围外）：聚合根递归列各库内全部文件（仅列库本身作顶层）；多选/右键（FR-69）；跨库筛选搜索（筛选仍在选定库内当前目录递归，行为不变）。

## 3. 设计（怎么做）

浏览模型由「单库」扩展为「聚合虚拟根 → 逐库下钻」，属架构决策，另见 [ADR-0037](../adr/0037-aggregate-directory-browse.md)（勿在此重复决策正文）。要点：

- **后端**：`library.BrowseDirectory` 增聚合根分支——`parent_path == "__root__"` 时忽略 `library_id`，返回所有启用库作为 `DirInfo`（`name`=label/回退 path、`path`=库 path、新增 `library_id` 字段填该库 ID），面包屑单段 `{name:"全部存储库", path:"__root__"}`；其余 `parent_path` 走原单库逻辑不变。`handler.BrowseDirectory` 对 `__root__` 放宽 `library_id` 必填校验。`models.DirInfo` 增 `LibraryID int64 json:"library_id,omitempty"`。
- **前端**：`BrowsePage` 无库定位查询参数时以 `__root__` 初始化（移除 `paths[0]` 回退）；`useDirectoryBrowse` 去 `initialized` 锁，新增「进入库」语义——根项点击带 `library_id` 时切库下钻，面包屑回到 `__root__` 时切回根（`library_id` 置空）。`DirectoryBrowser` 进入目录回调透传目录项（含可选 `library_id`）。
- **真源**：库列表真源仍是 `library_paths`，媒体真源仍是 `media_files`，聚合根不新增持久状态。

## 4. 任务拆分

- [x] 后端：`models.DirInfo` 增 `library_id`；`BrowseDirectory` 聚合根分支；handler 放宽校验 → 单测覆盖（聚合根返回所有启用库 / 禁用库不列 / 单库下钻不变）
- [x] 前端：去 `paths[0]` 与 `initialized` 锁；根→库下钻 / 库→根回退；mock/handlers 与 api 适配 → vitest 覆盖
- [x] 文档同步：PRD 状态、ARCHITECTURE §5.0、API.md browse、CHANGELOG、ADR-0037

## 5. 验收标准

- 注册 ≥2 库时，`/browse` 默认展示虚拟根、列出全部启用库；点库进入该库树、面包屑可回根；新增库后回到根即时出现。
- 后端 `go test ./internal/api/... ./internal/library/...` 覆盖聚合根与单库下钻不变全绿。
- 前端 `npx tsc --noEmit` + `npx vitest run` 全绿（BrowsePage 聚合根渲染/下钻、单库定位参数路径不回归）。
- 真机维度：多库真机浏览切换属界面行为，标「待真机验」（自动化测试已覆盖核心逻辑）。

## 6. 风险 / 待定

- 哨兵 `__root__` 不与真实路径冲突（真实路径不含该串），已在 ADR 论证。
- 带 `library_id` + 真实 `path` 的旧库定位深链（`LibraryManagerPage` 点卡片跳转）须保持不变——前端「有显式库定位则直接进库、否则虚拟根」分支需覆盖此路径。
