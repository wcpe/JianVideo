# 功能规格：转码预设与预生成队列（FR-77）

> 状态：开发中　·　关联 PRD：FR-77　·　分支：feature/fr-77-presets

## 1. 背景与目标

当前转码均为按需触发：首次播放某媒体时才同步切片（`PreSliceWithCodec`），首播需等待 ffmpeg 切片完成，体验有冷启动延迟。本功能（P7）让用户：

- 自定义「转码预设」（目标编码 + 目标分辨率）作为可复用模板；
- 手动把媒体「加入预生成队列」，后台串行预转码、产出切片缓存到 `hlsDir/{mediaID}/`，从而预热首播。

属 PRD 第七期（P7），复用 FR-49~53 已建的转码管线与 FR-29 已建的任务队列范式。

## 2. 需求（要什么）

范围内：

- 转码预设 CRUD：name + codec（h264/h265/av1/vp9）+ width（0=源宽）+ height（0=源高）。
- 预生成任务：把「某媒体 + 某预设」入队，单 worker 串行执行，状态持久化，重启恢复。
- 预生成执行：按预设 codec 调 transcoder 的 `PreSliceWithCodec` 同步切片（已存在切片则复用），预热首播。
- 端点：预设 CRUD + 入队 + 列任务。
- 前端：转码预设管理页（列/建/改/删 + 任务列表）+ 播放页「加入预生成」入口。

不做（范围外，YAGNI）：

- 预设的 description / enabled / bitrate / 多码率档位字段。
- 批量笛卡尔积（媒体 × 预设）入队、任务 retry、auto-generate、settings 全局开关。
- **分辨率缩放进 ffmpeg 管线**：现有 `PreSliceWithCodec` 不支持任意分辨率缩放（TS 路径按源分辨率选码率档位、fMP4 路径不缩放）。本期预设的 width/height 仅作为预设定义元数据落库与展示，预生成执行只按 codec 预热，不改转码管线参数。需要真正缩放是更大的转码改动，另立 FR。

## 3. 设计（怎么做）

### 3.1 数据模型（新表，main.go AutoMigrate 追加）

- `TranscodePreset`：id / name / codec / width / height / created_at / updated_at。
- `TranscodeTask`：id / media_id / preset_id / codec / width / height / status(pending/running/completed/error) / error / created_at / started_at / completed_at。

入队时把预设的 codec/width/height 快照进 TranscodeTask，任务不强依赖预设后续是否被改/删（与 ScanTask 把 scan_type 落到任务一致）。

### 3.2 预生成队列（复用 task_queue 范式）

复用 `internal/library/task_queue.go` 的范式（单 worker 串行 + SQLite 持久化 + RecoverRunning 重启恢复 + 内存映射传执行目标），新建 `internal/transcoder/pregen_queue.go`。放 transcoder 包以保持依赖单向（exec 直接调本包的 `PreSliceWithCodec`，无需反向依赖 library）。

exec 函数签名经注入（便于单测替身），生产实现把媒体按 preset codec 调 `PreSliceWithCodec` 产出切片。执行目标（媒体 inputPath/width/height、hlsDir、hlsMgr）经入队内存映射传递，重启恢复时按 media_id 反查 media_files 重建。

详见 [ADR-0039](../adr/0039-transcode-pregeneration-queue.md)。

### 3.3 端点（internal/api，依赖 web→api→transcoder 单向）

- `GET /api/transcode/presets`、`POST /api/transcode/presets`、`PUT /api/transcode/presets/:id`、`DELETE /api/transcode/presets/:id`
- `POST /api/transcode/tasks` {media_id, preset_id} → 入队
- `GET /api/transcode/tasks?status=` → 列任务

### 3.4 前端

- 新页 `/transcode`（TranscodePage）+ AppLayout navItems（图标 IconMovie）：预设列/建/改/删 + 任务列表。
- 播放页 `/play/:id` 新增「加入预生成」按钮：选预设后 `POST /api/transcode/tasks`。

## 4. 任务拆分

- [ ] 数据模型 `TranscodePreset` / `TranscodeTask`（含状态常量）+ main.go AutoMigrate
- [ ] 预设存储/校验（CRUD + codec 白名单校验）
- [ ] 预生成队列（pregen_queue：入队/串行/状态流转/RecoverRunning）+ main.go 接线
- [ ] 端点（预设 CRUD + 入队 + 列任务）+ 路由注册
- [ ] 前端 API 客户端 + 类型 + mock
- [ ] 前端转码预设页 + 播放页「加入预生成」入口 + navItems
- [ ] 文档同步：PRD 状态、ARCHITECTURE §3、API.md、CHANGELOG、ADR-0039

## 5. 验收标准

- 后端：预设 CRUD（建/列/改/删，非法 codec 拒绝）；队列入队后 pending→running→completed 状态流转；预生成 exec 按 preset codec 调 `PreSliceWithCodec`（mock 断言）；RecoverRunning 重置残留 running 重新执行；受影响包 `go test` 全绿、`go vet` 干净。
- 前端：预设页 CRUD 渲染与交互、加入队列、任务列表渲染单测通过；`tsc --noEmit` + `vitest run` + `npm run build` 全绿。
- 真机维度：实际 ffmpeg 预转码产出可播切片缓存、预热后首播无冷启动等待——标「待真机验」（无 ffmpeg 环境下单测用注入 mock transcoder 覆盖逻辑）。

## 6. 风险 / 待定

- width/height 不进管线（见 §2 不做）：若后续需要真实缩放，预设字段已就位，按新 FR 扩 `PreSliceWithCodec`。
- 预生成与按需播放并行写同一 `hlsDir/{mediaID}/`：`PreSliceWithCodec` 内部已 RemoveAll + 重建该目录，串行队列下不与自身并发；与播放路径的并发由既有切片机制约束，不在本期扩大。
