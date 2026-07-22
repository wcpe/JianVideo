# 功能规格：API 契约优先（OpenAPI）

> 状态：已交付@v0.25.0　·　关联 PRD：FR2-071　·　阶段：对齐-C

## 1. 背景与目标

引入 `api/openapi.yaml` 真源、oapi-codegen 生成与防漂移门禁，对齐 JianArtifact ADR-0004 思路，契约语义贴合 JianVideo。

## 2. 需求（要什么）

- 范围内：openapi 骨架、结构校验、Go `ServerInterface` 生成、`task gen` / `gen:check`、核心/v2 路径首切、media-client/mock 相对契约的 **路径** 防漂移（无新 npm 依赖）、`/health`/`auth`/`/api/v2/*` 用契约类型响应并单挂路由。
- 不做：一次覆盖全部历史端点；借机改 API 语义；**禁止** 现网直接 `RegisterHandlers` 全量挂载（与手写 auth 冲突）；本波 **不** 引入 openapi-typescript 等 TS 代码生成。

## 3. 设计（怎么做）

- 真源：`api/openapi.yaml`（OpenAPI 3.0.3）。
- 首切路径：`/health`、`/api/auth/*`、`/api/v2/media`、`/api/v2/media/{id}`、`/api/v2/tasks/{id}`。
- 结构 + client 门禁：`scripts/openapi-check.mjs`（无第三方 YAML/OpenAPI 依赖）。
  - 契约结构与 `REQUIRED_PATHS`。
  - `CLIENT_PATH_SURFACE`：`packages/media-client/src/index.ts`、`packages/mock/src/index.ts` 必须保留契约针；提取生产源 `/api/v2/*` 字面量并要求被 openapi paths 覆盖。
  - 历史路径（如 `/api/tasks`、`/api/library/*`）不在本门禁范围。
- 代码生成：`go run oapi-codegen@v2.8.0`（**不入 go.mod**）；产物 `apps/server/internal/openapi/api.gen.go`。
- 运行时依赖：`github.com/oapi-codegen/runtime v1.6.0`。
- 防漂移：`task gen:check`（Go）；`openapi:check`（契约 + client 路径）。
- **运行时桥接**（不 `RegisterHandlers`）：
  - `openapi.GetHealth` → web 单挂 `/health`
  - auth 成功/错误体用 `LoginResponse` / `SetupStatus` / `Error`
  - `api.ListMediaV2` / `GetMediaV2` / `GetTaskV2` → `RegisterRoutes` 内 `/api/v2/*` 单挂；委托 library/tasks，响应映射为契约 `MediaPage` / `MediaItem` / `TaskItem`

## 4. 任务拆分

- [x] openapi 骨架与目录（`api/openapi.yaml`）
- [x] 校验脚本与单测（`scripts/openapi-check*.mjs`）
- [x] 工具链入口（package.json / Taskfile / Makefile）
- [x] oapi-codegen 生成管线（`internal/openapi` + `task gen`，Go 侧）
- [x] Go 生成物防漂移（`task gen:check`）
- [x] media-client / mock 相对 openapi 的 v2 **路径** 防漂移（无新依赖）
- [x] `/health` 经 openapi 类型响应（`openapi.GetHealth`，单挂）
- [x] auth 成功/错误体用契约类型（`LoginResponse` / `SetupStatus` / `Error`；状态码语义不变）
- [x] `/api/v2/media`、`/api/v2/media/{id}`、`/api/v2/tasks/{id}` 薄适配单挂 + 单测
- [x] 文档与 invariants
- [ ] 全量历史端点迁入；现有 Handler 逐步实现 `ServerInterface` 并切换注册
- [ ] media-client / mock **由契约生成** TypeScript 类型与 client（可选后续，需确认依赖）

## 5. 验收标准

### 5.1 已交付

- [x] `api/openapi.yaml` 存在且声明 openapi 3.x + info/paths/components。
- [x] 必选路径齐全；`node scripts/openapi-check.mjs` / `make openapi-check` 绿。
- [x] `cd apps/server && task gen` 可重生成；`task gen:check` 绿。
- [x] `internal/openapi` 可编译；runtime 为 go.mod 直接依赖。
- [x] 生成物注明 DO NOT EDIT，不得手改。
- [x] media-client / mock 生产源中的 `/api/v2` 路径与 openapi 对齐；缺针或幽灵路径失败。
- [x] `GET /health` 响应为契约 `HealthStatus`（`{"status":"ok"}`）；`web` 包测绿。
- [x] auth login/setup/setup-status 成功体与错误体用 openapi 类型；logout 仍 204（历史语义，openapi 声明 200 不在本波改）。
- [x] `GET /api/v2/media`、`/api/v2/media/{id}`、`/api/v2/tasks/{id}` 返回契约形态；`api` 包 v2 单测绿。

### 5.2 后续

- [ ] TS client / mock 由同一契约 **生成** 类型/客户端（需确认依赖）。
- [ ] 生产路由全部由生成接口注册（禁止一步全量 `RegisterHandlers`）。
- [ ] logout 状态码与 openapi 对齐（若改 200 需客户端确认）。

## 6. 风险 / 待定

- 历史端点仍以 `docs/API.md` 为准，分期迁入 openapi。
- 现有 `internal/api` / `web` 手写路由与 `openapi.ServerInterface` 并存；后续按端点切换，勿全量挂载。
- `pnpm openapi:check` 可能触发 pnpm 装依赖门；优先 `node` / `make openapi-check`。
- media-client 仍用手写 `/api/tasks` 等历史路径；仅 v2 表面受路径门禁约束。
- v2 media 的 `kind` 由扩展名/Format 推断；与 library 完整类型规则偶有差异时以 Format 回退为准。
