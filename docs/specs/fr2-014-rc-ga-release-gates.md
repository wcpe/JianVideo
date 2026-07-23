# 功能规格：FR2-014 门控式 RC/GA 发布切片

> 状态：开发中　·　关联 PRD：FR2-014　·　阶段：P7/P8/GA　·　决策：[ADR-0064](../adr/0064-gated-rc-ga-release.md)、[ADR-0065](../adr/0065-pre-1-0-formal-only-release-channels.md)

## 1. 背景与目标

FR2-014 是覆盖初始化向导、设置、诊断、队列监控、Benchmark、E2E、发布包与 1.0 质量门的综合运维能力。本规格只实现其中“发布门”切片：把 `dev` 实验构建、公开正式版与（自 1.0 起的）RC/GA 分开，并确保公开发布在固定提交上通过同一质量门、双平台构建、校验和与发布资产回验后才创建公开版本。

**渠道策略（ADR-0065）**：`1.0.0` 之前所有公开版本一律为正式版（只推 `v0.Y.Z`）；**自 `1.0.0` 起**才推 `vX.Y.Z-rc.N` 与对应稳定 `vX.Y.Z`。本规格的 RC 流水线能力从 1.0 起启用；1.0 前只走稳定正式路径。

本切片进入开发中不代表 FR2-014 整体完成。初始化向导、设置与诊断收口、运维监控、备份恢复、一体化部署、完整 Benchmark/E2E 收口和 P7 其他交付能力仍按各自规格与路线图推进。

## 2. 范围

### 2.1 范围内

- 建立 `dev` 长期集成分支与 `main` 唯一发布分支的发布语义。
- 用 `experimental.yml` 取代滚动 GitHub `dev` 预发布：只在 `dev` 固定 SHA 上生成短期 Actions 实验工件。
- 新增 tag 触发 `release.yml`：推送稳定 `vX.Y.Z` 后在固定 SHA 上重跑质量门与构建，再公开 latest 正式 Release（`0.y.z` 与 `1.0.0+` 正式版均走此路径）。
- 新增 tag 触发 `rc.yml`：推送 `vX.Y.Z-rc.N` 后读取固定 SHA 的 `VERSION`，完整过门后为该 tag 公开候选 Release（**产品策略仅自 `1.0.0` 起使用**，禁止新建 `0.y.z-rc.N`）。
- 复用 `ci.yml` 的 workspace、独立前端、Go、Playwright 四项质量门。
- 复用 `build.yml` 的 Linux/Windows CGO 原生构建，并让调用方固定源 SHA、校验产物与 `checksums.txt`。
- tag 由发布者推送触发 CI；工作流**不创建 tag**，只创建/校验/公开 Release。
- 正式版与 RC 均先创建 draft，上传后从 Release 回下载复验，成功才公开。
- 自更新“候选版”频道只消费公开 RC（自 1.0 起产生），不消费 dev 实验工件。

### 2.2 范围外

- 不把 FR2-014 整体标为已交付。
- 不实现 P7 其余初始化、设置、诊断、监控、备份恢复、一体化部署或跨端运维能力。
- 不修改 ROADMAP 阶段边界；P7 仍为 `0.28.x`，P8 仍为 `0.29.x`（阶段内正式版），GA 仍为 `1.0.0`（经 `1.0.0-rc.N`）。
- 不自动 bump `VERSION`、不自动生成 CHANGELOG 内容、不替开发者选择 RC 序号。
- 不把 `0.y.z` 阶段交付叙事成 RC/GA（ADR-0065）。
- 不引入 Docker、交叉编译、外部制品库、签名服务或后台静默更新。
- 不删除历史 Release/tag；旧 `dev` 仅停止继续产生和消费。

## 3. 设计

### 3.1 分支流

1. 功能与修复分支经 PR 合入 `dev`。
2. `dev` push 运行质量门并生成实验工件，供集成试用。
3. 候选提交经 PR 或受控提升进入 `main`；push `main` 只运行质量门。
4. **`1.0.0` 前**：准备 `VERSION=0.Y.Z` 与 CHANGELOG 稳定段，打并推送 `v0.Y.Z` 走正式发布（无 RC 步骤）。
5. **自 `1.0.0` 起**：RC 准备提交把 `VERSION` 设为 `X.Y.Z-rc.N`（`X >= 1`），CHANGELOG 增加对应非空版本段，打并推送 `vX.Y.Z-rc.N`。
6. RC 验收发现问题则回 `dev` 修复、提升后递增 `rc.N` 推新 tag，不得覆盖旧 RC；final RC 通过后收口 `VERSION=X.Y.Z` 再推稳定 tag。

### 3.2 实验工件

- 触发：仅 push `dev`，没有手动入口。
- 权限：`contents: read`。
- 版本：取当前提交可达的最近稳定 tag 基线与其后提交距离，生成 `<base>-dev.N.g<shortsha>`；无稳定 tag 时从 `0.0.0` 起算。
- 构建：固定触发 SHA，先调用 `ci.yml` 四项质量门，再调用 `build.yml` 生成 Linux/Windows amd64 单二进制与 SHA-256。
- 工件：`jianvideo-experimental-<shortsha>-linux-amd64`、`jianvideo-experimental-<shortsha>-windows-amd64`，保留 7 天。
- 禁止：tag、GitHub Release、latest 标记、自更新频道、写仓库权限。

### 3.3 正式版门禁（`0.y.z` 与稳定 `X.Y.Z`）

