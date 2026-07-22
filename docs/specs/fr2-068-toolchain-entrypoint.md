# 功能规格：工具链入口对齐（Makefile + Taskfile）

> 状态：已交付@v0.25.0　·　关联 PRD：FR2-068　·　阶段：对齐-A

## 1. 背景与目标

对齐 JianArtifact：根 Makefile 统一入口，后端 `apps/server/Taskfile.yml` 承载 lint/test/build；前端继续 pnpm/turbo + `apps/web`。

## 2. 需求（要什么）

- 范围内：server Taskfile；Makefile 委托 task；`install`/`dev`/`check`；OPERATIONS/README 命令表；`pnpm go:*` 走 task。
- 不做：Docker 全量 check 容器（072）；OpenAPI gen（071）；换包管理器。

## 3. 设计（怎么做）

- `apps/server/Taskfile.yml`：vet/lint/vuln/test/coverage/build/build:hwaccel/quality/clean；`CGO_ENABLED=1`；二进制输出仓库根 `dist/`。
- 根 Makefile：`build` = frontend + `task build`；lint/test/… 委托 task；`check` = `pnpm quality`。
- 无 task 时文档说明安装方式。

## 4. 任务拆分

- [x] server Taskfile
- [x] 根 Makefile 委托 + install/dev/check
- [x] package.json go:* 委托 task
- [x] OPERATIONS / README
- [x] 冒烟：task --list、task vet

## 5. 验收标准

- `cd apps/server && task --list` 可见 build/lint/test 等。
- `make help` 列出统一入口；文档与命令一致。
- `task vet` 可在 apps/server 执行通过（或仅路径正确）。

## 6. 风险 / 待定

- Windows 需 go-task 在 PATH；CI 当前仍用 pnpm go:*（已改 task），需镜像内有 task 或 CI 继续装 task。
