# 功能规格：CI 质量门工作流

> 状态：开发中　·　关联 PRD：FR-128　·　分支：claude/suspicious-snyder-718ae6　·　决策：ADR-0047

## 1. 背景与目标

现 CI（build/prerelease/release）只编译产物、从不跑任何 lint/测试/E2E。本 FR 新增独立 `.github/workflows/ci.yml`，在 PR 与 push main 触发，统一执行 FR-122~127 落地的全部质量门，任一失败挡合并。`build.yml` 等发版工作流职责不变。属第十三期（P13），收口本批。见 ADR-0047。

## 2. 需求（要什么）
- 新增 `.github/workflows/ci.yml`，触发：`pull_request` + `push`（branches: main）。
- 三个 job（均 ubuntu-latest），任一失败即整体失败：
  - **go-quality**：Go 静态检查门禁 + 测试。
  - **web-quality**：前端 lint + 格式检查 + 覆盖率门禁。
  - **e2e**：Playwright 端到端（独立 job，失败挡合并；复用 retries:2）。
- 工具版本固定（golangci-lint pin 到与本地一致的 v2.12.x；setup-go 用 go.mod 版本；node 20）。
- `build.yml`/`release.yml`/`prerelease.yml` 不改。
- 不做（范围外）：改业务代码、改既有发版工作流。

## 3. 设计（怎么做）

各 job 步骤（命令与本地已验证一致，保证本地 = CI）：

- **go-quality**：
  1. checkout；setup-go（`go-version-file: go.mod`）；setup-node（20, cache npm, cache-dependency-path frontend/package-lock.json）。
  2. 构建前端（`npm --prefix frontend ci && npm --prefix frontend run build`）——golangci-lint 与 `go test` 分析 main 包依赖 go:embed 的 `frontend/dist`，必须先构建。
  3. golangci-lint：用 `golangci/golangci-lint-action@v6`（`version: v2.12.2`）跑 `golangci-lint run ./...`（含 gofmt/goimports formatters）。
  4. `go test ./...`。
- **web-quality**：
  1. checkout；setup-node（20, cache npm, frontend/package-lock.json）。
  2. `npm --prefix frontend ci`。
  3. `npm --prefix frontend run lint`。
  4. `npm --prefix frontend run format:check`。
  5. `npm --prefix frontend run test:coverage`（阈值门禁，不达标失败）。
- **e2e**：
  1. checkout；setup-go（go.mod）；setup-node（20）。
  2. `npm ci`（根，装 @playwright/test 等）+ `npm --prefix frontend ci`（webServer 会 build 前端）。
  3. `npx playwright install --with-deps chromium`。
  4. `npm run e2e`（playwright；webServer 自行 `npm --prefix frontend run build && go run .`、隔离 DB）。CI 环境 `CI=true` 启用 retries:2、单 worker。

可加 `concurrency`（同 ref 取消旧跑）降资源占用。

## 4. 任务拆分
- [ ] 写 `.github/workflows/ci.yml`（三 job、触发、版本固定）
- [ ] 校验 YAML 合法（actionlint 若可用；否则人工核对缩进/字段）
- [ ] 核对每步命令与本地已验证命令一致（make lint=golangci-lint run、npm 各脚本、npm run e2e）
- [ ] 文档同步：PRD 状态、CHANGELOG（用户可见性低，按需）、ARCHITECTURE/CONTRIBUTING 若需写明 CI 质量门

## 5. 验收标准（AC-32，CI 实跑为真机维度）
- `ci.yml` 在 PR 与 push main 触发，依次/并行跑 go-quality / web-quality / e2e；任一失败即整体失败、阻断合并。
- 本地 `golangci-lint run ./...`、`npm run lint`/`format:check`/`test:coverage`、`npm run e2e` 与 CI 行为一致（命令对齐）。
- `build.yml` 发版职责不变。
- **CI 实跑验证须 push 后在 GitHub Actions 观测**——本机无法运行 GitHub Actions，标「CI 待实跑」，由用户 push 后确认（不自动 push）。

## 6. 风险 / 待定
- 本机无法运行 GitHub Actions，ci.yml 只能做 YAML 正确性 + 命令对齐验证，真实绿需 push 后观测。
- e2e job 重（构建前端 + go run + 浏览器），CI 时长增加；retries:2 缓解抖动。
- golangci-lint v2 action 版本与本地 2.12.2 对齐，避免版本漂移致结果不一致。
- 不自动 push、不改既有发版工作流。
