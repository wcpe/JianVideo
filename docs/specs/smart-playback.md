# 功能规格：智能播放策略

> 状态：开发中　·　关联 PRD：FR-05　·　分支：feature/fr-05-directplay

## 1. 背景与目标
解决浏览器无法播放所有视频格式的问题。通过智能决策，对浏览器兼容格式（H.264+AAC 的 MP4）直出播放，对其他格式无缝切入转码模式，让用户无需关心底层格式差异。属于第一期（MVP）P1 能力。

## 2. 需求（要什么）
- 根据媒体文件的容器格式、视频编码、音频编码自动判断是否需要转码
- 直出条件：容器=mp4 AND 视频编码=h264 AND 音频编码=aac
- 不满足直出条件的所有其他格式 → 转码模式，返回 HLS 流 URL
- 提供 `GET /api/play/:id` 端点，返回播放信息（URL、格式、是否需要转码、硬件加速类型）
- 直出模式返回文件路径（浏览器直接播放）
- 转码模式返回转码流 URL（后续由 FR-06~FR-12 实现真实转码，当前返回占位 URL）

范围内：播放策略决策逻辑、API 端点、格式判断
不做（范围外）：真实转码实现（FR-06~FR-12）、前端播放器实现（FR-16）、ABR 多码率（FR-07）

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

### API 端点

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | /api/play/:id | 获取播放信息（直出或转码） |

### 关键机制
- 直出判断：`format == "mp4" && video_codec == "h264" && audio_codec == "aac"`
- 直出 URL：直接返回 `file_path`（浏览器通过文件路径播放）
- 转码 URL：返回占位路径 `/api/play/hls/:id/index.m3u8`（后续由 FR-06 实现）
- 硬件加速：直出时为空字符串，转码时返回检测到的硬件加速类型（默认 "software"）
- 大小写不敏感：编码名称比较时统一转小写

## 4. 任务拆分
- [ ] 创建 `internal/transcoder/codec.go`（格式检测函数）
- [ ] 创建 `internal/transcoder/playback.go`（播放策略决策）
- [ ] 编写单元测试（playback 决策逻辑）
- [ ] 新增 API handler `GetPlayInfo`
- [ ] 注册路由
- [ ] 编写 API 层测试
- [ ] 文档同步：PRD 状态、ARCHITECTURE、API、CHANGELOG

## 5. 验收标准
- H.264+AAC 的 MP4 文件返回 `format: "direct"`，`transcode_required: false`
- H.265+MKV 文件返回 `format: "hls"`，`transcode_required: true`
- 不存在的媒体文件返回 404
- 编码名称大小写不敏感（H264=AAC 等同于 h264+aac）
- 缺少编码信息的文件默认需要转码
- 所有测试从红转绿

## 6. 风险 / 待定
- 转码 URL 当前为占位，待 FR-06~FR-12 实现真实转码后替换
- 硬件加速类型检测（FR-10~FR-12）尚未实现，当前默认 "software"
- 直出模式下浏览器直接播放文件，需确保文件路径可通过 HTTP 访问（后续需配置静态文件服务或通过 API 代理）
