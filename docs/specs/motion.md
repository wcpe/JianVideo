# 功能规格：全局微交互与动效

> 状态：开发中　·　关联 PRD：FR-96　·　分支：feature/fr-96-motion

## 1. 背景与目标
第九期界面打磨。当前界面交互偏「静态生硬」：卡片/按钮/导航项 hover 无平滑反馈、页面切换是硬替换无过渡、路由切换与数据加载时缺乏全局进度提示。本 FR 在 FR-92~95 地基之上做全局微交互与动效打磨，提升精致感与操作反馈。属于第九期（P9）。

## 2. 需求（要什么）
- **hover 过渡与抬升**：卡片 / 按钮 / 导航项 hover 加平滑过渡（背景 / 边框 / 轻微 translateY 抬升 + 阴影），约 120–160ms，复用 FR-92 阴影 token。
- **路由切换动画**：页面切换加轻量 fade/slide。
- **全局顶部加载条**：路由切换 / 数据加载时顶部细进度条（nprogress 风格），自实现、不引第三方库。
- **全部动效带 `prefers-reduced-motion: reduce` 兜底**：开启「减少动态效果」时禁用过渡 / 动画。
- 范围内：纯 CSS + Mantine 既有能力；hover 全局规则放 `index.css`；路由过渡与顶部进度条组件放 `App.tsx` 包装 `<Routes>`。
- 不做（范围外）：不重排布局、不改 `AppLayout.tsx`、不改各页业务逻辑、不引新依赖、不做复杂编排动画。

## 3. 设计（怎么做）
- `index.css`：
  - 给可点表面（`.mantine-Card-root`、`.mantine-Button-root`、`.mantine-NavLink-root` / `[data-active]` 导航项）加 `transition`（background-color / border-color / box-shadow / transform，约 140ms）与 hover 抬升（`translateY(-2px)` + 加深阴影，复用 `--mantine-shadow-*` token）。
  - 顶部进度条 keyframes（`jianvideo-topbar-*`）。
  - 在已有的 `@media (prefers-reduced-motion: reduce)` 块内补上对上述选择器的 `transition: none` / `transform: none` / `animation: none`。
- `frontend/src/components/RouteTransition.tsx`：以 `useLocation().pathname` 为 key，用 CSS 类 `route-fade` 给页面挂载时做轻量 fade/slide-in；`prefers-reduced-motion` 下动画被 index.css 兜底关闭。
- `frontend/src/components/TopProgressBar.tsx`：自实现顶部进度条。监听 `useLocation` 变化触发一段「启动→爬升→完成淡出」状态机（纯 setTimeout + state），固定定位于视口顶部，宽度按进度变化，复用品牌紫；`prefers-reduced-motion` 下不做缓动（瞬时）。
- `App.tsx`：在 `<BrowserRouter>` 内、`<Routes>` 外挂 `<TopProgressBar />`，用 `<RouteTransition>` 包住 `<Routes>`。**不改 `AppLayout.tsx`**。

## 4. 任务拆分
- [x] 复制 spec 模板 → `docs/specs/motion.md`，PRD FR-96 置「开发中」
- [x] index.css：hover 过渡 + 抬升 + reduced-motion 兜底 + 顶部进度条 keyframes
- [x] `RouteTransition.tsx`、`TopProgressBar.tsx`
- [x] `App.tsx` 接入（不碰 AppLayout）
- [x] 测试：index.css reduced-motion 断言；两组件渲染断言
- [x] 文档同步：PRD 状态、CHANGELOG 未发布段末尾追加

## 5. 验收标准
- `frontend/` 下 `npm run build`（tsc -b）与 `npm run test`（vitest）全绿。
- index.css 在 `prefers-reduced-motion: reduce` 下对 hover / 路由 / 进度条动效均有禁用兜底（测试断言）。
- `RouteTransition` 包裹页面渲染不破坏内容；`TopProgressBar` 渲染不报错。
- **真机验收（需用户确认）**：切页面有过渡、卡片/按钮/导航 hover 有平滑反馈与抬升、路由/加载时顶部有进度条、系统开启「减少动态效果」后上述动效禁用。单元测试不替代真机验收。

## 6. 风险 / 待定
- jsdom 不渲染 CSS 与动画，hover/动画的视觉表现只能真机验收；单测覆盖到「规则存在 + reduced-motion 兜底 + 组件可渲染」。
- 顶部进度条自实现状态机用 setTimeout，需确保组件卸载时清理定时器，避免泄漏。
