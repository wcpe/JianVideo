# 功能规格：媒体健康巡检（FR-73）

> 状态：开发中　·　关联 PRD：FR-73　·　分支：feature/fr-73-health

## 1. 背景与目标

媒体库长期运行后，部分媒体可能出现：源文件被外部删除、文件被截断成 0 字节、视频损坏（ffprobe 无法解析）、缩略图无法生成。这些"问题媒体"散落在库中，用户难以集中发现与清理。

FR-73 提供一个**只读巡检**：后台扫描全部未软删媒体，逐项判定健康状态，把问题汇总为可浏览的清单。属 P7，单用户、本地能力，不引入新重型件。

## 2. 需求（要什么）

- 后台巡检全部**未软删**媒体，逐项判定四类问题：
  - `zero_byte`：`file_size == 0`
  - `missing`：本地文件 `os.Stat` 不存在（排除 `smb://` 远程路径，不误判）
  - `broken`：视频经 ffprobe 探测失败（容器/流无法解析）
  - `no_thumbnail`：缩略图缺失且无法生成
- 巡检结果落入**独立问题表** `media_health_issues`，每轮巡检**先清空再写入**（报告即当轮快照）。
- 巡检走后台（不同步阻塞请求），提供触发端点、进度查询、问题清单查询。
- 前端「问题媒体」页：触发巡检（带进度）+ 按问题类型分组列出问题媒体，可对问题项批量删除进回收站（复用 FR-69）。
- 范围内：只读巡检 + 清单展示 + 复用回收站删除。
- 不做（范围外）：自动修复 / 重新生成缩略图 / 自动删除；不改 `media_files` 任何列；**绝不改 `deleted_at`**（软删真源归 FR-25/27）；不复用 `scan_tasks` 队列表。

## 3. 设计（怎么做）

**新机制，写 ADR-0038**（只读巡检、独立问题表、不复用软删真源）。详见 `docs/adr/0038-media-health-inspection.md`，此处不重复决策正文。

- **数据模型**：新增 `media_health_issues` 表（`internal/db/models/media_health_issue.go`），`main.go` AutoMigrate 列表追加一行。不在 `media_files` 加列。
- **巡检执行（`internal/library/health.go`）**：
  - 独立后台 goroutine + 进度状态单例（`HealthScanStatus`，参照 `scan_status.go`），不复用 `TaskQueue`——巡检是单次全局操作、每轮清空重写问题表，语义与「按库排队的扫描任务」不同，复用反而要在队列里塞特例。
  - 单飞：已有巡检在跑时再次触发直接返回，不并发两轮。
  - 纯判定函数无 IO 副作用、可穷举单测：`classifyMediaIssues(mf, statFn, probeFn, thumbFn)` 返回问题类型列表。
  - 视频健康探测**新增** `ProbeVideoHealth(path) error`：跑 ffprobe 判可解析性，**不改动** `probeVideoMetadata` 的静默吞错行为（那是元数据降级链，返回 ok/err 是两种语义）。
  - 缩略图健康判定：`FindThumbnailPath` 不存在则尝试**同步**生成一次（捕获 error），仍失败记 `no_thumbnail`。新增同步生成入口 `TryGenerateThumbnail(path) error`，不动既有异步 `GenerateThumbnail`。
- **端点（`internal/api`）**：
  - `POST /api/library/health/scan`：触发后台巡检，返回 `{"status":"scanning"}` 或已在跑时 `{"status":"already_running"}`。
  - `GET /api/library/health/status`：返回巡检进度（idle/scanning/completed/error + total/checked + issue_count）。
  - `GET /api/library/health/issues`：返回问题清单（按问题类型分组，每项含媒体基本信息 + issue_type + detail）。
  - 删除复用既有 `POST /api/library/media/batch-delete`（FR-69），不新增删除端点。
- **前端**：新页 `InspectPage`（路由 `/inspect` + 导航项「巡检」），`api/health.ts` 客户端，`types` 加 `HealthIssue`/`HealthScanStatus`。

## 4. 任务拆分

- [x] 后端：`MediaHealthIssue` 模型 + `main.go` AutoMigrate 追加
- [x] 后端：`ProbeVideoHealth` / `TryGenerateThumbnail`（不动原静默逻辑）
- [x] 后端：`HealthScanStatus` 单例 + `classifyMediaIssues` 纯函数 + 巡检后台 goroutine（单飞、清空重写）
- [x] 后端：三个 health 端点 + 路由注册
- [x] 后端单测：各判定（0 字节/缺失/ffprobe 失败/缩略图）、巡检只读不改 deleted_at、单飞
- [x] 前端：`api/health.ts` + `types` + `InspectPage` + 路由 + 导航项
- [x] 前端 vitest：页面渲染/触发/进度/分组列表/删除
- [x] 文档同步：PRD 状态、ARCHITECTURE §3 表、API、CHANGELOG、ADR-0038

## 5. 验收标准

- 后端：构造 0 字节 / 缺失文件 / ffprobe 失败 / 缩略图失败样本，`classifyMediaIssues` 各判定正确；巡检写入问题表后 `media_files.deleted_at` 不变（断言）；重复触发不并发。
- 前端：`InspectPage` 渲染、触发巡检、进度展示、按类型分组列出问题、批量删除调用 batch-delete，vitest 全绿。
- 完成判据：前端 `tsc --noEmit` + `vitest run` + `npm run build` 全绿；后端 `go build ./...` + `go vet` + 受影响包 `go test` 全绿。
- 真机维度：真实损坏样本的 ffprobe 失败、HEIC 缩略图生成失败属真机验证，标「待真机验」。

## 6. 风险 / 待定

- 大库巡检逐项 ffprobe 开销大：本期单 worker 串行、逐项超时（复用 probe 10s 超时），不做并发优化（YAGNI，P7 不镀金）。
- 缩略图同步生成会阻塞巡检 goroutine：已限单 worker、单次超时，可接受。
