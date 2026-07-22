# 功能规格：全局搜索与多维筛选排序

> 状态：已交付@v0.25.0　·　关联 PRD：FR2-046　·　阶段：P4 `0.25.x`　·　前置：FR-35/36 列表筛选、FR2-007 索引

## 1. 背景与目标

现有 `GET /api/library/media` 已支持表达式搜索、类型/大小/时间/路径/标签/收藏/推断筛选与基础排序。P4 要求补齐 **片名（推断标题）、时长、分辨率** 等维度，并保证大库分页与后端索引协同，前端不做全量过滤。

## 2. 范围

### 2.1 范围内

- 后端 `MediaFilter` 扩展：
  - `duration_min` / `duration_max`（秒）
  - `width_min` / `height_min`（分辨率下界；可选 `width_max`/`height_max`）
  - 搜索裸词/表达式覆盖推断标题（`media_inferences.title` 或等价 join）
  - 排序：`duration` / `duration_asc` / `resolution`（按 width*height 或 height）可选
- API 查询参数与 `docs/API.md` 同步。
- 前端 `MediaQueryFilters` / Browse / Timeline：时长与分辨率控件；标签筛选入口可见。
- 游标分页：不支持的新排序回退 offset 分页并文档标明。

### 2.2 范围外

- 向量语义搜索（P6）。
- 全文引擎替换 SQLite LIKE（除非 Benchmark 证明必须，另开 ADR）。

## 3. 设计

- `applyMediaFilter` 增加 duration/width/height 条件（列已在 `media_files`）。
- 推断标题：`Terms` 增加对 inference title 的 OR 子查询，或搜索时 left join 推断表。
- 前端控件：时长预设（≥1m/≥10m/≥1h）、分辨率预设（≥720p/≥1080p/≥4K）。
- 所有筛选保持参数化，防注入。

## 4. 任务拆分

- [x] 规格冻结（本文）。
- [x] 后端 filter/order + 单测。
- [x] API 文档与 handler 解析。
- [x] 前端筛选 UI（时长/分辨率控件 + 请求参数）。
- [x] CHANGELOG / PRD 状态。

## 5. 验收标准

- 按时长/分辨率筛选返回集合正确，分页 total 一致。
- 搜索能命中推断片名（有推断数据时）。
- 前端变更筛选只触发后端请求，不在客户端滤 100% 已加载以外的数据冒充全库结果。
- 非法数值参数 400 或安全忽略策略与单测一致（实现时二选一并写清）。

## 6. 风险

- 无索引列上的 duration/resolution 过滤在 5m/10m 可能偏慢；超限时补组合索引，不改变 API 语义。
