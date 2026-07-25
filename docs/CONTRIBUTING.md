# 演进与维护指南

> 本文规定文档如何随代码演进、ADR 如何迭代、新需求如何落地。**目标：防止文档腐朽、变味、漂移。**

## 1. 黄金法则：文档即代码（docs-as-code）

- 文档与代码在**同一仓库、同一次变更（PR）**里一起改。
- **完成定义（DoD）**：一个变更没有把受影响的文档改到一致，就**不算完成**。
- **单一真源**：同一事实只在一处权威描述，别复制散落到多处各说各话；要复用就引用。

## 2. 文档地图（谁管什么 · 何时更新 · 入库否）

| 文档 | 管什么 | 何时更新 | 入库 |
|---|---|---|---|
| `docs/PRD.md` | 需求（WHAT/WHY）：目标、角色、功能需求、验收 | 需求增删改时 | ✓ 活文档 |
| `docs/ROADMAP.md` | v2 阶段路线与版本线：P0/P1/... 对应的 `0.y.x` 版本线、进入/退出条件 | 阶段边界或版本线规则变化时 | ✓ 活文档 |
| `docs/specs/<feature>.md` | 非平凡功能的开发期工作规格（需求/设计/任务/验收） | 开发该功能时 | ✓ 留作记录 |
| `docs/ARCHITECTURE.md` | 系统设计（HOW）：模块、数据模型、机制、部署 | 结构/机制/依赖变化时 | ✓ |
| `docs/adr/*` | 重大决策的"为什么" | 做出/推翻架构决策时（见 §3） | ✓ |
| `docs/API.md` | 对外接口契约 | 接口变更时 | ✓ |
| `CHANGELOG.md` | 变更史 | 每个用户可见变更 | ✓ |
| `README.md` | 入口与导航 | 总览变化时 | ✓ |
| `.tmp/实施计划.md` | 里程碑、勾选、探索笔记 | 随便改，做完即弃 | ✗ 易朽 |

**判据：活文档（长期维护、是真源）入库；易朽稿（做完即弃）留 `.tmp/`。**

## 3. ADR 生命周期（不可变 + 取代）

- ADR 一旦"已接受"就**不可变**：**不编辑**旧 ADR 的决策正文。它是**决策史，永久保留、只增不删、编号永不复用**。
- 决策变了 → 写一条**新 ADR 取代旧的**：
  - 新 ADR 在背景里写"取代 ADR-NNNN"。
  - 旧 ADR 只把**状态行**改为"已被 ADR-MMMM 取代"并加链接（**不动正文**）。
- 状态：`提议中 → 已接受 → 已弃用 / 已被取代`。
- **何时写 ADR**：引入新技术、采用或推翻一个架构模式、做有长期影响且有争议的取舍。小决定不用写。

### 3.1 ADR 实操（维护期）

- **编号**：= 现有最大编号 + 1，永不复用、不补洞（现有最大看 `docs/adr/` 目录，别硬记某个数）。
- **写不写**：日常加功能若落在既有决策内，不写；只有上面"何时写"的情形才写。
- **取代长什么样**：当一个新需求推翻了某条既有架构决策，按以下步骤处理：
  1. 新建一条新 ADR（取下一个空号），背景写"取代 ADR-NNNN"。
  2. 把被取代的旧 ADR 状态行从"已接受"改为"已被该新 ADR 取代" + 链接，**正文一字不动**。
  3. 同步改受影响的 `.claude/rules/*`（如撤掉对应的不变量红线）、`ARCHITECTURE.md`、相关技能。

  旧决策"当初为什么"完整留存，新决策"为什么改"也有据——这就是 ADR 防漂移的价值。

**规模与导航（别慌通读）**：ADR 有意稀少——成熟项目一辈子也就几十到一两百条，不会上万；写得很快是"把日常决策误当架构决策"的滥用信号（日常变更靠 PRD 状态列 + CHANGELOG，不靠 ADR）。理解系统看 `ARCHITECTURE.md`（永远是现状综合），ADR 只在查"当初为什么"时按需翻；被取代的归档不打扰，**当前架构 = 未被取代的活跃集**，不必也别想通读所有 ADR。

