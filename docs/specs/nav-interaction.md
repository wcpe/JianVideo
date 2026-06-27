# 功能规格：导航交互完善（FR-115）

> 状态：开发中　·　关联 PRD：FR-115（扩 FR-54/FR-95/FR-96）　·　分支：feature/fr-c-app-shell

## 1. 背景与目标
应用框架（FR-95）落地后的交互打磨。当前左侧导航的收缩/展开按钮置于 navbar 顶部、logo 点击回首页、非激活导航项仅靠 Tooltip 提示而缺背景反馈。本 FR 完善这三处导航交互，提升可发现性与一致的悬停反馈。属 P11。

## 2. 需求（要什么）
- 展开/收缩按钮移到左侧导航底部右下角（从 navbar 顶部移走）。
- 点击 logo 切换导航展开/收缩；移除 logo「点击回首页」——回首页改由导航「时间轴」项进入（「时间轴」项即 `/` 首页入口，已具备，无需新增）。
- 补齐所有导航项的 hover 背景与过渡态：非激活项也要有可见 hover 背景 + 平滑过渡，复用品牌紫/设计 token。
- 范围内：仅改 `AppLayout` 壳（收缩按钮位置、logo 行为、导航项 hover 样式）。
- 不做（范围外）：移动端抽屉交互改动；导航项集合 / 路由表改动；FR-114 刷新按钮（另条 FR）。

## 3. 设计（怎么做）
- 无新 ADR（纯前端壳交互，不涉架构决策）。
- 收缩按钮移底部：把原 navbar 顶部的收缩 `ActionIcon` 移到 navbar 底部、版本/协议入口下方，靠右（`justify="flex-end"`）下角放置；aria 与图标随收缩态切换逻辑不变。收缩态（64px）下亦靠右下，居中兜底避免破版。
- logo 切换收展：`AppShell.Header` 的品牌标志由 `<Link to="/">` 改为 `UnstyledButton`，`onClick` 调 `toggleNavCollapsed()`；移除回首页语义。
- 导航项 hover：`renderNavLink` 给 `Group` 加 hover 背景与 `transition`。用 CSS 实现（`index.css` 加 `.nav-link` 类 + `:hover` 背景），复用 token（`--mantine-color-default-hover` 或品牌紫浅底的较弱态）；激活项保持品牌紫浅底不被 hover 覆盖；`prefers-reduced-motion` 下过渡兜底关闭。
- 约束：`navItems` 扁平真源不动；命令面板 flat map 复用不破坏；FR-83 分组渲染沿用；FR-54 收缩持久化不回归。

## 4. 任务拆分
- [x] `AppLayout`：收缩/展开按钮移到 navbar 底部右下角
- [x] `AppLayout`：logo 由回首页改为切换收展（`UnstyledButton` + `toggleNavCollapsed`）
- [x] `AppLayout` + `index.css`：补齐所有导航项 hover 背景与过渡态
- [x] `AppLayout.test.tsx` 新增断言（收缩按钮居底、点 logo 切换收展、导航项有 hover 类）
- [x] 文档同步：PRD 状态、spec、CHANGELOG

## 5. 验收标准
- 收缩/展开按钮位于左侧导航底部右下角。
- 点击 logo 在展开/收缩两态间切换且状态持久化（localStorage）；不再回首页。
- 点击「时间轴」导航项回首页（`/`）正常。
- 所有导航项（激活与非激活）均有可见 hover 背景与平滑过渡，`prefers-reduced-motion` 下过渡关闭。
- 既有 `AppLayout.test.tsx` 断言全绿（含 FR-54/61/83/85/74/95 回归）。
- `frontend/` 下 `npm run build`（tsc -b）+ vitest 全绿。
- 真机维度（桌面 + 窄屏）：收缩按钮位置 / logo 切换 / hover 反馈 — 标「待真机验」，由主控统一真机抽查。

## 6. 风险 / 待定
- 既有测试 `「收起导航」按钮位于 navbar 顶部（先于首个导航链接出现）`（FR-95）需相应改为断言按钮在导航链接之后（底部）。
- logo 不再是 `<Link>`：若有测试断言 logo href 指向 `/` 需相应调整（当前测试未发现此断言）。
