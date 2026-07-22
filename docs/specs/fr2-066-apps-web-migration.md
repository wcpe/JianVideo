# 功能规格：生产主端 frontend → apps/web

> 状态：开发中　·　关联 PRD：FR2-066　·　阶段：对齐-A　·　分支：未指定

## 1. 背景与目标

生产 Web 原在 `frontend/`，与 `apps/wiki`、`apps/mock-studio` 分轨。按 ADR-0054 / ADR-0060 迁入 `apps/web`，收口 monorepo 落点；`go:embed` 仍读 `frontend/dist` 直至 FR2-067。

## 2. 需求（要什么）

- 范围内：源码迁 `apps/web`；包名 `@jianvideo/web`；质量门 / CI / Makefile / Playwright 路径；`frontend/` 降为 embed shim；相对 `packages/*` 引用修正。
- 不做：后端搬迁；改业务 API；重写 UI 栈；本 FR 内改 `//go:embed` 路径（留 067）。

## 3. 设计（怎么做）

- `git mv frontend → apps/web`，修正 `file:` 依赖与 vite alias（`../../packages`）。
- `apps/web` 的 `build` 末尾 `scripts/sync-embed-dist.mjs` → `frontend/dist`。
- 根脚本 `frontend:*` 兼容名转发 `npm --prefix apps/web`；新增 `web:*`。
- CI `cache-dependency-path` / install / build 改为 `apps/web`。

## 4. 任务拆分

- [x] 建立 `apps/web` 并迁移源码与构建配置
- [x] 更新 workspace 脚本、turbo 兼容、CI、Makefile、Playwright
- [x] 主路径 typecheck / test / build 绿
- [x] 文档与 CHANGELOG
- [ ] PRD 发版时标已交付（由 release 技能）

## 5. 验收标准

- 生产构建入口为 `apps/web`；无生产依赖必须读 `frontend/src`（仅 dist shim）。
- lint/typecheck/test/build 绿（本轮：typecheck 绿；1197 tests 绿；build+sync 成功）。
- `frontend/dist` 在 build 后存在，可供 go:embed。

## 6. 风险 / 待定

- `package-lock` 仍由 npm 管理 apps/web，未强制并入 pnpm workspace（与 wiki 双轨；后续可再收）。
- FR2-067 切换 embed 后删除 `frontend/` 与 sync 脚本。