## 4. 变更工作流（新需求 / 新功能如何落地）

```
1. 改 PRD（docs/PRD.md）         增/改需求，标 FR2 编号、阶段与状态
2. 查 ROADMAP（docs/ROADMAP.md）  确认该需求属于当前版本线，越界先问
3. 影响架构？→ 写新 ADR          引入/推翻架构决策时（必要时取代旧 ADR）
4. 非平凡功能 → 写 spec          在 docs/specs/ 下写需求 / 设计 / 任务 / 验收
5. 改 ARCHITECTURE.md            反映新模块 / 数据模型 / 机制 / 依赖
6. 改 API.md                    接口契约变更
7. 实现 + 测试                  过验证门（全部测试绿）
8. 记 CHANGELOG（未发布段）       用户可见变更，引用具体模块/能力
9. 发版                         按当前版本线 bump 版本号、定稿 CHANGELOG 本版本段
```

> 一次只做一件事；破坏性变更必须在 CHANGELOG + 相关文档写清迁移。

## 5. 防漂移检查清单（每次变更自检）

- [ ] 代码改了，PRD / ARCHITECTURE / API 是否还一致？
- [ ] 新增或推翻了架构决策，是否有对应 ADR（且旧 ADR 已标记取代）？
- [ ] 破坏性变更，CHANGELOG 与迁移说明是否写了？
- [ ] 文档里的版本号 / 坐标 / 路径 / 端点是否过时？
- [ ] 同一事实是否只有一个真源？

## 6. 定期复检

- 每个里程碑 / 发版前，过一遍 §2 文档地图，确认无漂移。
- 发现"代码这样、文档那样" → **当 bug 修**，不积压。

## 7. 与 AI 协作

本仓库常与 AI 代理协作。文档同步要求已固化为 `.claude/rules/doc-sync.md`，未来任何会话改代码都会被要求同步文档，避免跨会话漂移。

## 8. 分支模型与发布渠道

采用 `dev` 集成、`main` 发布的门控流程（见 [ADR-0064](adr/0064-gated-rc-ga-release.md)、[ADR-0065](adr/0065-pre-1-0-formal-only-release-channels.md)）：

- **`dev`**：长期集成与实验分支；`feature/*`、`fix/*`、`refactor/*` 等短分支经 PR 合入 `dev`。push `dev` 运行质量门并生成短期 Actions 实验工件，但不创建 tag、GitHub Release，也不进入自更新频道。
- **`main`**：唯一可发布、受保护分支；可发布提交从 `dev` 经 PR 或受控提升进入 `main`。push `main` 只运行质量门，不自动发布。
- **`hotfix/*`**：从出问题的发布 tag 切分支做最小修复，修复必须回流 `dev`，再按正式补丁（`0.y.z`）或自 1.0 起的 RC/GA 流程提升到 `main`。
- **回滚**优先 `git revert`，不重写已 push 历史（`sdd-rollback-change` 技能）。

版本号唯一来源是根 `VERSION` 文件，构建把版本号注入到编译产物中。**发布入口是推送 `v*` tag**。工作流在固定 SHA 上跑完质量门与双平台构建后，创建 draft Release、回下载校验资产并公开；**不再由工作流创建 tag**，也不使用 GitHub App 或 Ruleset bypass。

**渠道启用时机**（见 [ADR-0065](adr/0065-pre-1-0-formal-only-release-channels.md)）：

- **`1.0.0` 之前**：只发正式版——准备 `VERSION=0.Y.Z` 与 CHANGELOG 稳定段，推送 `v0.Y.Z`，由 `release.yml` 公开正式 Release。**禁止**新建 `v0.Y.Z-rc.N` 或把某次 `0.x` 发布称为 RC/GA。
- **自 `1.0.0` 起**：才启用 RC/GA——推 `vX.Y.Z-rc.N` 触发 `rc.yml`，推 `vX.Y.Z` 触发 `release.yml`。

