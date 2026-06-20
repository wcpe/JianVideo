# 接口契约：轻量级单用户视频媒体服务器

> 对外接口的单一真源。始终原地更新到当前契约。

## 1. 通用约定

- **协议**：HTTP/HTTPS RESTful API
- **认证**：基于 Cookie 的会话认证，登录后返回 `Set-Cookie` 头部
- **编码**：请求/响应体使用 JSON（`Content-Type: application/json`），视频流使用 `video/mp2t`
- **分页**：列表接口支持 `page`（从 1 开始）和 `page_size`（默认 20，最大 100）参数
- **时间格式**：ISO 8601（`YYYY-MM-DDTHH:MM:SSZ`）
- **静态资源**：前端文件通过 `go:embed` 内嵌，由 `/` 路径提供服务

## 2. 错误约定

统一错误返回结构：

```json
{
  "code": "ERROR_CODE",
  "message": "人类可读的错误描述"
}
```

| HTTP 状态码 | 含义 |
|---|---|
| 400 | 请求参数错误 |
| 401 | 未认证 |
| 403 | 禁止访问 |
| 404 | 资源不存在 |
| 500 | 服务器内部错误 |

## 3. 端点 / 方法

### 登录

- **方法 / 路径**：`POST /api/auth/login`
- **请求**：
  ```json
  {
    "username": "string",
    "password": "string"
  }
  ```
- **响应**（200）：
  ```json
  {
    "username": "string"
  }
  ```
- **错误**：`401` 用户名或密码错误

### 登出

- **方法 / 路径**：`POST /api/auth/logout`
- **响应**（200）：空

### 获取媒体库目录列表

- **方法 / 路径**：`GET /api/library/paths`
- **响应**（200）：每项含 `media_count`，即该库已索引（未软删）的媒体文件数量，供存储库卡片展示。
  ```json
  {
    "items": [
      {
        "id": 1,
        "path": "/media/movies",
        "type": "local",
        "label": "电影",
        "enabled": true,
        "media_count": 42
      }
    ]
  }
  ```

### 添加媒体库目录

- **方法 / 路径**：`POST /api/library/paths`
- **请求**：
  ```json
  {
    "path": "\\\\192.168.1.100\\Share\\Movies",
    "type": "smb",
    "label": "NAS 电影",
    "smb_username": "optional",
    "smb_password": "optional"
  }
  ```
- **响应**（201）：目录记录对象
- **错误**：`400` 本地路径不可访问、不是目录或请求参数错误；`500` 保存失败
- **说明**：`local` 路径必须在服务器本机存在且为目录；`smb` 路径支持 UNC 或 `smb://host/share/path` 输入，服务端统一存储为 `host/share/path`，凭据通过 `/api/smb/credentials` 保存。

### 删除媒体库目录

- **方法 / 路径**：`DELETE /api/library/paths/:id`
- **响应**（204）：空

### 浏览目录

- **方法 / 路径**：`GET /api/library/browse`
- **查询参数**：
  - `library_id`：媒体库 ID（必填）
  - `parent_path`：父目录路径（必填）
- **响应**（200）：
  ```json
  {
    "breadcrumbs": [
      {"name": "media", "path": "/media"},
      {"name": "movies", "path": "/media/movies"}
    ],
    "directories": [
      {"name": "动作片", "path": "/media/movies/动作片"}
    ],
    "files": [
      {
        "id": 1,
        "file_name": "电影名.mkv",
        "file_path": "/media/movies/电影名.mkv",
        "file_size": 10737418240,
        "format": "mkv",
        "duration": 7200.0,
        "width": 1920,
        "height": 1080
      }
    ]
  }
  ```
- **说明**：通过 `file_path` 前缀匹配一次查询，Go 层按第一级子目录聚合分组。Windows 盘符路径使用正斜杠规范化，面包屑不会添加额外前导 `/`（例如 `D:/Videos`）。

### 获取媒体文件列表

