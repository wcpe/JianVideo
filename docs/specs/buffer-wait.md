# 功能规格：播放末端缓冲等待

> 状态：开发中　·　关联 PRD：FR-18　·　分支：feature/fr-18-buffer

## 1. 背景与目标
解决播放器在追播场景下读取到文件末端时可能触发 Network Error 导致播放器崩溃的问题。当播放速度追上文件写入速度时，播放器应自动进入"缓冲等待"状态，显示友好的等待 UI，并在新数据写入后自动恢复播放，无需用户任何操作。属于第一期（MVP）P1 能力。

## 2. 需求（要什么）
- 播放器追上文件写入末端时自动进入"缓冲等待"状态，而非崩溃
- 缓冲等待期间显示"等待新数据..."UI 提示
- 检测到新数据写入时自动恢复播放，无需用户操作
- 监听 mpegts.js 的 ERROR 事件，自动重试播放（延迟 1 秒），不抛出未捕获异常
- 后端 HLS 切片写入器支持"追播模式"：写入速度跟得上播放速度时自动等待，避免读取越界

范围内：前端 VideoPlayer 组件的缓冲等待 UI 和错误重试逻辑，后端 HLS 写入器的追播等待机制
不做（范围外）：缓冲区的精细进度显示（FR-20 范畴）、网络断线重连、自适应码率切换

## 3. 设计（怎么做）

### 前端：VideoPlayer 组件（frontend/src/components/VideoPlayer.tsx）

新建 VideoPlayer React 组件，封装 mpegts.js 播放器，提供：

1. **缓冲等待状态机**：
   - 状态：`playing` | `buffering` | `error`
   - 当 mpegts.js 触发 `websocket` 或 `fetch` 流的 `onerror`/`onend` 时，判定为到达文件末端
   - 进入 `buffering` 状态，显示"等待新数据..."覆盖层

2. **错误重试机制**：
   - 监听 mpegts.js 的 `ERROR` 事件
   - 延迟 1 秒后自动销毁旧播放器实例并重新初始化
   - 重试时保持相同的流 URL

3. **自动恢复**：
   - 在 `buffering` 状态下，定时轮询 m3u8 索引文件
   - 检测到新 TS 切片出现时，重新初始化 mpegts.js 播放器并自动播放

### 后端：HLS 切片写入器（internal/player/hls.go）

新建 HLS 切片写入器，封装 HLS 文件的创建和写入逻辑：

1. **追播模式**：
   - 维护写入位置和已写入切片列表
   - 当消费者（播放器）读取到最后一片时，等待而非返回错误
   - 新数据到达时唤醒等待的消费者

2. **文件句柄管理**：
   - 使用 `http.ServeContent` 或自定义 `http.Handler` 处理 Range 请求
   - 读取到文件末尾时，如果处于追播模式，阻塞等待新数据（最多阻塞 30 秒后返回当前可用数据）

### 模块依赖
- `frontend/src/components/VideoPlayer.tsx` → 新建，依赖 mpegts.js
- `internal/player/hls.go` → 新建，依赖 net/http、os、sync
- `internal/player/hls_test.go` → 新建

### ADR 引用
- 追播等待策略：[ADR-0021](adr/0021-buffer-wait.md)

## 4. 任务拆分
- [x] 规格文档（docs/specs/buffer-wait.md）
- [x] ADR-0021 决策记录
- [x] PRD §4 FR-18 状态翻转为「开发中」
- [ ] 后端 HLS 切片写入器（internal/player/hls.go + hls_test.go）
- [ ] 前端 VideoPlayer 组件（frontend/src/components/VideoPlayer.tsx）
- [ ] 前端测试（如项目配置了 vitest/jest）
- [ ] 文档同步：ARCHITECTURE、CHANGELOG

## 5. 验收标准
- 播放器读取到文件末端时不抛出 Network Error，不崩溃
- 缓冲等待时显示"等待新数据..."UI 提示
- 新数据写入后自动恢复播放，无需用户操作
- mpegts.js ERROR 事件触发后 1 秒内自动重试
- 后端 HLS 写入器在追播模式下正确等待新数据
- 所有新增 Go 测试通过（`go test ./internal/player/...`）
- 手动验收：启动后端 + 前端，播放正在写入的 TS 文件，确认末端缓冲行为正确

## 6. 风险 / 待定
- mpegts.js 的 ERROR 事件类型较多（网络错误、解码错误、媒体错误），需精确区分"文件末端"和"真实错误"
- 追播等待的超时时间需可配置，当前硬编码 30 秒
- 前端轮询 m3u8 的频率会影响恢复延迟，当前设定 2 秒
