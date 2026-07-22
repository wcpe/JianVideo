# 功能规格：根目录允许清单与卫生门

> 状态：已交付@v0.25.0　·　关联 PRD：FR2-065　·　阶段：对齐-A

## 1. 背景与目标

仓库根目录当前混有：业务 `main*.go`、`internal/`、运行期产物（`jianvideo.db`、`hls/`、`thumbnails/`、`image_cache/`、`coverage.out` 等）与工程文件。用户要求**根目录禁止堆垃圾**，为后续 `apps/web` / `apps/server` 迁移铺路。

运行期路径现状：

- 入口使用根包 `config`：`DB_PATH` 默认 `jianvideo.db`（落在 CWD 根）。
- `main.go` 将 `hls`、`tools`、`backups` 等放在 `filepath.Dir(cfg.DBPath)` 下；`dataDir` 同为 DB 父目录。
- `internal/config` 已默认 `data/jianvideo.db`，与根 `config` 不一致。
- `.gitignore` 已忽略 `/data/`、`*.db`、`/hls/`、`/thumbnails/` 等，但默认路径仍往根写。

目标：

1. 文档化根目录 **allowlist** 与 **禁止项**。
2. 默认可写数据落入 `data/`（改默认 `DB_PATH` 即可带动派生目录）。
3. 提供可检测的卫生门（脚本 + 单测），接入 `pnpm quality` / workspace 门。
4. 迁移债务（根上 `main.go`/`internal/`/`frontend/`）**单独列出、本 FR 不强制删除**，由 066/067 清除；门禁在「债务模式」下允许这些路径存在，在「严格模式」下拒绝。

## 2. 需求（要什么）

### 2.1 根目录 allowlist（允许存在）

**文件（精确名）**

- `VERSION`、`LICENSE`、`README.md`、`SECURITY.md`、`CHANGELOG.md`、`Makefile`
- `go.mod`、`go.sum`（A 期迁 server 前仍在根；067 后改为 `go.work` + `apps/server/go.mod`）
- `package.json`、`package-lock.json`、`pnpm-lock.yaml`、`pnpm-workspace.yaml`、`turbo.json`
- `playwright.config.ts`、`.editorconfig`、`.gitattributes`、`.gitignore`、`.golangci.yml`

**目录（精确名）**

- `.git`、`.github`、`.claude`、`.tmp`（过程稿，gitignore）
- `apps`、`packages`、`docs`、`scripts`、`config`、`e2e`、`frontend`（债务，066 清除）
- `internal`（债务，067 清除）
- `data`（运行期数据，gitignore；本 FR 起为默认数据根）
- `node_modules`、`dist`、`build`、`.turbo`、`.idea`、`.narrafork` 等工具/IDE 目录（已 gitignore 或不入库）
- 未来：`deploy`、`api`（072/071）

### 2.2 禁止项（根目录不得出现）

**运行期 / 缓存（无论是否 gitignore）**

- 根级 `*.db`、`*.db-wal`、`*.db-shm`（应在 `data/`）
- 根级目录：`hls`、`thumbnails`、`image_cache`、`covers`、`metadata_temp`、`timeline_previews`、`tools`（运行下载）、`backups`（若因默认路径生成在根）
- 根级 `coverage.out`、`coverage/`（应 gitignore 且不应作为默认落点）
- 根级 `playwright-report`、`test-results`（已 gitignore；门禁可 WARN 或 FAIL 可配）

**业务源码债务（严格模式 FAIL；默认债务模式仅记录，066/067 后改严格）**

- 根级 `*.go`（含 `main.go`、`main_*_test.go`）
- 根级 `internal/` 业务树

### 2.3 默认数据目录

- 根包 `config.Load`：`DB_PATH` / 兼容现有 env 名的默认值改为 `data/jianvideo.db`（相对 CWD）。
- 启动时确保 `filepath.Dir(DBPath)` 可创建（`MkdirAll` 已有路径可复用或补一行）。
- 文档说明：旧根目录库文件不会自动搬迁；用户可设 `DB_PATH=jianvideo.db` 保持旧路径，或手动移到 `data/`。
- **不**在本 FR 做自动迁移拷贝（避免静默搬库）。

