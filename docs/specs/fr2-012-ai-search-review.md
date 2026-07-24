# 功能规格：AI 搜索、去重与审核流

> 状态：开发中（首切+二切：向量搜索/OCR/去重候选/确认驳回 API + 设置开关 + 重复项 AI Tab + 详情审核区已落地；模型列表/人脸等后置）　·　关联 PRD：FR2-012　·　阶段：P6 `0.27.x`　·　前置：[fr2-011](fr2-011-ai-pipeline.md) · [fr2-061](fr2-061-file-hash-dedup.md) · ADR：[0059](../adr/0059-ai-pipeline-vector-index.md)

## 0. 切片范围

| 切片 | 内容 | 首切 |
|------|------|------|
| A | 嵌入式向量索引隔离点（sqlite-vec 或本地向量文件，ADR-0059）；`ai_embeddings` 表/存储 + repository | ✅ |
| B | embedding 任务：`task_type=embedding` 经 FR2-011 管线入队；写入向量；按 Space 隔离 | ✅ |
| C | 语义搜索 API：`q` + Space + 可选 media 类型；返回相似度排序列表 | ✅ |
| D | **一种**结构化能力走通（默认 **OCR** 或 **对象/场景** 二选一，实现时锁一种 stub+可选真实后端） | ✅ |
| E | AI 去重：基于 embedding 相似度候选 + 与 FR2-061 哈希去重结果并列展示（不替代哈希） | ✅ API + 重复项「AI 相似」Tab |
| F | 审核流：结果列表、人工确认/驳回（写 `manual`）、批量操作与审计 | ✅ confirm/reject API + 详情面板审核区；批量后置 |
| G | 人脸、视频理解完整产品化、多模型热切换 UI | 后置 / 可分期 |

**首切建议**：向量检索 + 一种可测的结构化 task_type + 默认关闭仍生效；人脸/审核 UI 后置。

## 1. 背景与目标

在 FR2-011 管线之上交付 **可搜索、可重建、可人工纠偏** 的 AI 能力。向量与 AI 中间结果是可重建数据；人工确认优先。未配置模型/向量时整体关闭（与 011 同一门）。

## 2. 需求（要什么）

### 2.1 范围内

- **向量索引（嵌入式）**：
  - 隔离在数据层扩展点；上层只见 `UpsertEmbedding` / `SearchSimilar` / `DeleteByMedia|Batch`。
  - 每条向量绑定：`space_id`、`media_id`、`model_id`、`model_version`、`dim`、`batch_id`。
  - 可整批删除重建；换 embedding 模型必须换 version 并允许旧向量失效策略（首切：同 media 同 model 覆盖）。
- **语义搜索**：
  - `GET/POST /api/ai/search`：`q` 文本（首切文本 query → embedding → topK）；过滤当前用户可访问 Space。
  - 返回 media 摘要 + score；不返回跨 Space 命中。
- **结构化 AI 能力（首切一种）**：
  - 经 `POST /api/ai/infer` 指定 `task_type`；结果进 `ai_results.payload_json`。
  - 首切锁定一种：`ocr` **或** `object_scene`（实现 PR 写死选择并在 CHANGELOG 说明）。
  - stub 实现必须可单测；真实二进制为可选依赖，缺省时 stub/跳过集成测。
- **AI 去重（二切）**：
  - 相似度阈值可配；输出「疑似重复组」；与 FR2-061 精确/感知哈希结果分开展示，禁止自动删文件。
- **审核流（二切）**：
  - 列表：待审 / 已确认 / 已驳回；单条确认写 `manual=true` 与审计。
  - viewer 只读；确认/驳回 ≥ editor。
- **可见性**：结果与向量一律 Space 隔离；匿名分享不触发 AI 任务（呼应 FR2-010/055）。

### 2.2 不做

- 外部向量库（Milvus/Qdrant 等）。
- 自动删除原媒体或自动改可信元数据。
- 强制捆绑某一商业模型。
- 500 万～1000 万向量压测不达标时静默换外部库（须另写 ADR）。
- 完整人脸库身份管理产品（可后置最小 face detection-only）。

## 3. 设计（怎么做）

- 依赖 FR2-011：`ai_enabled`、模型/节点、结果表、`ai.infer` worker。
- 向量实现候选（首切二选一，写进实现 PR）：
  1. **sqlite-vec**（扩展隔离点 + 标注 SQLite 独有）；
  2. **本地向量文件 + 内存/磁盘索引**（无扩展时的兜底）。
  - 选型证据：单测 + 小规模检索正确性；大规模性能见 FR2-003 预算，不达标再 ADR。
- embedding 模型：首切允许 stub 固定维随机/哈希向量（仅测通路）；真实模型作为可选配置。
- 搜索路径：query → embed（可缓存短 query）→ SearchSimilar → 拼 media 元数据（经 library repository，尊重家长分级 FR2-051）。
- 去重：cosine/L2 阈值；组内保留「主条目」策略仅作展示建议。
- 审核：复用 `ai_results.manual`；可选 `review_status` 字段若 manual 语义不够再加迁移。

## 4. 任务拆分

- [x] 向量存储隔离接口 + 一种嵌入式实现（`ai_embeddings` BLOB + 余弦）
- [x] embedding 入队/worker + Upsert/Delete
- [x] 语义搜索 API + Space 过滤
- [x] 一种结构化 task_type：stub **OCR**
- [x] 单测：关闭门、跨 Space 不可搜、重建删向量、stub 通路
- [x] 文档：ARCHITECTURE / PRD / CHANGELOG / 本 spec
- [x] 二切（后端）：AI 去重候选 `GET /api/ai/duplicates`（余弦阈值，默认 0.92；不删文件）
- [x] 二切（后端）：审核 `POST /api/ai/results/:id/confirm|reject`（confirm→`manual=true`；reject 仅非 manual；审计）
- [x] 二切（前端）：设置页 `ai_enabled` 开关（随 FR2-011）
- [x] 二切（前端）：去重候选「AI 相似」Tab + 详情面板确认/驳回
- [ ] 后置：人脸 / 视频理解 / 多模型切换；审核列表筛选与批量操作

## 5. 验收标准

- `ai_enabled=false` 时搜索与 infer 均不可用。
- 同 Space 写入 embedding 后，语义搜索能命中该 media（stub 可用固定向量构造「必中」用例）。
- 其他 Space 的 media 永不出现在结果中。
- rebuild 删除非 manual 结果与对应向量后，搜索不再命中，直到重新入队。
- 结构化 task_type 至少有 stub 单测证明结果落库可查。
- 不引入 Redis/外部向量中间件依赖进 `go.mod` 业务必选路径。

## 6. 风险 / 待定

- sqlite-vec 与 CGO/部署镜像兼容性：若阻塞，首切改本地向量文件方案。
- 中文 OCR 与视频抽帧成本：首切可用图片 media 或单帧 stub。
- 相似度阈值默认值需可配置，避免误报刷屏。
- 与 FR2-061 去重 UI 如何并列：二切时统一「重复与相似」入口文案。
