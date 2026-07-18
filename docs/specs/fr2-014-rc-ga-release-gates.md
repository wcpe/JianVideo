# 功能规格：FR2-014 门控式 RC/GA 发布切片

> 状态：开发中　·　关联 PRD：FR2-014　·　阶段：P7/P8/GA　·　决策：[ADR-0064](../adr/0064-gated-rc-ga-release.md)

## 1. 背景与目标

FR2-014 是覆盖初始化向导、设置、诊断、队列监控、Benchmark、E2E、发布包与 1.0 质量门的综合运维能力。本规格只实现其中“发布门”切片：把 `dev` 实验构建、公开 RC 与稳定 GA 分开，并确保 RC/GA 在固定提交上通过同一质量门、双平台构建、校验和与发布资产回验后才创建公开版本。

本切片进入开发中不代表 FR2-014 整体完成。初始化向导、设置与诊断收口、运维监控、备份恢复、一体化部署、完整 Benchmark/E2E 收口和 P7 其他交付能力仍按各自规格与路线图推进。

## 2. 范围

### 2.1 范围内

- 建立 `dev` 长期集成分支与 `main` 唯一发布分支的发布语义。
- 用 `experimental.yml` 取代滚动 GitHub `dev` 预发布：只在 `dev` 固定 SHA 上生成短期 Actions 实验工件。
- 新增无输入 `rc.yml`：从 `main` 读取 `VERSION=X.Y.Z-rc.N`，完整过门后自动创建 RC tag 与公开候选 Release。
- 把 `release.yml` 改为带唯一必填 `final_rc_tag` 的 GA 提升：只允许指定同基线、已公开 RC 的后继提交在发布元数据收口后发布稳定版。
- 复用 `ci.yml` 的 workspace、独立前端、Go、Playwright 四项质量门。
- 复用 `build.yml` 的 Linux/Windows CGO 原生构建，并让调用方固定源 SHA、校验产物与 `checksums.txt`。
- tag 只由最终 publish job 在全部门禁通过后创建；禁止人工预推或占位 tag。
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
3. 候选提交经 PR 或受控提升进入受保护 `main`；push `main` 只运行质量门。
4. RC 准备提交把 `VERSION` 设为 `X.Y.Z-rc.N`，并在 CHANGELOG 增加对应非空版本段；从 `main` 手动运行无输入 RC 工作流。
5. RC 验收发现代码问题时，修复回 `dev`，再次提升到 `main`，递增为新的 `rc.N`，不得覆盖旧 RC。
6. final RC 通过后，只做 GA 允许的版本、CHANGELOG 与正式文档收口，再从 `main` 手动运行 GA 工作流并填写该 `final_rc_tag`。

### 3.2 实验工件

- 触发：仅 push `dev`，没有手动入口。
- 权限：`contents: read`。
- 版本：取当前提交可达的最近稳定 tag 基线与其后提交距离，生成 `<base>-dev.N.g<shortsha>`；无稳定 tag 时从 `0.0.0` 起算。
- 构建：固定触发 SHA，先调用 `ci.yml` 四项质量门，再调用 `build.yml` 生成 Linux/Windows amd64 单二进制与 SHA-256。
- 工件：`jianvideo-experimental-<shortsha>-linux-amd64`、`jianvideo-experimental-<shortsha>-windows-amd64`，保留 7 天。
- 禁止：tag、GitHub Release、latest 标记、自更新频道、写仓库权限。

### 3.3 RC 门禁

- 入口必须是 `refs/heads/main` 的无输入 `workflow_dispatch`。
- 固定 `source_sha=github.sha`；后续 checkout、质量门、构建和发布全部使用该 SHA。
- `VERSION` 正则为 `^[0-9]+\.[0-9]+\.[0-9]+-rc\.[1-9][0-9]*$`。
- CHANGELOG 必须存在与 `VERSION` 精确对应的非空版本段。
- `v${VERSION}` tag 和同名 Release 必须都不存在；同时通过 GitHub matching-refs API 校验该序号严格等于同基线现有最大 RC 序号加一，且 prepare 与 publish 阶段都要复检。
- RC 与 GA 共享仓库级 `release-publication` 并发组，公开发布必须串行执行且不取消进行中的发布。
- 调用 `ci.yml`，workspace-quality、web-quality、go-quality、e2e 四项全部成功。
- 调用 `build.yml` 对固定 SHA 做 Linux/Windows 原生构建；工件名、平台资产和 checksum 必须完整。
- publish job 最后执行：先创建指向 `source_sha` 的轻量 tag，再以 `tag_name=v${VERSION}`、`target_commitish=source_sha` 创建带唯一归属标记的 draft Release；上传资产后以本地校验和为信任根回下载复验，成功才移除标记并公开为 `prerelease=true`、`make_latest=false`。

### 3.4 GA 门禁

