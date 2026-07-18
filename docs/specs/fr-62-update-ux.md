# 功能规格：应用更新交互完善（FR-62）

> 状态：开发中　·　关联 PRD：FR-62（扩 FR-46）　·　分支：feature/fr-62-update-ux

## 1. 背景与目标

应用更新区（系统诊断「应用更新」子 tab，FR-59 后位于 `sys=update`）的交互有两处粗糙：

- 「检查更新」按钮写死走 `checkUpdate(channel, false)`（走后端 TTL 缓存），用户没有「强制绕缓存重查」的入口，想看 GitHub 最新状态只能等缓存过期。
- 更新（`handleApplyUpdate`）与回滚（`handleRollback`）的二次确认用原生 `window.confirm`，与全站 Mantine 视觉不一致、不可测试、移动端体验差。

本功能属第七期（P7）界面与运维完善，扩展 FR-46 自更新能力，纯前端交互打磨，不改后端契约。

## 2. 需求（要什么）

- 范围内：
  - **默认缓存展示**：进入应用更新子 tab，默认检查走缓存（`force=false`），保持现状。
  - **「获取更新」强制刷新**：参照编解码测试的「测试」（force=false）+「重新测试」（force=true）双钮模式，为更新检查提供走 `force=true` 的强制刷新入口，绕过后端 TTL 缓存重查 GitHub。文案清晰区分「默认缓存」与「强制获取」。
  - **模态框确认**：更新 / 回滚的二次确认由原生 `window.confirm` 改为 Mantine `<Modal>` 自建确认对话框（标题 / 正文 / 确认 / 取消按钮）。`@mantine/modals` 不在依赖中（见 §6），故用 `@mantine/core` 的 `<Modal>` + `useDisclosure`，**不新增依赖**。
- 不做（范围外）：
  - 不改后端 `/api/system/update/*` 任一端点（`force` 后端已支持）。
  - 不改更新频道（正式版 / 候选版 RC）持久化逻辑、不改重启轮询逻辑。
  - 不引入 `@mantine/modals`（禁 npm install）。

## 3. 设计（怎么做）

仅改 `frontend/src/pages/SystemPage.tsx`（应用更新子 tab）：

1. **双钮检查**：把原单个「检查更新」拆为两个按钮——
   - 「检查更新」：`handleCheckUpdate(false)`，走缓存（默认、命中即时返回）。
   - 「获取更新」：`handleCheckUpdate(true)`，`force=true` 绕缓存重查。
   - `handleCheckUpdate` 由无参改为收 `force: boolean`，透传给 `systemApi.checkUpdate(channel, force)`。

2. **确认模态框**：用 `@mantine/core` 的 `Modal` + `@mantine/hooks` 的 `useDisclosure` 自建两个确认对话框（更新、回滚各一）。点「立即更新并重启」/「回滚到上一版」先开对应模态框，模态框内「确认」执行原 `applyUpdate`/`rollbackUpdate`+重启轮询逻辑、「取消」关闭模态框。原 `window.confirm` 分支移除，原有副作用（busy 态、错误兜底、`waitForRestart`）不变。

无后端改动、无数据模型改动、无新 ADR、无新依赖。

频道用户界面术语统一为“候选版 RC”。候选版缓存迁移后仅接受严格合法的 RC 结果；历史 `dev` 缓存在读取时失效并清除，正式版缓存保持兼容。

## 4. 任务拆分

- [x] 测试先行：补 vitest 用例（force=true 调用断言、模态框打开/确认/取消流程、缓存默认不触发强制请求）
- [x] 改 `SystemPage.tsx`：双钮检查 + 两个确认模态框替换 `window.confirm`
- [x] 文档同步：PRD 状态、CHANGELOG（API/ARCHITECTURE 无契约改动，无需改）

## 5. 验收标准

- 进入应用更新子 tab，点「检查更新」走 `force=false`（不带 `force` query），点「获取更新」请求带 `force=true`。
- 点「立即更新并重启」弹 Mantine 模态框（非原生 confirm）；点模态框「确认」才触发 `applyUpdate`；点「取消」不触发。
- 点「回滚到上一版」弹 Mantine 模态框；「确认」触发 `rollbackUpdate`、「取消」不触发。
- `npx tsc --noEmit` 与 `npx vitest run` 全绿；改动文件 eslint 干净。
- 真机维度：实际点击两钮 / 两模态框的视觉与重启流程为「待真机验」（自动化以 mock 覆盖请求与交互流）。

## 6. 风险 / 待定

- `@mantine/modals` 是否可用：经核查 `frontend/package.json` 依赖仅含 `@mantine/core`/`form`/`hooks`/`notifications`，`node_modules/@mantine/modals` 不存在 → **不可用**，故采用 `@mantine/core` 的 `<Modal>` 组件自建确认框（符合任务约束、不需 npm install）。
