# 功能规格：用户相册/合集

> 状态：开发中　·　关联 PRD：FR-40　·　分支：feature/fr-40-albums

## 1. 背景与目标
媒体库目录按磁盘物理结构组织，用户无法把分散在不同目录的媒体（如同一次旅行的视频与照片）归到一起浏览。FR-40 引入“相册/合集”概念：在物理目录之外，提供一层用户手动维护的逻辑集合，支持跨目录把任意媒体加入同一相册。属于 P2 阶段（媒体管理增强）。

## 2. 需求（要什么）
- 相册管理：新建相册（名称必填、描述可选）、列出全部相册、删除相册。
- 相册成员：把某条媒体加入相册、从相册移出媒体；同一媒体在同一相册内不重复。
- 相册浏览：查看某相册内的全部媒体成员（复用现有媒体卡片展示）。
- 删除相册仅删除 `albums` 与该相册的 `album_items` 记录，**不删除源文件、不删除 `media_files` 记录**。
- 前端 `/albums` 页：列相册、建相册、删相册、进入相册看内容、在相册内移出成员；从相册详情可“加入媒体”（从媒体库选择媒体加入当前相册）。
- 前端 api 走 real + mock 双实现（`VITE_USE_MOCK`），新增路由与导航项。
- 范围内：基于已有 `models.Album` / `models.AlbumItem`（albums/album_items 表）写业务逻辑与端点。
- 不做（范围外）：相册封面自动选取（CoverMediaID 字段已存在但本期不强制设置）、相册分享（FR-43）、相册排序拖拽、相册嵌套。

## 3. 设计（怎么做）
- 后端：在 `media-library` 模块新增 `internal/library/album_service.go`，提供相册 CRUD 与成员增删；端点挂在 `internal/api`，路由 `/api/albums`（与 `/api/library` 平级，归属媒体库分组）。
  - `POST /api/albums` 建相册；`GET /api/albums` 列相册（含成员数）；`DELETE /api/albums/:id` 删相册（事务内删 album + album_items）。
  - `GET /api/albums/:id/items` 列相册成员（返回 `MediaFile` 列表）；`POST /api/albums/:id/items` 加成员（body `{media_id}`，重复幂等）；`DELETE /api/albums/:id/items/:mediaId` 移出成员。
  - 成员加入时校验 media 存在；唯一索引 `(album_id, media_id)` 已由 foundation 建好，重复加入做幂等处理（FirstOrCreate）。
- 数据模型：直接使用已存在的 `models.Album`、`models.AlbumItem`，不改结构、不动 AutoMigrate。
- 前端：新增 `src/api/albums.ts`（real+mock+VITE_USE_MOCK）、`src/pages/AlbumsPage.tsx`、`src/types` 增 `Album`/`AlbumItem` 类型、`App.tsx` 受保护路由 `/albums`、`AppLayout` 导航项。MSW handlers 增相册端点供测试。
- 涉及契约新增（新端点）：同步 `docs/API.md`、`docs/ARCHITECTURE.md`（数据模型 albums/album_items + 接口概览）。不涉及架构决策变更，无需 ADR。

## 4. 任务拆分
- [x] 后端相册服务（建/列/删 + 成员增删）+ 单测
- [x] 后端相册端点 + handler 测试
- [x] 前端 api（real+mock）+ 类型
- [x] 前端 AlbumsPage + 路由 + 导航 + 页面测试
- [x] 文档同步：PRD 状态、ARCHITECTURE、API、CHANGELOG

## 5. 验收标准
- 后端：相册服务与 handler 测试覆盖「建相册→加跨目录媒体→列成员→移出→删相册」全链路，且删相册后 `media_files` 记录仍在（断言源媒体记录未被删）。`go test ./internal/library/... ./internal/api/...` 全绿。
- 前端：AlbumsPage 测试覆盖列相册、建相册、看相册内容、加入/移出媒体；`npm run build && npm run test` 全绿。
- 手动验收（可选，不替代自动化）：真机建相册、跨目录加媒体、浏览、删相册后源文件仍在磁盘。

## 6. 风险 / 待定
- 加成员需要 media 存在性校验，media 不存在返回 404。
- 删相册必须在事务内同时删 album 与 album_items，避免遗留孤儿成员。