### 8.1 正式版发布流程（`0.y.z` 与 `1.0.0` 稳定 tag 通用）

**强制顺序（先绿后发，详见 `.claude/rules/release-discipline.md`）：**

0. **普通 commit 合入 `dev`，且 `dev` 实验构建 / 四项质量门全绿**（有 run 证据）。禁止用 `chore(release)` 去试 CI。
1. **经 PR 提升到 `main`**（走分支保护：必需 checks + reviews）。**禁止**为推 `main` 临时关闭或修改分支保护。
2. 确认 **`main` 上该提交质量门也绿**。
3. 仅在以上全绿后，在可发布提交上准备 `VERSION=X.Y.Z`，CHANGELOG 有对应非空稳定版本段（独立 `chore(release)` commit）。
4. **确认 release commit 已在 `origin/main` 历史**，再打并推送 tag `vX.Y.Z`；`release.yml` 校验 VERSION 与 tag 一致、Release 不存在，再跑四项质量门与 Linux/Windows 构建。
5. 通过后为该 tag 创建 draft Release，回下载校验后公开为正式版（`prerelease=false`、latest）。
6. 修复不得覆盖既有 tag；需要时递增 patch 再发新正式版。

错误发版：删错误 tag + `git revert` 元数据 + PR 回流 `main`，**不** force-push、**不**改保护。

### 8.2 RC 发布流程（**仅自 `1.0.0` 起**）

1. 在可发布提交上准备 RC 元数据：`VERSION=X.Y.Z-rc.N`（`X >= 1`），CHANGELOG 有对应非空版本段。
2. 在该提交打并推送 tag `vX.Y.Z-rc.N`；`rc.yml` 自动校验 VERSION 与 tag 一致、Release 不存在、RC 序号为同基线最高，再跑四项质量门与 Linux/Windows 构建。
3. 质量与构建通过后，publish job 为**已有** RC tag 创建 draft Release，上传并回下载校验资产后公开为候选版、非 latest。
4. RC 发现代码问题时回 `dev` 修复，提升后递增 `rc.N` 再推新 tag；不得覆盖既有 RC tag。
5. final RC 验收通过后，收口 `VERSION=X.Y.Z`、CHANGELOG 稳定版段与正式文档，在同一代码基线上按 §8.1 推送 `vX.Y.Z`。

### 8.3 v2 版本线规则

从 `0.21.0` 开始，JianVideo v2 使用固定的阶段版本线：

- P0：`0.21.x` 规格冻结与技术基线
- P0.5：`0.21.x` 架构与工具链冻结门
- P1：`0.22.x` Mockup、UI 博物馆与 PixiJS 原型实现
- P2：`0.23.x` 存储库、索引与转码队列
- P3：`0.24.x` 播放体验（王牌）
- P4：`0.25.x` 高密度媒体浏览器
- P5：`0.26.x` Space、安全与多用户
- P6：`0.27.x` AI 索引、搜索与审核
- P7：`0.28.x` 多端与交付质量门
- P8：`0.29.x` 1.0 候选准备（阶段内仍为正式版 `0.29.x`）
- GA：`1.0.0`（经 `1.0.0-rc.N` 后）

同一 `MINOR` 下的所有 patch 都属于同一阶段，`PATCH` 按需要递增且不设固定上限。P0.5 是 `0.21.x` 内部冻结门，不单独占用新的 minor 版本线。`1.0.0` 前属于 `0.y.z` 预稳定阶段且**只发正式版**；`1.0.0` 后严格按 SemVer 执行：patch=兼容修复，minor=兼容新增，major=破坏性变更，并可使用 RC→GA。

## 9. 文档如何长期演进（本次会话之后）

这些文档不是一次性产物，而是随项目活下去。每篇的演进方式不同：

