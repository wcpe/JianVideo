# ADR-0064：门控式 RC/GA 发布与 dev 实验工件

## 状态
已接受（取代 [ADR-0032](0032-release-engineering.md) 与 [ADR-0042](0042-prerelease-version-strategy.md)；**何时**启用 RC/GA 渠道由 [ADR-0065](0065-pre-1-0-formal-only-release-channels.md) 补充：`1.0.0` 前仅正式版，`1.0.0` 起才推 RC/GA）

## 背景

ADR-0032 建立了 GitHub Actions 原生构建、GitHub Releases 分发和二进制自更新，ADR-0042 又为滚动 `dev` 预发布规定了提交距离版本号。随着仓库进入 v2 阶段，这套机制暴露出三个长期风险：

1. `main` 的普通 push 会直接刷新可被自更新消费的预发布，集成分支产物与公开候选版混为一谈。
2. 正式发布以预先存在的 tag 触发，tag 可能在质量门、固定提交构建和产物回验之前就成为公开事实；失败后容易留下无产物或指向错误提交的发布引用。
3. RC 与 GA 没有独立门禁，无法证明 GA 的业务代码与最后一个已验收 RC 完全一致，也无法阻止 RC 验收后混入代码变化。

因此需要把“日常实验构建”“公开 RC”“稳定 GA”拆成不同信任级别，并让 tag 成为所有门禁通过后的发布结果，而不是发布入口。本 ADR 完整重述仍保留的构建、分发、发布说明与自更新决策，并取代 ADR-0032、ADR-0042 的旧触发与滚动 `dev` Release 策略。

## 决策

### 1. 分支与信任边界

- `dev` 是长期集成与实验分支；`feature/*`、`fix/*`、`refactor/*` 等短分支经 PR 合入 `dev`。
- `main` 是唯一可发布、受保护分支；候选提交从 `dev` 经 PR 或受控提升进入 `main`。
- push 到 `main` 只运行质量门，不自动创建快照、tag 或 Release。
- `VERSION` 继续作为版本号唯一真源；发布工作流不接受人工输入的版本号或源提交 SHA。

### 2. dev 只产出实验工件

- `.github/workflows/experimental.yml` 只在 push 到 `dev` 时运行，没有手动入口。
- 工作流固定触发提交 SHA，以该提交可达的最近稳定 tag 为 `<base>`，以稳定 tag 到当前提交的距离为 `N`，注入实验版本 `<base>-dev.N.g<shortsha>`；没有稳定 tag 时以 `0.0.0` 为基线。
- 复用 `build.yml` 在 Linux/Windows 原生 runner 上分别以 `CGO_ENABLED=1` 构建，不交叉编译、不引入 Docker；产物仍为内嵌前端的单二进制并附 SHA-256 校验。
- 实验产物只作为保留 7 天的 GitHub Actions 工件，名称包含 `experimental`、平台与短 SHA；不创建 tag、不创建 GitHub Release，也不进入任何自更新频道。
- 实验工作流权限保持 `contents: read`，不能写仓库引用或发布对象。

### 3. RC 由推送 RC tag 触发

- `.github/workflows/rc.yml` 由推送 `v*-rc.*` tag 触发；固定 `source_sha=github.sha`。
- `VERSION` 必须严格匹配 `X.Y.Z-rc.N`（`N >= 1`），且等于推送 tag 去掉 `v` 前缀。
- 同名 Release 不得已存在；该 RC tag 必须已是同基线最高现有 RC。
- RC 与 GA 共享仓库级 `release-publication` 并发组，所有公开发布串行执行且不取消进行中的发布。
- 验证 CHANGELOG 对应 RC 版本段非空后，复用 `ci.yml` 四项质量门与 `build.yml` 双平台构建。
- publish 使用 `publish-from-tag`：**不创建 tag**，只创建带 ownership marker 的 draft Release；上传后回下载校验，成功才以 `prerelease=true`、`make_latest=false` 公开。

### 4. GA 由推送稳定 tag 触发

- `.github/workflows/release.yml` 由推送 `v*` tag 触发，并排除包含 `-rc.` 的 ref。
- `VERSION` 必须是严格稳定版 `X.Y.Z`，且等于推送 tag 去掉 `v` 前缀；同名 Release 不得已存在。
- CHANGELOG 必须存在目标稳定版的非空版本段。
- 在当前固定 SHA 上重新执行四项质量门和 Linux/Windows 构建；不能直接搬运 RC 资产。
- publish 使用 `publish-from-tag`；回下载校验通过后公开为 `prerelease=false`、latest。

