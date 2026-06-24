# 功能规格：应用框架重构（FR-95）

> 状态：开发中　·　关联 PRD：FR-95（扩 FR-54/FR-55/FR-61）　·　分支：feature/fr-95-app-frame

## 1. 背景与目标
第九期界面升级。应用框架壳（`AppLayout`）经设计走查暴露问题：顶栏元素挤一排、侧栏激活态仅靠变色不明显、logo 存在感弱、深层页缺路径指引、收起导航入口埋在底部不易发现。本 FR 重构应用框架壳，复用第九期地基（FR-92 token / FR-93 品牌紫 / FR-94 组件规范），提升导航清晰度与品牌感。属 P9。

## 2. 需求（要什么）
- 顶栏分组 + 用户头像下拉菜单：把「用户名 + 退出登录」收进一个 Mantine `Menu`（头像触发），减少顶栏拥挤；命令面板/更新提示/扫描指示/主题切换仍外露。
- 侧栏激活态 pill：按当前路由 `useLocation` 判断激活项，激活项用品牌紫浅底（`var(--mantine-color-purple-light)`）+ 紫字，圆角用 token；收缩态下激活态亦可辨。
- 放大 logo：顶栏左上品牌标志放大、提对比，品牌存在感更强。
- 深层页面包屑：目录浏览 / 播放页 / 相册（含相册详情）三类深层页加路由层级面包屑，新增 `PageBreadcrumbs` 组件。
- 收起导航入口前移：把「收起导航」按钮从 navbar 底部移到 navbar 顶部，更易发现。
- 范围内：仅改 `AppLayout` 壳与三类深层页头部接面包屑、新增 `PageBreadcrumbs`。
- 不做（范围外）：路由表改动（App.tsx 不动）；其余深层页面包屑（后续 FR）；引新依赖；FR-96+ 的动效/无障碍/空态等。

## 3. 设计（怎么做）
- 无新 ADR（纯前端壳重构，不涉架构决策）。
- 用户菜单：`AppLayout` 顶栏用 Mantine `Menu`，target 为头像 `Avatar`（取用户名首字），下拉含用户名（dimmed 标签）与「退出登录」项。
- 激活 pill：`renderNavLink` 接入 `useLocation`，按 `path` 与 `pathname` 匹配（`/` 用精确匹配，其余用前缀匹配），激活态加 `--mantine-color-purple-light` 背景 + 紫字 + token 圆角；收缩态同样高亮底。
- logo：`AppShell.Header` 内 `IconVideo` 由 22 放大至更大尺寸并提对比。
- 面包屑：新增 `PageBreadcrumbs`（Mantine `Breadcrumbs` + react-router `Link`），各深层页顶部渲染，末项为当前页（非链接）。`PageBreadcrumb` 数据由各页就地构造（首页固定 + 当前页，相册详情追加相册名）。
- 收起入口前移：把 navbar 底部的收缩 `ActionIcon` 移到 navbar 顶部（logo 区下方、导航组上方），aria 与图标随收缩态切换的逻辑不变。
- 约束：`navItems` 扁平真源不动，命令面板 flat map 复用不破坏；分组渲染 `renderNavGroups` 沿用；复用品牌紫与 token、不写死颜色/圆角。

## 4. 任务拆分
- [ ] 新增 `PageBreadcrumbs` 组件 + 测试
- [ ] `AppLayout`：顶栏用户头像下拉菜单（含退出登录）
- [ ] `AppLayout`：侧栏激活态 pill（`useLocation` 判定，展开/收缩态均高亮）
- [ ] `AppLayout`：放大 logo
- [ ] `AppLayout`：收起导航入口前移至 navbar 顶部
- [ ] 目录浏览 / 播放页 / 相册三类深层页接 `PageBreadcrumbs`
- [ ] `AppLayout.test.tsx` 新增断言（用户菜单含退出登录、激活 pill、面包屑）
- [ ] 文档同步：PRD 状态、ARCHITECTURE（若涉及）、CHANGELOG

## 5. 验收标准
- 顶栏用户头像点击展开菜单，菜单内含「退出登录」；点击触发登出跳转 `/login`。
- 当前路由对应的导航项呈现品牌紫浅底激活 pill（展开态与收缩态均可辨）。
- 目录浏览 / 播放页 / 相册三类深层页渲染路由层级面包屑，末项为当前页。
- 「收起导航」按钮位于 navbar 顶部；收缩/展开行为与持久化（FR-54）不回归。
- 既有 `AppLayout.test.tsx` 断言全绿（含 FR-54/61/83/85/74 回归）。
- `frontend/` 下 `npm run build`（tsc -b）+ `npm run test`（vitest）全绿。
- 真机维度（桌面 + 窄屏）：顶栏分组 / 用户菜单 / 激活 pill / 面包屑 — 标「待真机验」，由主控统一真机抽查。

## 6. 风险 / 待定
- 用户菜单从「用户名 + 退出按钮」改为头像下拉，既有测试若直接断言「退出登录」按钮可见需相应改为先开菜单。
- 激活态前缀匹配需避免 `/` 误匹配所有路径（用精确匹配根路径）。
