# 功能规格：图片查看器 UI 重做

> 状态：开发中　·　关联 PRD：FR-106（扩 FR-34/FR-38/FR-39）　·　分支：feature/fr-106-image-viewer-ui

## 1. 背景与目标
第九期界面设计系统下，图片灯箱（`MediaDetailPanel`）的图片交互已由 FR-105 增强（缩放/平移/旋转/导航/幻灯片），
但右侧**信息呈现与工具栏**仍偏粗糙：EXIF 为键值 `space-between` 两端拉满、长串 break-all 难读，光圈/快门/ISO 未标准化，
GPS 只给外部 OSM 链接、跳出站外，工具只有「下载原文件」一项，信息栏不可折叠、全屏态也常占右侧挤压图片。
本 FR 重做查看器的信息呈现与工具区，使其精致、沉浸、与网格操作一致，属第九期（P9）。

## 2. 需求（要什么）
- **EXIF 图标化 + 单位格式化**：右侧 EXIF 区由 `space-between` 键值改为**定宽 label 两列**，给相机/镜头/光圈/快门/ISO 加 tabler 图标；
  光圈/快门/ISO 标准化为 `f/1.8`、`1/200s`、`ISO 100`（后端已存 `f/2.8`、`1/60` 裸值，前端归一化补 `f/`/`s`/`ISO ` 前后缀，已含则不重复）。
- **GPS 改站内地图**：GPS 由「在外部地图打开」改为「在站内地图打开」（跳 `/map?lat=&lon=` 站内地图并定位），外部 OSM 链接保留为次要入口。
- **工具栏齐全**：查看器工具补齐**收藏 / 打标签 / 分享 / 下载**（旋转复用 FR-105 已加按钮），与网格操作一致，复用既有 favorite/tag/share/download 能力。
- **沉浸可折叠信息栏**：信息栏可一键折叠（纯图模式右侧详情收起，图片更沉浸）。
- **复制**：EXIF 摘要 / 文件路径 / GPS 坐标可一键复制（`useClipboard`）。
- 范围内：仅 `MediaDetailPanel` 右侧信息/工具区与 `/map` 接受定位参数。
- 不做（范围外）：不动 FR-105 的图片缩放/平移/导航/幻灯片/旋转交互逻辑、不动 FR-102 视频内嵌分支、不新增后端端点、不引新依赖。

## 3. 设计（怎么做）
- **EXIF 单位格式化纯函数**（`MediaDetailPanel.tsx` 内或 `utils`）：`formatAperture`/`formatShutter`/`formatIso`，
  对已带/未带标准标记的输入幂等归一化，便于穷举单测。
- **定宽两列 + 图标**：复用 FR-101 `SystemPage` 已确立的「定宽 label 列 + 紧凑 value 列」范式，给 EXIF 行加 tabler 图标（相机/镜头/光圈/快门/ISO）。
- **站内地图入口**：用 `useNavigate` 跳 `/map?lat=<lat>&lon=<lon>`；`MapPage` 读 `useSearchParams`，有合法 lat/lon 时把地图初始 `center` 定位到该坐标并提升 `zoom`。保留外部 OSM `Anchor` 为次要链接。
- **工具栏**：在标题区/信息区补 收藏（`setMediaFavorite`）、打标签（复用 `useBatchActions.openAddTag` + `BatchActionsModals`，单 id 列表）、分享（`ShareDialog`，`resourceType='media'`）、下载（既有 `/download` 链接）。收藏态由父层数据驱动，本地乐观切换 + 调接口。
- **可折叠信息栏**：本地 `infoCollapsed` state，折叠时右侧 `ScrollArea` 不渲染、左侧预览吃满宽度；提供「折叠/展开信息栏」按钮。
- **复制**：`@mantine/hooks` 的 `useClipboard`，给 EXIF 摘要、路径、GPS 坐标加复制按钮/图标。
- 无新 ADR；复用 FR-92 token、FR-93 品牌紫、FR-99/FR-101 既有范式。

## 4. 任务拆分
- [x] EXIF 单位格式化纯函数 + 穷举单测
- [x] EXIF 定宽两列 + tabler 图标
- [x] GPS 站内地图入口（跳 `/map?lat=&lon=`）+ `MapPage` 读参定位 + 保留外链
- [x] 工具栏补齐 收藏/打标签/分享/下载（复用既有能力）
- [x] 可折叠信息栏
- [x] EXIF/路径/GPS 一键复制
- [x] 文档同步：PRD 状态、CHANGELOG

## 5. 验收标准
- 单测：EXIF 为定宽两列、光圈/快门/ISO 标准化格式（`f/1.8`/`1/200s`/`ISO 100`）、GPS 有站内地图入口（跳 `/map` 带 lat/lon）、
  工具栏含收藏/分享/旋转/下载、信息栏可折叠、复制按钮存在；既有 FR-102/105 断言全绿。
- `frontend/` 下 `npm run build`（tsc -b）+ `npm run test`（vitest）全绿。
- 真机（标「待真机验」）：EXIF 排版精致 + 图标/单位、GPS 进站内地图定位、工具齐全、信息栏折叠沉浸。

## 6. 风险 / 待定
- 后端 EXIF 裸值格式（`f/2.8`/`1/60`）若与假设不符，格式化纯函数需保持幂等不破坏；已据 `internal/library/metadata.go` 确认。
- `/map` 读参定位为本 FR 顺带的最小增强，不扩展地图其它能力（YAGNI）。
