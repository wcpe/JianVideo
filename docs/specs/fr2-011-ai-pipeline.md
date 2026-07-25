# 功能规格：AI 可替换管线

> 状态：开发中（首切骨架 + 设置页总开关 + 模型/节点列表与启用已落地）　·　关联 PRD：FR2-011　·　阶段：P6 `0.27.x`　·　前置：[fr2-037](fr2-037-task-queue-center.md) · ADR：[0059](../adr/0059-ai-pipeline-vector-index.md) / [0055](../adr/0055-task-queue-persistence.md) / [0056](../adr/0056-space-permission-model.md) / [0058](../adr/0058-data-layer-abstraction.md)

## 0. 切片范围

| 切片 | 内容 | 首切 |
|------|------|------|
| A | Schema：`ai_models` / `ai_inference_nodes` / `ai_results` + 迁移；repository 接口 | ✅ |
| B | 模型注册与推理节点接口契约；`ai.*` 任务类型注册到通用队列（ADR-0055） | ✅ |
| C | **AI 默认关闭门**：未配置模型/节点/显式启用时 API 与 worker 整体拒绝 | ✅ |
| D | 结果表写入 + 审计元数据（model/version/Space/批次）；重建入口（整批删可重建结果） | ✅ |
| E | 单测：默认关闭；启用后入队；结果按 Space 隔离；重建不覆盖 `manual=true` | ✅ |
| F | 设置页：AI 总开关、模型/节点列表只读与启用 | ✅ |
| G | 具体模型运行时绑定（人脸/OCR 等） | 不做（归 FR2-012 首切） |

**首切建议**：只搭可替换管线骨架与默认关闭门；不绑死任何具体模型实现。

## 1. 背景与目标

P6 要把 AI 能力放进主线，但必须可替换、可审计、默认可关。ADR-0059 已锁契约：模型注册表、推理节点接口、结果表、嵌入式向量、重建策略、默认关闭。本 spec 落地 **FR2-011 管线骨架**，让后续 FR2-012 各能力只注册 task-type + worker，不各自造队列或写死模型。

与 **FR2-031 离线影视推断** 区分：031 是本地规则解析、无模型；本管线是可选 AI 推理路径，默认关闭。

## 2. 需求（要什么）

### 2.1 范围内

- **模型注册表**（`ai_models`）：
  - 主键维度：`id`、`name`、`version`、`task_type`（`face` / `ocr` / `object_scene` / `video_understanding` / `embedding`）、`status`（`available` / `disabled`）。
  - 不存权重文件本身；可记本地路径或 endpoint 引用（实现细节可配置）。
- **推理节点**（`ai_inference_nodes`）：
  - `id`、`name`、`kind`（`local` / `self_hosted`）、`endpoint`（可空）、`enabled`、能力声明（支持的 task_type 列表）。
  - 业务只依赖接口：`Infer(ctx, request) (result, error)`；禁止业务层 import 具体运行时 SDK。
- **任务类型**：在通用队列注册 `ai.infer`（及按需细分 `ai.face` 等，首切可用统一 `ai.infer` + payload.task_type）。
  - 入队立即返回 task id；请求线程不跑推理。
  - payload 至少含：`space_id`、`media_id`（或资源范围）、`task_type`、`model_id`、`node_id`、可选输入范围。
- **结果表**（`ai_results`）：
  - `id`、`space_id`、`media_id`、`task_type`、`model_id`、`model_version`、`node_id`、`batch_id`、`payload_json`（结构化摘要）、`manual`（人工确认/纠正）、`created_at` / `updated_at`。
  - 经 repository 访问；按 Space 行级隔离。
- **默认关闭门**（NFR）：
  - 全局设置：`ai_enabled`（默认 `false`）。
  - 未启用 **或** 无可用 model+node 时：所有 AI 入队 API → `503 AI_DISABLED`（或 `403`，口径固定一种并写 API.md）；worker 即使被误唤醒也 no-op。
