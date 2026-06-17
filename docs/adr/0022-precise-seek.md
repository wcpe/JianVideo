# ADR-0022：精准进度拖拽与 HTTP Range 支持

## 状态
已接受

## 背景
FR-19 要求精准进度拖拽（Seek 响应 < 2 秒）。当前后端仅有媒体库管理 API，缺少播放流控接口。需要新增 HTTP Range 请求支持，使播放器能够按需请求文件的任意字节范围，实现快速 Seek。

## 决策
在 `internal/playback` 模块中实现 HTTP Range 请求解析与文件流输出，通过标准 `net/http` 的 `http.ServeContent` 处理 Range 逻辑，新增 `/api/play/:id/stream`、`/api/play/:id/seek`、`/api/play/:id/progress` 三个接口。

## 理由
- `http.ServeContent` 是 Go 标准库内置的 Range 请求处理器，自动处理 `Content-Range`、`Content-Length`、`If-Range` 等复杂逻辑，无需手动解析
- 新增 `playback` 模块保持职责单一，不与 `library` 模块耦合
- Seek 操作本质是客户端发起新的 Range 请求，后端 API 仅做位置记录和确认
- 进度追踪通过前端上报缓冲区间实现，后端被动存储

## 后果
- 正面：播放器可实现毫秒级 Seek（受限于 GOP 对齐和 I 帧命中）
- 正面：标准 HTTP Range 兼容所有 HTTP 客户端和 CDN
- 负面：需要维护播放会话状态（内存存储，重启丢失，可接受）
- 后续约束：转码模式下 Seek 需要 GOP 对齐，本 ADR 仅覆盖原始文件播放场景

## 备选方案
- **手动解析 Range header**：自行实现 `bytes=START-END` 解析和 206 响应。落选原因：`http.ServeContent` 已覆盖所有边界条件（多范围、重叠、越界），重复造轮子
- **HLS 切片 Seek**：通过 m3u8 切片索引实现 Seek。落选原因：HLS 切片 Seek 精度受切片时长限制（通常 6-10 秒），无法满足毫秒级要求；且当前无 HLS 切片生成模块
