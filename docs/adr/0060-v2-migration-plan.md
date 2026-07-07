# ADR-0060：从 v0.20 单体迁移到 apps/packages 的分批方案与顺序

## 状态
已接受

## 背景

ADR-0054 冻结了 v2 目标仓库结构：可运行端放 `apps/*`，共享能力放 `packages/*`，用 pnpm workspace + Turborepo 统一编排跨语言质量门。但它只定义了终点形态，没有定义如何从当前 `v0.20` 单体安全迁移过去。

当前迁移起点（见 `docs/ARCHITECTURE.md` §2、§2.1）：

- 后端为单体：`main.go` 装配 `internal/*` 共 18 个模块（api、web、library、transcoder、player、playback、auth、db、settings、share、smb、watcher、netproxy、dblog、metrics、update、config），依赖方向 `web → api → library/playback/player/transcoder → db` 严格单向。
- 前端为单应用：`frontend/src/{api,components,pages,hooks,stores,mocks,utils,types,data,theme}`，构建产物落 `frontend/dist/`，经 `go:embed` 内嵌进 Go 二进制。

`.claude/rules/architecture-invariants.md` §0 明确：apps/packages 迁移真正落地前，旧分层、旧模块名和旧技术栈锁定仍然有效；一旦改变模块依赖方向或前端包边界，必须在同一变更中写取代 ADR 并同步更新不变量。`docs/ROADMAP.md` §3 通用质量门要求：目录迁移不得破坏现有 SQLite 数据、配置、REST API、`go:embed` 单二进制部署和历史媒体库。

需要一条决策定义迁移的批次、顺序与兼容合同，避免一次性大爆炸迁移带来的不可回滚风险。

## 决策

采用**四批、每批独立可编译、各自过质量门**的分批迁移方案，顺序如下：

1. **建工作区骨架**：引入 pnpm workspace + Turborepo 根配置与 `packages/{eslint-config,typescript-config}` 共享配置，先不移动业务代码。
2. **抽共享 packages**：建立 `packages/{ui,theme,i18n,media-client,render-pixi,mock,benchmark}`，承接后续从 `frontend/` 拆出的能力。
3. **前端迁移**：把 `frontend/` 迁到 `apps/web`，同时按下表把 `frontend/src/*` 拆进 `packages/*`。
4. **后端迁移**：把 `main.go + internal/*` 迁到 `apps/server`，`internal/` 18 个模块作为 `apps/server` 的内部包，依赖方向与分层保持不变。

**起点 → 终点映射表**

| 当前起点 | 迁移终点 |
|---|---|
| `main.go` + `internal/{api,web,library,transcoder,player,playback,auth,db,settings,share,smb,watcher,netproxy,dblog,metrics,update,config}` | `apps/server`（`internal/*` 作为其内部包，分层与依赖方向不变） |
| `frontend/src/api` | `packages/media-client`（API client、Query keys、任务状态类型） |
| `frontend/src/components` | `packages/ui` |
| `frontend/src/theme` | `packages/theme` |
| `frontend/src/mocks` | `packages/mock`（MSW handlers） |
| `frontend/src/{pages,hooks,stores,utils,types,data,assets}` | `apps/web`（应用壳层、路由、页面） |
| （新增，无现存源）i18n 资源 | `packages/i18n` |
| （新增，无现存源）PixiJS 渲染层 | `packages/render-pixi` |
| （新增，无现存源）前后端 Benchmark | `packages/benchmark` |
| `frontend/dist/` 构建产物 | `apps/web` 构建产物，仍 `go:embed` 内嵌进 `apps/server` 单二进制 |

**兼容合同（迁移全程不破坏）**

- SQLite 数据库文件与 schema：库文件路径、`library_paths` / `media_files` 等表结构不变。
- 配置：`config` 环境变量优先加载语义与变量名不变。
- REST API：所有 `/api/*` 路径与响应契约不变。
- 部署形态：仍为 `go:embed` 内嵌前端产物的单二进制，`apps/web` 构建产物替换 `frontend/dist/` 作为嵌入源。
- 历史媒体库：已注册的库路径与历史资产不受迁移影响。

**迁移完成后**（第 4 批落地同一变更内）：同步 `docs/ARCHITECTURE.md` §2/§2.1 的目录结构与依赖方向；按 `architecture-invariants.md` §0/§1 要求，把"当前单体锁定"替换为 apps/packages 依赖方向（`apps/web → packages/*`；`apps/server` 内部维持现有分层），并注明本 ADR 为取代依据。

ADR-0055 至 ADR-0059 定义的 v2 能力，在迁移后按其性质落位到 `apps/server` 内部包或对应 `packages/*`。

## 理由

- **分批可回滚**：每批只动一个边界，落地后代码都能编译并过 `lint/typecheck/test/build`，出问题可停在上一批，风险与回滚成本可控。
- **骨架先行**：先建工作区与共享配置，能让后续每批迁移的目录一进来就被统一质量门覆盖。
- **packages 先于 web**：先建好共享包目标位，再迁前端，避免"边迁边找不到落点"。
- **后端最后迁**：后端是单二进制与 `go:embed` 的最终装配点，放最后能保证前批产物已就位、嵌入源明确。
- **兼容合同显式化**：把不可破坏的五项（数据、配置、API、单二进制、历史库）写死为合同，防止迁移中静默破坏用户数据与部署形态。

## 后果

- 迁移横跨 P0.5 到 P1：P0.5 冻结方案（本 ADR），实际目录移动在 P1 分批执行。
- 迁移完成前，`docs/ARCHITECTURE.md` 仍描述 v0.20 单体现状；目标结构以 ADR-0054、本 ADR、PRD 与 specs 为准。
- 每批迁移都是非平凡结构变更，须各自有 `docs/specs/` 规格与验收，不得无 spec 直接搬代码。
- 第 4 批落地时必须在同一变更内改 ADR-0060、更新 `architecture-invariants.md` §0/§1 与 `ARCHITECTURE.md`，否则视为文档漂移、迁移未完成。
- 前端拆分后，`apps/web` 只依赖 `packages/*`，不得反向依赖；`apps/server` 内部继续单向分层。

## 备选方案

- **大爆炸一次性迁移**：一次性把前后端全部搬到 apps/packages。被否——中间态无法编译、难以回滚，且违背"每批过质量门"的通用质量门。
- **只迁前端不迁后端**：仅把 `frontend/` 迁到 `apps/web`，后端维持 `main.go + internal/*` 单体。被否——无法用 Turborepo 统一编排跨语言质量门，也无法支撑后续多端复用后端能力。
- **维持单体不迁**：不做迁移，继续在 `frontend/` + `internal/*` 上演进。被否——违背已接受的 ADR-0053/0054 与 v2 多端路线，工作区与共享包边界无从落地。

关联：PRD FR2-002、FR2-015；`docs/ROADMAP.md` P0.5/P1；取代 `architecture-invariants.md` §0 中"当前单体锁定"的依据 ADR。
