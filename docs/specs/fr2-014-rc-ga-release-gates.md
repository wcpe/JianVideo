# 功能规格：FR2-014 门控式 RC/GA 发布切片

> 状态：开发中　·　关联 PRD：FR2-014　·　阶段：P7/P8/GA　·　决策：[ADR-0064](../adr/0064-gated-rc-ga-release.md)

## 1. 背景与目标

FR2-014 是覆盖初始化向导、设置、诊断、队列监控、Benchmark、E2E、发布包与 1.0 质量门的综合运维能力。本规格只实现其中“发布门”切片：把 `dev` 实验构建、公开 RC 与稳定 GA 分开，并确保 RC/GA 在固定提交上通过同一质量门、双平台构建、校验和与发布资产回验后才创建公开版本。

本切片进入开发中不代表 FR2-014 整体完成。初始化向导、设置与诊断收口、运维监控、备份恢复、一体化部署、完整 Benchmark/E2E 收口和 P7 其他交付能力仍按各自规格与路线图推进。

## 2. 范围

### 2.1 范围内

- 建立 `dev` 长期集成分支与 `main` 唯一发布分支的发布语义。
- 用 `experimental.yml` 取代滚动 GitHub `dev` 预发布：只在 `dev` 固定 SHA 上生成短期 Actions 实验工件。
- 新增 tag 触发 `rc.yml`：推送 `vX.Y.Z-rc.N` 后读取固定 SHA 的 `VERSION`，完整过门后为该 tag 公开候选 Release。
- 新增 tag 触发 `release.yml`：推送稳定 `vX.Y.Z` 后在固定 SHA 上重跑质量门与构建，再公开 latest 正式 Release。
- 复用 `ci.yml` 的 workspace、独立前端、Go、Playwright 四项质量门。
- 复用 `build.yml` 的 Linux/Windows CGO 原生构建，并让调用方固定源 SHA、校验产物与 `checksums.txt`。
- tag 由发布者推送触发 CI；工作流**不创建 tag**，只创建/校验/公开 Release。
- RC/GA 先创建 draft，上传后从 Release 回下载复验，成功才公开。
- 自更新“候选版”频道只消费公开 RC，不消费 dev 实验工件。

### 2.2 范围外

- 不把 FR2-014 整体标为已交付。
- 不实现 P7 其余初始化、设置、诊断、监控、备份恢复、一体化部署或跨端运维能力。
- 不修改 ROADMAP 阶段边界；P7 仍为 `0.28.x`，P8 RC 仍为 `0.29.x`，GA 仍为 `1.0.0`。
- 不自动 bump `VERSION`、不自动生成 CHANGELOG 内容、不替开发者选择 RC 序号。
- 不引入 Docker、交叉编译、外部制品库、签名服务或后台静默更新。
- 不删除历史 Release/tag；旧 `dev` 仅停止继续产生和消费。

## 3. 设计

### 3.1 分支流

1. 功能与修复分支经 PR 合入 `dev`。
2. `dev` push 运行质量门并生成实验工件，供集成试用。
3. 候选提交经 PR 或受控提升进入 `main`；push `main` 只运行质量门。
4. RC 准备提交把 `VERSION` 设为 `X.Y.Z-rc.N`，并在 CHANGELOG 增加对应非空版本段；在该提交打并推送 `vX.Y.Z-rc.N`。
5. RC 验收发现代码问题时，修复回 `dev`，再次提升，递增为新的 `rc.N` 并推新 tag，不得覆盖旧 RC。
6. final RC 通过后，收口 `VERSION=X.Y.Z`、CHANGELOG 与正式文档，在同一代码基线上打并推送 `vX.Y.Z`。

### 3.2 实验工件

