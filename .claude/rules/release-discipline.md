# 发布流程纪律（强制）

> 适用于本仓库所有发版、打 tag、推 `main`、改分支保护的操作。
> 目标：防止「用 release commit 试 CI」「为合 main 放开保护」再次发生。

## 1. 发版前置条件（强制顺序）

**必须按这个顺序，禁止跳步：**

1. **普通功能/修复 commit** 合入 `dev`（`feat`/`fix`/`refactor`/`test`/`ci` 等，**不是** `chore(release)`）。
2. **`dev` 实验构建（或等价四项质量门）全绿**，有 run URL 与 conclusion=success 证据。
3. **经 PR 把可发布提交提升到 `main`**（走分支保护：必需 status checks + PR reviews）。
4. **`main` 上该提交的质量门也绿**（push `main` 触发的质量门 conclusion=success）。
5. **仅在以上全绿之后**，才允许独立的 `chore(release): 发布 X.Y.Z`（VERSION / CHANGELOG 定稿 / README 版本行）。
6. **release commit 已在 `main` 历史**后，才打并推送 `vX.Y.Z` tag 触发 `release.yml`。

### 1.1 明确禁止

- **禁止**用 `chore(release): 发布 …` 去「测 CI / 试发版」。
- **禁止**在质量门未绿时 bump `VERSION`、把 CHANGELOG「未发布」收成正式版本段、打正式 tag。
- **禁止**在 `dev` 上打正式 tag 却不在 `origin/main` 历史（`release.yml` 会拒）。
- **禁止**为了推 `main` / 发版去改、临时关闭、绕过分支保护（含 `enforce_admins`、required reviews、required status checks）。
- **禁止擅自开 PR**：未经用户在当前对话明确说「开 PR / 创建 PR」，不得 `gh pr create`（详见 `git-commit.md` §5）。「发版 / 推 main / 撤回」不构成开 PR 授权。
- 错误发版的处理以用户当轮指示为准；**不得**默认走 Revert+PR。未获指示时只报告现状与可选方案并停下。

## 2. 正确的「测 CI」方式

要验证某次改动是否绿：

1. 用**普通 commit**（描述真实改动）push 到 `dev`（或开 PR）。
2. 盯 `实验构建` / `质量门` run，等 conclusion。
3. 红了 → 再开 **fix/ci 或 fix 业务** 的普通 commit 修，再等绿。
4. **整条链路绿了**，再单独做 release 元数据 commit。

把 release 元数据混进「还在修 CI」的提交，会污染版本真源，并触发错误的正式发布流水线。

## 3. `main` 分支保护（不可妥协）

- `main` 是唯一发布分支，**受保护**：必需 PR、必需四项 status checks、`enforce_admins` 开启。
- **任何会话 / 代理 / 脚本不得**调用 GitHub API 修改 `branches/main/protection` 来「临时解禁」。
- 合入 `main` 的路径以仓库保护与用户当轮指示为准；代理**不得**为合 main 自行开 PR 或改保护。
- 若推 `main` 被保护挡住：报告阻塞，列出可选方案（用户自己开 PR / 用户授权开 PR / 用户改流程），**等待指示**，不自作主张。

## 4. 错误发版

以用户当轮明确指示为准。常见选项仅供报告，不自动执行：

- 删错误 tag（若尚未公开 Release）
- 历史改写 / force-with-lease（需用户明确授权）
- Revert（仅当用户明确要求）

**默认禁止**：未授权就 Revert、未授权就开 PR、未授权就改保护。

## 5. 与既有文档的关系

- 分支模型与 tag 触发：`docs/CONTRIBUTING.md` §8、`docs/specs/fr2-014-rc-ga-release-gates.md`、ADR-0064/0065。
- 验证门：`testing-and-quality.md`。
- 提交信息与禁开 PR：`git-commit.md`。
- 本文件补充的是 **操作纪律红线**；与上述冲突时，以「先绿后发、不碰保护、不擅自开 PR」为准。
