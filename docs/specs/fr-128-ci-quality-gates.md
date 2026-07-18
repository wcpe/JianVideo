# 功能规格：CI 质量门工作流

> 状态：已实现　·　关联 PRD：FR-128　·　决策：ADR-0047、ADR-0054

## 1. 背景与目标

构建、预发布与正式发布工作流只负责产物构建和发布。独立 `.github/workflows/ci.yml` 在 Pull Request 与 push 到 `main` 时执行完整质量门，覆盖根 workspace、独立前端、Go 后端和 Playwright E2E；任一阻断项失败即阻止合并。

本地与 CI 必须复用同一组 `quality:*` 权威入口，避免 YAML 内维护另一套容易漂移的命令。`build.yml`、`prerelease.yml`、`release.yml` 的职责保持不变。

## 2. 需求

- CI 使用四个相互独立、可并行的阻断 job：
  - **workspace-quality**：`apps/*`、`packages/*` 的构建、类型检查、lint、格式、测试与覆盖率，以及根生产依赖审计。
  - **web-quality**：独立 `frontend/` 的构建、类型检查、lint、格式、测试与覆盖率，以及前端生产依赖审计。
  - **go-quality**：Go vet、漏洞扫描、覆盖率门与 golangci-lint；golangci-lint 显式启用 staticcheck、gofmt 和 goimports。
  - **e2e**：真实 Go 服务、隔离数据库和 Chromium 上的全部 Ubuntu 可执行 Playwright 用例。
- 根 `pnpm quality` 聚合四类门禁，作为本地全仓严格入口。
- 新失败、覆盖率阈值失败、未解释跳过、发生过重试的测试均阻断。
- Linux CI 安装并验证 ffmpeg/ffprobe；依赖真实媒体的 Linux 用例不得以缺少工具为由跳过。
- 不修改发版工作流，不自动提交或推送。

## 3. 本地权威入口

根 `package.json` 提供以下入口：

- `quality:workspace`：`build` → `typecheck` → `lint` → `format:check` → `coverage`。
  - `coverage` 已执行 Vitest，不重复运行独立 `test`。
  - `format:check` 检查 `apps/*`、`packages/*` 的源码与包根配置，不扫描构建、覆盖率或依赖产物。
- `quality:frontend`：`frontend:build` → `frontend:typecheck` → `frontend:lint` → `frontend:format:check` → `frontend:test:coverage`。
- `quality:go`：`go:vet` → `go:vuln` → `go:coverage` → `go:lint`。
  - `go:coverage` 已执行 Go 测试，不重复运行独立 `go:test`。
- `quality:e2e`：先验证 Playwright 跳过/重试策略，再以零重试运行全部 Playwright spec。
- `quality`：依次组合上述四个入口。

独立的 `test`、`frontend:test`、`go:test` 等快速命令继续保留，供开发阶段按组件使用。

## 4. CI 设计

### 4.1 workspace-quality

1. checkout；setup-node 20；启用 Corepack。
2. `pnpm install --frozen-lockfile`。
3. `pnpm audit --prod --audit-level high`。
4. `pnpm quality:workspace`。

### 4.2 web-quality

1. checkout；setup-node 20，并按 `frontend/package-lock.json` 缓存 npm 依赖。
2. 启用 Corepack；执行 `npm --prefix frontend install`。
3. `npm --prefix frontend audit --omit=dev --audit-level=high`。
4. `pnpm quality:frontend`。

旧前端继续使用 `npm install`，避免 Windows 生成的 lock 在 Linux 上缺少平台可选依赖时触发严格同步失败。

### 4.3 go-quality

1. checkout；setup-go 使用 `go.mod`；setup-node 20；启用 Corepack。
2. 安装 ffmpeg，并构建 `frontend/dist`，满足 `go:embed`。
3. 安装 govulncheck v1.4.0 与 golangci-lint v2.12.2。
4. 运行 `pnpm quality:go`；golangci-lint 按 `.golangci.yml` 执行 staticcheck、gofmt、goimports 等固定检查器。

Go 覆盖率由 `scripts/go-coverage-gate.mjs` 统一执行测试并按现有包级阈值判定；缺少结果或低于阈值均失败。govulncheck 发现项目代码可达漏洞时失败。

### 4.4 e2e

1. checkout；setup-go；setup-node 20。
2. 安装并校验 ffmpeg/ffprobe；安装根依赖、前端依赖和 Chromium。
3. `pnpm quality:e2e` 启动 Playwright webServer，构建前端并运行真实 Go 服务，数据库位于 `.tmp/e2e-run`。
4. 命令行固定 `--retries=0`；自定义 reporter 再次检查 retry 记录，防止配置漂移后仅重试通过。

CI 不排除真实媒体用例。允许跳过的范围仅为：

- `windows-headed-pwa.acceptance.spec.ts`：要求 Windows 原生 Chrome、安装态 PWA 与人工证据，不适用于 Ubuntu runner，由 Windows 专项验收补偿。
- `key_flows_e2e.spec.ts` 的“进入播放路由即发起编码协商”：仅当同文件更强的真实媒体播放用例已通过时，允许跳过该无 ffmpeg 降级路径。

其他任何跳过均由 reporter 将整次测试结果改为失败。新增允许跳过项必须同时更新本规格、reporter 和 reporter 单测，并说明不可在 Ubuntu CI 执行的环境依赖与补偿证据。

## 5. 验收标准

- PR 与 push 到 `main` 时启动 workspace-quality、web-quality、go-quality、e2e；任一失败阻断合并。
- `pnpm quality` 覆盖 `apps/*`、`packages/*`、`frontend/`、Go 与 Playwright。
- 根 workspace 和独立前端均执行构建、类型、lint、格式、测试与覆盖率门。
- 根和前端生产依赖审计不得报告 high/critical 漏洞。
- Go 实际执行 govulncheck 与包级覆盖率门，不能退化为仅 `go test`。
- E2E 在 Ubuntu+ffmpeg 上执行全部可运行用例，包括真实媒体用例；不得依赖重试变绿。
- reporter 仅放行本规格列出的跳过，其他跳过自动失败。
- 发版工作流保持不变。
- GitHub Actions 真机结果由用户 push 后观察；本任务不自动 push。

## 6. 风险与限制

- 四类门禁总耗时较长，CI 通过独立 job 并行执行；本地可按 `quality:*` 分拆运行。
- Go SQLite 测试和覆盖率依赖 CGO/C 编译器；CI 使用 Ubuntu runner 的现有工具链。
- E2E 会构建前端、启动 Go 服务、生成真实媒体并运行 Chromium，资源消耗高于组件测试，但仍作为阻断门。
- Windows headed 与安装态 PWA 无法在 Ubuntu runner 验证，必须继续保留 Windows 专项验收证据。
