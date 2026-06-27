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

## 7. 真机走查后续修复 / 增强（v0.18.0 之后）

> v0.18.0 发布后真机走查反馈，三处导航交互打磨：① 底部重排、② 版本号位置、③ 可拖拽调宽。属本 FR 后续修复增强（不改 PRD FR-115 状态行，记 CHANGELOG）。

### 7.1 导航底部重排（解决展开态底部挤成多行）
- **版本号移到页眉**：当前版本号（取自系统信息）移到页眉品牌「JianVideo」**右侧**展示，`v<x.y.z>` 小号 + dimmed、与 logo 同一行（`data-testid="app-version"`，缺省不显）。
- **导航左下角只保留「开源协议」**：与右下角收缩按钮**同处底部一行**——展开态 `justify="space-between"`（左协议、右收缩），不再换行、不再平铺版本号长文本。
- **收缩态（64px）**：底部整行居中；协议改图标（`IconLicense`）+ Tooltip，收缩按钮仍在其后；避免 64px 内文字截断。
- **移动端抽屉底部**：不受导航宽度/收缩影响，保留「版本号 + 开源协议」原样（`renderDrawerVersionLicense`）。

### 7.2 导航栏可拖动调整宽度（扩 FR-54）
- navbar 右边缘加**拖拽手柄**（`.nav-resize-handle` 窄竖条，绝对定位贴右缘、`cursor: col-resize`、hover/拖拽出品牌紫提示条；`role="separator"` + `aria-valuemin/max/now` 无障碍）。
- 拖动调整**展开态**宽度，夹紧 **min 160px / max 360px**；宽度持久化到 `localStorage`（键 `jianvideo.nav.width`），刷新保留。
- 新增 `useNavWidth` hook（与 `useNavCollapsed` 同风格）：`[width, setWidth]`，内部纯函数 `clampNavWidth` 负责夹紧/取整/非有限数回退默认（默认 180，沿用原固定展开宽度）；`setWidth` 自动夹紧并持久化。
- **仅展开态生效**：收缩态固定 64px 图标态（不显手柄、不可拖）；移动端抽屉无 navbar 不受影响。
- 拖拽中给 navbar 加 `.nav-resizing` 关闭宽度过渡动画（避免逐帧过渡卡顿）；`prefers-reduced-motion` 下手柄提示条过渡关闭。
- `AppShell.Navbar` 的 `width` 由原固定 `NAVBAR_WIDTH_EXPANDED` 改为读 `navWidth`（收缩态仍 `NAVBAR_WIDTH_COLLAPSED=64`）。

### 7.3 验收补充
- 页眉品牌后显示版本号（小号 dimmed）；导航底部左协议、右收缩单行不换行（展开/收缩态均可达）。
- navbar 右缘可拖拽改宽、夹紧 160–360、刷新保留；收缩态/移动端不受影响。
- 新增 `useNavWidth.test.ts`（夹紧/持久化/越界）；`AppLayout.test.tsx` 调整版本号断言（移至页眉）+ 新增拖拽手柄/拖拽更新宽度/收缩态无手柄断言。
- `npm run build` + vitest 全绿；真机维度（拖拽手感、版本位置、底部单行、`col-resize` 光标）标「待真机验」。

## 8. 真机走查第二批后续修复 / 增强（v0.18.0 之后）

> v0.18.0 发布后第二批真机走查反馈，导航交互三处打磨。属本 FR 后续修复增强（不改 PRD FR-115 状态行，记 CHANGELOG）。

### 8.1 收缩态隐藏开源协议入口
- 导航**收缩态（64px）完全不显示**「开源协议」入口（原图标 + Tooltip 取消），底部仅留收缩/展开按钮，避免 64px 图标态拥挤。
- 展开态保持左下「开源协议」文字链接不变。
- `renderLicenseLink(collapsed)` 收缩态返回 `null`；`AppLayout.test.tsx` 原「收缩态仍提供协议入口」断言改为「收缩态不再展示协议入口、仅留收缩/展开按钮」。

### 8.2 导航收缩↔展开切换加宽度过渡动画
- 给 `navbar` 宽度加平滑过渡（`.mantine-AppShell-navbar { transition: width 180ms ease }`），切换收/展时宽度渐变而非突变。
- **保留拖拽不动画机制**：拖拽调宽时 `.nav-resizing` 关闭该过渡（拖拽逐帧不要动画）。
- 图标↔文字标签随切换轻微淡入（`.nav-link .mantine-Text-root` 透明度过渡，最小改动、不引位移）。
- `prefers-reduced-motion` 下关闭上述新增过渡。

### 8.3 导航相关按钮配色统一
- 全排查导航相关按钮各状态配色，统一到设计 token、不写死颜色：
  - 主导航项默认 / hover / 激活 / focus：hover `--mantine-color-default-hover`、激活 `--mantine-color-purple-light`、focus 全局品牌紫 outline，均已 token 化。
  - 底部收缩/展开、logo 切换、拖拽手柄沿用既有 token（gray subtle / 品牌紫）。
  - **AnchorNav 的 `NavLink` 显式 `color="purple"`**：激活态显式锁定品牌紫，不依赖隐式 `primaryColor`，避免 Mantine 默认 active 串成蓝色，与主导航激活态一致。
- 跨亮 / 暗主题核对一致。

### 8.4 验收补充
- 收缩态底部仅收缩/展开按钮、无开源协议入口；展开态左下协议链接保留。
- 切换收/展宽度平滑过渡（约 180ms）、拖拽调宽仍无动画；`prefers-reduced-motion` 下过渡关闭。
- 导航相关按钮（含 AnchorNav 激活态）跨主题配色一致、无误蓝、激活/ hover 不互相覆盖——标「待真机视觉复验」。
- `npm run build` + vitest 全绿。