- **重建策略**：
  - `POST` 管理接口：按 Space / media / task_type / batch 删除 **非 manual** 的 `ai_results`（及后续向量，见 FR2-012）。
  - 重建 = 删可重建结果 + 再入队；**不得覆盖 `manual=true`**。
- **审计**：`ai.model.*` / `ai.infer.enqueued|succeeded|failed` / `ai.results.rebuilt` 等事件带 `space_id`。
- **权限**：viewer 可读本 Space 已确认/可见结果；入队与重建 ≥ editor；模型/节点管理 ≥ owner（与 Space 角色矩阵对齐）。

### 2.2 不做

- 具体人脸/OCR/视频理解算法与权重下载（FR2-012）。
- 外部向量库 / 外部消息队列（ADR-0059 / 0055 禁止）。
- 请求线程同步推理。
- 联网商业 AI SaaS 作为 1.0 必选依赖。
- 把 AI 结果当可信源覆盖人工元数据。

## 3. 设计（怎么做）

- 新包建议：`apps/server/internal/ai`（或 `internal/aipipeline`）：registry、node 接口、result repository、enablement 门、与 `tasks` 的 worker 注册。
- 数据层：GORM 实体 + repository；SQLite 独有扩展（若有）隔离标注（ADR-0058）。
- 队列：复用 `internal/tasks`，不新建队列表。
- API（示意，真源落 `docs/API.md` / OpenAPI）：
  - `GET /api/ai/status` → `{ enabled, models, nodes }`
  - `GET /api/ai/models`、`GET /api/ai/nodes`（owner/editor 可读配置态）
  - `POST /api/ai/infer` body：`media_id`、`task_type`、可选 `model_id` → task id
  - `GET /api/ai/results?media_id=` Space 过滤
  - `POST /api/ai/results/rebuild` 管理重建
- 设置键：`ai_enabled`；可选后续 `ai_disabled_spaces`（二切）。
- 与 FR2-031：推断路径保持独立；UI 展示优先级仍为人工 > display_name > 高置信规则推断 > 文件名；AI 结果另面板展示，不自动改 display_name（除非产品另开）。

## 4. 任务拆分

- [x] 迁移：`ai_models` / `ai_inference_nodes` / `ai_results`
- [x] repository + 默认关闭门（settings + 无模型时拒绝）
- [x] 节点接口 + 内存/stub 节点（单测用）
- [x] `ai.infer` worker 注册与入队 API
- [x] 结果写入、Space 过滤、manual 保护
- [x] 重建 API + 审计
- [x] 单测矩阵（见 §5）
- [x] 文档：API / ARCHITECTURE / PRD 状态 / CHANGELOG（API 摘要见 CHANGELOG + 本 spec）
- [x] 二切：设置页 AI 总开关（`ai_enabled`）
- [x] 后置：设置页模型/节点只读列表与启用 UI（`PUT /api/ai/models/:id/status`、`PUT /api/ai/nodes/:id/enabled`）

## 5. 验收标准

- `ai_enabled=false` 时 `POST /api/ai/infer` 失败且不创建任务行。
- 启用且注册 stub 模型/节点后，入队返回 task id；成功后 `ai_results` 可按 media 查到，且跨 Space 不可见。
- 对 `manual=true` 结果执行 rebuild 不删除该行。
- 进程重启后队列中的 `ai.infer` 按通用队列策略恢复，不丢单。
- 无具体外部模型二进制时，仅用 stub 即可跑通 CI。

## 6. 风险 / 待定

- 任务类型粒度：首切统一 `ai.infer` vs 按能力拆分——首切统一，payload 带 `task_type`。
- 模型文件分发与校验：二期；首切允许 endpoint/path 配置。
- 与 FR2-012 向量表的外键/batch_id 对齐：在 012 中引用同一 `batch_id` 语义。
