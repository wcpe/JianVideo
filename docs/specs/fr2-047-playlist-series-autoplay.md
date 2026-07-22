# 功能规格：播放列表/合集与剧集自动连播

> 状态：已交付@v0.25.0　·　关联 PRD：FR2-047　·　阶段：P4 `0.25.x`　·　前置：albums API、FR2-031 推断、PlayPage

## 1. 背景与目标

已有相册/合集（albums）CRUD 与成员管理。P4 要求：手动合集可作播放列表；剧集库按季/集自动下一集连播。

## 2. 范围

### 2.1 范围内

- 播放列表：从合集页或播放页「下一首/上一首」按合集顺序播放。
- 剧集连播：当媒体有推断 `title+season+episode`（或库类型 series）时，播放结束自动定位同 title 下一集。
- 设置：允许关闭自动连播（用户偏好，持久化到现有设置或 local）。
- API：若现有 list album items 不足序，补稳定 sort_order；下一集查询端点或前端按已有列表计算（优先服务端，避免大合集全量拉取）。

### 2.2 范围外

- 智能推荐列表、跨 Space 合集。
- 多用户协作编辑播放列表（P5 权限模型后再做）。

## 3. 设计

- `GET /api/library/media/:id/next-episode` → 同 Space 下同 title、更大 episode（同 season）或下一 season 的 ep1。
- 播放页：`ended` 事件后若开启连播则 `navigate` 下一媒体并续播策略（不继承进度）。
- 合集顺序播放：`albumId` query 进入播放页时维护 playlist context。

## 4. 任务拆分

- [x] 规格冻结（本文）。
- [x] next-episode API + 单测。
- [x] PlayPage 连播与合集上下文。
- [x] 文档与 CHANGELOG。

## 5. 验收标准

- 剧集有序数据下播完 E01 自动进 E02；无下一集时停止并提示。
- 关闭自动连播后 ended 不跳转。
- 合集顺序播放与 items 顺序一致。
- Space 隔离：不连播到其他 Space 媒体。

## 6. 风险

- 推断标题不一致导致断连：允许人工纠正推断后恢复。
