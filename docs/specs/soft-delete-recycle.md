# 功能规格：软删除与回收站

> 状态：开发中　·　关联 PRD：FR-25　·　分支：feature/fr-25-softdelete

## 1. 背景与目标
当前删除媒体会物理删除 `media_files` 数据库记录。误删后无法恢复，需重新扫描整库才能找回元数据。本功能（P2）把删除改为「软删除」：仅在数据库标记，源文件与磁盘均不动；并提供回收站页查看被软删的媒体、一键还原回常规列表。

## 2. 需求（要什么）
- 删除媒体 → 软删除：设置 `media_files.deleted_at = now`，不物理删除数据库记录、不删除磁盘源文件。
- 常规列表/计数排除已软删项：`ListMediaFilesFiltered`（时间轴、相册选择器、目录视图等的真源）加 `deleted_at IS NULL`，与 FR-23 `ListLibraryPathViews`、FR-41 筛选口径一致。
- 回收站列表端点：列出全部已软删媒体（`deleted_at IS NOT NULL`），按软删时间倒序。
- 还原端点：清空指定媒体的 `deleted_at`，使其回到常规列表。
- 前端 `/recycle` 回收站页：展示软删项 + 还原按钮 + 左侧导航项。
- 范围内：软删标记、常规口径排除、回收站列/还原、回收站页。
- 不做（范围外）：回收站「彻底删除」与按盘符回收站目录清理（属 FR-26）；定时自动清理（FR-28）；相册成员列表对软删项的过滤（FR-40 自身口径，本次不动）；播放/详情端点对软删项的拦截（不在验收范围，保持现状以最小改动）。

## 3. 设计（怎么做）
模块：`internal/library`（服务）、`internal/api`（端点/路由）、`frontend`（页面/接口/导航）。复用 foundation 已加的 `media_files.deleted_at *time.Time`（普通索引字段，非 GORM `gorm.DeletedAt`，故需手工写 `deleted_at IS NULL` 过滤）。

- 服务层 `internal/library/service.go`：
  - `DeleteMediaFile(id)` 由物理 `Delete` 改为 `Update("deleted_at", now)`；记录不存在（RowsAffected==0）仍返回「媒体文件不存在」。
  - 新增 `ListDeletedMediaFiles()`：查 `deleted_at IS NOT NULL`，按 `deleted_at DESC` 排序。
  - 新增 `RestoreMediaFile(id)`：`Update("deleted_at", nil)`；记录不存在返回错误。
- 服务层 `internal/library/favorites_tags.go`：`ListMediaFilesFiltered` 增加 `deleted_at IS NULL`。
- 端点 `internal/api/handler.go` + 路由 `internal/api/router.go`：
  - `GET /api/library/recycle` → `ListDeletedMediaFiles`，返回 `{items}`。
  - `POST /api/library/media/:id/restore` → `RestoreMediaFile`，成功 204。
- 前端：`api/library.ts` 增 `getRecycleMediaFiles`/`restoreMediaFile`（real+mock）；新增 `pages/RecyclePage.tsx`；`App.tsx` 加 `/recycle` 路由；`AppLayout.tsx` 加导航项；MSW 增回收站/还原 handler。

数据模型与依赖方向不变，无新增 ADR。

## 4. 任务拆分
- [ ] 后端测试先行：软删除、常规列表排除、回收站列表、还原（红→绿）
- [ ] 服务层与端点实现
- [ ] 前端测试先行：回收站页列出软删项 + 还原（红→绿）
- [ ] 前端页面/接口/路由/导航实现
- [ ] 文档同步：PRD 状态、ARCHITECTURE、API、CHANGELOG

## 5. 验收标准
- 删除媒体后，它从常规列表（时间轴/`GET /api/library/media`）消失，且出现在回收站页/`GET /api/library/recycle`；磁盘源文件仍存在（软删不碰文件系统，由「不调用任何文件删除」保证，单元测试覆盖记录仍在 + `deleted_at` 非空）。
- 在回收站页点「还原」后，媒体回到常规列表、从回收站消失。
- 各库已索引媒体计数（FR-23）不含软删项（既有口径，保持）。
- 自动化：`internal/library` 与 `internal/api` 相关测试、前端 `RecyclePage` 测试全绿。

## 6. 风险 / 待定
- `GetMediaFileByID` 不区分软删状态（回收站/还原需读取软删记录），故软删后仍可被详情/播放端点访问到——本次不拦截，留待 FR-26/后续按需收口。
