# 功能规格：Go 静态检查门禁

> 状态：开发中　·　关联 PRD：FR-122　·　分支：claude/suspicious-snyder-718ae6

## 1. 背景与目标

现 CI（build/prerelease/release）只编译不做任何 Go 质量检查；`gofmt`/`go vet`/`golangci-lint` 仅散见于 `docs/specs/*` 的人工验收清单，易遗漏。本 FR 落地 `.claude/rules/static-analysis.md` 既定的 Go 工具链为「可本地跑 + 配置就位」的门禁（CI 接线见 FR-128）。属第十三期（P13），见 ADR-0047。

## 2. 需求（要什么）

- 新增 `.golangci.yml`（golangci-lint v2 schema），启用 static-analysis.md 列出的检查器：`govet`/`staticcheck`/`errcheck`/`ineffassign`/`revive`/`bodyclose`/`sqlclosecheck`，formatters 用 `gofmt`/`goimports`。
- `Makefile` 加 `lint` 目标并在 `help` 列出；只管 Go。
- 固定工具版本（CI 安装固定 tag 的 golangci-lint，与本地约定一致）——CI 接线落在 FR-128，本 FR 在 spec/文档约定版本。
- 修复**全部存量告警**至 `golangci-lint run` 全绿（本批全修，real fix 非 suppress）。
- 范围内：Go 代码（`internal/` + `config/` + `main.go` + `e2e/`）。
- 不做（范围外）：前端 lint（FR-123）、CI workflow 接线（FR-128）、引入新依赖。

## 3. 设计（怎么做）

### 3.1 CRLF 与跨平台确定性（关键）
- 实测：本机 `core.autocrlf=true`、无 `.gitattributes`，工作树为 CRLF，导致**独立** `gofmt -l .` 把每个文件都误报为待格式化（差异仅 `\r`）。
- 但 `golangci-lint run`（formatters 含 gofmt）**对 CRLF 安全**——实测全仓只报 3 处**真实** gofmt 问题、非每文件噪声。
- 因此 `make lint` 用 `golangci-lint run` 作为唯一权威门（已含 gofmt+goimports 格式检查），**不**调用独立 `gofmt -l`/`goimports -l`，从而无需对全仓做行尾 renormalize（避免巨大机械 diff）。这是对 PRD「gofmt 检查 + goimports 检查 + golangci-lint run」意图的等价实现（三者均由 golangci-lint 的 formatters 覆盖）。

### 3.2 `.golangci.yml`（v2）
- `version: "2"`；`linters.enable` 增 `bodyclose`/`revive`/`sqlclosecheck`（`errcheck`/`govet`/`ineffassign`/`staticcheck`/`unused` 为 v2 默认集）；`formatters.enable: [gofmt, goimports]`。
- 如需对生成/特例代码豁免规则，集中在 `linters.exclusions` 声明并注明理由（不散落 `//nolint`，遵 static-analysis.md §3）。

### 3.3 `Makefile` lint 目标
- `lint: golangci-lint run`（含格式 + 静态检查）；`.PHONY` 与 `help` 补 `lint`。
- 注：分析 `main` 包依赖 `//go:embed frontend/dist`，须先 `make frontend`（或已有 dist）。CI 在 lint 前构建前端（FR-128）。`make lint` 只管 Go、不自动构建前端，help 注明此前提。

### 3.4 存量告警修复（实测 106 处，internal+config+e2e；main 包待前端构建后再测）
- `errcheck`(50)：测试内 `os.Setenv/Unsetenv` 未检查返回值——优先改 `t.Setenv`，其余显式检查或就地 `_ =` 并加中文注释说明可忽略原因。
- `revive`(33)：按规则逐项 real fix。
- `staticcheck`(11)：QF1012 用 `fmt.Fprintf` 取代 `WriteString(fmt.Sprintf(...))`；ST1005 错误串首字母不大写（如 `"Range 头为空"`→ 调整）；QF1008 去冗余嵌入字段选择。
- `unused`(4)：`serveHLSSegment`/测试辅助 `sanitizeDBName`/`argValues`/`killProcessGroup`——逐一确认确实未用（注意 `platform_windows.go` 的平台条件编译）后删除；有平台用途则按构建标签保留并消除误报。
- `bodyclose`(3)：补 `resp.Body.Close()`。
- `ineffassign`(2)：消除无效赋值。
- gofmt(3)：`golangci-lint fmt` 自动修。

## 4. 任务拆分
- [x] `.golangci.yml`（v2，全套 + formatters）
- [ ] `Makefile` lint 目标 + `.PHONY` + help
- [ ] `golangci-lint fmt` 自动修格式（gofmt/goimports）
- [ ] 逐类修复 106 处告警（real fix，保持 `go test` 行为不变）
- [ ] 构建前端后对 `./...`（含 main）跑 `golangci-lint run` 补修 main 包告警
- [ ] 验证：`golangci-lint run ./...` 0 issue + 受影响包 `go test` 全绿
- [ ] 文档同步：PRD 状态、CHANGELOG

## 5. 验收标准（AC-26）
- `make lint`（即 `golangci-lint run ./...`，前端已构建）全绿：0 issue（含 gofmt/goimports 格式、govet/staticcheck/errcheck/ineffassign/revive/bodyclose/sqlclosecheck/unused）。
- 修复后受影响 Go 包 `go test ./...` 全绿（证明修复未改变行为）。
- 工具版本约定固定（CI 与本地一致；CI 接线在 FR-128 验收）。

## 6. 风险 / 待定
- `main` 包告警须构建前端后才能测，数量未知（106 仅 internal+config+e2e）；预计很少（main.go 体量小）。
- 删除 `unused` 函数（尤其平台条件编译的 `killProcessGroup`）需谨慎核实，勿误删平台路径；若确为死代码则删，否则按 build tag 修正。
- `errcheck` 对测试 `os.Setenv` 的修复优先 `t.Setenv`（更安全、自动还原），避免大面积 `_ =` 降低可读性。
- 工具版本固定的具体方式（CI pin tag vs `go.mod` tool 指令）在 FR-128 接线时定。
