# 功能规格：文件详情面板

> 状态：开发中　·　关联 PRD：FR-34（载体）、FR-38（EXIF 填充）　·　依赖：FR-31（已存 EXIF/媒体时间）、FR-42（下载）

## 1. 背景与目标

当前时间轴点击媒体：图片弹 `ImagePreviewModal`、视频直接跳 `/play/:id`，没有统一的"选中即看详情"的浏览体验，也无处展示 FR-31 已提取的 EXIF。本功能（P3，承载 P2 的 FR-38）做一个**文件详情面板**：选中项打开面板，左侧预览、右侧元数据，支持全屏、上/下一项快捷键、图片滚轮缩放。

## 2. 需求（要什么）

- 范围内（FR-34）：
  - 新增 `MediaDetailPanel`（全屏 Modal）：左侧预览、右侧详情。
  - 左侧预览：图片 → 可滚轮缩放的原图（`/raw`，1–4 倍，换项复位）；视频 → 缩略图 + 「打开播放」按钮（跳 `/play/:id`，不在面板内嵌完整 HLS 播放——保持轻量）。
  - 右侧详情：显示名/真实文件名、类型、大小、分辨率、时长（视频）、媒体时间及来源、加入/修改时间、所属库；底部「下载原文件」入口（复用 FR-42）。
  - 导航：`←`/`→` 上/下一项（在当前已加载列表内，端点夹紧），`Esc` 关闭；面板内提供上一项/下一项/关闭/全屏切换按钮。
  - 接入时间轴页：点击媒体（图片与视频统一）打开面板定位到该项。
- 不做（范围外）：
  - 面板内嵌视频转码播放（仍走 PlayPage）。
  - 目录视图接入（FR-33 时再接）；EXIF 区块（FR-38）。
  - 跨页加载更多时的自动续航导航（仅在已加载项内导航）。

## 3. 设计（怎么做）

- 组件 `frontend/src/components/MediaDetailPanel.tsx`：props `{ files: MediaFile[]; initialIndex: number | null; onClose; customImageExtensions }`。内部 `idx` 状态由 `initialIndex` 初始化；`files` 仅追加增长，索引保持有效。
  - 图片缩放：`onWheel` 阻止默认 + 调 `scale`（clamp 1–4），换项 `useEffect` 复位为 1。
  - 键盘：`opened` 时挂 `keydown`，`ArrowLeft/Right` 导航、`Escape` 关闭；卸载移除。
  - 全屏：切换 Mantine `Modal` 的 `fullScreen`。
  - 视频预览：`MediaThumbnail` + 「打开播放」`Button`→`navigate('/play/'+id)`。
- 接入 `TimelinePage`：`handleOpen` 改为按 `id` 在 `infinite.items` 求索引并打开面板（图片与视频统一）；保留 `ImagePreviewModal`（仍由 `BrowsePage` 使用，FR-33 再统一）。

## 4. 验收标准

- 点击时间轴媒体打开详情面板：图片显示可滚轮缩放的预览、视频显示缩略图 + 播放按钮。
- 右侧展示文件元数据；底部可下载原文件。
- `←`/`→` 在已加载列表内切换上/下一项并在端点夹紧；`Esc` 关闭；全屏切换可用。
- 前端 `npm run build`、`npm run test`（含面板组件测试：渲染/导航/缩放复位）全绿。

## 5. 风险 / 待定

- 大列表导航：仅在已加载项内导航，端点夹紧，不在面板内触发 loadMore（避免与无限滚动耦合，YAGNI）。
- EXIF 展示是 FR-38，本期面板右侧预留区块，不提前实现。
