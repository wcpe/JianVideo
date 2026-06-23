# 功能规格：时间轴可拖动 scrubber 与浮层预览

> 状态：开发中　·　关联 PRD：FR-68（补齐 FR-32 未实现的拖动）　·　分支：feature/fr-67-timeline

## 1. 背景与目标

FR-32「时间轴缩放与拖动」中，缩放（日/月/年）已交付，但「按时间拖动」从未实现——左侧日期轴是纯静态展示，滚动只能靠鼠标滚轮 + 被动 `IntersectionObserver` 加载更多。用户在大媒体库里要快速跳到某个时间段只能反复滚动。

本规格补齐 FR-32 的拖动能力：在时间轴右侧提供一条**可拖动的时间滑块（scrubber）**，拖动时浮层预览该位置对应的时间段（日期文本 + 该段首项缩略图），松手后滚动跳转到对应日期分组。纯前端交互，无需后端改动。属于第七期界面交互完善。

## 2. 需求（要什么）

- 时间轴右侧新增竖向 scrubber 轨道，与当前分组列表（按 granularity 分组后的 groups）一一对应：轨道顶部对应最新分组、底部对应最旧分组。
- 拖动 scrubber 时，按指针在轨道内的纵向比例（0..1）映射到目标分组下标，实时浮层预览该分组的日期标签（复用分组键，按粒度展示）与该分组首个媒体的缩略图（`/api/library/thumbnail/:id`）。
- 松手（pointer up）后，滚动跳转到该目标分组（复用 `useWindowVirtualizer.scrollToIndex`）。
- 位置 ↔ 目标分组 的映射抽为纯函数，便于穷举单测。
- 分组为空 / 加载中 / 错误态不渲染 scrubber。
- 键盘可达性：scrubber 元素可聚焦，上下方向键移动一个分组并跳转（尽力保留，非硬性）。

范围内：
- 纯前端 scrubber 组件 + 位置映射纯函数 + 与虚拟器 `scrollToIndex` 的接线。
- 拖动浮层（日期文本 + 首项缩略图）。

不做（范围外）：
- 后端接口改动（缩略图端点已存在）。
- 连续时间轴（按真实时间戳线性映射像素）——本期按「分组等分」映射，足够定位且与现有分组模型一致；线性时间映射如有需要后续再提。
- 移动端触摸优化的额外手势（pointer 事件已统一覆盖鼠标 / 触摸）。

## 3. 设计（怎么做）

### 位置映射（纯函数，`utils/timeline.ts`）

新增 `positionToGroupIndex(fraction: number, groupCount: number): number`：
- 入参 `fraction` 为指针在轨道内的纵向比例（0=顶部=最新分组，1=底部=最旧分组），`groupCount` 为分组总数。
- 把 `[0,1]` 均匀切成 `groupCount` 段，返回所落段的下标，并钳制到 `[0, groupCount-1]`。
- `groupCount<=0` 返回 0；`fraction` 超界先钳制到 `[0,1]`。
- 无副作用，便于穷举单测（边界 0 / 1 / 越界 / 单组）。

### scrubber 组件（`components/TimelineScrubber.tsx`）

- 接收 `groups: DateGroup[]`、`onSeek(index)`，自身管理「是否拖动中」与「当前 hover/拖动下标」局部状态。
- 竖向轨道，绑定 `onPointerDown/Move/Up`（`setPointerCapture` 保证拖出轨道仍跟手），按轨道 `getBoundingClientRect` 计算 `fraction` → `positionToGroupIndex` → 当前下标。
- 拖动中渲染浮层（绝对定位于指针纵向位置附近）：日期标签（复用 `splitDate` 同源的格式，直接展示分组键）+ 该分组 `files[0]` 缩略图（复用 `MediaThumbnail`）。
- `onPointerUp` 调 `onSeek(index)` 并退出拖动态。
- 可聚焦（`tabIndex=0` + `role="slider"` + `aria-*`），方向键移动一个分组并 `onSeek`。

### 接线（`components/TimelineView.tsx`）

- 既有 `useWindowVirtualizer` 增用 `scrollToIndex(index, { align: 'start' })` 作为跳转实现。
- 在虚拟化容器旁渲染 `TimelineScrubber`，`onSeek` 回调里调 `virtualizer.scrollToIndex`。
- scrubber 仅在有分组（非 loading / 非 error / 非空）时渲染。

## 4. 任务拆分

- [x] `positionToGroupIndex` 纯函数 + 单测（边界穷举）
- [x] `TimelineScrubber` 组件（pointer 拖动 + 浮层预览 + 键盘可达）
- [x] TimelineView 接线 `scrollToIndex`
- [x] 组件测试：拖动映射到目标分组、松手触发 onSeek
- [x] 文档同步：PRD FR-68/FR-32 状态、CHANGELOG

## 5. 验收标准

- [x] `positionToGroupIndex` 单测覆盖：顶部→最新组、底部→最旧组、越界钳制、单组、空组。
- [x] 组件测试：在 scrubber 上模拟拖动到某纵向位置，浮层显示对应分组日期；松手触发 `onSeek` 携带正确下标。
- [x] `npx tsc --noEmit` 与 `npx vitest run` 全绿。
- [ ] 真机维度（待真机验）：浏览器内实际拖动 scrubber 浮层预览跟手、松手滚动跳转到对应日期分组、缩略图正确加载——jsdom 无真实布局/滚动，需用户在真机复验。

## 6. 风险 / 待定

- jsdom 无 `getBoundingClientRect` 真实尺寸与 `scrollToIndex` 真实滚动：组件测试通过注入/桩化轨道尺寸验证映射与回调，真实滚动效果归入真机验收。
- 「分组等分」映射在分组数很少时每段较大，定位粒度受分组数限制；与现有分组模型一致、可接受，连续时间映射留作后续。
