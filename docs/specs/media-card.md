# 功能规格：媒体卡与网格重设计（FR-99）

> 状态：开发中　·　关联 PRD：FR-99（扩 FR-D/FR-E/FR-69）　·　分支：feature/fr-99-media-card

## 1. 背景与目标

第九期界面打磨。时间轴（`TimelineView`）与目录浏览（`DirectoryBrowser`）的媒体卡此前
信息密度低（文件名 / 类型徽标 / 大小 / 时长各占一行，卡片偏高）、缺少卡片悬停快捷操作、
视频卡与图片卡视觉无区分、选中态反馈弱、批量操作只能靠右键菜单。本 FR 在不动后端、
不引新依赖、不重排页面整体布局的前提下重设计媒体卡与网格，复用 FR-69 多选基建、
FR-91 批量能力、FR-92 阴影 / 圆角 token、FR-93 品牌紫。属第九期（界面打磨）。

## 2. 需求（要什么）

- **信息密度**：媒体卡信息由「多行徽标」精简为缩略图底部渐变叠层内的单行紧凑信息
  （文件名 + 类型 + 大小 +（视频）时长），卡片更矮、网格更密。
- **hover 操作浮层**：卡片悬停浮出快捷操作（播放 / 收藏 / 更多），无需点进详情。
- **视频时长角标 + 播放叠层**：视频卡缩略图右下角叠时长角标、中心淡播放三角；图片卡无。
- **选中态强反馈**：多选选中项加品牌紫粗边框 + 勾选叠层。
- **sticky 批量操作条**：选中 ≥1 项时页面浮出 sticky 条（「已选 N 项」+ 删除 / 加相册 /
  打标签 / 打包下载 + 清除选择），复用 FR-91 已有批量回调。
- **响应式列数**：网格用 `minmax` 自适应列数，超宽屏增列、窄屏减列。

- 范围内：`MediaThumbnail.tsx`（叠层能力）、`TimelineView.tsx`、`DirectoryBrowser.tsx`
  的卡片与网格、`TimelinePage.tsx`/`BrowsePage.tsx` 的 sticky 批量条接入；新增
  `MediaCardOverlay.tsx`（卡片叠层复用件）、`SelectionBatchBar.tsx`（sticky 批量条）。
- 不做（范围外）：后端改动、引新依赖、页面整体布局重排、新增多选逻辑（消费已有 state）、
  列表档（`displayMode==='list'`）行内布局重设计（仅图标档网格卡重设计）。

## 3. 设计（怎么做）

- **MediaThumbnail**：新增可选 `overlay`（ReactNode，叠在缩略图之上）与
  `objectFit`（默认 `contain`，网格卡可传 `cover` 更饱满），不破坏既有 202 轮询 / 降级逻辑。
- **MediaCardOverlay**：纯展示组件。底部渐变层承载单行信息；视频右下角时长角标、
  中心播放三角；选中时叠品牌紫勾选层。接收 `file`/`isImage`/`isVideo`/`selected`/`checkboxMode`。
- **hover 浮层**：卡片右上角一组 `ActionIcon`（播放 / 收藏 / 更多），默认透明、
  `hover-card:hover` 时显现（沿用 FR-96 `.hover-card` 过渡），各按钮 `stopPropagation`。
- **SelectionBatchBar**：sticky 定位（页面底部），`count>0` 才渲染；消费父页已持有的
  selectedIds 与 FR-91 批量回调 + 删除 + 清除选择。
- **响应式列数**：图标档网格由固定 `cols` 改 `SimpleGrid type="container"` + `minmax`
  自适应（或等效），超宽增列窄屏减列。

无新 ADR（未引入新架构决策 / 新技术 / 新依赖）。

## 4. 任务拆分

- [x] 新建 `MediaCardOverlay.tsx`（角标 / 播放叠层 / 选中叠层 / 底部信息层）
- [x] 新建 `SelectionBatchBar.tsx`（sticky 批量条）
- [x] `MediaThumbnail.tsx` 支持 `overlay` 与 `objectFit`
- [x] `TimelineView.tsx` 卡片重设计 + 响应式列数 + hover 浮层 + 批量条
- [x] `DirectoryBrowser.tsx` 图标档卡片重设计 + 响应式列数 + 批量条
- [x] `TimelinePage.tsx`/`BrowsePage.tsx` 接入 sticky 批量条（消费已有多选 state）
- [x] 测试先行：视频角标 / 播放叠层 / hover 浮层 / 选中态 / 批量条出现且含按钮
- [x] 文档同步：PRD 状态、CHANGELOG

## 5. 验收标准

- 视频卡缩略图渲染时长角标与中心播放叠层；图片卡不渲染（自动化断言）。
- 卡片 hover 浮层含播放 / 收藏 / 更多操作（自动化断言存在）。
- 多选选中项有品牌紫边框 / 勾选叠层（`data-selected` + 视觉断言）。
- 选中 ≥1 项时 sticky 批量条出现，含「删除 / 加相册 / 打标签 / 打包下载」按钮且回调可触发
  （自动化断言）。
- 既有 `TimelineView`/`DirectoryBrowser`/`MediaThumbnail` 测试全绿，`npm run build`
  与 `npm run test` 全绿。
- **真机验收（需用户确认）**：卡片 hover 浮层、视频角标、选中批量条、超宽 / 窄屏列数
  自适应——标「待真机验」，自动化测试不替代。

## 6. 风险 / 待定

- 信息叠层在浅色 / 高亮缩略图上的可读性依赖底部渐变遮罩强度——真机抽查。
- 响应式列数用 `SimpleGrid type="container"` 需祖先无影响 container 查询的约束；
  退化方案保留断点式 `cols`。