### 5. tag 由发布者推送，工作流只公开 Release

- 发布入口是人工推送 `v*` tag；工作流不创建、不改写、失败时也不删除 tag。
- 不使用 GitHub App 身份、`v*` Ruleset bypass 或 production 环境审批作为发布前置。
- 发布阶段失败时，工作流只能清理“由本次运行创建、仍带归属标记”的 draft Release；不得删除既有或他人创建的发布引用。

### 6. 保留的构建、发布说明与分发决策

- 继续使用 GitHub Actions 原生 runner 矩阵构建 Linux/Windows amd64 单二进制，遵守 ADR-0027 的 CGO 原生构建边界；不交叉编译、不以 Docker 作为发布构建基础。
- GitHub Releases 继续作为公开 RC/GA 的分发渠道；每个平台二进制均由 `checksums.txt` 提供 SHA-256 完整性校验。
- Release notes 继续以 `CHANGELOG.md` 为真源，并由 ADR-0041 规定的共享抽取逻辑生成；CHANGELOG 缺少目标版本段时发布失败，不以空白兜底掩盖文档遗漏。

### 7. 自更新频道

- `stable` 频道只选择公开的稳定 GA Release（`prerelease=false`）。
- `prerelease` 频道对用户显示为“候选版 RC”，只选择合法的 `X.Y.Z-rc.N` 公开候选 Release；不再消费 `dev` tag、开发预览 Release 或 Actions 实验工件。
- RC 按 SemVer 比较，同一基线以更大的 RC 序号为更新；稳定版高于同基线 RC。旧滚动 `dev` 格式只作为历史兼容输入，不再由当前发布流程产生。
- 二进制自更新仍按当前平台选择资产、下载并校验 `checksums.txt`、原子替换、自动重启并保留上一版回滚；仅鉴权用户可显式触发，不做后台静默更新。

## 理由

- 把 dev 实验工件与公开 RC 分离，避免未经候选验收的集成产物进入自更新渠道。
- 以推送 tag 作为唯一发布入口，流程简单可操作；工作流仍强制四门与双平台构建后才公开 Release，避免未验证资产进入分发渠道。
- tag 已存在但质量门失败时，只留下未公开 draft/无 Release，不自动删除 tag；修复后递增 `rc.N` 或修正元数据后重推新 tag。
- RC/GA 都从固定 SHA 重新构建并回下载校验，避免可变分支、错用工件或上传损坏造成供应链漂移。
- 保留原生 runner、单二进制、SHA-256、CHANGELOG 真源与自更新回滚，延续已经验证过的交付形态。

## 后果

- 正面：`dev` 可持续集成而不污染公开频道；RC/GA 有统一质量门与可审计的 draft→公开路径；失败不会留下未经验证的公开 Release。
- 约束：发布前必须保证 `VERSION`、CHANGELOG 与即将推送的 tag 一致；RC 修复必须递增 `rc.N`，不得覆盖既有 tag。
- 约束：推错 tag 会直接触发流水线；不设 GitHub App / Ruleset / production 审批作为额外托管门禁。
- 兼容：历史 `dev` Release 与旧格式版本可保留查询，但不再更新，也不再作为候选版频道目标。
- 真机验收：首次 RC 与 GA 必须在 GitHub 上核验四门复用、固定 SHA、draft 公开时点、资产回下载校验、RC 非 latest、GA latest 与失败清理。

## 备选方案

- **工作流过门后才创建 tag（旧方案）**：可避免半发布 tag，但依赖 `workflow_dispatch`、可选 App 身份与 Ruleset，运维成本高，落选。
- **让 main 每次 push 自动发布预览**：把集成提交直接暴露给自更新用户，无法表达 RC 信任等级，落选。
- **GA 直接复用 RC 二进制资产**：可减少构建时间，但无法证明 GA 提交下的构建可重复，也绕过最终门禁，落选。
- **引入 GitHub App + tag Ruleset 托管门禁**：增加 Secrets/Ruleset/环境审批复杂度，与“推 tag 即发”目标冲突，落选。
