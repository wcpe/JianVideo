# 功能规格：存储库后缀列管（FR-64）

> 状态：已完成（待发版）　·　关联 PRD：FR-64　·　分支：feature/fr-66-directory

## 1. 背景与目标

后缀管理此前只能「加」：后端仅 `GET /api/library/extensions`（列内置+自定义）+ `POST`（增），无删除；前端有「加后缀」输入但不列出已加后缀、不能删除。本 FR 补齐删除端点与前端后缀管理界面，并提供视频/图片/全部筛选视图（P7，扩 FR-01）。

## 2. 需求（要什么）

- 后端新增 `DELETE /api/library/extensions`：只删自定义后缀，内置不可删，删不存在返回合理错误。
- 前端后缀管理 UI：列出该库后缀（内置标识不可删 / 自定义可删 + 删除按钮）。
- 后缀列表顶部加 视频/图片/全部 三档筛选（全部=同显两类）。
- 添加后缀仍 video/image 二选一（不改数据模型）。
- 不做（范围外）：删除内置后缀；新增后缀类型（仍仅 video/image）；跨库后缀复用。

## 3. 设计（怎么做）

数据模型不变（`media_extensions` 唯一键 `(library_id, extension)`、内置后缀 `IsBuiltIn=1` 临时合成不落库）。无新架构决策，无需 ADR。

- **后端**：`library.DeleteMediaExtension(libraryID, extension)`——规范化后缀，内置（命中 `builtInMediaExtensions`）拒删、删不存在（`RowsAffected==0`）报错；handler `DELETE /api/library/extensions`（query：`library_id`+`extension`），成功 204、失败 400；router 注册。
- **前端**：`api/library.ts` 加 `deleteMediaExtension`（real+mock）；`useLibraryPaths` 在加载后缀策略时一并缓存各库完整后缀列表 `extensionsByLibrary`，新增 `handleDeleteExtension`；`LibraryPathManager` 卡内新增「视频/图片/全部」`SegmentedControl` 筛选 + 后缀徽标列表（内置 outline + 「（内置）」无删除入口、自定义 light + 删除 `×`）。mock handlers 加 DELETE。

## 4. 任务拆分

- [x] 后端：service `DeleteMediaExtension` + handler + router → 单测（删自定义/拒删内置/删不存在）
- [x] 前端：api + hook + `LibraryPathManager` 列出/删除/筛选 UI + mock → vitest（列出/删除/筛选）
- [x] 文档同步：PRD 状态、ARCHITECTURE、API.md、CHANGELOG

## 5. 验收标准

- 后端 `go test ./internal/api/... ./internal/library/...` 覆盖删自定义成功、拒删内置、删不存在报错全绿。
- 前端 `npx tsc --noEmit` + `npx vitest run` 全绿（列出内置/自定义、删除走 DELETE、筛选切换）。
- 真机维度：后缀列管为纯 CRUD 界面行为，自动化测试已覆盖，标「待真机验」。

## 6. 风险 / 待定

- DELETE 用 query 参数（与 GET 列表一致），后缀含特殊字符已由后端 `normalizeExtension` 仅允许 `[a-z0-9]` 限制，无注入面。
