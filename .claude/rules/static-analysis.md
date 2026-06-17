# 代码风格与静态检查（防风格 / 质量漂移）

> 统一各组件的格式化与静态检查工具，并要求 CI 强制门禁——风格一致、低级问题挡在合并前。

## 1. 各组件工具链

按本仓库各组件实际所用语言，从下表选取对应工具并固定到工程配置里：

- **Go**：`gofmt` / `goimports` 格式化 + `golangci-lint`（可启用 govet / staticcheck / errcheck / ineffassign / revive / bodyclose / sqlclosecheck 等）。
- **TypeScript / React**：`eslint` + `prettier`。

> 多语言仓库逐组件各选其一，配置文件放在对应组件工程内或仓库根，版本固定。

## 2. 强制要求

- **本地**：提交前自行跑对应组件的 format + lint，不把格式问题留给 CI。
- **依赖漏洞**：用对应生态的漏洞发现工具（Go 用 `govulncheck`、Node 用 `npm audit`）。
- 工具与规则版本固定（写进 `go.mod` / `package.json`），避免不同机器结果不一致。

## 3. 与现有规则的关系

- 本规则是 `testing-and-quality.md` 的补充：测试管"行为对不对"，静态检查管"写法干不干净"。
- 禁用某条 lint 要在**配置里集中声明并注明原因**，不在代码里零散 `//nolint` / `// eslint-disable` 关闭（除非有明确理由并写明）。
