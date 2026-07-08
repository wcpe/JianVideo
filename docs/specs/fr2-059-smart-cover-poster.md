# 功能规格：智能封面/海报

> 状态：已审核接受　·　关联 PRD：FR2-059　·　阶段：P2 `0.23.x`　·　分支：待定

## 1. 背景与目标

当前视频只有普通缩略图抽帧，相册封面也不等同于媒体智能封面。P2 需要本地抽帧生成媒体封面/海报，并允许用户手动更换。封面属于可重建缓存数据，但人工选择必须作为库内元数据保留。

目标：

- 为视频/图片建立媒体封面模型。
- 本地生成封面候选，用户可手动选择已有候选帧；本规格不支持上传外部图片。
- 封面缓存登记到 FR2-048，清理后可重建。

前置依赖：FR2-007（Space/索引）、FR2-017（迁移框架）、FR2-037（任务队列）、FR2-040（审计核心）、FR2-048（缓存资产）、FR2-028（缩略图/抽帧基础）。

## 2. 需求（要什么）

- 自动封面：视频按多个时间点抽帧生成候选；图片默认使用自身或缩略图。
- 手动封面：用户从候选中选择，或重新生成候选。
- 封面字段记录当前封面来源、候选列表、是否人工选择。
- 生成走 FR2-037 任务队列。
- 产物登记为 `cache_assets(kind=cover)`。
- 列表、详情、播放页优先显示当前封面，缺失时回退缩略图。
- 范围内：封面 schema、候选生成、选择 API、缓存登记、基础 UI。
- 不做（范围外）：联网海报下载、AI 美学评分、图片编辑、外部海报库。

## 3. 设计（怎么做）

Schema：

- `media_covers`：`media_id`、`space_id`、`selected_asset_id`、`source`、`manual`、`updated_at`。
- `cover_candidates`：`id`、`media_id`、`asset_id`、`timestamp_seconds`、`score`、`created_at`。
- 人工选择语义不得只依赖可清理的 `selected_asset_id`；必须同时保存 `selected_source`、`selected_timestamp_seconds`、`manual` 和候选指纹。封面缓存被清理后，重建应能按这些字段恢复同一选择语义。

任务：

- `cover.generate`：生成候选并登记缓存资产。
- `cover.refresh`：清理旧候选后重建。

API：

- `GET /api/library/media/:id/covers`
- `POST /api/library/media/:id/covers/generate`
- `PUT /api/library/media/:id/cover`

前端：

- 详情页展示候选封面，支持选择当前封面。
- 列表组件优先取封面 URL，失败回退缩略图。

## 4. 任务拆分

- [ ] 定义封面与候选 schema。
- [ ] 实现本地抽帧候选生成任务。
- [ ] 接入缓存资产登记和清理重建。
- [ ] 新增封面查询/生成/选择 API。
- [ ] 前端详情页封面选择与列表回退展示。
- [ ] 接入审计事件。
- [ ] 补单元测试：候选时间点、选择逻辑、回退策略。
- [ ] 补集成测试：视频生成候选、选择、清理后重建。
- [ ] 补 E2E：详情页更换封面并列表同步。
- [ ] 文档同步：PRD 状态、ARCHITECTURE、API、CHANGELOG。

## 5. 验收标准

- 测试视频能生成多个本地封面候选。
- 用户选择封面后，列表和详情刷新仍显示选择结果。
- 清理封面缓存后可重新生成；人工选择的语义不丢失。
- 清理封面缓存后，`selected_asset_id` 可重建，但 `selected_source` / `selected_timestamp_seconds` / `manual` 等人工选择语义不得丢失。
- 封面生成和选择写审计事件。
- `go test`、集成测试、Playwright 封面 E2E、`pnpm run quality` 全绿。
- Go 单二进制 serve 后，测试视频生成并更换封面实跑通过。

## 6. 风险 / 待定

- 已确认：本规格只支持本地抽帧候选，不支持用户上传外部图片作为封面。
- “智能”本期限定为规则化抽帧候选，不引入 AI 评分。
