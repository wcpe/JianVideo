# 功能规格：统一媒体查看器框架（FR-107）

> 状态：开发中　·　关联 PRD：FR-107（ref，扩 FR-34/FR-85）　·　分支：feature/fr-107-unified-viewer

## 1. 背景与目标
图片灯箱 `MediaDetailPanel` 与播放页 `PlayPage` 是两套各自实现的媒体查看器，存在重复的地址构造逻辑（原图 raw、视频流 stream、HLS master 的绝对化）。本 FR 属第九期收敛性重构（ref），目标是消除两处查看器的重复、统一其共享构造行为，**对外行为完全不变**。

## 2. 需求（要什么）
- 识别图片灯箱与播放页中真实重复的逻辑，抽成共享件复用。
- 范围内：抽出共享的媒体地址构造（raw / stream / HLS master 绝对化），两处查看器改为复用，消除复制粘贴。
- 不做（范围外）：
  - 不强行把「单视频沉浸路由」与「多项模态灯箱」两类异构查看器合并为单一巨型组件——二者上下文（路由 vs 弹窗、单项 vs 列表导航、协商/字幕/fill vs 缩放/旋转/幻灯）差异大，强合并改动大、收益小且引入回归风险，违背简单优先与「宁可范围小」。
  - 不改任何对外行为：FR-102 直接播放、FR-103 沉浸布局、FR-104 控件、FR-105 缩放/平移/环绕/旋转/幻灯、FR-106 EXIF/工具栏/折叠均保持原样。
  - 不动后端、不引新依赖、不加新用户可见能力（YAGNI）。

## 3. 设计（怎么做）
- 新增 `frontend/src/utils/media-url.ts`：
  - `mediaRawUrl(id)` — 原图相对地址，图片预览与相邻预加载共用。
  - `mediaStreamUrl(id)` — 视频流地址，绝对化（避开 mpegts.js Web Worker 相对 URL 问题），与播放页降级路径一致。
  - `mediaHlsMasterUrl(id)` — HLS master 绝对化地址。
- `MediaDetailPanel.tsx`：删除内联的 `rawUrl` / `streamUrl`，改用共享构造器。
- `PlayPage.tsx`：删除内联 `toAbsolute` + `hlsUrl` / `streamUrl` 构造，改用共享构造器。
- 无模块/依赖方向变化（仍属 web 层内部工具），无新 ADR。

## 4. 任务拆分
- [x] 抽出 `utils/media-url.ts` 共享地址构造
- [x] `MediaDetailPanel` 复用共享构造器，删除本地重复
- [x] `PlayPage` 复用共享构造器，删除本地重复
- [x] 补共享件特征单测（`media-url.test.ts`）锁定行为
- [x] 文档同步：PRD 状态（计划→开发中）

## 5. 验收标准
- `frontend/` 下 `npm run build`（tsc -b）通过、`npm run test`（vitest）受影响范围全绿且零回归。
- 既有 `MediaDetailPanel.test.tsx`（18）与 `PlayPage.test.tsx`（19）测试一字不改全数通过（行为不变铁证）。
- 新增 `media-url.test.ts` 锁定共享地址行为。
- 真机（待用户确认）：图片灯箱与视频播放页的导航/全屏/工具栏行为一致、图↔视频切换正常、FR-102~106 全部不回退。

## 6. 风险 / 待定
- 全量并行测试在本机重负载下偶发 5000ms 超时（AppLayout/LibraryManagerPage/PlayPage 等），与本改动无关——隔离重跑全绿；属环境性 flaky（见项目记忆）。
- 异构查看器深度合并留作后续（如确有需求再评估），本 FR 按最小安全收敛交付。
