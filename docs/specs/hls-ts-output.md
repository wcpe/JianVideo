# 功能规格：TS 流（MPEG-TS）强制输出

> 状态：开发中　·　关联 PRD：FR-06　·　分支：fr-06-hls

## 1. 背景与目标

转码输出需要固定采用 HLS（m3u8 + TS 切片）格式，确保所有视频（无论原始格式）都能被前端 mpegts.js 实时播放。属于第一期 MVP 核心能力。

## 2. 需求（要什么）

- 转码输出固定采用 mpegts 格式（`-f mpegts`）
- 同时生成 HLS 切片文件（`.ts`）和内存管道
- 管理 HLS 切片目录（`data/hls/{media_id}/`）
- 生成 m3u8 索引文件（追播模式：持续追加新切片，不写 `EXT-X-ENDLIST`）
- 切片时长目标 3 秒（`hls_time=3`）
- 切片命名：`segment_000.ts`, `segment_001.ts`, ...
- m3u8 格式：标准 HLS v3，包含 `EXTM3U`、`EXT-X-VERSION:3`、`EXT-X-TARGETDURATION:3`、`EXT-X-MEDIA-SEQUENCE:0`
- API 端点：
  - `GET /api/play/hls/:id/index.m3u8` → 返回 m3u8 索引
  - `GET /api/play/hls/:id/:segment` → 返回切片文件

- 范围内：切片目录管理、m3u8 生成与追播、API 端点
- 不做（范围外）：前端播放器实现（FR-16）、自适应码率（FR-07）、字幕处理

## 3. 设计（怎么做）

### 模块：`internal/player`

新增 `player` 包，包含两个文件：

**`hls.go`** — HLS 切片写入器：
- `HLSSegmentWriter` 结构体：管理单个 m3u8 的切片写入
- 方法：`WriteSegment([]byte) error` — 写入一个 ts 切片并更新 m3u8
- 方法: `Close()` — 写入 `EXT-X-ENDLIST`（转码结束时调用）
- 切片命名规则：`segment_%03d.ts`
- m3u8 追播模式：每次写入切片后追加 `#EXTINF:3.000,\nsegment_NNN.ts\n`，不写 `EXT-X-ENDLIST`

**`hls_manager.go`** — HLS 会话管理器：
- `HLSManager` 结构体：管理所有媒体文件的 HLS 会话
- 方法：`GetOrCreateWriter(mediaID int64) (*HLSSegmentWriter, error)` — 获取或创建写入器
- 方法：`RemoveWriter(mediaID int64)` — 清理会话
- 方法：`GetM3U8(mediaID int64) (string, error)` — 读取 m3u8 文件内容
- 方法：`GetSegment(mediaID int64, name string) ([]byte, error)` — 读取切片文件内容
- 目录结构：`data/hls/{media_id}/index.m3u8`, `data/hls/{media_id}/segment_000.ts`, ...

### API 层：`internal/api/handler.go`

新增 HLS 播放路由：
- `GET /api/play/hls/:id/index.m3u8` → 返回 m3u8 索引（Content-Type: `application/vnd.apple.mpegurl`）
- `GET /api/play/hls/:id/:segment` → 返回切片文件（Content-Type: `video/mp2t`）

### 数据模型变更

无新增表。transcode_sessions 表已有 `output_url` 字段可复用。

## 4. 任务拆分

- [x] 创建规格文档 `docs/specs/hls-ts-output.md`
- [x] 创建 ADR `docs/adr/0010-hls-ts-output.md`
- [x] 实现 `internal/player/hls.go` — HLS 切片写入器
- [x] 实现 `internal/player/hls_manager.go` — 会话管理器
- [x] 添加 API 路由 `/api/play/hls/:id/...`
- [x] 编写测试（红→绿）
- [x] 文档同步：PRD 状态、ARCHITECTURE、CHANGELOG
- [x] 中文 commit

## 5. 验收标准

- `GET /api/play/hls/:id/index.m3u8` 返回正确格式的 m3u8 索引
- `GET /api/play/hls/:id/segment_000.ts` 返回切片文件
- m3u8 追播模式：每次追加新切片，不写 `EXT-X-ENDLIST`
- 切片目录自动创建（`data/hls/{media_id}/`）
- 全部单元测试通过

## 6. 风险 / 待定

- CGO 转码管道（FR-04/08）尚未实现，当前阶段 HLS 切片写入器和 API 端点独立可用，后续对接 FFmpeg 输出
- 切片清理策略：当前版本不清理（转码结束后文件保留），后续版本可添加定时清理
