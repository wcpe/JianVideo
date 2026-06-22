# 接口契约：轻量级单用户视频媒体服务器

> 对外接口的单一真源。始终原地更新到当前契约。

## 1. 通用约定

- **协议**：HTTP/HTTPS RESTful API
- **认证**：基于 Cookie 的会话认证，登录后返回 `Set-Cookie` 头部（HttpOnly `auth_token`）。除 `/api/auth/login`、`/api/auth/logout`、`/health` 及前端静态资源外，所有 `/api/*` 端点均强制校验 JWT（Cookie `auth_token` 或 `Authorization: Bearer <token>` 任一有效），未携带或无效凭据返回 `401`
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
  - `sort`：排序方式，`time_desc`（默认，按入库时间降序）/ `time_asc` / `name` / `media_time`（按媒体时间降序，缺失回退入库时间，FR-31）/ `media_time_asc`（按媒体时间升序）
  - `page`：页码
  - `page_size`：每页条数
  - `search`：搜索关键词（可选）
  - `favorite`：传 `true`/`1` 时仅返回已收藏媒体（可选，FR-41）
  - `tag_id`：传标签 ID 时仅返回打了该标签的媒体（可选，FR-41）
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
        "added_at": "2025-01-01T12:00:00Z",
        "media_time": "2024-12-25T08:30:00Z",
        "media_time_source": "exif",
        "camera": "Canon EOS R5",
        "lens": "RF24-70mm F2.8 L IS USM",
        "aperture": "f/2.8",
        "shutter": "1/200",
        "iso": 400,
        "gps_lat": 31.23,
        "gps_lon": 121.47
      }
    ],
    "total": 100,
    "page": 1,
    "page_size": 20
  }
  ```
- **说明**：仅返回未软删的媒体（`deleted_at IS NULL`）；已软删项见回收站接口（FR-25）。

### 获取媒体文件详情

- **方法 / 路径**：`GET /api/library/media/:id`
- **响应**（200）：媒体文件详情对象（含字幕轨道信息，以及 FR-44 的 `last_position`、`watched`、`last_watched_at`）

### 继续观看列表（FR-44）

- **方法 / 路径**：`GET /api/library/continue-watching`
- **查询参数**：
  - `limit`：返回条数上限，默认 `12`，超过 `50` 时收敛到 `50`
- **响应**（200）：
  ```json
  {
    "items": [
      {"id": 1, "file_name": "电影名.mkv", "duration": 7200.0, "last_position": 1234.5, "watched": false, "last_watched_at": "2025-01-01T12:00:00Z"}
    ]
  }
  ```
- **说明**：返回「有进度（`last_position>0`）且未看完（`watched=false`）」的媒体，按 `last_watched_at` 倒序，排除已删除记录，供首页「继续观看」区块展示。

### 重命名媒体文件

- **方法 / 路径**：`PUT /api/library/media/:id/rename`
- **请求**：
  ```json
  {"new_name": "新文件名.mp4"}
  ```
- **响应**（200）：更新后的媒体文件对象
- **说明**：`new_name` 仅允许单层文件名（不含 `/`、`\` 或 `.`/`..`）。后端先对磁盘文件原子改名，再更新 `media_files.file_path`/`file_name`/`format`，数据库更新失败时尽力回滚磁盘改名；旧缩略图按旧路径 hash 命名，重命名后失效并异步为新文件重新生成。SMB 远程文件暂不支持。
- **错误**：`400` 新名不合法或不支持的文件类型，`404` 媒体记录不存在，`409` 目标文件名已存在，`500` 重命名失败

### 修改显示名（FR-30）

- **方法 / 路径**：`PUT /api/library/media/:id/display-name`
- **请求**：
  ```json
  {"display_name": "我的影片"}
  ```
- **响应**（200）：更新后的媒体文件对象（含 `display_name`）
- **说明**：仅更新库内显示名（`media_files.display_name`），**不改动磁盘真实文件名与路径**；服务端对 `display_name` 去首尾空白后落库，空串表示清除显示名。列表/卡片/详情展示名优先用 `display_name`，为空时回退 `file_name`。需改磁盘真实文件名请改用「重命名媒体文件」端点。
- **错误**：`400` 请求体无效，`404` 媒体记录不存在，`500` 更新失败

### 软删除媒体文件（FR-25）

- **方法 / 路径**：`DELETE /api/library/media/:id`
- **响应**（204）：无响应体
- **说明**：软删除——仅置 `media_files.deleted_at = now`，不物理删除数据库记录、不删除磁盘源文件。软删后该媒体从常规列表（`GET /api/library/media`）与各库已索引计数中排除，进入回收站。重复删除已软删项返回 `500`（视为不存在）。
- **错误**：`400` ID 无效，`500` 删除失败（含记录不存在）

### 列出回收站（FR-25）

- **方法 / 路径**：`GET /api/library/recycle`
- **响应**（200）：`{"items": [...]}`，含全部已软删媒体（`deleted_at IS NOT NULL`），按软删时间倒序。
- **错误**：`500` 查询失败

### 还原媒体文件（FR-25）

- **方法 / 路径**：`POST /api/library/media/:id/restore`
- **响应**（204）：无响应体
- **说明**：清空 `media_files.deleted_at`，使媒体回到常规列表、从回收站消失。
- **错误**：`400` ID 无效，`404` 回收站中不存在该媒体文件

### 清理回收站（FR-26）

- **方法 / 路径**：`POST /api/library/recycle/cleanup`
- **响应**（200）：`{"moved": N, "failed": M}`，分别为成功移动并删记录的项数、移动失败跳过的项数。
- **说明**：对全部软删项，把磁盘源文件移动到其所在盘符对应的回收站目录（取自设置键 `recycle_bin_paths` 的 JSON 映射，盘符大小写不敏感），目标按删除日期分子目录 `<回收站目录>/<deleted_at 日期 YYYY-MM-DD>/<原文件名>`；移动成功后删除 `media_files` 记录。先移动成功、后删记录，保证「记录还在 = 文件未移出库」一致。
- **校验先行**：只要存在任一软删项所在盘符未配置回收站路径（含 SMB / 无盘符项），整体拒绝，不移动任何文件、不删任何记录。
- **错误**：`409` 存在盘符未配置回收站路径（message 含缺失盘符），`500` 配置非法 JSON 或清理失败，`503` 设置服务未启用

### 获取图片原始内容

- **方法 / 路径**：`GET /api/library/media/:id/raw`
- **响应**（200）：图片二进制内容，`Content-Type` 由文件后缀或内容探测确定
- **说明**：仅支持本地图片文件，用于前端预览；视频和 SMB 图片不走此接口。HEIC/RAW（cr2/nef/arw/dng/rw2 等）经外部 ImageMagick（`magick`）转成 JPEG 后返回，转换结果缓存于数据目录下 `image_cache/`（按「源路径 + 源修改时间」hash 命名，二次命中不重转）（FR-37）。
- **错误**：`400` 非图片或不支持的路径，`404` 记录或文件不存在，`503` 未安装 ImageMagick 无法转换 HEIC/RAW，`500` 图片转换失败

### 获取媒体缩略图

- **方法 / 路径**：`GET /api/library/thumbnail/:id`
- **响应**（200）：缩略图 JPEG 二进制内容（320px 宽，视频取第 2 秒帧、图片缩放）
- **说明**：缩略图在扫描入库时异步生成，存于数据目录下的 `thumbnails/`（按原始路径 hash 命名）。普通图片/视频经 ffmpeg 生成，HEIC/RAW 经外部 ImageMagick 生成（FR-37）。缩略图尚未生成时返回 `202` 并触发后台异步生成，前端可稍后重试。
- **错误**：`202` 缩略图生成中，`404` 媒体记录不存在

### 设置媒体收藏（FR-41）

- **方法 / 路径**：`PUT /api/library/media/:id/favorite`
- **请求**：
  ```json
  {"favorite": true}
  ```
- **响应**（200）：更新后的媒体文件对象（含 `favorite` 字段）
- **说明**：设置或取消收藏，重复设同值幂等。
- **错误**：`400` 请求体无效，`404` 媒体记录不存在

### 列出标签（FR-41）

- **方法 / 路径**：`GET /api/library/tags`
- **响应**（200）：`{"items": [{"id": 1, "name": "旅行", "created_at": "..."}]}`，按名升序

### 创建标签（FR-41）

- **方法 / 路径**：`POST /api/library/tags`
- **请求**：
  ```json
  {"name": "旅行"}
  ```
- **响应**（201）：标签对象。名按去首尾空白规整，同名复用已有标签。
- **错误**：`400` 标签名为空

### 列出媒体的标签（FR-41）

- **方法 / 路径**：`GET /api/library/media/:id/tags`
- **响应**（200）：`{"items": [...]}`，该媒体绑定的标签，按名升序

### 给媒体打标签（FR-41）

- **方法 / 路径**：`POST /api/library/media/:id/tags`
- **请求**：`{"tag_id": 1}` 或 `{"name": "旅行"}`（按名时先建/取标签再绑定）
- **响应**（201）：绑定的标签对象。重复打同标签幂等。
- **错误**：`400` 缺少 `tag_id`/`name`、标签名为空，或媒体/标签不存在

### 解除媒体标签（FR-41）

- **方法 / 路径**：`DELETE /api/library/media/:id/tags/:tag_id`
- **响应**（204）：无内容。绑定不存在视为幂等成功。

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
- **查询参数**：`mode`（可选）——`full` 全量扫描（遍历后对账已删文件），`incremental` 或缺省/非法值为增量更新（只索引新增）。
- **响应**（200）：
  ```json
  {"status": "scanning"}
  ```
- **说明**：按 `LibraryPath.type` 分发本地递归扫描或 SMB 扫描，识别内置图片/视频后缀和该目录绑定的自定义后缀；重复扫描不会重复入库。扫描在后台 goroutine 中异步执行，接口立即返回，不阻塞主线程；实际进度通过「扫描进度」SSE 端点获取，入库的媒体文件会异步生成缩略图。`mode=full` 时在入库后对账：库内未软删但源文件已不存在的记录标记软删进回收站（FR-27，复用 FR-25 软删，不物理删除、不动磁盘）；对账仅本地扫描启用。
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

### 列出相册

- **方法 / 路径**：`GET /api/albums`
- **响应**（200）：
  ```json
  {
    "items": [
      {"id": 1, "name": "旅行", "description": "2025 夏", "cover_media_id": 0, "created_at": "2026-06-21T08:00:00Z", "updated_at": "2026-06-21T08:00:00Z", "item_count": 3}
    ]
  }
  ```
- **说明**：按创建时间倒序返回全部相册，`item_count` 为相册内媒体成员数量。

### 创建相册

- **方法 / 路径**：`POST /api/albums`
- **请求**：
  ```json
  {"name": "旅行", "description": "可选描述"}
  ```
- **响应**（201）：新建的相册对象
- **错误**：`400` 名称为空或参数无效

### 删除相册

- **方法 / 路径**：`DELETE /api/albums/:id`
- **响应**（204）：空
- **说明**：仅在同一事务内删除相册与其全部成员关联（`albums` + `album_items`），**不删除源文件，也不删除 `media_files` 记录**。
- **错误**：`404` 相册不存在

### 查询相册成员

- **方法 / 路径**：`GET /api/albums/:id/items`
- **响应**（200）：
  ```json
  {"items": [ { "id": 1, "library_id": 1, "file_name": "a.mp4", "file_path": "D:/A/a.mp4" } ]}
  ```
- **说明**：返回相册内的媒体成员（`MediaFile` 列表），按加入顺序排列；成员可跨多个媒体库目录。

### 加入相册成员

- **方法 / 路径**：`POST /api/albums/:id/items`
- **请求**：
  ```json
  {"media_id": 123}
  ```
- **响应**（204）：空
- **说明**：把指定媒体加入相册；同一媒体重复加入幂等成功，不产生重复成员。
- **错误**：`404` 相册或媒体不存在

### 移出相册成员

- **方法 / 路径**：`DELETE /api/albums/:id/items/:mediaId`
- **响应**（204）：空
- **说明**：从相册移出指定媒体，仅删除成员关联，不影响源文件与媒体记录。

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

### 上报观看位置（FR-44）

- **方法 / 路径**：`PUT /api/play/:id/position`
- **请求**：
  ```json
  {"position": 123.4}
  ```
- **响应**（200）：更新后的媒体文件对象（含 `last_position`、`last_watched_at`）
- **说明**：持久化「用户观看位置」（秒）到 `media_files.last_position` 并刷新 `last_watched_at`，供下次进入同一视频续播。`position` 为负时归零。此为用户观看位置，**区别于**下方「获取转码状态」中的转码/缓冲进度，二者互不复用。
- **错误**：`400` 请求体无效，`404` 媒体记录不存在

### 标记已看（FR-44）

- **方法 / 路径**：`PUT /api/play/:id/watched`
- **响应**（200）：更新后的媒体文件对象（`watched=true`、`last_position=0`）
- **说明**：标记视频已看完，并清零续播位置（已看完不再续播），刷新 `last_watched_at`。
- **错误**：`404` 媒体记录不存在

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
