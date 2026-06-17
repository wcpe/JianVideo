# 功能规格：并发 Range 请求支持

> 状态：开发中　·　关联 PRD：FR-09　·　分支：feature/fr-09-range

## 1. 背景与目标
解决用户拖动进度条（Seek）时，后端仍在传输旧位置的媒体数据，导致 Seek 响应延迟的问题。需要在 Seek 时中断当前正在传输的旧连接，迅速开启新的 Range 请求线程，实现毫秒级 Seek 响应。属于第一期（MVP）P1 能力。

## 2. 需求（要什么）
- Seek 时后端能中断当前正在传输的旧连接
- 迅速开启新的 Range 请求线程（goroutine）
- 使用 `context.Context` 管理转码 goroutine 的生命周期
- Seek 操作：cancel 旧 context → 启动新 goroutine → `av_seek_frame` 定位
- HTTP Range 头解析：解析 `Range: bytes=xxx-xxx` 转换为时间位置
- 并发安全：多个 Seek 请求竞争时，只保留最新的
- 每个转码会话跟踪 `context.CancelFunc`

范围内：Seek 中断机制、Range 头解析、TranscodeSession 管理、并发安全
不做（范围外）：前端播放器实现、mpegts.js 集成、ABR 多码率切换、实际 FFmpeg CGO 编解码器集成（本阶段用模拟管道验证 Seek 机制）

## 3. 设计（怎么做）

### 模块
- `internal/transcoder/session.go`：TranscodeSession 会话管理，每个会话跟踪 context.CancelFunc
- `internal/transcoder/seek.go`：Seek 逻辑，Range 头解析，并发安全
- `internal/api/handler.go`：扩展 Play handler 支持 Range 请求

### 关键机制

#### TranscodeSession 管理
```go
type TranscodeSession struct {
    mu       sync.Mutex
    cancelFn context.CancelFunc
    status   string // "running" | "stopped" | "seeking"
}
```

#### Seek 流程
1. 客户端发送带 Range 头的请求
2. 解析 Range 头，转换为时间位置（秒）
3. 加锁，cancel 旧 context
4. 启动新 goroutine + 新 context
5. 在 goroutine 中调用 av_seek_frame 定位
6. 开始从新的时间位置输出数据

#### 并发安全
- `sync.Mutex` 保护 Seek 操作的原子性
- 多次快速 Seek 只保留最新的，旧 goroutine 被 cancel 后自动退出
- context 传播：cancel 信号通过 context 传递到转码 goroutine

### 数据模型
- 不新增数据库表
- `TranscodeSession` 为内存结构，管理单个媒体文件的转码生命周期

### API 变更
- `GET /api/play/:id` 支持 `Range: bytes=start-end` 请求头
- 响应返回 `Content-Range` 和 `Content-Length` 头
- 响应状态码：200（完整内容）或 206（部分内容）

## 4. 任务拆分
- [ ] 规格文档（本文件）
- [ ] ADR-0012 决策记录
- [ ] 测试先行：编写 session.go 和 seek.go 的单元测试
- [ ] 实现 TranscodeSession（session.go）
- [ ] 实现 Seek 逻辑和 Range 头解析（seek.go）
- [ ] 扩展 Play handler 支持 Range 请求
- [ ] 文档同步：PRD 状态、ARCHITECTURE、CHANGELOG

## 5. 验收标准
- Seek 时旧转码 goroutine 被正确 cancel
- 新 Range 请求能立即启动新的转码 goroutine
- 多个并发 Seek 请求只保留最新的，不产生数据竞争
- Range 头解析正确，支持 `bytes=start-end` 格式
- context.CancelFunc 被正确存储和调用
- 单元测试全部通过（红→绿验证）
- 无竞态条件（`go test -race` 通过）

## 6. 风险 / 待定
- 实际 av_seek_frame 需要 FFmpeg CGO 绑定，本阶段先验证 Seek 机制框架，后续集成真实 FFmpeg 时替换
- Range 字节位置到时间位置的转换依赖视频元数据（duration/bitrate），当前使用 MediaFile 中的 Duration 估算
- 内存管道（io.Pipe）用于模拟转码输出，后续替换为真实的 AVIO 上下文
