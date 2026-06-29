# 功能规格：前端 Prettier 落地与 ESLint 告警清零

> 状态：开发中　·　关联 PRD：FR-123　·　分支：claude/suspicious-snyder-718ae6

## 1. 背景与目标

前端已有 `eslint.config.js`（flat config，typescript-eslint + react-hooks + react-refresh）与 `lint` 脚本，但**无 Prettier**，且 `npm run lint` 从未进 CI、现有 16 处告警（15 error + 1 warning）从未被挡。本 FR 补齐 Prettier 工具链、全量格式化、清零 eslint 告警，作为前端工具链基座（FR-124~127 在此之上分支）。属第十三期（P13），见 ADR-0047。CI 接线见 FR-128。

## 2. 需求（要什么）

- 装 `prettier` + `eslint-config-prettier`（devDependencies）。
- 加 Prettier 配置（`.prettierrc.json`）与 `.prettierignore`（忽略 dist、node_modules、coverage 等）。
- `package.json` 加脚本：`format`（`prettier --write .`）、`format:check`（`prettier --check .`）、`lint:fix`（`eslint . --fix`）；`lint` 已有保留。
- `eslint.config.js` 末尾接入 `eslint-config-prettier`，关闭与 Prettier 冲突的格式类规则。
- `prettier --write` 全量格式化（**format-only 单独 commit、行为不变**）。
- 清零全部 eslint 告警（**与格式 commit 分开**）。
- 范围内：仅前端（`frontend/`）。
- 不做（范围外）：覆盖率门禁（FR-126）、mock 补全（FR-124）、CI 接线（FR-128）、改动运行时业务行为。

## 3. 设计（怎么做）

### 3.1 Prettier 配置（贴合现有代码风格，减少无谓 churn）
观察 `vitest.config.ts` 等：单引号 + 分号。故 `.prettierrc.json`：
```json
{ "singleQuote": true, "semi": true, "printWidth": 100, "trailingComma": "all", "endOfLine": "lf" }
```
`.prettierignore`：`dist`、`node_modules`、`coverage`、`*.snap`、`src/mocks` 可保留格式化（不忽略）。

### 3.2 eslint-config-prettier 接入
`eslint.config.js` 的 config 数组末尾追加 `eslintConfigPrettier`（import 自 `eslint-config-prettier`），确保它在最后、覆盖前面规则集里与 Prettier 冲突的格式规则。

### 3.3 ESLint 告警清零（16 处）
- **`react-hooks/refs`（VideoPlayer.tsx 多处，latest-ref 模式：渲染期写 `xxxRef.current = latestProp`）**：该模式合法且 React 文档认可于「回调/effect 读取最新值」场景；强行迁移到 effect 改变 ref 写入时序，在**高风险播放器组件**（追播/seek）有回归风险。按 static-analysis.md §3「禁用某条 lint 在配置集中声明并注明原因」，在 `eslint.config.js` **集中关闭** `react-hooks/refs` 并加中文理由（与既有 `react-hooks/set-state-in-effect` 关闭同理）。
- **`react-refresh/only-export-components`（SystemPage.tsx:29）**：real-fix——把非组件导出（常量/函数）移到独立文件，或调整导出结构，使该文件只导出组件。
- **`react-hooks/exhaustive-deps`（VideoPlayer.tsx:320 warning，cleanup 用 `videoRef.current`）**：real-fix——effect 内把 `videoRef.current` 复制到局部变量，cleanup 用该局部变量。
- 其余若有：逐项 real-fix。

### 3.4 提交拆分（按 git-commit.md §3）
1. `build(web)`：prettier + eslint-config-prettier 依赖 + 配置 + 脚本 + eslint 接入。
2. `style(web)`：`prettier --write` 全量格式化（format-only，行为不变）。
3. `fix(web)`：eslint 告警清零（含集中关闭 react-hooks/refs + real-fix 其余）。

## 4. 任务拆分
- [ ] 装依赖 + `.prettierrc.json` + `.prettierignore` + package.json 脚本 + eslint-config-prettier 接入 →（commit 1）
- [ ] `npm run format` 全量格式化 →（commit 2）
- [ ] 清零 eslint 告警 →（commit 3）
- [ ] 验证：`npm run format:check` 干净、`npm run lint` 0 问题、`npm run build`（tsc -b + vite build）通过、`npm test` 全绿
- [ ] 文档同步：PRD 状态、CHANGELOG（按需）

## 5. 验收标准（AC-27）
- `npm run format:check` 全绿；`npm run lint` 0 error/warning。
- 全量格式化为独立提交、与 eslint 告警修复提交分开。
- `npm run build`（tsc -b + vite build）与 `npm test` 均通过（格式化与告警修复未破坏行为）。

## 6. 风险 / 待定
- 全量 `prettier --write` 触及约 200+ 文件，diff 巨大但为纯格式化；endOfLine `lf` 配 autocrlf=true，提交时 EOL-only 变化会在 `git add` 时折叠（仅真实格式改动入库），需确认 commit 2 不夹带行为变更。
- 集中关闭 `react-hooks/refs` 是务实取舍而非「修复」；若后续愿承担风险，可单独立项把 VideoPlayer latest-ref 迁移到 effect。
- `npm test` 在格式化后须全绿，确认 Prettier 未改变模板字符串/JSX 语义（极少见但需验证）。
