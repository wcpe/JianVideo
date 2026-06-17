# ADR-0012: 并发 Range 请求支持

## 状态
已接受

## 背景
用户拖动进度条时，后端仍在传输旧位置的媒体数据，导致 Seek 响应延迟。系统需要支持 Seek 时中断旧连接并迅速开启新 Range 请求线程。这是第一期（MVP）P1 能力，关联 FR-09。

## 决策
引入 `internal/transcoder` 包，通过 `TranscodeSession` 管理每个转码 goroutine 的 context.CancelFunc。Seek 时加锁 → cancel 旧 context → 启动新 goroutine → av_seek_frame 定位。Range 头解析为时间位置后触发 Seek。并发安全通过 `sync.Mutex` 保证，多个 Seek 只保留最新。

## 理由
- `context.Context` 是 Go 中管理 goroutine 生命周期的标准方式，cancel 信号可传播到嵌套调用
- `sync.Mutex` 轻量级， Seek 操作频率低（用户拖拽），不引入 channel 增加复杂度
- TranscodeSession 作为内存结构，不持久化到数据库，符合"转码会话是临时过程"的语义
- 框架先于实现：先搭建 Seek 机制骨架（context 管理 + mutex + Range 解析），后续集成 FFmpeg CGO 时只需替换转码管道

## 后果
- 新增 `internal/transcoder` 包，包含 session.go 和 seek.go
- Play handler 需要扩展支持 Range 请求
- 转码 goroutine 的生命周期由 TranscodeSession 统一管理
- 后续 FR-08（后端流式输出）和 FR-17（边下边播）依赖本机制

## 备选方案
- 使用 channel 传递 Seek 信号：增加复杂度，且 channel 关闭后无法重新打开，不适合重复 Seek
- 使用 sync.WaitGroup 等待旧 goroutine 退出：无法保证及时中断，旧 goroutine 可能在阻塞 IO
- 使用进程级隔离（每次 Seek 重启进程）：太重，启动延迟高
