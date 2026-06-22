# 功能规格：下载原文件

> 状态：开发中　·　关联 PRD：FR-42　·　依赖：FR-13（鉴权，已修复）

## 1. 背景与目标

当前只有图片有 `GET /api/library/media/:id/raw`（且 HEIC/RAW 会被转成 JPEG），没有「下载原始文件」的能力，视频更无从下载。本功能（P2）新增鉴权后的**原文件下载**端点，对图片与视频一视同仁：直接回传磁盘上的原始字节（不转码、不转换），浏览器以附件形式保存。

## 2. 需求（要什么）

- 范围内：
  - 新增 `GET /api/library/media/:id/download`：鉴权后回传该媒体的原始文件，`Content-Disposition: attachment`，文件名用真实 `file_name`（支持中文，按 RFC 5987 编码）。
  - 图片与视频均支持；大文件支持 HTTP Range（`c.File` 经 `http.ServeFile` 天然支持，便于断点续传）。
  - 软删项不可下载（复用 `GetMediaFileByID` 的 `deleted_at IS NULL` 口径，FR-25）。
  - 前端：媒体详情/播放页与图片预览弹窗提供「下载原文件」入口，指向该端点。
- 不做（范围外）：
  - SMB 远程文件下载（FR-02 真机受限）：`smb://` 路径返回 `400`，与现有 raw 端点一致。
  - 打包多文件 / 相册批量下载（未要求，避免镀金）。
  - 免登公开下载：属分享链接（FR-43），本功能仅鉴权后下载。

## 3. 设计（怎么做）

- 处理器 `Handler.DownloadMediaFile`（`internal/api/handler.go`）：
  - 解析 `:id`（非法 → `400`）；`GetMediaFileByID` 取记录（不存在/已软删 → `404`）。
  - `smb://` 前缀 → `400 UNSUPPORTED_PATH`。
  - `os.Stat` 校验磁盘文件存在（不存在 → `404 FILE_NOT_FOUND`）。
  - 设 `Content-Disposition: attachment; filename*=UTF-8''<percent-encoded file_name>`，`c.File(mf.FilePath)` 回传（含 Range 支持）。
- 路由：`lib.GET("/media/:id/download", h.DownloadMediaFile)`（`internal/api/router.go`，鉴权组内）。
- 前端：`api/library.ts` 加 `mediaDownloadURL(id)` 返回端点 URL；播放页/图片预览加「下载原文件」按钮（`<a download>` 或新标签打开）。

## 4. 验收标准

- 对图片与视频 `GET /media/:id/download` 返回 `200` 且带 `Content-Disposition: attachment`、文件名为真实文件名；字节与磁盘原文件一致。
- 非法 ID → `400`；不存在/已软删 → `404`；`smb://` → `400`；磁盘文件缺失 → `404`。
- 未鉴权访问被 `auth.APIGuard` 拦截（`401`，复用 FR-13）。
- 后端 `go build ./...` 通过、受影响包 `go test` 全绿；前端 `npm run build`/`npm run test` 全绿。

## 5. 风险 / 待定

- 中文/特殊字符文件名：用 RFC 5987 `filename*=UTF-8''` 编码，避免乱码与头注入。
- 大文件：`c.File` 走 `http.ServeFile` 流式 + Range，不一次性读入内存（区别于 raw 端点的 `os.ReadFile`）。