- 触发：仅 push `dev`，没有手动入口。
- 权限：`contents: read`。
- 版本：取当前提交可达的最近稳定 tag 基线与其后提交距离，生成 `<base>-dev.N.g<shortsha>`；无稳定 tag 时从 `0.0.0` 起算。
- 构建：固定触发 SHA，先调用 `ci.yml` 四项质量门，再调用 `build.yml` 生成 Linux/Windows amd64 单二进制与 SHA-256。
- 工件：`jianvideo-experimental-<shortsha>-linux-amd64`、`jianvideo-experimental-<shortsha>-windows-amd64`，保留 7 天。
- 禁止：tag、GitHub Release、latest 标记、自更新频道、写仓库权限。

### 3.3 RC 门禁

- 入口是推送 tag：`refs/tags/v*-rc.*`。
- 固定 `source_sha=github.sha`；后续 checkout、质量门、构建和发布全部使用该 SHA。
- `VERSION` 正则为 `^[0-9]+\.[0-9]+\.[0-9]+-rc\.[1-9][0-9]*$`，且必须等于推送 tag 去掉 `v` 前缀。
- CHANGELOG 必须存在与 `VERSION` 精确对应的非空版本段。
- 同名 Release 不得已存在；该 RC tag 必须已是同基线最高现有 RC。
- RC 与 GA 共享仓库级 `release-publication` 并发组，公开发布必须串行执行且不取消进行中的发布。
- 调用 `ci.yml` 四项质量门与 `build.yml` 双平台构建。
- publish job 使用 `publish-from-tag`：不为已推送 tag 再创建 tag，只创建 draft Release、上传资产、回下载复验后公开为 `prerelease=true`、`make_latest=false`。

### 3.4 GA 门禁

- 入口是推送稳定 tag：`refs/tags/v*`，并排除包含 `-rc.` 的 ref。
- `VERSION` 必须严格匹配 `X.Y.Z`，且等于推送 tag 去掉 `v` 前缀。
- 同名 Release 不得已存在。
- CHANGELOG 必须存在目标稳定版的非空版本段。
- 在当前固定 SHA 上重新执行四项质量门和 Linux/Windows 构建，不能复制 RC 资产冒充 GA。
- publish 使用 `publish-from-tag`；回下载校验通过后公开为 `prerelease=false`、latest。

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
- [x] 将 RC/GA 入口简化为推送 `v*` tag 自动发布，工作流不再创建 tag。
- [ ] 线上运行首次 RC/GA 演练并保存验收证据。

## 5. 验收标准

- push `dev` 先通过四项质量门，再只生成保留 7 天的 Actions 实验工件；没有 tag、Release 或自更新可见版本，权限不高于 `contents: read`。
- push `main` 只运行质量门，不自动发布任何工件到 GitHub Releases。
- 推送 `vX.Y.Z-rc.N` / `vX.Y.Z` 且 VERSION/CHANGELOG 合法时，触发对应 RC/GA 工作流；tag 与 VERSION 不一致、CHANGELOG 段缺失、Release 重名时失败且不公开 Release。
- RC/GA 的四项质量门和双平台构建使用同一固定 SHA；任一门失败时不存在新公开 Release。
- RC 公开后为 `prerelease=true` 且非 latest；GA 公开后为 `prerelease=false` 且 latest。
- RC/GA 资产回下载后，平台二进制的 SHA-256 与发布的 `checksums.txt` 一致；不一致时 draft 不公开。
- 自更新候选版频道能从较低 RC 升级到较高 RC，不再显示或安装旧 `dev` 开发预览；稳定频道只选择 GA。
- 线上首次 RC 与 GA 必须人工核对 GitHub Actions 调用链、固定 SHA、tag 目标、draft 公开时点、资产集合与 latest 标记。

## 6. 风险与限制

- tag 由人工推送，推错 tag 会直接触发发布流水线；必须保证 VERSION、CHANGELOG 与 tag 一致后再推。
- Actions 工件、GitHub Release API 与回下载依赖 GitHub 服务可用性；外部失败应保持 draft/失败状态，不能把未验证版本公开。
- 本规格完成只代表 FR2-014 发布门切片完成，PRD 状态保持“开发中”，直到其余运维与交付能力分别验收。