| 文档 | 演进方式 |
|---|---|
| `docs/PRD.md` | **增量 + 状态流转**：加需求即加一行 FR2（`计划`→`开发中`→`已交付@vX.Y.Z`），已交付的保留并标版本、不删——它是需求登记册 |
| `docs/ROADMAP.md` | **阶段线原地维护**：只在阶段边界、版本线规则、进入/退出条件变化时更新 |
| `docs/ARCHITECTURE.md` | **原地更新**：始终反映当前系统真貌；结构 / 机制变了就改它 |
| `docs/adr/*` | **只追加 + 取代**：决策变了写新 ADR 取代旧的，旧的不删（§3） |
| `docs/API.md` | **原地更新**：始终是当前契约 |
| `CHANGELOG.md` | **累积 + 发版分段**：变更先进未发布段，发版时切成 `## X.Y.Z` 段 |

**文档冷热分层**（哪些常动、哪些少动，心里有数就不会该改的没改、不该动的乱动）：

| 冷热 | 文档 | 多久动一次 |
|---|---|---|
| 🔥 高频（几乎每次迭代） | `CHANGELOG.md` | 每个用户可见变更 |
| 🔥 高频 | `docs/PRD.md` | 每个新需求 / 交付（加行 / 改状态） |
| 🌡 中频 | `docs/ROADMAP.md` | 阶段边界或版本线规则变化时 |
| 🌡 中频（有相应变化才动） | `docs/ARCHITECTURE.md`、`docs/API.md` | 结构 / 机制 / 接口变更时 |
| 🌡 中频 | `docs/OPERATIONS.md` | 部署 / 运维方式变化时 |
| 🌡 中频 | `docs/specs/<feature>.md` | 功能开发期；交付后基本不动 |
| ❄ 低频 | `docs/adr/*`、`README.md`、`SECURITY.md` | 架构决策时追加 / 总览或安全模型变化时 |
| 🧊 近乎不变（改它=动项目根基） | `.claude/rules/*`（尤其 `architecture-invariants`）、全局 `sdd-*` 技能、`.editorconfig`、`.gitignore`、`VERSION`（仅发版动） | 极少；改不变量 / 红线要慎重并配 ADR |

把"改不变量 / 红线"当大事——它们近乎不变，真要改先走 ADR，别随手动。

**会话之间如何接续**：这套靠 `.claude/rules/`（红线，项目内）+ 全局 `sdd-*` 技能（流程）**自我维持**——任何新会话进入仓库会自动加载规则，按 `sdd-develop-feature` / `sdd-fix-bug` / `sdd-refactor-code` / `sdd-rollback-change` / `sdd-release-version` 等技能干活，每一步被 `doc-sync` 要求同步文档。所以"本次会话结束"不影响延续：下个会话读 PRD / ARCHITECTURE / ADR 接上下文，照技能与规则继续推进，文档随之演进、不漂移。

## 10. 维护迭代周期（稳态操作手册）

P0 规格冻结后进入 v2 稳态迭代。**每个工作项的标准循环**：

1. **识别工作项**，选对应技能（路由见下表）。
2. **开分支**：`feature/*` / `fix/*` / `refactor/*` / `hotfix/*`（§8）。
3. **按技能走**：读相关 PRD / ARCHITECTURE / ADR → 测试先行 → 实现（守不变量、简单优先）→ 过验证门 → `doc-sync` 同步文档。
4. **发 PR**：填防漂移自检模板 → 评审 → 合入 `dev`。
5. **dev 自动出实验工件**（Actions artifact，不建 tag/Release），供集成试用。
6. **攒够一批 → 准备 VERSION/CHANGELOG 后推 `v*` tag 发版**：`1.0.0` 前只推正式 `v0.Y.Z`；`1.0.0` 起可先推 `vX.Y.Z-rc.N` 再推稳定 `vX.Y.Z`（ADR-0065）。CI 自动过门、构建并公开 Release；工作流不创建 tag。
7. **生产事故** → `sdd-hotfix` 旁路：从发布 tag 切分支最小修 → 出补丁版 → 回流 `main`。

→ 回到 1。

**工作项 → 技能 路由**：

