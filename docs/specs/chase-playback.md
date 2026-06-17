# 功能规格："边下边播"与实时追随

> 状态：开发中　·　关联 PRD：FR-17　·　分支：feature/chase-playback

## 1. 背景与目标

转码或文件持续写入时，播放器需要自动追播新数据，延迟控制在 3-5 秒内。FR-06 定义了 HLS 切片输出格式，FR-08 实现了流式输出，FR-16 引入了 mpegts.js 播放内核。FR-17 在此基础上实现追播闭环——后端持续写入切片，前端自动检测并追加播放。

## 2. 需求（要什么）

- 后端 HLS 切片持续写入，追播模式下永不写入 EXT-X-ENDLIST
- m3u8 索引文件持续追加新切片条目，播放器通过轮询检测新切片
- 追播延迟控制在 3-5 秒（通过 stashInitialSize 控制）
- 前端 VideoPlayer 组件封装追播模式，支持 isLive 开关
- 播放器自动追播新数据，无需用户手动操作

范围内：
- 后端 HLS 切片管理（internal/player/hls.go）
- 前端 VideoPlayer 追播组件（frontend/src/components/VideoPlayer.tsx）
- mpegts.js 追播模式配置

不做（范围外）：
- FR-18 播放末端缓冲等待逻辑（自动暂停/恢复）
- FR-07 自适应码率（ABR）
- FR-19 精准 Seek（HTTP Range）
- 切片文件清理策略（后续版本处理）

## 3. 设计（怎么做）

### 3.1 后端 HLS 切片管理器

新增 `internal/player/hls.go`，管理单个转码会话的 HLS 切片写入：

```
HLSManager
  - outputDir: string          // 切片输出目录
  - segmentDuration: float64   // 切片时长（秒），默认 2.0
  - segments: []SegmentInfo   // 已写入的切片列表
  - mu: sync.Mutex             // 并发保护
  - isLive: bool               // 是否追播模式
```

核心方法：
- `WriteSegment(data []byte) error` — 写入一个 TS 切片，更新 m3u8
- `GetM3U8() string` — 返回当前 m3u8 内容
- `SetLiveMode(live bool)` — 切换追播模式

m3u8 格式（追播模式）：
```
#EXTM3U
#EXT-X-VERSION:3
#EXT-X-TARGETDURATION:3
#EXT-X-MEDIA-SEQUENCE:0
#EXTINF:2.000,
segment_000.ts
#EXTINF:2.000,
segment_001.ts
...
```
（无 EXT-X-ENDLIST）

### 3.2 前端 VideoPlayer 追播组件

新建 `frontend/src/components/VideoPlayer.tsx`：

```tsx
interface VideoPlayerProps {
  src: string;          // m3u8 URL
  isLive: boolean;      // 是否追播模式
}
```

追播模式关键配置：
- `isLive: true` — 启用 mpegts.js 实时模式
- `stashInitialSize: 1024 * 1024 * 2` — 初始缓冲区 2MB，控制追播延迟
- 轮询 m3u8 检测新切片，自动追加到 MSE

### 3.3 接口变更

无新增 API。复用现有 `/api/play` 路由，通过查询参数区分追播模式：

```
GET /api/play/stream?media_id=123&live=true   # 追播模式
GET /api/play/stream?media_id=123              # 普通模式
```

## 4. 任务拆分

- [x] 创建规格文档 `docs/specs/chase-playback.md`
- [x] 创建 ADR `docs/adr/0020-chase-playback.md`
- [x] PRD §4 FR-17 状态翻转为「开发中」
- [ ] 后端：实现 `internal/player/hls.go` HLS 切片管理器
- [ ] 后端：编写 HLS 管理器单元测试
- [ ] 前端：安装 mpegts.js 依赖
- [ ] 前端：实现 VideoPlayer 追播组件
- [ ] 文档同步：ARCHITECTURE、CHANGELOG

## 5. 验收标准

- AC-NFR-03：追播延迟 3-5 秒
  - 验收方式：通过 HLSManager 测试验证 m3u8 无 EXT-X-ENDLIST，切片持续追加
- HLSManager.WriteSegment 正确写入切片文件并更新 m3u8
  - 验收方式：单元测试
- VideoPlayer 组件支持 isLive 属性切换
  - 验收方式：TypeScript 编译通过
- 追播模式下 m3u8 不包含 EXT-X-ENDLIST
  - 验收方式：单元测试

## 6. 风险 / 待定

- 追播延迟受切片时长和网络缓冲影响，精确控制需要实机测试
- mpegts.js 的 stashInitialSize 值需要根据实际网络环境调优
- 切片文件清理策略在后续版本处理
