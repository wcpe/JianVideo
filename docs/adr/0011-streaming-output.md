# ADR-0011：基于子进程的流式转码管道

## 状态
已接受

## 背景
FR-08 要求 FFmpeg 转码数据通过 HTTP ResponseWriter 实时流式输出给客户端。架构文档（ARCHITECTURE.md §5.2）指定使用 CGO 绑定（csnewman/ffmpeg-go）直接调用 libavcodec/libavformat C API，通过自定义 AVIO 上下文写入 ResponseWriter。

但实际部署环境（Windows）通常不安装 FFmpeg 开发库（libavcodec-dev 等），CGO 编译无法通过。需要在架构意图与工程可行性之间做出选择。

## 决策
使用 `exec.Command` 启动 ffmpeg 子进程，将其 stdout（MPEG-TS 裸流）通过 `io.Copy` 桥接至 HTTP ResponseWriter，实现流式输出。

## 理由
- **跨平台可用性**：ffmpeg CLI 在所有平台（Windows/Linux/macOS）上均可获得，无需 CGO 或系统级开发库
- **流式行为等价**：ffmpeg stdout 输出与 CGO AVIO 自定义写入在 HTTP 层面的效果一致，客户端无感知
- **进程隔离**：子进程崩溃不影响主服务进程，天然隔离
- **可替换性**：通过 `Transcoder` 接口抽象，未来可替换为 CGO 实现（加 build tag `cgo`）
- **实现简单**：避免 CGO 的内存管理复杂性（AVFrame、AVPacket 的生命周期管理）

## 后果
- 每个转码会话对应一个 ffmpeg OS 进程（而非线程），资源开销略高于 CGO 方案
- 无法精确控制 AVIO 写入时序，但 MPEG-TS 流格式本身不要求精确时序
- 需要管理 ffmpeg 子进程的生命周期（context cancel → 杀进程）
- 未来若需更低延迟或更细粒度控制，需替换为 CGO 实现

## 备选方案
- **CGO + ffmpeg-go（架构文档指定）**：直接调用 libavcodec C API，性能最优但需要 FFmpeg 开发库，Windows 开发环境不可用。保留为未来优化路径。
- **HTTP 代理到 ffmpeg 内置 HTTP 服务器**：让 ffmpeg 起 HTTP 客户端再代理，多一层网络跳转，增加延迟，无必要。
