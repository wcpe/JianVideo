# 功能规格：业务 Go 全部迁入 apps/server

> 状态：已交付@v0.25.0　·　关联 PRD：FR2-067　·　阶段：对齐-A

## 1. 背景与目标

用户硬约束：业务 Go 不得留在仓库根。将 `main.go`、`internal/*`、根验收测、根 `config`、Go 流程 e2e 迁入 `apps/server`，引入 `go.work`，单二进制 embed `apps/web` 构建产物。

## 2. 需求（要什么）

- 范围内：目录迁移、embed 源切换、`go.work`、质量门 / CI / Makefile / Playwright 路径、根卫生 strict 可过、文档真貌。
- 不做：改 module path（仍 `github.com/wcpe/JianVideo`）；GORM→sqlx；OpenAPI；分层重构（070）；默认 CGO=0。

## 3. 设计（怎么做）

- `apps/server` 持有 `go.mod`；根 `go.work` → `use ./apps/server`。
- `//go:embed all:web/dist`；`apps/web` build 末尾 sync 到 `apps/server/web/dist`。
- Playwright e2e 留根 `e2e/`；Go e2e 在 `apps/server/e2e`。
- 静态资源前缀由 `frontend/dist` 改为 `web/dist`（router + 测试 MapFS）。

## 4. 任务拆分

- [x] 创建 `apps/server` 并迁移代码
- [x] go.work + embed web/dist + router 前缀
- [x] Makefile / package.json / CI / Playwright / coverage
- [x] 根无业务 Go；删除 frontend shim
- [x] 构建与关键测试
- [x] ARCHITECTURE / CHANGELOG / PRD 开发中
- [ ] 全量 go test / 发版标已交付

## 5. 验收标准

- 根无 `main*.go` / `internal/` / 生产 `frontend/` 源码树。
- `go build -C apps/server` 成功；config/web 测试绿。
- 兼容合同：数据路径语义、配置名、REST、单二进制、历史库不破。
- 干净树 `root-hygiene --strict` 可通过。

## 6. 风险 / 待定

- 全量 `./...` 测试耗时长，本轮做抽样；CI 全量门仍跑。
- `go build -C` 需较新 Go 工具链（仓库已 1.26+）。