- **方法 / 路径**：`GET /api/library/media`
- **查询参数**：
  - `library_id`：按媒体库过滤（可选）
  - `sort`：排序方式，`time_desc`（默认）/ `time_asc` / `name`
  - `page`：页码
  - `page_size`：每页条数
  - `search`：搜索关键词（可选）
- **响应**（200）：
  ```json
  {
    "items": [
      {
        "id": 1,
        "file_name": "电影名.mkv",
        "file_path": "/media/movies/电影名.mkv",
        "file_size": 10737418240,
        "format": "mkv",
        "video_codec": "hevc",
        "audio_codec": "aac",
        "duration": 7200.0,
        "width": 1920,
        "height": 1080,
        "bitrate": 12000000,
        "added_at": "2025-01-01T12:00:00Z"
      }
    ],
    "total": 100,
    "page": 1,
    "page_size": 20
  }
  ```

### 获取媒体文件详情

- **方法 / 路径**：`GET /api/library/media/:id`
- **响应**（200）：媒体文件详情对象（含字幕轨道信息）

### 重命名媒体文件

- **方法 / 路径**：`PUT /api/library/media/:id/rename`
- **请求**：
  ```json
  {"new_name": "新文件名.mp4"}
  ```
- **响应**（200）：更新后的媒体文件对象
- **说明**：`new_name` 仅允许单层文件名（不含 `/`、`\` 或 `.`/`..`）。后端先对磁盘文件原子改名，再更新 `media_files.file_path`/`file_name`/`format`，数据库更新失败时尽力回滚磁盘改名；旧缩略图按旧路径 hash 命名，重命名后失效并异步为新文件重新生成。SMB 远程文件暂不支持。
- **错误**：`400` 新名不合法或不支持的文件类型，`404` 媒体记录不存在，`409` 目标文件名已存在，`500` 重命名失败

### 获取图片原始内容

- **方法 / 路径**：`GET /api/library/media/:id/raw`
- **响应**（200）：图片二进制内容，`Content-Type` 由文件后缀或内容探测确定
- **说明**：仅支持本地图片文件，用于前端预览；视频和 SMB 图片不走此接口。
- **错误**：`400` 非图片或不支持的路径，`404` 记录或文件不存在

### 获取媒体缩略图

- **方法 / 路径**：`GET /api/library/thumbnail/:id`
- **响应**（200）：缩略图 JPEG 二进制内容（320px 宽，视频取第 2 秒帧、图片缩放）
- **说明**：缩略图在扫描入库时异步生成，存于数据目录下的 `thumbnails/`（按原始路径 hash 命名）。缩略图尚未生成时返回 `202` 并触发后台异步生成，前端可稍后重试。
- **错误**：`202` 缩略图生成中，`404` 媒体记录不存在

### 查询媒体库后缀

- **方法 / 路径**：`GET /api/library/extensions`
- **查询参数**：`library_id`（必填）
- **响应**（200）：
  ```json
  {
    "items": [
      {"library_id": 1, "extension": "jpg", "type": "image", "is_builtin": 1},
      {"library_id": 1, "extension": "foo", "type": "video", "is_builtin": 0}
    ]
  }
  ```
- **说明**：返回内置后缀和绑定到该媒体库目录的自定义后缀；自定义后缀不会影响其他目录。

### 添加媒体库自定义后缀

- **方法 / 路径**：`POST /api/library/extensions`
- **请求**：
  ```json
  {
    "library_id": 1,
    "extension": ".foo",
    "type": "video"
  }
  ```
- **响应**（201）：空
- **说明**：`type` 仅允许 `video` 或 `image`；后缀会规范化为小写且去掉前导点。内置后缀无需入库，重复添加视为幂等成功。
- **错误**：`400` 参数无效、目录不存在或后缀格式不支持

### 扫描媒体库目录

- **方法 / 路径**：`POST /api/library/scan/:id`
- **响应**（200）：
  ```json
  {"status": "scanning"}
  ```
- **说明**：按 `LibraryPath.type` 分发本地递归扫描或 SMB 扫描，识别内置图片/视频后缀和该目录绑定的自定义后缀；重复扫描不会重复入库。扫描在后台 goroutine 中异步执行，接口立即返回，不阻塞主线程；实际进度通过「扫描进度」SSE 端点获取，入库的媒体文件会异步生成缩略图。
- **错误**：`400` ID 无效，`404` 目录不存在

### 扫描进度（SSE）

- **方法 / 路径**：`GET /api/library/scan/progress`
- **响应**：`Content-Type: text/event-stream`，每 500ms 推送一条 `progress` 事件，扫描完成或出错后服务端关闭连接。事件 `data` 为 JSON：
  ```json
  {
    "status": "scanning",
    "library_id": 1,
    "current_path": "D:/Videos/Movies/a.mp4",
    "total_files": 120,
    "scanned_files": 30,
    "error": "",
    "started_at": "2026-06-20T20:00:00Z",
    "completed_at": "0001-01-01T00:00:00Z"
  }
  ```
- **说明**：`status` 取值 `idle` / `scanning` / `completed` / `error`，全局共享单一扫描状态（同一时刻仅跟踪一个扫描任务）。前端据此渲染进度条，`completed` 后自动刷新媒体列表。

### 获取播放地址

- **方法 / 路径**：`GET /api/play/:id`
- **查询参数**：
  - `format`：播放格式，`direct`（直出）/ `hls`（转码），默认自动判断
  - `resolution`：目标分辨率，`1080p` / `720p` / `480p`（可选，ABR 模式下自动）
- **响应**（200）：
  ```json
  {
    "url": "/api/play/stream/123",
    "format": "hls",
    "transcode_required": true,
    "hw_accel": "nvenc"
  }
  ```

### 获取视频流

- **方法 / 路径**：`GET /api/play/stream/:id`
- **请求头**：支持 `Range` 头部（用于 Seek）
- **响应**（200 / 206）：
  - `Content-Type: video/mp2t`
  - 支持 `Content-Range` 响应头
- **错误**：`404` 媒体文件不存在，`500` 转码启动失败

### 获取 HLS 索引

- **方法 / 路径**：`GET /api/play/hls/:id/index.m3u8`
- **响应**（200）：`Content-Type: application/vnd.apple.mpegurl`

### 获取 HLS 切片

- **方法 / 路径**：`GET /api/play/hls/:id/:quality_segment_001.ts`
  - `:quality` 为码率档位：`1080p` / `720p` / `480p`
- **响应**（200）：`Content-Type: video/mp2t`

### 获取 ABR Master Playlist

- **方法 / 路径**：`GET /api/play/hls/:id/master.m3u8`
- **响应**（200）：`Content-Type: application/vnd.apple.mpegurl`
- **说明**：返回包含多码率 `EXT-X-STREAM-INF` 标签的 master playlist，供 hls.js 自适应切换

### 获取转码状态

- **方法 / 路径**：`GET /api/transcode/status/:media_id`
- **响应**（200）：
  ```json
  {
    "status": "running",
    "pid": 12345,
    "hw_accel": "nvenc",
    "progress": 0.45
  }
  ```

### 查询硬件加速能力

- **方法 / 路径**：`GET /api/transcode/hwaccel`
- **响应**（200）：
  ```json
  {
    "available": ["nvenc", "qsv"],
    "preferred": "nvenc"
  }
  ```

### 系统信息

- **方法 / 路径**：`GET /api/system/info`
- **响应**（200）：
  ```json
  {
    "app_version": "0.3.0",
    "os": "linux", "arch": "amd64", "num_cpu": 8, "hostname": "nas01",
    "go_version": "go1.22.5",
    "ffmpeg": { "available": true, "path": "/opt/jianvideo/ffmpeg", "version": "ffmpeg version 6.1.1 ..." },
    "hwaccel": { "available": [], "preferred": "libx264", "intel_gpu": false, "h264_supported": true, "h265_supported": true, "software_fallback": true }
  }
  ```
- **说明**：`hwaccel` 复用 `GET /api/transcode/hwaccel` 的结构；`app_version` 由构建期 `-ldflags -X main.version` 注入。

### 编解码器实测

- **方法 / 路径**：`POST /api/system/codec-test`
- **响应**（200）：
  ```json
  {
    "ffmpeg_available": true,
    "results": [
      { "encoder": "libx264", "family": "software", "codec": "h264", "compiled": true, "tested_ok": true, "detail": "" },
      { "encoder": "h264_qsv", "family": "qsv", "codec": "h264", "compiled": true, "tested_ok": false, "detail": "<stderr 尾部>" }
    ]
  }
  ```
- **说明**：对候选编码器用外部 ffmpeg 跑一小段试编码（`-f lavfi … -f null`）。`compiled` 表示是否编入当前 ffmpeg，`tested_ok` 表示试编码是否成功。ffmpeg 不可用时返回 `ffmpeg_available:false` 且 `results` 为空。逐个试编码，响应可能耗时数秒。

### 获取字幕轨道列表

- **方法 / 路径**：`GET /api/play/:id/subtitles`
- **响应**（200）：
  ```json
  {
    "tracks": [
      {
        "index": 0,
        "file_name": "电影名.srt",
        "format": "srt",
        "url": "/api/play/1/subtitles/0"
      }
    ]
  }
  ```
- **说明**：返回媒体文件同目录下的外挂字幕轨道列表（SRT/ASS/SSA/SUP），按文件名匹配

### 获取字幕内容

- **方法 / 路径**：`GET /api/play/:id/subtitles/:index`
- **响应**（200）：`Content-Type: text/vtt; charset=utf-8`
- **说明**：返回指定字幕轨道的 WebVTT 转换内容，SUP 格式返回空 WebVTT 占位
- **错误**：`400` 索引格式无效，`404` 索引超出范围

### 保存 SMB 凭据

- **方法 / 路径**：`POST /api/smb/credentials`
- **请求**：
  ```json
  {
    "host": "192.168.1.100",
    "username": "user",
    "password": "pass",
    "share": "ShareName",
    "domain": "WORKGROUP"
  }
  ```
- **响应**（204）：空
- **说明**：SMB 凭据使用 AES-256-GCM 加密后存储在本地文件（`data/smb_credentials.enc`）。加解密主密码由服务端通过 `SMB_MASTER_PASSWORD` 环境变量统一配置，**必须显式设置**——未设置（或为空串）时服务端拒绝保存/加载凭据，不再回退弱默认主密码；请求体无需也不应包含主密码
- **错误**：`400` 请求参数错误，`503` 未配置 `SMB_MASTER_PASSWORD` 环境变量，`500` 加密/存储失败

### 获取系统配置

- **方法 / 路径**：`GET /api/config`
- **响应**（200）：
  ```json
  {
    "server_port": 8080,
    "ffmpeg_path": "/usr/bin/ffmpeg",
    "ffprobe_path": "/usr/bin/ffprobe",
    "library_paths_count": 3,
    "media_files_count": 1500
  }
  ```

### 读取运行期设置

- **方法 / 路径**：`GET /api/settings`
- **响应**（200）：
  ```json
  {
    "settings": {
      "scan_interval": "3600",
      "recycle_bin_paths": "{\"D\":\"D:/.recycle\"}"
    }
  }
  ```
- **说明**：返回全部运行期设置（key → value，值统一为字符串；结构化值如每盘符回收站路径以 JSON 字符串存于单 key）。设置以 SQLite `settings` 表为真源，为回收站清理、定时扫描等能力提供配置真源（FR-24）。
- **错误**：`503` 设置服务未启用

### 写入运行期设置

- **方法 / 路径**：`PUT /api/settings`
- **请求**：
  ```json
  {
    "settings": {
      "scan_interval": "7200",
      "recycle_bin_paths": "{\"D\":\"D:/.recycle\"}"
    }
  }
  ```
- **响应**（200）：与 `GET /api/settings` 同结构，返回写入后的全部设置（回读结果）。
- **说明**：批量 upsert 键值，同一 key 覆盖旧值；写入在单事务内原子完成，提交成功后回读返回。
- **错误**：`400` 请求参数错误或 `settings` 为空，`503` 设置服务未启用，`500` 保存失败
