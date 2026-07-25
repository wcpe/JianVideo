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
- **禁止** force-push `main` / 重写已 push 的发版历史来「修」错误发版；错误发版用 `git revert` + 删错误 tag（若尚未公开可用 Release）+ PR 回流。

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
- 合入 `main` 的唯一合法路径：`dev` → PR → checks 绿 → review → merge（或仓库已文档化的受控提升，且仍不绕过保护）。
- 若 PR 被保护挡住：修 checks / 补 review，**不要**改保护规则。

## 4. 错误发版的撤回

若已误推 `chore(release)` 或误打 tag：

1. **立刻** `git push origin :refs/tags/vX.Y.Z` 删除错误 tag（若 Release 已公开，按运维流程处理，禁止覆盖同名 tag）。
2. 用 `git revert` 撤回 release 元数据 commit（恢复 `VERSION`/CHANGELOG/README），**不** `reset --hard` 已 push 历史。
3. 经 **PR** 把 revert 合入 `main`（仍走保护）。
4. 取消仍在跑的错误 `release.yml` run（若可取消）。
5. 回到 §1：普通 commit 修绿 → 再发版。

## 5. 与既有文档的关系

- 分支模型与 tag 触发：`docs/CONTRIBUTING.md` §8、`docs/specs/fr2-014-rc-ga-release-gates.md`、ADR-0064/0065。
- 验证门：`testing-and-quality.md`。
- 提交信息：`git-commit.md`。
- 本文件补充的是 **操作纪律红线**；与上述冲突时，以「先绿后发、不碰保护」为准。
