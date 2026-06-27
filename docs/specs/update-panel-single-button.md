# 功能规格：应用更新面板交互重构（单按钮 + 缓存优先）

> 状态：开发中　·　关联 PRD：FR-112（扩 FR-62/FR-46）　·　分支：feature/fr-b-console-settings

## 1. 背景与目标

当前「应用更新」区块有两个按钮——「检查更新」（force=false 走后端 TTL 缓存）与「获取更新」（force=true 强制重查），且进入页面会先回填本地缓存、再后台 force=false 自动联网刷新。双按钮语义易混淆、进入即联网在国内直连 GitHub 常超时拖慢页面。本变更简化为**单按钮 + 缓存优先、不自动联网**。属于 P11（界面交互打磨）。

## 2. 需求（要什么）

- 范围内：
  - 合并为**一个「检查更新」按钮**，点击 = **force 强制直连重查**（去掉「获取更新」按钮）。
  - 进入「应用更新」tab：**仅展示上次本地缓存**——
    - 无缓存 → **不显示更新区**（不显示版本/更新提示/占位引导文案）。
    - 有缓存 → 直接显示缓存的版本与发布说明。
    - **不自动联网**（去掉进入即后台 force=false 刷新的逻辑）。
  - 切换频道（正式版/测试版）时同样只回填该频道缓存、不自动联网。
  - 执行更新**成功后清理本地缓存**并重新拉取（下次进入会重新 force 拉）。
- 不做（范围外）：
  - 不改后端更新检查/应用/回滚/进度端点与契约。
  - 不改 `UpdateIndicator` 页眉提示逻辑（它仍非 force 命中后端 TTL 缓存）；仅需保证其消费的本地缓存读写契约不被破坏。

## 3. 设计（怎么做）

- 纯前端，无后端/API/数据模型改动，无新依赖、无新 ADR。
- `SystemPage` 应用更新区块：
  - 删除「获取更新」按钮，「检查更新」按钮 onClick 改为 `handleCheckUpdate(true)`（force）。
  - 删除进入/切频道时「先回填缓存再后台 force=false 刷新」的副作用，改为**仅同步回填该频道本地缓存**（`loadCachedUpdate(channel)`），不发起网络请求。
  - 无缓存时 `updateInfo` 为 null → 既有「无 updateInfo 且无 error」分支已渲染引导占位；按需改为**不显示更新区**（占位文案也不显示），仅保留按钮区。
  - `handleCheckUpdate` 成功后仍写本地缓存（供下次进入展示）。
  - 更新 `runApply` 成功路径：apply 成功后清理本地缓存（`clearCachedUpdate(channel)`）并重新 force 拉一次（`handleCheckUpdate(true)`），保证下次进入展示最新。
- `update-cache.ts` 新增 `clearCachedUpdate(channel)`（移除该频道缓存键），与既有 load/save 同构、安全兜底。
- `UpdateIndicator` 不改：它直接调 `checkUpdate(channel,false)`、不读本地缓存键，故缓存键删除不影响其工作；新增 `clearCachedUpdate` 不破坏既有 load/save 契约。

## 4. 任务拆分

- [x] 复制模板写本 spec；PRD FR-112 状态「计划」→「开发中」
- [x] 测试先行：`update-cache.test.ts` 加 `clearCachedUpdate`；`SystemPage.test.tsx` 改更新区断言（单按钮 force / 无缓存不显示更新区 / 有缓存进入不联网 / 更新成功清缓存重拉）
- [x] 实现 `clearCachedUpdate`；重构 `SystemPage` 应用更新区块（单按钮 + 缓存优先不联网 + 成功清缓存重拉）
- [x] 文档同步：PRD 状态、ARCHITECTURE（应用更新交互描述）、CHANGELOG 未发布段
- [x] 前端验证门：`npm run build` + 相关 vitest 全绿

## 5. 验收标准

- 面板仅一个更新按钮（「检查更新」），无「获取更新」。
- 无缓存进入：不显示版本/更新区。
- 有缓存进入：直接显示缓存版本，且**不发起网络请求**（断言 check 端点未被调用）。
- 点「检查更新」触发 force=true 请求。
- 更新成功后本地缓存被清、随后重新 force 拉取。
- `UpdateIndicator` 仍正常（不报错）。
- `npm run build` 通过；相关 vitest 绿。

## 6. 风险 / 待定

- 「无缓存不显示更新区」与既有「点击检查更新引导占位」文案冲突——按 FR-112 取消该占位（仅留按钮）；保留频道切换器与按钮始终可见。
