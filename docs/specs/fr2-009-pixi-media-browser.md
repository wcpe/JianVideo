# 功能规格：PixiJS 高密度媒体浏览器

> 状态：候选发布@v0.25.0-rc.5　·　关联 PRD：FR2-009　·　阶段：P4 `0.25.x`　·　前置：[fr2-003](fr2-003-performance-budget.md)、[fr2-063](fr2-063-pixijs-prototype-benchmark.md)、[ADR-0053](../adr/0053-video-ai-pixijs-space-baseline.md)

## 1. 背景与目标

P1 已在 `packages/render-pixi` 交付网格/时间轴原型、纹理池与 Benchmark harness；生产 `BrowsePage`/`TimelinePage` 仍以 DOM 资源管理器为主。P4 要把 PixiJS 做成**正式媒体浏览主体验**，在真实索引与 Space 边界上完成 100 万级滚动验收。

目标：

- 生产路径提供 Pixi 网格与时间轴两种主视图，支持筛选、拖拽定位、预览浮层。
- React 只做壳层、工具栏、详情/表单浮层；滚动热区不随每帧重渲染。
- 数据窗口化：前端只持有可见窗口 + overscan + 选中态，筛选/排序走后端。
- 附 `media-ui-target-1m` Benchmark 报告，对照 [fr2-003](fr2-003-performance-budget.md) §3。

## 2. 范围

### 2.1 范围内

- 在 `packages/render-pixi` 扩展生产级网格/时间轴渲染：可见窗口、纹理池、过期缩略图取消、hover/选中 HLS 预览限流。
- 在 `frontend` 增加/改造媒体浏览器页（可复用 Browse/Timeline 路由壳），挂载 Pixi 热区并接真实 `GET /api/library/media` 分页/游标。
- 多选、打开详情、跳转播放与现有批量/详情能力衔接（FR2-053 / FR2-032）。
- 100 万 mock 与真实库小样本均可滚动；输出 Benchmark 到 `.tmp/`。

### 2.2 范围外

- 图片高级编辑（FR2-038）、视频粗剪（FR2-039）。
- 批量转码/移动业务语义扩展（FR2-053 单独规格）。
- AI 搜索、向量检索（P6）。
- Desktop/移动原生端（P7 复用 player-core/render-pixi）。

## 3. 设计

- **壳层**：工具栏（视图切换、排序、筛选入口）、状态栏、详情 Drawer/Modal 仍 React。
- **热区**：`render-pixi` 接收 `items[window]`、`scroll`、`selection`；纹理 key 用 `media_id + tier`；滚动时只更新 Pixi stage。
- **数据**：优先游标分页；筛选变更重置窗口；请求 generation 防止迟到回填。
- **预览**：仅 hover/选中请求缩略图或 HLS 轻量预览；快速滚动丢弃过期结果。
- **性能**：对象数随可视窗口增长，不随库规模线性增长；对照 fr2-003 帧耗时/长任务/纹理上限。

## 4. 任务拆分

- [x] 规格与验收标准冻结（本文）。
- [x] `render-pixi` 生产网格 API（数据绑定、选中、滚动、销毁）；时间轴热区仍复用现有 TimelineView DOM 虚拟化。
- [x] 前端 `/media-grid` 接入真实 API + 筛选状态。
- [x] 多选/详情/播放跳转联调。
- [x] 1m Benchmark 报告与回归测试（窗口化仿真 + 单元测试）。
- [x] 文档：PRD 状态、CHANGELOG。

## 5. 验收标准

- 生产浏览器可在网格/时间轴间切换，连续滚动不白屏、不卡死。
- `media-ui-target-1m` 滚动报告满足 fr2-003 §3：p95 ≤ 16.7ms、p99 ≤ 33ms，10s 内无 >200ms 长任务（本机环境标注）。
- 筛选/排序后只请求后端分页结果，前端不全量数组过滤。
- hover/选中才触发预览请求；快速滚动后无错误堆积。
- 点击项可打开详情或进入播放页；卸载销毁 Pixi 与纹理无泄漏（单测/手工核对）。

## 6. 风险

- 真实缩略图 IO 与 mock 差异大，需限流与占位策略。
- SQLite 大库分页若超 fr2-003 后端门，需另开索引 ADR，不阻塞前端窗口化。
