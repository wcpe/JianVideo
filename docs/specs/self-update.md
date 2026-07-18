# 规格：远程自更新（FR-46）

> 决策见 [ADR-0064](../adr/0064-gated-rc-ga-release.md)。只消费门控发布产生的公开 RC/GA GitHub Release（平台产物 + `checksums.txt`）；`dev` Actions 实验工件不属于更新源。

## 需求
服务器端鉴权用户可在 UI 检查并应用来自 GitHub Releases 的新版本，替换运行中的二进制并自动重启，失败或主动可回滚。

## 设计

### 后端 `internal/update`
- **频道**：`stable`=正式版 GA（公开且 `prerelease=false`）/ `prerelease`=候选版 RC（公开且 `prerelease=true`、tag 严格匹配 `vX.Y.Z-rc.N`，其中 `N>=1`）。频道持久化于设置 `update_channel`（请求不带 channel 时取设置，再无则回退正式版）。`prerelease` 是兼容保留的接口枚举，用户界面名称统一为“候选版 RC”。
- **更新源边界**：跳过 draft、tag 非法、资产不完整的 Release；候选版完整资产严格定义为非空的 `jianvideo-linux-amd64`、`jianvideo-windows-amd64.exe`、`checksums.txt`，高版本 RC 不完整时继续回退选择下一个完整合法 RC。候选版频道不选择历史滚动 `dev` Release，也不读取 `experimental.yml` 产生的 Actions 工件；正式频道保持历史 Release 选择兼容。
- **检测 `Check`**：按 GitHub 分页语义拉 `GET /repos/wcpe/JianVideo/releases?per_page=100&page=N`，从第 1 页顺序请求，遇短页结束，最多拉取 10 页；第 10 页仍为满页时返回错误并拒绝使用部分结果。正式版保持按列表顺序取首个公开 GA 的历史行为，候选版对拉取全集按版本基线与 RC 序号选择最高合法完整 RC；返回 `current/latest/has_update/tag/prerelease/notes/asset_name`。
- **更新判定 `hasUpdate`**：按 SemVer 比较 `MAJOR.MINOR.PATCH` 与预发布标识；同一基线 `rc.N` 仅在目标序号更大时提示更新，稳定版高于同基线任意 RC。无法解析的历史版本不作为新的候选目标；当前安装若是旧 `dev` 格式，可升级到合法 RC/GA，但当前发布流程不再产生新的 `dev` 版本。
- **资产匹配 `selectBinaryAsset`**（纯函数）：按命名约定 `jianvideo-<goos>-<arch>[.exe]` 选当前平台二进制；`checksums.txt` 单独取。候选 Release 缺任一受支持平台资产或校验和时视为不可用。
- **校验**：下载二进制到临时文件，解析 `checksums.txt` 比对 sha256，校验失败拒绝替换。
- **替换重启 `replaceAndRestart`**：当前 exe 改名为 `.old` → 新文件移到原路径（赋可执行位）→ 启动新进程（继承参数/环境/工作目录）→ 当前进程延时退出（给 HTTP 响应留时间）。移动失败即就地回退。Windows 借“改名旧 exe”绕开运行中文件锁。
- **回滚 `Rollback`**：存在 `.old` 时用其恢复并重启。

### API（`/api/system/update/*`，复用 FR-13 鉴权，APIGuard 已保护 `/api/*`）
- `GET /check?channel=stable|prerelease` → 检测结果；`prerelease` 对应候选版 RC。
- `POST /apply` `{channel}` → 下载+校验（同步，失败返错），成功后响应“更新中，服务即将重启”并在后台替换+重启。
- `POST /rollback` → 回滚到上一版并重启。

### 前端（系统诊断页 `/system`）
- “检查更新”按钮 + 频道切换（**正式版 / 候选版 RC**，切换即持久化到设置 `update_channel`）；展示当前版本、最新版本、RC/GA 标识与发布说明；有更新时“立即更新”（二次确认）；更新后轮询 `/health` 等待重启完成；保留“回滚”入口。
- 候选版本地缓存仅接受 `tag/latest` 均严格匹配 `vX.Y.Z-rc.N`（`N>=1`）且 `prerelease=true` 的结果；升级后旧 `dev` 缓存会在读取时清除并返回空，用户需重新检查更新。正式版缓存保持历史兼容。
- 不向用户展示或安装 `dev` 实验工件；需要试用 `dev` 时由开发者从对应 Actions run 手工下载实验资产。

## 验收标准
- 单测覆盖：稳定版与 RC SemVer 比较、`rc.0` 排除、同基线 RC 序号、稳定版高于同基线 RC、频道筛选、draft/非法 tag/旧 `dev` 排除、不完整高版本 RC 回退、第二页合法 RC 选择、分页参数与短页停止、满页达到上限失败关闭、资产匹配、checksums 解析与 sha256 校验。
- 候选版频道只返回门控 `rc.yml` 公开且三项资产完整非空的合法 RC；`dev` Release 与 Actions 实验工件均不可见。
- **真机（push 后）**：对真实仓库跑通“RC 检测→下载→校验→替换→自动重启→版本变化”，再从 final RC 升级到同基线 GA并回滚一次；checksum 篡改时拒绝替换；仅鉴权可触发。

## 不做（YAGNI）
- 不做增量/差分更新、不做多版本并存、不做自动后台静默更新（均需用户显式触发）。
- 不把 `dev` Actions 实验工件包装成公开 Release 或自更新频道。
