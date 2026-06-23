# 功能规格：页眉「更新可用」提示

> 状态：开发中　·　关联 PRD：FR-58　·　分支：main

## 1. 背景与目标
属于第六期（界面与运维完善）。FR-46 已实现服务器端二进制自更新，但「检查更新」入口深藏在「系统信息」tab 的应用更新区块，用户不主动进入就感知不到有新版本。FR-58 在全局页眉加一个「更新可用」提示：有新版本时常驻展示、点击直达更新区，让用户在任意页面都能第一时间发现并跳转更新。

## 2. 需求（要什么）
- 范围内：
  - 页眉（`AppShell.Header`）新增「更新可用」指示器，**仅当更新检查结果为「有更新」（`has_update=true`）时显示**；并展示目标版本号（如「v0.9.0 可更新」）。
  - 数据来源**复用 FR-46 已有的更新检查端点 + 其后端 TTL 缓存**：页眉挂载时按持久化频道调一次 `GET /api/system/update/check`（**非 force**，命中后端 10 分钟 TTL 缓存即廉价返回），不新增端点、不绕开缓存、不 force。
  - 检查失败 / 无更新 / 未登录场景：页眉**不显示**指示器，失败**静默**（复用 FR-46 优雅降级语义），不报错、不影响其余布局渲染。
  - 点击指示器 → 用 React Router 导航到「系统信息」tab 的应用更新区块（`/system?tab=system`），尽量定位到更新卡片（锚点 / 滚动），不整页 reload。
- 不做（范围外）：
  - 不碰 FR-46 的检查 / 下载 / 重启 / 回滚逻辑本身，不碰频道设置交互。
  - 不新增轮询定时器（一次挂载检查即可，后端缓存兜底），不做 WebSocket/SSE 推送。
  - 不自动触发下载 / 更新。
  - 不在页眉新增频道选择（频道沿用持久化设置）。

## 3. 设计（怎么做）
- 新增组件 `frontend/src/components/UpdateIndicator.tsx`：
  - 挂载时先读持久化更新频道（`getSettings()` → `update_channel`，读失败回退 `stable`，与 `SystemPage` 一致），再调 `systemApi.checkUpdate(channel, false)`。
  - 仅当返回 `has_update===true` 时渲染一个带提示徽标的 `ActionIcon`（图标 + `Tooltip`「有新版本可更新」，按钮文案含目标版本 `latest||tag`）。
  - `catch` 静默（不 setState 错误、不抛出）；无更新 / 失败均返回 `null`，不影响页眉其余元素。
  - 点击 → `useNavigate()` 导航到 `/system?tab=system`，并附 hash 锚点 `#update`（滚动定位）。
- `AppLayout` 在页眉右侧（`ScanTaskIndicator` 旁）挂载 `<UpdateIndicator />`。
- `SystemPage` 应用更新卡片加可定位锚点 `id="update"`（仅加 `id`，不改更新交互）。
- 复用既有契约：检查端点返回字段 `has_update`（是否有更新）、`latest`/`tag`（目标版本）。无新 ADR、无新依赖、无后端改动。

## 4. 任务拆分
- [x] 写 vitest（mock 更新检查 api）：有更新→渲染提示 + 点击导航 `/system`；无更新 / 失败→不渲染、不抛错。
- [x] 新增 `UpdateIndicator` 组件并接入 `AppLayout` 页眉。
- [x] `SystemPage` 更新卡片加锚点 `id="update"`。
- [x] 文档同步：PRD 状态、CHANGELOG 未发布段、ARCHITECTURE（前端页眉数据流）。

## 5. 验收标准
- 检查返回「有更新」→ 页眉渲染提示（含目标版本号）；点击触发 `navigate` 到 `/system?tab=system#update`。
- 检查返回「无更新」 / 检查失败（reject）→ 页眉**不**渲染提示，且不抛错、不影响其余布局（页眉 logo / 用户名 / 主题 / 退出仍在）。
- `npx tsc --noEmit` + `npx vitest run` 全量绿；改动文件 eslint 干净。
- 真机（需用户确认）：构造「有更新」（或 mock 后端响应）下页眉显提示、点击跳更新区；「无更新」下页眉无提示。

## 6. 风险 / 待定
- 锚点定位依赖 `ConsolePage` 默认进入系统信息 tab 且 `SystemPage` 卡片带 `id="update"`；若 tab 切换为懒挂载（`keepMounted={false}`），首次进入需等卡片渲染后浏览器再按 hash 滚动，定位为「尽力而为」，做不到精确滚动时至少跳到 system tab 页（满足验收下限）。
