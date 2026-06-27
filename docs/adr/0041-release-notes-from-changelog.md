# ADR-0041：发布说明源自 CHANGELOG

## 状态
已接受

## 背景
ADR-0032 确立的发布体系中，正式发布（FR-47）此前用 `softprops/action-gh-release` 的 `generate_release_notes: true`——由 GitHub 从提交记录自动汇总 Release notes，与 `CHANGELOG.md`（项目入库的变更真源）脱节，二者内容可能不一致；提交粒度的自动列表也不如 CHANGELOG 人工整理的版本段可读。
预发布（FR-48）虽已从 CHANGELOG 抽取「自上个正式版以来的变更」，但其内联 awk 匹配的标题是 `## 未发布版本`，而 CHANGELOG 实际标题为 `## 未发布`（无「版本」二字），导致该段长期抽空、预发布正文显示「（暂无记录）」。
此外正式与预发布各自内联一份 awk 抽取逻辑，存在重复、且无法本地测试，「测的和跑的不是一套」。

## 决策
正式发布与预发布的 Release notes **统一来源于 `CHANGELOG.md`**，由共享脚本 `scripts/changelog-extract.sh` 抽取：
- 正式发布抽「## X.Y.Z（日期）」段（X.Y.Z 取自 VERSION 真源），作为 `body_path` 传给 action-gh-release，并去掉 `generate_release_notes`。
- 预发布抽「## 未发布」段（修正标题不匹配 bug）。
- 抽取逻辑收敛到单一脚本（带 fence 状态机：代码块围栏内的 `## ` 不误判为段落结束），两个 workflow 调用同一脚本；脚本由 `scripts/changelog-extract_test.sh` 单测覆盖，保证「本地测试绿 = CI 真能抽对」。

本决策**扩展、不取代** ADR-0032：发布触发方式、构建矩阵、分发与自更新机制均不变，仅改变 Release notes 的内容来源。

## 理由
- CHANGELOG 是入库的、人工整理的变更真源；以它为 Release 正文比提交自动汇总更准确、可读，且单一真源不再二处分叉。
- 抽取逻辑做成可被单测覆盖的独立脚本，避免内联 awk「测的和跑的不是一套」，并消除正式/预发布两处重复。
- 脚本以版本号 / `unreleased` 为参数、读 CHANGELOG 路径，纯文本处理无新增依赖，符合「简单优先」。

## 后果
- 正面：Release 正文与 CHANGELOG 一致且可读；预发布变更段恢复显示实际内容；抽取逻辑可本地回归。
- 负面 / 约束：CHANGELOG 标题格式（`## 未发布`、`## X.Y.Z（日期）`）成为发布流程的隐性契约，格式漂移会导致抽空——已由脚本测试与发版前 CHANGELOG 先行约束兜底（正式段抽空时 workflow 兜底文案）。
- 真机验收门控：CI 真实发布 / 预发布的正文正确性需推送线上后核验（同 ADR-0032 的真机门控）。

## 备选方案
- **保留 `generate_release_notes: true`**：正文与 CHANGELOG 分叉、可读性差，落选。
- **继续内联 awk**：无法本地测试、两处重复、已埋下标题不匹配 bug，落选。
