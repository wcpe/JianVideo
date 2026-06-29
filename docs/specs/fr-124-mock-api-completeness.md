# 功能规格：前端 Mock API 端点补全

> 状态：开发中　·　关联 PRD：FR-124　·　分支：claude/suspicious-snyder-718ae6

## 1. 背景与目标

前端测试与 dev 模式经 MSW 模拟后端：`frontend/src/mocks/handlers.ts`（现 58 个 handler）+ `data.ts` + `beforeAll.ts`(测试 server) + `browser.ts`(dev worker)。但 `frontend/src/api/` 实际调用约 78 处端点，handler 未必 100% 覆盖；测试 setup 现为 `onUnhandledRequest:'warn'`（漏网请求只告警、可能静默打到真实网络）。本 FR 把 MSW handler 补到对 api 层 100% 覆盖，并把未处理请求策略改为 `error` 永久强制完整性。属第十三期（P13），依赖 FR-123（已落地）。

## 2. 需求（要什么）
- 枚举 `frontend/src/api/*.ts`（排除 `*.mock.ts`/`*.test.ts`）实际调用的全部后端端点（方法 + 路径模板）。
- 对照 `handlers.ts`，补齐缺失 handler，使每个 api 端点都有对应 mock。
- 新增/补齐的 mock 响应**数据形状贴近真实后端**（字段名/类型对齐 api 层 TS 返回类型与后端契约）。
- 把测试 setup 的 `onUnhandledRequest` 由 `warn` 改为 `error`，作为完整性的永久门禁（漏网即测试失败）。
- 范围内：仅 `frontend/src/mocks/` 与必要的 setup。
- 不做（范围外）：改 api 层本身、改业务组件、覆盖率门禁（FR-126）、浅测补强（FR-125）。

## 3. 设计（怎么做）
- 静态枚举：扫 `frontend/src/api/*.ts` 的 `client.{get,post,put,delete,patch}('<path>')`，归一路径模板（`:id` 等参数段用通配 `*`）。
- 覆盖对照：逐端点核对 `handlers.ts` 是否有匹配（注意 MSW 路径用 `*/api/...` 通配前缀 + `:param`）。
- 补缺：为缺失端点加 handler，返回贴近真实的 mock（成功路径为主；必要的错误路径如 401/404 可按需）。数据复用/扩充 `data.ts`。
- 强制完整性：`beforeAll.ts`（或 setup）`server.listen({ onUnhandledRequest: 'error' })`。
- 验证：全量 `npm test` 在 `error` 策略下**无未处理请求**且全绿——这既证明覆盖完整、又不破坏既有断言。

## 4. 任务拆分
- [ ] 枚举 api 端点清单（方法+路径），落在报告里
- [ ] 对照 handlers.ts 找缺口
- [ ] 补齐缺失 handler（贴近真实数据形状）
- [ ] `onUnhandledRequest` 改 `error`
- [ ] 验证：`npm test` 全绿且无 unhandled（必要时先临时打印 unhandled 定位）
- [ ] `npm run lint` + `npm run format:check` 仍绿（新代码符合 prettier/eslint）
- [ ] 文档同步：PRD 状态、CHANGELOG（按需）

## 5. 验收标准（AC-28）
- 可列出 `frontend/src/api` 全部后端端点清单，且 `handlers.ts` 对每个端点有对应 handler（100% 覆盖）。
- 测试在 `onUnhandledRequest:'error'` 下运行**无任何未处理请求**。
- `npm test` 全绿（705+ 用例不回归）；`npm run lint` 0 告警、`npm run format:check` 干净。

## 6. 风险 / 待定
- 改 `error` 后，若某些既有用例本就依赖「漏网请求被吞/告警」可能暴露失败——逐一补 handler 解决，不得为过测把策略改回 warn。
- 部分端点仅在特定交互触发、测试未必覆盖；静态枚举补齐可保证「api 层任何调用都有 mock」，比仅靠测试触发更完整。
- mock 数据形状对齐后端契约：以 api 层 TS 返回类型为准，必要时查后端 handler 确认字段。
