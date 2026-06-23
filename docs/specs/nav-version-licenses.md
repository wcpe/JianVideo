# 功能规格：版本号与开源协议入口移入导航

> 状态：开发中　·　关联 PRD：FR-61　·　分支：feature/fr-61-nav

## 1. 背景与目标

FR-57 把版本号与「开源协议」入口放在全局页脚（`AppShell.Footer`），页脚常驻占用一行垂直空间。第七期（P7）做界面交互完善，FR-61 取代 FR-57 的页脚展示形态：移除页脚，把「版本号 + 开源协议链接」收进左侧导航底部（与 FR-54 的收缩按钮同区），并补齐移动端抽屉（原页脚在移动端可见，移除后需补，否则移动端丢失版本与协议入口）。仅前端布局重组，无后端、无新依赖、无新 ADR。

## 2. 需求（要什么）

- 范围内：
  - 移除 `AppShell.Footer` 及 `AppShell` 的 `footer` prop。
  - 桌面 `AppShell.Navbar` 底部（收缩/展开按钮一带，置于其上方）展示：版本号文本「JianVideo v{appVersion}」+「开源协议」链接（→ `/licenses`）。
  - 适配收缩态（64px）：收缩时版本/协议不展示长文本（避免截断），改用图标 + Tooltip 或精简展示，参照 `renderNavLink` 收缩态模式。
  - 移动端 `Drawer` 底部补上版本号 + 「开源协议」链接（点击后关闭抽屉）。
  - 版本号沿用现有取数（`getSystemInfo().app_version`），拉取失败静默不显版本、不阻塞布局（行为不变）。
- 不做（范围外）：
  - 不动协议页 `/licenses` 自身（FR-57 已交付）。
  - 不动版本号取数来源 / 接口。
  - 不动 FR-54 收缩按钮的既有行为与持久化。

## 3. 设计（怎么做）

只改 `frontend/src/components/AppLayout.tsx` 与其测试 `AppLayout.test.tsx`：

- 删除 `AppShell` 的 `footer={{ height: 36 }}` 与整段 `AppShell.Footer`。
- 桌面 Navbar 底部新增「版本 + 协议」区，置于收缩按钮 `Group` 上方：
  - 展开态：`Text` 显示「JianVideo v{appVersion}」，下方「开源协议」`Link`（小字 dimmed），与现页脚文案一致。
  - 收缩态：仅渲染开源协议图标按钮（如 `IconLicense`），hover 出 Tooltip「开源协议」；版本号在收缩态以 Tooltip 承载（hover 协议图标或单独版本图标时展示），避免 64px 内文字截断。
- 移动端 `Drawer` 的 `Stack` 底部追加版本文本 + 「开源协议」链接（`onClick` 调 `closeDrawer`）。
- 复用既有 `appVersion` state 与 `getSystemInfo` effect，不新增取数逻辑。

无架构决策变更（仅 UI 入口位置调整），无新 ADR。

## 4. 任务拆分
- [ ] 测试先行：导航底显版本/协议链接、页脚已移除、收缩态、移动端 Drawer 含协议入口（先红）
- [ ] 移除 `AppShell.Footer` 与 `footer` prop
- [ ] Navbar 底部加版本 + 协议（展开/收缩两态）
- [ ] 移动端 Drawer 底部补版本 + 协议
- [ ] 文档同步：PRD 状态、ARCHITECTURE、CHANGELOG（API 无变更）

## 5. 验收标准
- 桌面展开态：左侧导航底部可见版本号文本与「开源协议」链接（href=`/licenses`）；页面不再渲染 `AppShell.Footer`（footer 区移除）。
- 桌面收缩态（64px）：版本/协议以图标 + Tooltip 或精简形式展示，无文字截断；收缩/展开切换行为（FR-54）不受影响。
- 移动端抽屉：打开抽屉后底部可见版本号与「开源协议」链接，点击链接关闭抽屉。
- 自动化：`npx tsc --noEmit` 与 `npx vitest run`（含 AppLayout 新增用例）全绿。
- 真机维度：移动端断点下抽屉版本/协议入口与桌面收缩态视觉占位，标「待真机验」（窄屏视觉占位非单测可完全覆盖）。

## 6. 风险 / 待定
- 收缩态 64px 宽度有限，版本号长文本必须以 Tooltip 承载，不能直接平铺；已在设计中规避。