- 入口必须是 `refs/heads/main` 的 `workflow_dispatch`，唯一必填输入为 `final_rc_tag`。
- `VERSION` 必须严格匹配 `X.Y.Z`，tag 为 `vX.Y.Z`，同名 tag/Release 不得已存在。
- `final_rc_tag` 必须匹配同基线 `vX.Y.Z-rc.N`，是同基线最高现有 RC，且其 tag 提交是当前 GA 固定 SHA 的祖先；GitHub API tag ref 必须以 commit 类型指向本地 tag commit，该 commit 的 `VERSION` 必须与 tag 一致。
- final RC Release 必须已公开、`draft=false`、`prerelease=true`，资产恰为 Linux 二进制、Windows 二进制和 `checksums.txt`；GA 必须回下载三项资产并验证 checksum 清单及两个二进制，但不搬运 RC 资产，也不与 GA 重建产物逐字节比较。
- final RC 在 prepare 阶段 fail-fast 预检，并在 publish job 创建 GA tag 前再次完整复检。
- 从 final RC 到 GA 只允许修改：`VERSION`、`CHANGELOG.md`、`README.md`、`docs/**` 与 `.claude/rules/scope-discipline.md`；工作流、发布脚本、依赖、构建配置、业务代码或其他差异立即失败。
- CHANGELOG 必须存在目标稳定版的非空版本段。
- 在当前固定 SHA 上重新执行四项质量门和 Linux/Windows 构建，不能复制 RC 资产冒充 GA。
- publish 顺序与 RC 相同；回下载校验通过后才公开为 `prerelease=false`、latest。

### 3.5 tag、draft 与失败清理

- 普通用户不得人工创建、改写或删除 `v*` tag；托管侧通过 tag Ruleset 仅放行指定发布自动化身份。
- 实际 tag 仅由 RC/GA 最终 publish job 创建，且创建时所有前置门禁已经成功。
- publish 失败时，只能删除本次运行创建、仍指向本次 `source_sha` 的 draft/tag；发现引用不属于本次运行或已被改写时停止清理并报警。
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
- [x] 新增 RC 版本、CHANGELOG、严格递增序号、重复发布、固定 SHA、draft 与回下载校验门。
- [x] 改造 GA 为最后 RC 同代码基线的正式元数据提升门，并重新构建验收。
- [x] 把自更新测试版频道迁移为候选版 RC 选择与比较。
- [x] 同步 PRD、ARCHITECTURE、CONTRIBUTING、self-update、FR-128 历史衔接与 CHANGELOG。
- [ ] 在 GitHub 配置 `main` 分支保护、`v*` tag Ruleset、发布环境权限与自动化身份。
- [ ] 线上运行首次 RC/GA 演练并保存验收证据。

## 5. 验收标准

- push `dev` 先通过四项质量门，再只生成保留 7 天的 Actions 实验工件；没有 tag、Release 或自更新可见版本，权限不高于 `contents: read`。
- push `main` 只运行质量门，不自动发布任何工件到 GitHub Releases。
- 从非 `main` 运行 RC/GA、VERSION 格式错误、CHANGELOG 段缺失、tag/Release 重名时均在创建 tag 前失败。
- RC/GA 的四项质量门和双平台构建使用同一固定 SHA；任一门失败时不存在新公开 tag/Release。
- RC 公开后为 `prerelease=true` 且非 latest；GA 公开后为 `prerelease=false` 且 latest。
- RC/GA 资产回下载后，平台二进制的 SHA-256 与发布的 `checksums.txt` 一致；不一致时 draft 不公开并执行归属安全的清理。
- 指定 final RC 不是同基线、不是 GA 提交祖先、Release 未公开或资产元数据不完整时 GA 失败；其后出现任何非允许列表差异时也失败，只做允许的发布元数据收口时 GA 才重新构建并通过。
- 人工预推同名 tag 不能绕过门禁，反而使工作流因重复引用失败；正式流程中 tag 只由 publish job 创建。
- 自更新候选版频道能从较低 RC 升级到较高 RC，不再显示或安装旧 `dev` 开发预览；稳定频道只选择 GA。
- 线上首次 RC 与 GA 必须人工核对 GitHub Actions 调用链、固定 SHA、tag 目标、draft 公开时点、资产集合、latest 标记、Ruleset 拦截和失败清理。

## 6. 风险与限制

- GitHub tag Ruleset、环境审批和自动化身份是托管侧配置，仓库内测试无法完全替代线上复验。
- “最后 RC 后仅元数据差异”的允许列表必须保持窄且可审查；新增例外必须同步 ADR/spec，不得临时放宽绕过。
- Actions 工件、GitHub Release API 与回下载依赖 GitHub 服务可用性；外部失败应保持 draft/失败状态，不能把未验证版本公开。
- 本规格完成只代表 FR2-014 发布门切片完成，PRD 状态保持“开发中”，直到其余运维与交付能力分别验收。
