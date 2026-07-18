# ADR-0032：发布工程——CI 原生矩阵构建 + GitHub Releases 分发 + 二进制自更新

## 状态
已被 [ADR-0064](0064-gated-rc-ga-release.md) 取代

## 背景
项目走向公开分发，需要：① 自动产出 Windows/Linux 两套单二进制；② 用户能便捷获取更新。
约束：ADR-0027 因 mattn/go-sqlite3（CGO）定「各平台原生构建、不做交叉编译」；ARCHITECTURE §7「不做容器化部署（Docker）」。
本决策**不取代、不违背** ADR-0027——而是把「各平台原生构建」从本地 make 扩展到 CI 的原生 runner，并新增分发与自更新机制。

## 决策
1. **构建分发走 GitHub Actions 原生 runner 矩阵**：`ubuntu-latest`（linux/amd64）+ `windows-latest`（windows/amd64）各自 `CGO_ENABLED=1` 原生编译（前端 `npm build` → `go:embed` → `go build` 注入版本），**不引入 Docker、不交叉编译**。
2. **正式发布（FR-47）**：推送版本 tag `vX.Y.Z`（亦可在 Actions 页手动触发）即矩阵构建 + 创建 GitHub Release（`prerelease: false`），上传各平台二进制 + `checksums.txt`（sha256）。采用 tag 推送触发（而非 VERSION 变更触发）——推 tag 一定触发、是 GitHub 标准发布模式，且对「仅改提交消息」的历史改写仍可靠生效。
3. **预发布（FR-48）**：普通代码 push 到 main（排除 VERSION/文档变更）滚动刷新 `dev` 预发布（`prerelease: true`），上传最新主干产物。
4. **二进制自更新（FR-46）**：服务器端鉴权后经 GitHub Releases API 检测版本（稳定 / 可切预发布频道）→ 按 `GOOS/GOARCH` 选资产 → 下载 → 校验 `checksums.txt` 的 sha256 → 原子替换（Windows 用「改名旧 exe + 落新 exe + 重启」绕开运行中文件锁）→ 自动重启 → 保留上一版二进制可回滚。

## 理由
- CI 原生 runner 天然满足 CGO 各平台原生构建，比 Docker / 交叉编译工具链更简单、零新增本地依赖，且与 ADR-0027 一致。
- 单二进制 + checksums 的发布物最契合既有「单文件部署」形态；自更新让单用户免去手动下载替换。
- 校验 sha256 + 仅鉴权可触发 + 保留回滚，把「自我替换二进制」的风险压到可接受范围。

## 后果
- 正面：一次 push 出全平台产物；用户一键更新；FR-22 的 Linux 维度由 CI 产物在 Linux 跑通后归真。
- 负面 / 约束：依赖 GitHub Actions 与 GitHub Releases（外部服务）；自更新引入「服务器主动联网 GitHub + 自我替换二进制」的新面，须严守校验与鉴权；Windows 运行中 exe 锁定需「改名 + 重启」套路；CI 上 windows CGO 依赖 runner 自带 gcc（首跑验证）。
- 真机验收门控：CI 全绿 / Release 真出现 / 自更新全链路均需推送到线上仓库后才能验证。

## 备选方案
- **Docker 本地交叉编译**：违背 ADR-0027 +「不做容器化」，且 CI 原生 runner 已满足需求，落选。
- **改用 modernc.org/sqlite 纯 Go 驱动去 CGO**：可本地交叉编译，但需更换 DB 驱动并重验 WAL/并发/行为，改动面大于收益，本期不取（架构不变量允许该驱动，留作未来选项）。
- **仅检查 + 提示不自更新**：最安全但用户仍需手动替换，未满足「点击更新」诉求，落选。
