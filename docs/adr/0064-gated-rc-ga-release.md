# ADR-0064：门控式 RC/GA 发布与 dev 实验工件

## 状态
已接受（取代 [ADR-0032](0032-release-engineering.md) 与 [ADR-0042](0042-prerelease-version-strategy.md)）

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

### 3. RC 由无输入门控工作流发布

- `.github/workflows/rc.yml` 仅由无输入 `workflow_dispatch` 触发，且必须从 `refs/heads/main` 运行；从其他分支触发立即失败。
- `VERSION` 必须严格匹配 `X.Y.Z-rc.N`，其中 `N >= 1`；tag 为 `v${VERSION}`。工作流通过 GitHub matching-refs API 读取同基线现有 RC tag：没有既有 RC 时只能从 `rc.1` 起步，否则必须严格等于 `max(N)+1`；prepare 与最终 publish 均复检，禁止依赖本地可能过期的 tag 列表或覆盖既有候选版。
- RC 与 GA 共享仓库级 `release-publication` 并发组，所有公开发布串行执行且不取消进行中的发布。
- 工作流固定 `source_sha=github.sha`，验证 `VERSION` 与 `CHANGELOG.md` 对应 RC 版本段非空，复用 `ci.yml` 的四项质量门，并让 `build.yml` 对同一固定 SHA 完成 Linux/Windows 构建与校验。
- 只有上述门禁全部通过，最终 publish job 才创建指向 `source_sha` 的轻量 tag 与带每次运行唯一隐藏 ownership marker 的 draft Release。
- 上传资产后必须从 Release 回下载严格三项资产；下载的 `checksums.txt` 必须与本地 `prepare_payload` 生成的文件完全一致，再以本地校验和验证两个二进制。成功后才移除 marker，并以 `prerelease=true`、`make_latest=false` 公开 RC Release。

### 4. GA 只能提升最后一个已验证 RC

- `.github/workflows/release.yml` 只接受 `workflow_dispatch`，唯一必填输入为 `final_rc_tag`，且只能从 `refs/heads/main` 运行；不再响应人工预推的版本 tag。
- `VERSION` 必须是严格稳定版 `X.Y.Z`；`final_rc_tag` 必须匹配同基线 `vX.Y.Z-rc.N`，其提交必须是当前 GA 固定 SHA 的祖先。
- `final_rc_tag..GA` 只允许正式发布元数据差异：`VERSION`、`CHANGELOG.md`、`README.md`、`docs/**` 与范围纪律文档 `.claude/rules/scope-discipline.md`。工作流、发布脚本、依赖、构建配置、业务代码或其他文件有差异时一律拒绝 GA。
- 被指定的 final RC 必须是同基线最高现有 RC；其 API tag ref 必须以 commit 类型指向本地 tag commit，该 commit 的 `VERSION` 必须与 tag 一致。对应 Release 必须已经公开、`draft=false`、`prerelease=true`，资产集合严格为 Linux 二进制、Windows 二进制与 `checksums.txt`；GA 会回下载并验证 RC 自身校验和，但不搬运 RC 资产，也不把 RC 与 GA 重建二进制逐字节比较。
- final RC 在 prepare 阶段执行 fail-fast 预检，并在 publish job 创建 GA tag 前再次完整复检。
- GA 重新复用四项质量门，并对当前固定 SHA 重新构建 Linux/Windows 产物与校验和；不能直接搬运 RC 资产。
- 门禁通过后，publish job 才创建 `vX.Y.Z` tag 与 draft Release；资产上传和回下载校验通过后，才公开为 `prerelease=false`、latest 的稳定 Release。

### 5. 禁止人工预先推 tag

- 人工不得预先 push RC/GA tag，也不得创建占位 tag；tag 是门控发布成功后的结果。
- 仓库可预配置匹配 `v*` 的 tag Ruleset，但 Ruleset 只是权限策略，不是实际 Git 引用。普通用户不得创建、改写或删除 `v*` tag，仅放行指定发布自动化身份。
- 发布阶段失败时，工作流只能清理“由本次运行创建、仍指向本次固定 SHA”的 draft Release 与 tag；不得删除既有或他人创建的发布引用。

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
- 先验证、后创建 tag，使 tag/Release 成为门禁通过的证明，避免“引用已公开但构建失败”的半发布状态。
- GA 必须与最后一个 RC 保持同一代码基线且只允许元数据收口，能够证明用户验收过的就是最终发布代码。
- RC/GA 都从固定 SHA 重新构建并回下载校验，避免可变分支、错用工件或上传损坏造成供应链漂移。
- 保留原生 runner、单二进制、SHA-256、CHANGELOG 真源与自更新回滚，延续已经验证过的交付形态，只替换不安全的发布触发和频道语义。

## 后果

- 正面：`dev` 可持续集成而不污染公开频道；RC/GA 有可审计的统一门禁；GA 与最终 RC 的代码身份可证明；失败发布不会留下未经验证的公开版本。
- 约束：发布必须先把正确的 `VERSION` 与 CHANGELOG 版本段合入 `main`；RC 修复必须递增 `rc.N`；GA 必须显式指定已公开的 final RC，并且其后只能做允许列表内的发布元数据收口。
- 约束：仓库管理员需要配置 `main` 分支保护、`v*` tag Ruleset、发布环境权限与指定自动化身份；这些 GitHub 托管侧设置不能只靠仓库 YAML 自证。
- 兼容：历史 `dev` Release 与旧格式版本可保留查询，但不再更新，也不再作为候选版频道目标。
- 真机验收：首次 RC 与 GA 必须在 GitHub 上核验四门复用、固定 SHA、tag/draft 创建时点、资产回下载校验、RC 非 latest、GA latest、失败清理和自更新频道选择。

## 备选方案

- **继续 push tag 触发发布**：tag 在门禁前已存在，失败后形成半发布状态，落选。
- **让 main 每次 push 自动发布预览**：把集成提交直接暴露给自更新用户，无法表达 RC 信任等级，落选。
- **GA 直接复用 RC 二进制资产**：可减少构建时间，但无法证明 GA 元数据提交下的构建可重复，也绕过最终门禁，落选。
- **GA 允许 RC 后继续混入修复代码**：用户验收的 RC 与 GA 不再同一代码身份；任何代码修复都应发布新的 `rc.N`，故落选。
- **人工先建占位 tag，再由工作流补资产**：仍会暴露无产物或未验证引用，且清理归属不清，落选。
