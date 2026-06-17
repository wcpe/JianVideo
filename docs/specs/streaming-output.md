# 功能规格：后端流式输出

> 状态：开发中　·　关联 PRD：FR-08　·　分支：feature/streaming-output

## 1. 背景与目标

解决"转码进行中即可开始播放"的核心体验问题。当前（FR-01 骨架）没有任何转码能力，FR-08 要实现的机制是：FFmpeg 转码数据通过 HTTP ResponseWriter 实时刷新给客户端，禁止等待整个文件转完再返回。属于 P1（MVP）功能。

## 2. 需求（要什么）

- API 端点 `GET /api/play/stream/:id` 接受媒体文件 ID，返回 MPEG-TS 裸流
- 转码数据实时写入 HTTP 响应，客户端可立即开始播放
- 支持固定 GOP = 48 帧（`-g 48 -keyint_min 48 -sc_threshold 0`）
- 使用硬件加速编码器（通过 hwaccel.SelectBestEncoder() 获取），不支持时降级为软件编码
- 输出格式强制为 `-f mpegts`
- 需要 Range 头支持（Seek 场景，FR-09 的前置依赖）

**范围内**：
- 流式输出管道（ffmpeg 子进程 stdout → HTTP ResponseWriter）
- 硬件加速编码器自动选择与降级
- 固定 GOP 参数
- `GET /api/play/stream/:id` 端点

**不做（范围外）**：
- HLS m3u8 切片索引生成（FR-06 负责）
- 多码率 ABR（FR-07 负责）
- Range 请求的完整 Seek 处理（FR-09 负责，本 spec 只要求端点能接受 Range 头）
- 前端播放器实现（FR-16 负责）

## 3. 设计（怎么做）

### 3.1 模块结构

```
internal/
├── transcoder/
│   ├── pipeline.go    # 转码管道：构建 ffmpeg 命令、启动子进程、流式输出
│   └── streaming.go   # HTTP handler：GET /api/play/stream/:id
└── hwaccel/
    └── hwaccel.go     # 硬件加速编码器检测与选择
```

### 3.2 转码管道（pipeline.go）

- `Pipeline` 结构体封装一次转码会话
- 调用 `hwaccel.SelectBestEncoder("h264")` 获取最佳编码器
- 构建 ffmpeg 命令：`-i <input> -c:v <encoder> -g 48 -keyint_min 48 -sc_threshold 0 -f mpegts -`
- 通过 `exec.Command` 启动 ffmpeg 子进程
- stdout 通过 `io.Pipe` 或直接写入 ResponseWriter
- 使用 `context.Context` 管理生命周期（cancel → 杀进程）

### 3.3 流式输出（streaming.go）

- Gin handler 处理 `GET /api/play/stream/:id`
- 从数据库查询媒体文件路径（通过 library.Service）
- 设置响应头：`Content-Type: video/MP2T`, `Transfer-Encoding: chunked`
- 启动 ffmpeg 子进程，将 stdout 直接拷贝到 ResponseWriter
- 通过 `c.Stream()` 或手动 Flush 实现实时推送

### 3.4 接口依赖

| 模块 | 依赖方向 |
|---|---|
| `transcoder` | → `hwaccel`（获取编码器） |
| `transcoder` | → `library.Service`（查询媒体文件路径） |
| `api` | → `transcoder`（注册流式端点） |

## 4. 任务拆分

- [x] 创建 hwaccel 模块（hwaccel.go），提供 SelectBestEncoder() 接口
- [ ] 编写转码管道（pipeline.go）
- [ ] 编写流式输出 handler（streaming.go）
- [ ] 注册 `/api/play/stream/:id` 路由
- [ ] 编写测试（红→绿）
- [ ] 文档同步：PRD 状态、ARCHITECTURE、API、CHANGELOG、ADR-0011

## 5. 验收标准

- **AC-01**：`GET /api/play/stream/:id` 返回 HTTP 200，Content-Type 为 `video/MP2T`
- **AC-02**：响应数据在 ffmpeg 开始转码后立即返回，不等待转码完成
- **AC-03**：ffmpeg 进程在 HTTP 连接断开时自动终止（context cancel）
- **AC-04**：硬件加速编码器被优先使用，不可用时降级为软件编码
- **AC-05**：GOP 固定为 48 帧（`-g 48 -keyint_min 48 -sc_threshold 0`）
- **AC-06**：单元测试全部通过

## 6. 风险 / 待定

- **CGO vs 子进程**：CGO 绑定 FFmpeg（csnewman/ffmpeg-go）需要系统安装 FFmpeg 开发库，Windows 环境可能无法编译。当前使用 `exec.Command` 调用 ffmpeg CLI 作为实现方案，未来可通过构建标签替换为 CGO 实现。
- **测试环境**：单元测试中不实际调用 ffmpeg（通过 mock 管道验证流式行为），集成测试需要真实 ffmpeg。
- **Range 头支持**：FR-09 的完整 Seek 处理不在本 spec 范围内，但端点结构需预留。