### 2.4 卫生门

- 脚本：`scripts/root-hygiene.mjs`（Node，无新依赖）。
- 行为：
  - 扫描仓库根一层（不递归进 `apps/` 等）。
  - 对照 allowlist / 禁止规则输出违规列表。
  - 退出码：有 **禁止项（运行期垃圾）** → 非 0；仅有债务项 → 默认 0 并打印「债务」清单（`--strict` 时债务也非 0）。
- 单测：`scripts/root-hygiene.test.mjs`（node:test），用临时目录夹具覆盖：干净根通过、根上 db/hls 失败、债务模式放行 main.go。
- `package.json`：`quality:root` 跑测试+检查；挂入 `quality:workspace` 或 `quality` 前部（优先 `quality:workspace` 以便 CI workspace job 覆盖）。
- CI：复用现有 `pnpm quality:workspace`，不强制改 workflow 文件除非门未覆盖。

### 2.5 范围内 / 外

- 范围内：allowlist 文档、默认 DB 路径、卫生脚本与测试、质量门接线、`.gitignore` 补全（如 `covers/`、`metadata_temp/`）、简短 OPERATIONS/ARCHITECTURE 真貌注记（默认数据目录）。
- 不做：搬 `frontend/` / `apps/server`；删根 `main.go`；自动迁移旧库；引入新第三方依赖；改 REST API。

## 3. 设计（怎么做）

### 3.1 配置

改 `config/config.go`：

```go
DBPath: envString("DB_PATH", filepath.Join("data", "jianvideo.db")),
```

保留 env `DB_PATH` 覆盖。若同时存在历史 env 别名，仅在已有代码支持时兼容，不新增未文档化别名。

启动路径：`main.go` 已用 `filepath.Dir(cfg.DBPath)` 派生 hls 等；默认进入 `data/` 后根目录不再生成这些目录（新安装）。

### 3.2 卫生脚本结构

```text
scripts/root-hygiene.mjs     # checkRoot(root, { strict }) → { ok, violations, debt }
scripts/root-hygiene.test.mjs
```

规则表内嵌脚本并与 spec §2 同步；注释标明「改规则先改 spec」。

### 3.3 文档

- `docs/specs/README.md` 索引本文件。
- `docs/ARCHITECTURE.md`：运行期数据默认在 `data/`（相对 DB 父目录），根目录卫生门说明一句。
- 不把目标 apps 结构写成已完成真貌。

## 4. 任务拆分

- [x] 写本 spec 并入索引
- [x] 改 `config` 默认 `DB_PATH` + 单测
- [x] 实现 `root-hygiene.mjs` + 测试
- [x] `package.json` 接入 `quality:root` / workspace
- [x] 补强 `.gitignore`（covers、metadata_temp、timeline_previews 等）
- [x] 同步 ARCHITECTURE 一句 + CHANGELOG 未发布段
- [x] 跑 quality:root 夹具测试与 `go test ./config`；CI 等价已跟踪树债务模式通过；本机含历史运行期垃圾时 `node scripts/root-hygiene.mjs` 故意失败

## 5. 验收标准

- 新默认：未设 `DB_PATH` 时库文件路径为 `data/jianvideo.db`（或等价 filepath）。
- `node scripts/root-hygiene.mjs` 在干净夹具上 exit 0；根存在 `jianvideo.db` 或 `hls/` 时 exit ≠ 0。
- 当前仓库在债务模式下：运行期垃圾若在根则 FAIL；`main.go`/`internal` 仅报债务不阻断（直至 067）。
- `pnpm quality:root`（或等价）本地可跑且测试绿。
- 文档：allowlist 与禁止项可查；ARCHITECTURE 提及默认 `data/`。
- 兼容：显式 `DB_PATH=jianvideo.db` 时仍可使用旧布局。

## 6. 风险 / 待定

- 开发者本机根目录已有库：改默认后「像丢库」——需 README/CHANGELOG 醒目说明，用 env 保留旧路径。
- `config` 与 `internal/config` 双包：本 FR 对齐根 `config` 默认；合并包留给 067。
- package-lock 与 pnpm 双轨：卫生门不处理，属工具债。
