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
- **响应**（200）：
  ```json
  {
    "items": [
      {
        "id": 1,
        "path": "/media/movies",
        "type": "local",
        "label": "电影",
        "enabled": true
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
- **错误**：`400` 路径不可访问

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
- **说明**：通过 `file_path` 前缀匹配一次查询，Go 层按第一级子目录聚合分组

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

- **方法 / 路径**：`GET /api/play/hls/:id/1080p_segment_001.ts`
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
