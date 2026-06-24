# ADR-0039：转码预生成队列复用任务队列范式

## 状态
已接受

## 背景
FR-77 需要让用户把媒体「加入预生成队列」，后台串行预转码、产出切片缓存以预热首播。本项目已有两类成熟构件：

- FR-29 的扫描任务队列 `internal/library/task_queue.go`：单 worker goroutine 串行执行、以 SQLite 表为持久化真源、`RecoverRunning` 重启把残留 running 重置为 pending 重新入队、执行目标经入队内存映射传递（不冗余落库）。
- FR-49~53 的转码管线 `internal/transcoder`：`PreSliceWithCodec` 已能按目标编码同步产出切片到 `hlsDir/{mediaID}/`（已存在切片则复用）。

架构不变量要求：禁止引入 Redis/RabbitMQ 等外部队列中间件（简单优先）；模块依赖单向 `web → api → library/transcoder → db`。

## 决策
新建预生成队列 `internal/transcoder/pregen_queue.go`，照搬 FR-29 任务队列范式（单 worker 串行 + SQLite `TranscodeTask` 持久化 + `RecoverRunning` 重启恢复 + 内存映射传执行目标），exec 函数经注入、生产实现直接调本包 `PreSliceWithCodec` 按预设 codec 预热切片。队列落在 transcoder 包内。

## 理由
- 复用既有范式而非另造轮子：行为、测试套路、运维心智与 FR-29 一致，零新增中间件，守「简单优先」红线。
- 放 transcoder 包：exec 直接调同包 `PreSliceWithCodec`，无需 transcoder 反向依赖 library，保持依赖单向。预设/任务的纯数据读写仍走 db 模型，业务编排在 transcoder。
- 任务入队时把预设的 codec/width/height 快照进 `TranscodeTask`：任务执行不强依赖预设此后是否被改/删，与 FR-29 把 scan_type 落任务表同构。

## 后果
- 正面：与扫描队列同构，前端可复用 ScanTaskIndicator 式轮询范式；无新依赖；单 worker 串行天然避免并发抢占同一 `hlsDir/{mediaID}/`。
- 约束：预生成与扫描预切片、按需播放切片共享 `hlsDir`；`PreSliceWithCodec` 内部 RemoveAll 重建该媒体目录，串行队列内不自并发，跨路径并发沿用既有切片机制、本期不扩大。
- 局限：本期预生成只按 codec 预热，预设 width/height 仅落库为元数据、不进 ffmpeg 缩放参数（现有管线不支持任意缩放）。真正缩放需扩 `PreSliceWithCodec`，另立 FR。

## 备选方案
- 在 library 包扩展现有 `TaskQueue` 承载两类任务：会让扫描队列承担转码职责（上帝类）、且 library 需依赖 transcoder 执行切片，破坏依赖方向，弃用。
- 引入通用 job 框架 / 外部消息队列：违反「禁用重型件」架构不变量，弃用。
- 预设直接驱动 ffmpeg 分辨率缩放：超出现有管线能力，属更大改动，本期不做（见后果局限）。