- 入口是推送稳定 tag：`refs/tags/v*`，并排除包含 `-rc.` 的 ref。
- `VERSION` 必须严格匹配 `X.Y.Z`，且等于推送 tag 去掉 `v` 前缀。
- 同名 Release 不得已存在。
- CHANGELOG 必须存在目标稳定版的非空版本段。
- 在当前固定 SHA 上重新执行四项质量门和 Linux/Windows 构建；若存在同基线 RC，不能复制 RC 资产冒充正式版。
- publish 使用 `publish-from-tag`；回下载校验通过后公开为 `prerelease=false`、latest。
- **`0.y.z` 阶段发布只走本门禁**（ADR-0065）。

### 3.4 RC 门禁（仅自 `1.0.0` 起产品启用）

- 入口是推送 tag：`refs/tags/v*-rc.*`。
- 固定 `source_sha=github.sha`；后续 checkout、质量门、构建和发布全部使用该 SHA。
- `VERSION` 正则为 `^[0-9]+\.[0-9]+\.[0-9]+-rc\.[1-9][0-9]*$`，且必须等于推送 tag 去掉 `v` 前缀；**产品策略要求 major ≥ 1**，不新建 `0.y.z-rc.N` 公开候选。
- CHANGELOG 必须存在与 `VERSION` 精确对应的非空版本段。
- 同名 Release 不得已存在；该 RC tag 必须已是同基线最高现有 RC。
- RC 与正式发布共享仓库级 `release-publication` 并发组，公开发布必须串行执行且不取消进行中的发布。
- 调用 `ci.yml` 四项质量门与 `build.yml` 双平台构建。
- publish job 使用 `publish-from-tag`：不为已推送 tag 再创建 tag，只创建 draft Release、上传资产、回下载复验后公开为 `prerelease=true`、`make_latest=false`。

### 3.5 tag、draft 与失败清理

- tag 由发布者推送触发 CI；工作流不创建、不改写 tag。
- publish 失败时只清理本次运行创建、仍带归属标记的 draft Release；不删除已推送 tag。
- draft 未通过资产回下载校验不得公开；公开后的版本不允许原地覆盖，修复必须发布新 RC 或新稳定版本。

### 3.6 自更新契约

- `stable`：选择合法稳定版本 Release，排除 draft 与 prerelease。
- `prerelease`：UI 显示“候选版 RC”，只选择合法 `X.Y.Z-rc.N` Release，排除 draft、稳定版、旧滚动 `dev` 与 Actions 工件。
- 同一基线按 RC 序号递增；稳定版高于同基线 RC。实验版本不参与更新比较。
- 下载、平台资产选择、`checksums.txt` 校验、替换重启与回滚沿用既有自更新安全边界。

## 4. 任务拆分

- [x] 接受 ADR-0064，取代 ADR-0032 与 ADR-0042。
- [x] 让 `ci.yml` 可被 RC/GA 复用，同时保留 PR、push `main` 的日常门禁，并由 `experimental.yml` 在 push `dev` 时复用同一四门。
- [x] 让 `build.yml` 接受固定源 SHA并输出可校验的双平台工件。
- [x] 用 `experimental.yml` 替换滚动 `dev` Release 工作流。
- [x] 新增 RC 版本、CHANGELOG、固定 SHA、draft 与回下载校验门。
- [x] 改造 GA 为稳定 tag 触发的正式发布门，并重新构建验收。
- [x] 把自更新测试版频道迁移为候选版 RC 选择与比较。
- [x] 同步 PRD、ARCHITECTURE、CONTRIBUTING、self-update、FR-128 历史衔接与 CHANGELOG。
- [x] 将发布入口简化为推送 `v*` tag 自动发布，工作流不再创建 tag。
- [x] 接受 ADR-0065：文档与流程明确 `1.0.0` 前仅正式版、起才 RC/GA。
- [ ] 线上运行首次正式版（及日后 1.0 RC/GA）演练并保存验收证据。

## 5. 验收标准

- push `dev` 先通过四项质量门，再只生成保留 7 天的 Actions 实验工件；没有 tag、Release 或自更新可见版本，权限不高于 `contents: read`。
- push `main` 只运行质量门，不自动发布任何工件到 GitHub Releases。
- 推送 `vX.Y.Z` 且 VERSION/CHANGELOG 合法时触发正式发布工作流；自 `1.0.0` 起推送 `vX.Y.Z-rc.N` 触发 RC 工作流；tag 与 VERSION 不一致、CHANGELOG 段缺失、Release 重名时失败且不公开 Release。
- 正式版 / RC 的四项质量门和双平台构建使用同一固定 SHA；任一门失败时不存在新公开 Release。
- RC 公开后为 `prerelease=true` 且非 latest；正式版 / GA 公开后为 `prerelease=false` 且 latest。
- 发布资产回下载后，平台二进制的 SHA-256 与发布的 `checksums.txt` 一致；不一致时 draft 不公开。
- 自更新稳定频道只选择正式稳定 Release；候选版频道只在存在合法 `X.Y.Z-rc.N`（自 1.0 起产生）时升级，不再显示或安装旧 `dev` 开发预览。
- 文档与 CONTRIBUTING 明确：不得为 `0.y.z` 新建 RC 或把某次 `0.x` 称为 GA。
- 线上首次正式发布（及日后 1.0 RC/GA）必须人工核对 GitHub Actions 调用链、固定 SHA、tag 目标、draft 公开时点、资产集合与 latest 标记。

## 6. 风险与限制

- tag 由人工推送，推错 tag 会直接触发发布流水线；必须保证 VERSION、CHANGELOG 与 tag 一致后再推。
- Actions 工件、GitHub Release API 与回下载依赖 GitHub 服务可用性；外部失败应保持 draft/失败状态，不能把未验证版本公开。
- 本规格完成只代表 FR2-014 发布门切片完成，PRD 状态保持“开发中”，直到其余运维与交付能力分别验收。
