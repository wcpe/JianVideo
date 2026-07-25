# Git 提交规范

> 适用于本仓库所有 `git commit` 操作。

## 1. 提交信息语言（强制）

- **标题（Description）与正文（Body）必须使用简体中文。** 禁止英文、日文等非中文。
- Conventional Commits 的 type 与 scope 仍用英文小写（`feat`/`fix`/`refactor`/`docs`/`chore`/`test`/`build`/`ci`/`perf`/`style`）。
- **禁止在提交信息中添加任何 AI 签名或尾注**，例如 `Generated with ...`、`Co-Authored-By: ...`。不要附加作者/工具/来源署名。

### 1.1 标题格式

```
<type>(<scope>): <中文描述>
```

- `<scope>`：英文小写模块/能力域，可选。填 `web`、`library`、`transcoder`、`watcher`、`db`、`auth`、`player`、`api`、`config`、`docs` 等。
- `<中文描述>`：简洁陈述本次做了什么，必须中文，结尾不加句号。

### 1.2 正文格式

- 用空行与标题分隔，中文撰写，可用 `-` 列要点。
- 说明"为什么改"与"改动要点"，不逐行复述 diff。

### 1.3 示例

✅ 正确
```
feat(transcoder): 实现 NVIDIA NVENC 硬件加速转码

- 检测 GPU 可用性，自动选择 NVENC 编码器
- 转码失败时自动降级为软件编码
- 添加转码会话的硬件加速类型记录
```

❌ 错误（标题英文）
```
feat(transcoder): add NVENC hardware transcoding
```

### 1.4 禁止阶段性词语（强制）

提交按**功能点**描述，不按**开发阶段**描述。commit message（标题与正文）**禁止**出现阶段 / 批次性词语：`Phase 0`、`P0` / `P1` / `P2`、`MVP`、`Sprint`、`第一期` / `本次迭代` 等。

✅ 正确（描述功能点）
```
feat(player): 实现追播延迟自动调节
```

❌ 错误（用阶段词代替功能描述）
```
feat: 完成第一期核心转码功能
```

## 2. 文档入库边界（强制）

判据：**活文档（长期维护、是真源）入库；易朽稿（做完即弃）留 `.tmp/`。**

### 2.1 应当入库的耐久文档

- 产品 / 需求：`README.md`、`CHANGELOG.md`、`docs/PRD.md`、`docs/ROADMAP.md`（活文档，随需求变更同 PR 更新）。
- 架构：`docs/ARCHITECTURE.md`、`docs/adr/*.md`、`docs/API.md`。
- 协作治理：`docs/CONTRIBUTING.md`、`.claude/rules/*.md`。

### 2.2 严禁入库的易朽过程稿（已由 `.gitignore` 排除 `/.tmp/`）

- 实施计划 / 里程碑 / 临时路线图：`实施计划.md`、`PLAN.md`、临时 `roadmap.md` 等。正式产品路线图仅允许 `docs/ROADMAP.md`。
- 过程性报告：`IMPLEMENTATION.md`、`执行报告.md`、`分析.md`、`audit-*.md` 等。
- AI 助手过程性笔记、交流稿、思路记录。

## 3. 最小提交粒度（强制）

- **验证门通过才提交**：`git commit` 前，本次变更必须已过验证门（判据见 `testing-and-quality.md` §1）。**门未全绿、或完成被实测推翻时，不得提交**。
- **独立可编译**：每个 commit 落地后代码都能编译 / 构建通过，不留"半截"提交。
- **只做一件事**：一个 commit 只对应一个功能点 / 一个修复 / 一次重构，无关改动不混入。
- **不混类型**：不在同一 commit 里混 `feat` / `fix` / `refactor`——各自独立提交。
- **发版元数据单独提交**：`chore(release)` 只承载 VERSION/CHANGELOG/README 定稿，且**仅在**目标提交的 CI 已全绿、可发布提交已在 `main` 历史之后才允许（纪律见 `release-discipline.md`）。**禁止**用 release commit 试 CI。

✅ 正确（拆成独立、各自可编译、各做一件事）
```
feat(library): 媒体库支持多目录聚合管理
fix(transcoder): 修复 FFmpeg 进程僵尸问题
refactor(player): 提取 MSE 缓冲区控制为独立模块
```

❌ 错误（一个 commit 混了功能 + 修复 + 重构）
```
feat: 加多目录管理，顺便修个僵尸进程并重构播放器
```

## 4. 其他约束

- 禁止跳过 hooks（`--no-verify`）。禁止对已 push 的提交 `--amend`。
- 提交前确认未包含 `.env` / 凭据 / 大型二进制。

## 5. 禁止擅自开 PR（强制）

- **默认禁止**执行 `gh pr create` / `gh pr create ...` / 任何创建 Pull Request 的 API。
- **仅当**用户在**当前对话**中明确说出「开 PR」「创建 PR」「提 PR」等授权时，才允许创建。
- 用户说「提交 / 推送 / 发版 / 修 CI / 撤回」**不等于**授权开 PR。
- 合 `main` 若被分支保护挡住：向用户报告阻塞与可选方案，**等待指示**；不得自行开 PR「绕路」。
- 已误开的 PR：在用户要求关闭时立即 `gh pr close`，不再追加。
