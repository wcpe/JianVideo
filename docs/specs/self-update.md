# 规格：远程自更新（FR-46）

> 决策见 [ADR-0032](../adr/0032-release-engineering.md)。消费 FR-47/48 产出的 GitHub Release（产物 + `checksums.txt`）。

## 需求
服务器端鉴权用户可在 UI 检查并应用来自 GitHub Releases 的新版本，替换运行中的二进制并自动重启，失败或主动可回滚。

## 设计

### 后端 `internal/update`
- **频道**：`stable`（仅正式 Release）/ `prerelease`（含滚动 `dev` 预发布）。
- **检测 `Check`**：拉 `GET /repos/wcpe/JianVideo/releases`，按频道选目标 Release（跳过 draft），与当前版本（`main.version`）比对，返回 `current/latest/has_update/tag/prerelease/notes/asset_name`。
- **版本比较 `isNewer`**（纯函数）：按 `MAJOR.MINOR.PATCH` 数值比；同基线「正式版 > 预发布」；同基线两预发布标识不同视为有更新；无法解析时按字符串不等保守判定。
- **资产匹配 `selectBinaryAsset`**（纯函数）：按命名约定 `jianvideo-<goos>-<arch>[.exe]` 选当前平台二进制；`checksums.txt` 单独取。
- **校验**：下载二进制到临时文件，解析 `checksums.txt` 比对 sha256，**校验失败拒绝替换**。
- **替换重启 `replaceAndRestart`**：当前 exe 改名为 `.old` → 新文件移到原路径（赋可执行位）→ 启动新进程（继承参数/环境/工作目录）→ 当前进程延时退出（给 HTTP 响应留时间）。移动失败即就地回退。Windows 借「改名旧 exe」绕开运行中文件锁。
- **回滚 `Rollback`**：存在 `.old` 时用其恢复并重启。

### API（`/api/system/update/*`，复用 FR-13 鉴权，APIGuard 已保护 `/api/*`）
- `GET  /check?channel=stable|prerelease` → 检测结果。
- `POST /apply` `{channel}` → 下载+校验（同步，失败返错），成功后响应「更新中，服务即将重启」并在后台替换+重启。
- `POST /rollback` → 回滚到上一版并重启。

### 前端（系统诊断页 `/system`）
- 「检查更新」按钮 + 频道切换（稳定/预发布）；展示当前版本 vs 最新版本与发布说明；有更新时「立即更新」（二次确认）；更新后轮询 `/health` 等待重启完成；「回滚」入口。

## 验收标准
- 单测覆盖：版本比较各分支、资产匹配、checksums 解析与 sha256 校验、Release 选择（按频道，httptest 模拟 GitHub）。
- **真机（push 后）**：对真实 wcpe/JianVideo Release 跑通「检测→下载→校验→替换→自动重启→`/api/system/info` 版本变化」+ 一次回滚；checksum 篡改时拒绝替换；仅鉴权可触发。

## 不做（YAGNI）
- 不做增量/差分更新、不做多版本并存、不做自动后台静默更新（均需用户显式触发）。
