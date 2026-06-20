# 功能规格：智能播放策略

> 状态：开发中　·　关联 PRD：FR-05　·　分支：feature/fr-05-directplay

## 1. 背景与目标
解决浏览器无法播放所有视频格式的问题。通过智能决策，对浏览器兼容格式（H.264+AAC 的 MP4）直出播放，对其他格式无缝切入转码模式，让用户无需关心底层格式差异。属于第一期（MVP）P1 能力。

## 2. 需求（要什么）

策略：浏览器兼容格式直出（无转码开销）、不兼容格式无缝切入转码模式（HLS/TS），用户无感。

实际实现（已落地，v0.1.0）：**由前端主动探测决定播放路径**——
- 前端 `PlayPage` 挂载时并发请求 `GET /api/play/hls/:id/master`（HLS master.m3u8 探测）。
- master.m3u8 存在且响应 `Content-Type: */mpegurl` → 走 ABR/HLS 模式（mpegts.js / hls.js）。
- 否则降级到 `/api/play/:id/stream` 流式播放（浏览器原生 `<video>` 直出本地兼容文件；不兼容文件由后端流式转码后输出）。

后端不再单独提供 `GET /api/play/:id` 的 `PlayInfo` 决策端点（早期设计），因为：
- HLS 切片是否就绪可由 `/api/play/hls/:id/master` 直接判断，多一个 `playinfo` 端点等价但多一轮往返。
- 直出/转码的真实信令在 `/stream` 路径上由后端按文件实际编码即时决定，不需要前端预先知道。

范围内：直出/转码的策略与降级路径、媒体卡片到播放页的入口、前端探测逻辑。
不做（范围外）：真实转码实现（FR-06/08）、ABR 多码率（FR-07）、字幕（已由 FR-04 处理）。

## 3. 设计（怎么做）

### 模块
- `internal/transcoder/codec.go`：格式检测工具函数（判断是否为浏览器兼容格式）
- `internal/transcoder/playback.go`：播放策略决策（直出/转码判断、PlayInfo 结构）
- `internal/api/handler.go`：新增 `GetPlayInfo` handler
- `internal/api/router.go`：注册 `/api/play/:id` 路由

### 数据模型
- 复用已有 `MediaFile` 模型（含 `format`、`video_codec`、`audio_codec` 字段）
- 新增 `PlayInfo` 响应结构：

```go
type PlayInfo struct {
    URL             string `json:"url"`
    Format          string `json:"format"`           // "direct" 或 "hls"
    TranscodeRequired bool   `json:"transcode_required"`
    HWAccel         string `json:"hw_accel"`         // 使用的硬件加速类型，直出时为 ""
}
```

### API 端点（落地后）

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | /api/play/hls/:id/master | HLS master.m3u8；前端凭此探测 ABR/HLS 可用性 |
| GET | /api/play/:id/stream | 流式播放端点；直出文件 Range 透传，不兼容文件后端转码后输出 |

### 关键机制（落地后）
- 直出/转码决策由 `/stream` 路径承担：本地兼容 MP4 → Range 直传文件；不兼容 → FFmpeg 流式转码后输出（FR-08）。
- 大小写不敏感：编码名称比较时统一转小写（适用于 codec 元数据相关判断）。
- 前端探测降级：HLS 不可用 → 自动回退到 `/stream`（同一 URL 即可被原生 `<video>` 直出或承载转码流）。

## 4. 任务拆分
- [ ] 创建 `internal/transcoder/codec.go`（格式检测函数）
- [ ] 创建 `internal/transcoder/playback.go`（播放策略决策）
- [ ] 编写单元测试（playback 决策逻辑）
- [ ] 新增 API handler `GetPlayInfo`
- [ ] 注册路由
- [ ] 编写 API 层测试
- [ ] 文档同步：PRD 状态、ARCHITECTURE、API、CHANGELOG

## 5. 验收标准（与落地一致）
- H.264+AAC 的 MP4 → 浏览器原生 `<video>` 经 `/api/play/:id/stream` 直接播放，不触发 FFmpeg 转码（真机已验：`<video readyState=4`）。
- H.265/MKV 等不兼容格式 → 经 `/api/play/:id/stream` 由后端 FFmpeg 转码后输出，浏览器正常播放（真机已验：HEVC MKV 解码、播到结尾、error=null）。
- 不存在的媒体文件返回 404。
- 前端 HLS 探测不可用时自动降级到 `/stream`，不向用户暴露错误（控制台仅一次预期 404 探测请求，PlayPage 已捕获）。

## 6. 风险 / 待定
- 转码 URL 当前为占位，待 FR-06~FR-12 实现真实转码后替换
- 硬件加速类型检测（FR-10~FR-12）尚未实现，当前默认 "software"
- 直出模式下浏览器直接播放文件，需确保文件路径可通过 HTTP 访问（后续需配置静态文件服务或通过 API 代理）