| 来了什么 | 用哪个技能 |
|---|---|
| 新需求 / 新能力 | `sdd-develop-feature` |
| bug / 报错 / 行为不对 | `sdd-fix-bug` |
| 代码太乱 / 拆分 / 消除重复 | `sdd-refactor-code` |
| 撤掉某功能 / 回退 | `sdd-rollback-change` |
| 升级第三方依赖 | `sdd-bump-dependencies` |
| 纯文档工作（写 ADR / 改架构说明 / 修文档漂移 / 整理文档） | `sdd-update-docs` |
| 出快照 / 给人试用 | `sdd-publish-snapshot` |
| 正式发版 | `sdd-release-version` |
| 生产紧急修 | `sdd-hotfix` |
| 外部 / 计划外提交进来（队友直推、CI、合并、逆向后新提交）需对齐文档 | `sdd-reconcile-external-commits` |
| sdd-skills 更新了治理模板，要同步进本项目 | `sdd-sync-governance` |

**一句话**：稳态下你几乎不用从头想流程——认准工作项类型、调对应技能，它会带着你读文档、测试先行、同步文档、按分支与版本规矩走完。规则与技能就是把这套循环固化下来、跨会话不走样。

### 10.1 一次变更各动哪些（速查）

| 来了什么 | 要动 | 不用动 |
|---|---|---|
| **feat 新功能** | PRD §5 加一行 FR2（贴已有阶段 + 状态 `计划`）· 非平凡写 `docs/specs/<f>.md` · 结构变更动 `ARCHITECTURE` · 接口变更动 `API` · `CHANGELOG` +1 行 · 加测试 | 阶段线 · `VERSION`（发版才动） |
| **fix 修 bug** | `CHANGELOG` +1 行 · 复现 + 回归测试 | PRD · 阶段线 · `VERSION` · ADR · API |
| **refactor 重构** | 结构变才动 `ARCHITECTURE` · 测试前后同样全绿 | PRD · 阶段线 · API · 行为 |
| **rollback 回滚** | FR2 状态回退 · 取代相关 ADR · `CHANGELOG` +1（移除） | 阶段线 |
| **依赖升级** | 锁文件 · 有感知影响才记 `CHANGELOG` · 全测试绿 | PRD · 阶段线 · ADR |
| **架构决策** | **ADR +1 条（或取代旧的，编号 = 现有最大 +1）** · 更新 `ARCHITECTURE` | 阶段线（除非顺带开新阶段） |
| **发布正式版（`0.y.z` 与稳定 `X.Y.Z`）** | `VERSION` 改为 `X.Y.Z` · CHANGELOG 建稳定版段 · 推送 tag `vX.Y.Z` · CI 过门后公开 latest · 交付的 FR2 按真实范围翻状态 | 阶段线 · 覆盖已有 tag · `0.x` 禁止写成 RC/GA |
| **发布 RC（仅 `X >= 1`）** | `VERSION` 改为 `X.Y.Z-rc.N` · CHANGELOG 建对应版本段 · 推送 tag `vX.Y.Z-rc.N` · CI 过门后公开候选 Release | 阶段线 · 覆盖已有 tag · 不得用于 `0.y.z` |
| **发实验工件** | push `dev` 后先过四门，再构建保留 7 天的 Actions artifact（版本按最近稳定 tag 生成 `<base>-dev.N.g<sha>`） | `VERSION` · `CHANGELOG` · tag · Release · 阶段线 |
| **进入下一阶段** | 更新 `docs/ROADMAP.md` 进入 / 退出条件、PRD 阶段状态，并把 `VERSION` 切到下一条 minor 版本线 | P0.5 这类内部冻结门除外，它仍归属当前 minor 版本线 |

**谁常动 / 谁不动**：
- 🔥 高频：`CHANGELOG`（几乎每次）、PRD FR2 表（每个 feat 加行 / 发版按真实范围翻状态）、`VERSION`（正式版 / 自 1.0 起的 RC 发布准备时）。
- ❄ 低频：ADR（只在架构决策时 +1 或取代）。
- 🧊 几乎不动：**阶段版本线**（只在进入下一阶段时动）、`.claude/rules`（项目内）/ 全局 `sdd-*` 技能（动它 = 动根基，要配 ADR）。
