# ADR-0047：CI 引入质量门（静态检查 + 单测覆盖率 + E2E）

## 状态
已接受

## 背景
现状下 CI 三条工作流（`build.yml` 可复用构建、`prerelease.yml` 滚动预发布、`release.yml` 正式发布）**只编译产物，从不运行任何 lint / 测试 / E2E**：`build.yml` 仅做「装 Go/Node → npm 构建前端 → go build → 上传工件」。

与此同时项目已积累可观的自动化测试资产——前端 97 个 Vitest 测试文件、Go 各包单测、2 个 Playwright E2E（登录/库管理/分享），但**没有任何一个在 CI 中运行**。代码质量门（`gofmt`/`go vet`/`golangci-lint`/覆盖率/E2E）目前仅以人工验收形式散落在 `docs/specs/*` 的验收清单里，易遗漏、不可强制、不随规模扩展。

`.claude/rules/static-analysis.md` 与 `testing-and-quality.md` 已规定了工具链（Go：gofmt/goimports + golangci-lint；前端：eslint + prettier；覆盖率与高风险区测试）与「提交前本地跑、违规挡合并」的要求，但这些要求**尚未自动化落地为 CI 门禁**。本 ADR 记录首次在 CI 设质量门的决策，为 FR-122~128 提供架构依据。

## 决策
新增独立工作流 `.github/workflows/ci.yml`，在 **Pull Request 与 push 到 main** 两种事件触发，统一执行质量门：

- **Go 门**：`gofmt`/`goimports` 检查 + `golangci-lint run`（全套 linter：govet/staticcheck/errcheck/ineffassign/revive/bodyclose/sqlclosecheck 等）+ `go test`，与本地 `make lint` 镜像一致。
- **前端门**：`eslint` + `prettier --check`（format:check）+ `vitest` 覆盖率阈值门禁（@vitest/coverage-v8 + thresholds）。
- **E2E 门**：Playwright headless Chromium，作为**独立 job**运行；复用既有 `retries:2` / Service Worker 禁用 / 独立 `.tmp/e2e.db` 隔离配置。

任一门失败即整体失败、阻断合并。发版相关工作流（`build.yml` / `release.yml` / `prerelease.yml`）职责不变，继续专管产物构建与发布，不混入质量门。

## 理由
- **质量门自动化**：把既有规则（static-analysis.md / testing-and-quality.md）从「人工验收」升级为「合并前强制门禁」，低级问题挡在合并前。
- **与发版工作流分离**：`build.yml` 是 `workflow_call` 复用的发版构建件、仅在发版/预发布时触发；把日常质量门塞进去既污染其单一职责、又无法在普通 PR 上挡问题。独立 `ci.yml` 职责清晰。
- **PR + push 双触发**：PR 触发挡住合并前的问题；push main 触发兜住直接推送 main 的漏网场景。
- **E2E 独立 job 且挡合并**：E2E 慢（需构建前端 + 起 Go 服务 + 浏览器）且偶发抖动，独立 job + 既有 `retries:2` 隔离其影响；但仍设为阻断合并，保关键用户流程不回归（采纳用户在需求澄清中的明确选择）。

## 后果
- **正面**：合并前统一质量基线；`docs/specs` 手工验收负担下降；回归网首次覆盖「单测 + 覆盖率 + E2E」；本地 `make lint` 与 CI Go 门镜像，开发者本地即可自检。
- **负面 / 约束**：
  - CI 时长增加（尤以 E2E 构建前端 + 起服务为甚）。
  - E2E 偶发抖动需持续维护（已配重试缓解）。
  - golangci-lint 全套首次启用需一次性清理存量告警（FR-122，量未知）。
  - 覆盖率阈值需随测试演进维护（FR-126，先实测定档）。
- **后续约束**：新增前端代码须经 prettier 格式化与 eslint 零告警；新增 Go 代码须过 golangci-lint 全套；新增/改动应维持覆盖率不低于阈值；关键用户流程变更应同步 E2E。

## 备选方案
- **把质量门步骤塞进 `build.yml`**：落选。`build.yml` 为发版复用工作流、仅发版/预发布触发，混入门禁会职责不清且无法挡日常 PR。
- **仅本地 git hook、不做 CI 门**：落选。本地 hook 不可强制、易被绕过（`--no-verify`），无法作为合并门。
- **E2E 不进 CI、仅本地/发版前手动跑**：落选。关键流程回归将失去自动保障；用户已明确选择「E2E 独立 job 挡合并」。
- **Go 仅启用最小 linter 集**：落选。用户已明确选择「全套 + 本批全修」，一次清理到位避免长期半截门禁。
