# 接口契约：轻量级单用户视频媒体服务器

> 对外接口的单一真源。始终原地更新到当前契约。

## 1. 通用约定

- **协议**：HTTP/HTTPS RESTful API
- **认证**：基于 Cookie 的会话认证，登录后返回 `Set-Cookie` 头部（HttpOnly `auth_token`）。除 `/api/auth/login`、`/api/auth/logout`、`/api/auth/setup-status`、`/api/auth/setup`、`/health` 及前端静态资源外，所有 `/api/*` 端点均强制校验 JWT（Cookie `auth_token` 或 `Authorization: Bearer <token>` 任一有效），未携带或无效凭据返回 `401`
- **编码**：请求/响应体使用 JSON（`Content-Type: application/json`），视频流使用 `video/mp2t`
- **分页**：列表接口支持 `page`（从 1 开始）和 `page_size`（默认 20，最大 100）参数
- **时间格式**：ISO 8601（`YYYY-MM-DDTHH:MM:SSZ`）
- **静态资源**：前端文件通过 `go:embed` 内嵌，由 `/` 路径提供服务
- **数据库迁移（FR2-017）**：当前切片不新增对外 HTTP 迁移端点。v0.20 到 v2 schema 升级在服务启动期由 `internal/migration` 执行；dry-run、备份校验、重入和校验能力先作为 Go 内部契约提供，供后续 CLI 或管理端点复用。
- **Space 头（FR2-007）**：`GET/POST /api/library` 下的媒体列表、详情、目录浏览、统计、扫描、标签、回收站、上传入口，以及 `/api/transcode/tasks`、`/api/tasks` 任务入口支持 `X-JianVideo-Space-Id: <space_id>`。缺失时使用默认 `space-default`；显式传入非法格式返回 `400 INVALID_SPACE`；显式传入不存在的 Space 返回 `404 SPACE_NOT_FOUND`。当前仅实现最小 owner 归属，不暴露成员/角色矩阵。

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

### v2 mock 先行 API client 契约（FR2-006）

本节描述 `packages/media-client` 与 `packages/mock` 当前对齐的 mock 先行契约，仅用于多端 client / wiki / mock 测试。请求携带 `X-JianVideo-Space-Id: <space_id>` 表示当前 Space，client 同时支持 `Authorization: Bearer <token>` 承接既有单用户 JWT，并支持可配置 timeout / retry。

- **方法 / 路径**：`GET /api/v2/media?page=1&page_size=20`
- **响应**（200）：
  ```json
  {
    "items": [
      {
        "id": "media-family-001",
        "space_id": "space-default",
        "title": "家庭素材 001",
        "kind": "video",
        "duration_seconds": 120,
        "created_at": "2026-07-01T10:00:00Z"
      }
    ],
    "page": 1,
    "page_size": 20,
    "total": 1
  }
  ```

- **方法 / 路径**：`GET /api/v2/media/:id`
- **响应**（200）：单个媒体对象，字段同列表项；若媒体不属于当前 Space，返回 `404 MEDIA_NOT_FOUND`。

- **方法 / 路径**：`GET /api/v2/tasks/:id`
- **响应**（200）：
  ```json
  {
    "id": "task-transcode-default",
    "space_id": "space-default",
    "type": "transcode",
    "status": "running",
    "priority": 10,
    "progress": 0.5,
    "error": null,
    "created_at": "2026-07-01T10:00:00Z",
    "updated_at": "2026-07-01T10:00:02Z"
  }
  ```
- **说明**：`status` 对齐 ADR-0055，取 `pending` / `running` / `succeeded` / `failed` / `canceled`；client 兼容 mock 或旧队列返回的 `completed` / `error`，分别映射为 `succeeded` / `failed`。

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

### 首次初始化状态（FR-109）

- **方法 / 路径**：`GET /api/auth/setup-status`（免登）
- **响应**（200）：`needs_setup` 为 `true` 表示系统尚无任何用户、需首次初始化
  ```json
  { "needs_setup": true }
  ```

### 首次初始化（FR-109）

- **方法 / 路径**：`POST /api/auth/setup`（免登；仅在系统无用户时可用）
- **请求**：
  ```json
  {
    "username": "string",
    "password": "string"
  }
  ```
- **响应**（200）：创建首个账户并自动登录（下发 `Set-Cookie`）
  ```json
  { "username": "string" }
  ```
- **错误**：`400` 参数错误；`409` 系统已初始化（已存在用户）

### 修改密码（FR-108）

- **方法 / 路径**：`POST /api/me/password`（需登录；用户取自认证上下文）
- **请求**：
  ```json
  {
    "old_password": "string",
    "new_password": "string"
  }
  ```
- **响应**（204）：密码已更新（立即生效）
- **错误**：`400` 参数错误；`401` 未登录或当前密码错误

### 获取媒体库目录列表

- **方法 / 路径**：`GET /api/library/paths`
- **请求头**：`X-JianVideo-Space-Id` 可选；缺省为 `space-default`
- **响应**（200）：每项含 `media_count`，即该库已索引（未软删）的媒体文件数量，供存储库卡片展示。
  ```json
  {
    "items": [
      {
        "id": 1,
        "path": "/media/movies",
        "type": "local",
        "library_kind": "movie",
        "library_profile_json": "{}",
        "label": "电影",
        "enabled": true,
        "media_count": 42
      }
    ]
  }
  ```

### 获取媒体库内容分型

- **方法 / 路径**：`GET /api/library/kinds`
- **响应**（200）：
  ```json
  {
    "items": [
      {
        "kind": "movie",
        "name": "电影",
        "description": "面向电影与长片，后续用于标题与年份解析。",
        "naming_hint": "片名 (年份)/片名.ext",
        "scan_strategy": "按文件与上级目录识别单片资源"
      }
    ]
  }
  ```
- **说明**：当前内置 `movie` / `series` / `home_video` / `mixed` 四类；旧库与缺省值均为 `mixed`。`type=local/smb` 仍只表示来源类型，不表示内容分型。

### 添加媒体库目录

- **方法 / 路径**：`POST /api/library/paths`
- **请求头**：`X-JianVideo-Space-Id` 可选；缺省为 `space-default`
- **请求**：
  ```json
  {
    "path": "\\\\192.168.1.100\\Share\\Movies",
    "type": "smb",
    "library_kind": "movie",
    "label": "NAS 电影",
    "smb_username": "optional",
    "smb_password": "optional"
  }
  ```
- **响应**（201）：目录记录对象
- **错误**：`400` 本地路径不可访问、不是目录、请求参数错误或 `library_kind` 非法；`500` 保存失败
- **说明**：`local` 路径必须在服务器本机存在且为目录；`smb` 路径支持 UNC 或 `smb://host/share/path` 输入，服务端统一存储为 `host/share/path`，凭据通过 `/api/smb/credentials` 保存。

### 更新媒体库目录

- **方法 / 路径**：`PUT /api/library/paths/:id`
- **请求头**：`X-JianVideo-Space-Id` 可选；缺省为 `space-default`
- **请求**：
  ```json
  {
    "label": "家庭视频",
    "enabled": true,
    "library_kind": "home_video"
  }
  ```
- **响应**（200）：更新后的目录记录对象
- **说明**：当前可更新展示标签、启用状态与内容分型，不修改目录真实路径或来源类型。成功更新会在同一事务内写入 `library.updated` 审计事件。
- **错误**：`400` ID、请求体或 `library_kind` 无效，`404` 目录不存在，`500` 更新失败

### 删除媒体库目录

- **方法 / 路径**：`DELETE /api/library/paths/:id`
- **请求头**：`X-JianVideo-Space-Id` 可选；缺省为 `space-default`
- **响应**（204）：空
- **说明**：成功删除会在同一事务内写入 `library.deleted` 审计事件。

### 浏览目录

- **方法 / 路径**：`GET /api/library/browse`
- **请求头**：`X-JianVideo-Space-Id` 可选；缺省为 `space-default`
- **查询参数**：
  - `parent_path`：父目录的**真实磁盘路径**（必填）。取哨兵值 `__root__` 时返回**各盘符 / 共享根**（FR-121）作为顶层目录项
  - `sort`：文件排序（可选）：`name` / `size` / `type` / `time`（修改时间），缺省 `name`；目录恒在文件前、按名排序
  - `library_id`：可选、已弃用——目录导航按真实路径**跨库聚合**，不再按库收窄（仍接受以向后兼容，被忽略）
- **真实路径树聚合（FR-121，[ADR-0046](adr/0046-realpath-tree-directory-browse.md) 取代 ADR-0037）**：按真实磁盘路径跨当前 Space 的所有启用库合并成单一目录树。浏览路径 P 时，子目录 = `media_files` 中 `space_id = ? AND file_path LIKE 'P/%'`（跨库、未软删）的下一级目录去重，文件 = 目录恰为 P 的项；有路径包含关系的库在公共上级自然合并（加 `D:\1` 与 `D:\1\2` → `D: → 1 → 2`；加 `D:\` 库则整盘可浏览）。
- **根响应**（200，`parent_path=__root__`）：`directories` 为各盘符 / 共享根，面包屑单段 `{"name":"全部","path":"__root__"}`，`files` 为空：
  ```json
  {
    "breadcrumbs": [{"name": "全部", "path": "__root__"}],
    "directories": [
      {"name": "D:", "path": "D:"},
      {"name": "E:", "path": "E:"}
    ],
    "files": []
  }
  ```
- **路径响应**（200，如 `parent_path=D:/1`）：子目录跨库合并、不带 `library_id`；文件各带自身 `library_id`，按 `sort` 排序：
  ```json
  {
    "breadcrumbs": [
      {"name": "D:", "path": "D:"},
      {"name": "1", "path": "D:/1"}
    ],
    "directories": [
      {"name": "2", "path": "D:/1/2"}
    ],
    "files": [
      {"id": 1, "library_id": 1, "file_name": "a.png", "file_path": "D:/1/a.png", "file_size": 2400000, "format": "png", "modified_at": "2026-06-20T10:00:00Z"}
    ]
  }
  ```
- **说明**：`file_path` 前缀匹配一次 Space scoped 查询，Go 层按下一级目录去重分组。Windows 盘符路径正斜杠规范化、面包屑不加前导 `/`（如 `D:/1`）。顶层盘符 / 共享根由当前 Space 各启用库 `path` 推导（本地 `D:/...`→`D:`、UNC `//host/share/...`→`//host/share`）去重。

### 获取媒体文件列表

- **方法 / 路径**：`GET /api/library/media`
- **请求头**：`X-JianVideo-Space-Id` 可选；缺省为 `space-default`
- **查询参数**：
  - `library_id`：按媒体库过滤（可选）
  - `sort`：排序方式，`time_desc`（默认，按入库时间降序）/ `time_asc` / `name` / `media_time`（按媒体时间降序，缺失回退入库时间，FR-31）/ `media_time_asc`（按媒体时间升序）
  - `page`：页码
  - `page_size`：每页条数
  - `cursor`：游标分页 token（可选）；用于时间倒序列表，旧 `page/page_size` 仍可用
  - `search`：搜索（可选）。走 everything 式表达式解析（FR-35）：裸词→文件名包含（多词 AND）；`ext:jpg` 或 `ext:jpg,png`→按扩展名；`type:image`/`type:video`→按类型；`size:>10mb`/`size:<=2gb`/`size:>=500kb`（单位 b/kb/mb/gb/tb）→按大小。无法识别的 `key:val` 退化为文件名关键词。纯文本与旧行为一致（向后兼容）。
  - `favorite`：传 `true`/`1` 时仅返回已收藏媒体（可选，FR-41）
  - `tag_id`：传标签 ID 时仅返回打了该标签的媒体（可选，FR-41）
  - 结构化筛选（可选，FR-35，显式参数优先于 `search` 表达式同名约束）：`type`（`image`/`video`）、`size_min`/`size_max`（字节）、`time_from`/`time_to`（媒体时间范围，`RFC3339` 或 `YYYY-MM-DD`，按 `COALESCE(media_time, added_at)` 比较）、`path`（目录前缀）。以上全部走参数化查询，无 SQL 注入面。
  - `has_gps`：传 `true` 时仅返回带 GPS 坐标（`gps_lat != 0 OR gps_lon != 0`）的媒体（可选，FR-39 照片地图）。
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
    "page_size": 20,
    "next_cursor": "eyJzb3J0X3RpbWUiOiIyMDI2LTA3LTA4VDAwOjAwOjAwWiIsImlkIjoxfQ"
  }
  ```
- **说明**：仅返回当前 Space 未软删的媒体（`space_id = ? AND deleted_at IS NULL`）；已软删项见回收站接口（FR-25）。旧 `page/page_size` 响应同步携带 `next_cursor`，旧前端可忽略该字段。

### 获取媒体文件详情

- **方法 / 路径**：`GET /api/library/media/:id`
- **请求头**：`X-JianVideo-Space-Id` 可选；缺省为 `space-default`
- **响应**（200）：媒体文件详情对象（含字幕轨道信息，以及 FR-44 的 `last_position`、`watched`、`last_watched_at`）
- **说明**：跨 Space 请求返回 `404 NOT_FOUND`，不回退默认 Space。

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

### 那年今日列表（FR-72）

- **方法 / 路径**：`GET /api/library/on-this-day`
- **查询参数**：
  - `limit`：返回条数上限，默认 `12`，超过 `50` 时收敛到 `50`
- **响应**（200）：
  ```json
  {
    "items": [
      {"id": 1, "file_name": "海边日落.jpg", "media_time": "2022-06-23T12:00:00Z"}
    ]
  }
  ```
- **说明**：返回「往年同一天」拍摄的媒体——`media_time` 非空、未软删，且其月-日等于服务器本地「今天」的月-日、年份不等于今年，按 `media_time` 倒序，供首页「那年今日」回忆区块展示。

### 记录媒体查看（FR-120）

- **方法 / 路径**：`PUT /api/library/media/:id/viewed`
- **响应**（200）：`{"ok": true}`（把该媒体 `last_viewed_at` 置为当前时间）
- **说明**：媒体在查看器 / 播放页被打开时由前端调用，记录最近查看时间。非法 id → 400，记录不存在 → 404。与 FR-44 的 `last_watched_at`（仅视频播放进度）不同，覆盖图片 + 视频的「打开」。

### 最近查看列表（FR-120）

- **方法 / 路径**：`GET /api/library/recently-viewed`
- **查询参数**：`limit`（缺省 12，上限 50）
- **响应**（200）：`{"items": [ MediaFile, ... ]}`
- **说明**：返回 `last_viewed_at` 非空、未软删的媒体，按 `last_viewed_at` 倒序，供时间轴页「最近查看」回忆区块展示。

### 观看统计（FR-75）

- **方法 / 路径**：`GET /api/library/stats`
- **请求头**：`X-JianVideo-Space-Id` 可选；缺省为 `space-default`
- **查询参数**：无
- **响应**（200）：
  ```json
  {
    "total": 42,
    "watched": 18,
    "unwatched": 24,
    "recent_timeline": [{"date": "2026-06-24", "count": 5}],
    "position_heatmap": [3, 1, 0, 2, 1, 4, 0, 1, 2, 6],
    "by_library": [{"library_id": 1, "label": "电影", "watched": 12}],
    "by_format": [{"format": "mp4", "watched": 11}],
    "top_viewed": [{"id": 11, "file_name": "热门片.mp4", "view_count": 5}]
  }
  ```
- **说明**：聚合当前 Space 的观看统计，各维度均仅统计未软删媒体。`watched`/`unwatched` 为看完/未看完计数；`recent_timeline` 按 `last_watched_at` 本地时区天分桶（倒序、最近 30 天有观看的天）；`position_heatmap` 为续播进度（`last_position/duration`，`duration>0`）落入 10 档（下标 0=0-10%…9=90-100%）的媒体数；`by_library`/`by_format` 为各库/各格式已看媒体数；`top_viewed` 为观看次数（`view_count`，看完一次 +1）Top 10。供观看统计页（`/stats`）展示。

### 媒体库概览汇总（FR-117）

- **方法 / 路径**：`GET /api/library/summary`
- **请求头**：`X-JianVideo-Space-Id` 可选；缺省为 `space-default`
- **查询参数**：无
- **响应**（200）：
  ```json
  {
    "total": 12480,
    "video_count": 3210,
    "image_count": 9270,
    "total_size": 1979900000000,
    "total_duration": 2311200.0,
    "library_count": 5,
    "by_library": [
      {"library_id": 1, "label": "电影", "media_count": 3460, "video_count": 3460, "image_count": 0, "total_size": 580000000000, "total_duration": 1980000.0}
    ]
  }
  ```
- **说明**：当前 Space 的媒体库总量聚合，供首页概览看板（`/`）展示。全部维度 `WHERE space_id = ? AND deleted_at IS NULL`；视频/图片按内置图片扩展名集合区分（`LOWER(format) IN 内置图片集` 为图片，否则视频，与媒体筛选口径一致）；`total_size`=`SUM(file_size)`（字节）、`total_duration`=`SUM(duration)`（秒）；`library_count` 取当前 Space 启用库数；`by_library` 按 `library_id` 分组（`label` 取自 `library_paths`）。空库返回各项为零、`by_library` 为空数组，HTTP 200。一次聚合查询完成，避免 N+1。

### 媒体增长趋势（FR-118）

- **方法 / 路径**：`GET /api/library/trends`
- **请求头**：`X-JianVideo-Space-Id` 可选；缺省为 `space-default`
- **查询参数**：无
- **响应**（200）：
  ```json
  {
    "media_added": [
      {"date": "2026-05-01", "count": 10, "size": 1000000000, "duration": 3000.0},
      {"date": "2026-05-03", "count": 5, "size": 500000000, "duration": 1500.0}
    ]
  }
  ```
- **说明**：按当前 Space 的 `added_at` 本地时区天分桶的「按天新增媒体」全时段序列，供统计页「媒体」tab 算累计增长曲线（媒体数 / 容量 / 时长）。仅含有新增的天、升序；`count`=当天新增数、`size`=`SUM(file_size)`、`duration`=`SUM(duration)`；全程 `space_id = ? AND deleted_at IS NULL`。空库返回 `{"media_added": []}`。观看活跃趋势复用 `GET /api/library/stats` 的 `recent_timeline`（不另设端点）。

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

### 编辑备注（FR-137）

- **方法 / 路径**：`PUT /api/library/media/:id/notes`
- **请求**：
  ```json
  {"notes": "现场实拍"}
  ```
- **响应**（200）：更新后的媒体文件对象（含 `notes`）
- **说明**：仅更新库内备注（`media_files.notes`），自由文本、不改动磁盘文件；服务端对 `notes` 去首尾空白后落库，空串表示清除备注。备注内容纳入基础裸词搜索（与 文件名/显示名/相机/镜头 同列 OR 匹配）。
- **错误**：`400` 请求体无效，`404` 媒体记录不存在，`500` 更新失败

### 软删除媒体文件（FR-25）

- **方法 / 路径**：`DELETE /api/library/media/:id`
- **响应**（204）：无响应体
- **说明**：软删除——仅置 `media_files.deleted_at = now`，不物理删除数据库记录、不删除磁盘源文件。软删后该媒体从常规列表（`GET /api/library/media`）与各库已索引计数中排除，进入回收站。重复删除已软删项返回 `500`（视为不存在）。
- **错误**：`400` ID 无效，`500` 删除失败（含记录不存在）

### 批量软删媒体文件（FR-69）

- **方法 / 路径**：`POST /api/library/media/batch-delete`
- **请求体**：`{"ids": [101, 102, ...]}`，待软删的媒体 ID 数组。
- **响应**（200）：`{"deleted": N}`，N 为实际软删条数。
- **说明**：批量软删——在单事务内筛出所有 `deleted_at IS NULL` 的有效 ID 后置 `deleted_at = now`，并逐项写 `media.deleted` 审计事件；复用单条软删语义（不动磁盘源文件），软删项一并进回收站。跳过不存在 / 已软删的 ID（不计入 `deleted`、不报错）；空 `ids` 为 no-op 返回 `{"deleted": 0}`。
- **错误**：`400` 请求体无效，`500` 批量删除失败

### 批量打包下载（FR-91）

- **方法 / 路径**：`GET /api/library/media/batch-download?ids=101,102,...`
- **查询参数**：`ids` 逗号分隔的媒体 ID 列表（忽略空白与非法项）。
- **响应**（200）：`application/zip` 二进制，`Content-Disposition: attachment`，附件名 `媒体打包.zip`（按 RFC 5987 `filename*=UTF-8''` 编码）。经 `archive/zip` 流式 chunked 输出、边写边 flush（不一次性读入内存），用请求 context 控制取消、不设整体超时。
- **说明**：将选中媒体的原文件逐个写入 zip（不转码 / 不转换）。`smb://` 路径项与磁盘不可访问项跳过（不写入 zip、不报错），故返回 zip 内文件数可能少于请求 ID 数。鉴权后访问（FR-13）；软删项不计入（FR-25）。
- **上限**：选中数量 ≤ 500、预估总大小 ≤ 5 GiB，超限在写任何字节前拒绝。
- **错误**：`400` 未提供有效 ids / 数量超限 / 总大小超限，`500` 查询失败

### 扫描相似重复哈希（FR-70）

- **方法 / 路径**：`POST /api/library/duplicates/scan`
- **响应**（200）：`{"computed": N}`，N 为本次新计算并落库的 dHash 条数。
- **说明**：为当前 Space 全部「未软删且 `dhash=0`」的媒体计算并持久化基于缩略图的 64 位感知哈希（dHash）；缩略图缺失时先同步生成一次再计算。已算过的天然跳过（幂等，二次调用通常 `computed=0`）。同步执行 + 有界并发；单条失败仅记日志跳过、不影响整体（故缺缩略图等情况返回 `computed` 偏小而非报错）。
- **错误**：`500` 扫描失败

### 内容哈希回填（FR2-061）

- **方法 / 路径**：`POST /api/library/file-hashes/backfill`
- **响应**（202）：`{"status": "queued", "task_id": "123"}`
- **说明**：为当前 Space 创建 FR2-037 通用任务 `library.file_hash_backfill`，异步回填缺失、过期或算法不一致的源文件 SHA-256。任务按 Space 幂等，未完成的同 Space 回填会复用同一幂等键；执行过程按批次写进度与 checkpoint，取消 / 重试复用 `/api/tasks/:id` 能力。单个源文件不可读或 SMB 暂不支持时只跳过该项并记日志，不写入错误 hash。
- **错误**：`503` 任务中心未启用，`500` 入队失败

### 查询精确重复组（FR2-061）

- **方法 / 路径**：`GET /api/library/duplicates/exact`
- **响应**（200）：`{"groups":[{"content_hash":"...","file_size":123,"items":[MediaFile, ...]}]}`，每组为相同 `file_size + content_hash` 的媒体集合，组内按 id 升序，组间按首成员 id 升序。
- **说明**：查询当前 Space 内已计算且有效的 SHA-256 内容哈希。后端先读 `media_hash_groups` 候选快照，再回连 `media_files` 复验并排除已软删、missing、`content_hash_stale=true`、hash 为空或算法非 `sha256` 的媒体；供重复项页「精确重复」tab 展示并选取多余项批量软删。
- **错误**：`500` 查询失败

### 查询相似重复组（FR-70）

- **方法 / 路径**：`GET /api/library/duplicates`
- **响应**（200）：`{"groups": [[MediaFile, ...], ...]}`，每个内层数组为一个近似重复组（成员两两汉明距离 ≤ 默认阈值 10、各组 ≥2 项），排除已软删项；组内按 id 升序、组间按首成员 id 升序。
- **说明**：基于当前 Space 已计算的 `dhash` 列聚类，供「重复项」页「相似重复」tab 展示并选取多余项批量清理（复用批量软删端点进回收站）。新入库媒体若尚未计算 dHash 需先调扫描端点。
- **错误**：`500` 查询失败

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

### 下载原文件

- **方法 / 路径**：`GET /api/library/media/:id/download`
- **响应**（200）：媒体原始文件二进制，`Content-Disposition: attachment`，文件名为真实 `file_name`（按 RFC 5987 `filename*=UTF-8''` 编码，兼容中文）。经流式回传，支持 HTTP Range（断点续传）。
- **说明**：图片与视频一视同仁，回传磁盘原始字节（不转码/不转换，区别于 raw 端点）。鉴权后访问（FR-13）；软删项不可下载（FR-25）。SMB 远程文件暂不支持（FR-42）。
- **错误**：`400` ID 无效或 `smb://` 路径，`404` 记录不存在/已软删或磁盘文件不可访问

### 获取媒体缩略图

- **方法 / 路径**：`GET /api/library/thumbnail/:id`
- **查询参数**：`size`（可选）缩略图宽度，受支持白名单 `160` / `320` / `640`，缺省或非白名单值回落默认 `320`（FR-81 P12）
- **响应**（200）：缩略图 JPEG 二进制内容（按 `size` 缩放，视频取第 2 秒帧、图片缩放；带透明区的源已合成中性灰底，FR-81 P1）
- **说明**：缩略图在扫描入库时异步生成，存于数据目录下的 `thumbnails/`（按原始路径 hash 命名；**默认 320 尺寸命名不变**，非默认尺寸用 `<hash>_<size>.jpg` 并存）。普通图片/视频经 ffmpeg 生成，HEIC/RAW 经外部 ImageMagick 生成（FR-37）。请求尺寸尚未生成时返回 `202` 并触发后台异步生成该尺寸，前端可稍后重试。
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
- **请求头**：`X-JianVideo-Space-Id` 可选；缺省为 `space-default`
- **响应**（200）：`{"items": [{"id": 1, "space_id": "space-default", "name": "旅行", "created_at": "..."}]}`，当前 Space 内按名升序

### 创建标签（FR-41）

- **方法 / 路径**：`POST /api/library/tags`
- **请求头**：`X-JianVideo-Space-Id` 可选；缺省为 `space-default`
- **请求**：
  ```json
  {"name": "旅行"}
  ```
- **响应**（201）：标签对象。名按去首尾空白规整，同一 Space 内同名复用已有标签。
- **错误**：`400` 标签名为空

### 列出媒体的标签（FR-41）

- **方法 / 路径**：`GET /api/library/media/:id/tags`
- **请求头**：`X-JianVideo-Space-Id` 可选；缺省为 `space-default`
- **响应**（200）：`{"items": [...]}`，该媒体绑定的标签，按名升序
- **错误**：跨 Space 媒体 ID 返回 `404 NOT_FOUND`

### 给媒体打标签（FR-41）

- **方法 / 路径**：`POST /api/library/media/:id/tags`
- **请求头**：`X-JianVideo-Space-Id` 可选；缺省为 `space-default`
- **请求**：`{"tag_id": 1}` 或 `{"name": "旅行"}`（按名时先建/取标签再绑定）
- **响应**（201）：绑定的标签对象。重复打同标签幂等。
- **错误**：`400` 缺少 `tag_id`/`name`、标签名为空，或媒体/标签不存在

### 解除媒体标签（FR-41）

- **方法 / 路径**：`DELETE /api/library/media/:id/tags/:tag_id`
- **请求头**：`X-JianVideo-Space-Id` 可选；缺省为 `space-default`
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
- **说明**：兼容旧端点；内部映射到媒体类型规则服务，返回内置后缀和绑定到该媒体库目录的自定义后缀；自定义后缀不会影响其他目录。

### 查询媒体类型与扫描规则（FR2-025）

- **方法 / 路径**：`GET /api/media-types`
- **请求头**：`X-JianVideo-Space-Id` 可选；缺省为 `space-default`
- **查询参数**：
  - `library_id`：可选；传入后返回该媒体库的全局规则 + 每库覆盖规则
  - `type`：可选；按 `video` / `image` / `audio` / `subtitle` / `sidecar` 过滤规则
- **响应**（200）：
  ```json
  {
    "types": [
      {
        "type": "video",
        "name": "视频",
        "description": "可播放、可转码的视频文件。",
        "default_extensions": ["mp4", "mkv"],
        "capabilities": ["scan", "transcode", "thumbnail", "metadata"]
      }
    ],
    "rules": [
      {
        "id": "builtin:video:mp4",
        "space_id": "space-default",
        "library_id": 1,
        "type": "video",
        "extension": "mp4",
        "label": "MP4 视频",
        "description": "mp4 可扫描、预览、转码并提取技术元数据的视频文件。",
        "enabled": true,
        "builtin": true,
        "capabilities": ["scan", "transcode", "thumbnail", "metadata"]
      }
    ]
  }
  ```
- **说明**：内置规则由代码 registry 生成，禁用内置项时写 override 记录；扫描、列表 `type=` 筛选、统计、缩略图与上传校验共用该规则口径。

### 新增媒体类型规则（FR2-025）

- **方法 / 路径**：`POST /api/media-types/rules`
- **请求头**：`X-JianVideo-Space-Id` 可选；缺省为 `space-default`
- **请求**：
  ```json
  {"library_id": 1, "type": "image", "extension": ".rawx", "label": "RAWX 图片", "description": "自定义相机格式"}
  ```
- **响应**（201）：规则对象。
- **说明**：`extension` 规范化为不带点的小写后缀；`library_id` 省略表示全局规则。配置变更写入审计事件，不自动全库重扫。
- **错误**：`400` 目录不存在、媒体类型不支持或后缀格式不支持。

### 更新媒体类型规则（FR2-025）

- **方法 / 路径**：`PUT /api/media-types/rules/:id`
- **请求头**：`X-JianVideo-Space-Id` 可选；缺省为 `space-default`
- **请求**：`{"enabled": false}`，可选字段还包括 `label`、`description`、`library_id`
- **响应**（200）：更新后的规则对象。
- **说明**：内置规则 ID 形如 `builtin:video:mp4`；内置规则不可物理删除，但可通过 `enabled=false` 禁用扫描。

### 删除媒体类型规则（FR2-025）

- **方法 / 路径**：`DELETE /api/media-types/rules/:id`
- **响应**（204）：空
- **说明**：仅自定义规则可删除；删除内置规则返回 `400`，应改用禁用。

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
- **说明**：兼容旧端点；`type` 仅允许 `video` 或 `image`；后缀会规范化为小写且去掉前导点。内置后缀无需入库，重复添加视为幂等成功。
- **错误**：`400` 参数无效、目录不存在或后缀格式不支持

### 删除媒体库自定义后缀（FR-64）

- **方法 / 路径**：`DELETE /api/library/extensions`
- **查询参数**：
  - `library_id`：媒体库 ID（必填）
  - `extension`：要删除的后缀（必填，规范化为小写、去前导点后匹配）
- **响应**（204）：空
- **说明**：兼容旧端点；仅删除自定义后缀；**内置后缀不可删**（请求删除内置后缀返回 400）。删除不存在的自定义后缀返回 400。
- **错误**：`400` 无效 `library_id`、空 `extension`、尝试删除内置后缀或后缀不存在

### Web 上传媒体入库

- **方法 / 路径**：`POST /api/library/upload`
- **请求**：`multipart/form-data`
  - `file`（必填）：上传的图片或视频文件。
  - `target_dir`（可选）：临时指定的落盘目录；缺省取设置 `upload_target_dir`。须为某个已注册启用的本地库目录或其子目录。
  - `naming_rule`（可选）：命名规则，`original`（保留原样，直接落目标目录）或 `date`（按媒体时间分 `YYYY/MM` 子目录整齐归档）；缺省取设置 `upload_naming_rule`，再缺省按 `original`。
- **响应**（200）：
  ```json
  {"status": "uploaded", "library_id": 3, "file_path": "D:/media/photos/2024/06/IMG_001.jpg", "scan_task": 12}
  ```
- **说明**（FR-149，见 [ADR-0051](adr/0051-upload-ingestion.md)）：文件经后端流式落盘到目标位置后，自动入队该库增量扫描入库（保持「磁盘文件为入库真源」，`media_files` 仍由扫描填充，`scan_task` 为触发的扫描任务 ID，未启用队列时为 `0`）。目标目录经校验须在已注册本地库目录内（防越权写库外，`..` 逃逸与 SMB 库均拒绝）；仅接受图片/视频（按目标库扩展名策略，含自定义后缀）；重名按 `名(N).ext` 递增避让。单文件大小上限 2 GiB。
- **错误**：`400` 缺少文件 / 未指定目标位置且无默认 / 目标不在库内 / 非图片或视频 / 文件名非法；`500` 落盘或保存失败；`503` 设置服务未启用

### 扫描媒体库目录

- **方法 / 路径**：`POST /api/library/scan/:id`
- **请求头**：`X-JianVideo-Space-Id` 可选；缺省为 `space-default`
- **查询参数**：`mode`（可选）——`full` 全量扫描（遍历后分批对账缺失文件），`incremental` 或缺省/非法值为增量更新（只处理新增或变更路径）。
- **响应**（200）：
  ```json
  {"status": "queued", "task_id": 12}
  ```
- **说明**：触发扫描会先按当前 Space 查找媒体库目录；跨 Space 的目录 ID 返回 404，不入队、不回退默认 Space。成功后建一个 `pending` 扫描任务入队（FR-29），由单 worker 串行执行，接口立即返回任务 ID（未启用队列时回退直接异步扫描，返回 `{"status":"scanning"}`）。多次触发按入队顺序排队、不并发抢资源。worker 按 `LibraryPath.type` 分发本地递归扫描或 SMB 扫描，识别内置图片/视频后缀和该目录绑定的自定义后缀；重复扫描按 `space_id + library_id + file_path` 去重，不会重复入库，missing 记录源文件重新出现时恢复为 available，入库的媒体文件会异步生成缩略图。可选查询参数 `mode`（`full`/`incremental`，缺省增量，向后兼容，FR2-027）：`full` 在入库后分批对账——当前 Space 库内未软删且 active 的记录若源文件已不存在，标记 `file_state=missing` 并从常规列表隐藏，不进入回收站、不物理删除、不动磁盘；用户显式删除仍走回收站软删。对账仅本地扫描启用，SMB 轮询为增量语义。当前进行中任务的实时进度通过「扫描进度」SSE 端点获取，任务列表通过「扫描任务列表」端点获取。
- **错误**：`400` ID 无效，`404` 目录不存在，`500` 入队失败

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
- **说明**：`status` 取值 `idle` / `scanning` / `completed` / `error`；服务端仍单 worker 扫描，但响应只暴露当前 Space 的进度视图，其他 Space 的扫描返回 `idle` 视图。前端据此渲染进度条，`completed` 后自动刷新媒体列表。

### 扫描任务列表（FR-29）

- **方法 / 路径**：`GET /api/library/scan/tasks`
- **请求头**：`X-JianVideo-Space-Id` 可选；缺省为 `space-default`
- **响应**（200）：
  ```json
  {
    "tasks": [
      {
        "id": 12,
        "space_id": "space-default",
        "library_id": 1,
        "scan_type": "full",
        "status": "running",
        "scanned_files": 30,
        "total_files": 120,
        "payload_json": "{\"kind\":\"library\",\"path\":\"D:/Videos\",\"dir_type\":\"local\"}",
        "error": "",
        "created_at": "2026-06-22T20:00:00Z",
        "started_at": "2026-06-22T20:00:01Z",
        "completed_at": null
      }
    ],
    "current": { "id": 12, "status": "running", "...": "同上" }
  }
  ```
- **说明**：返回全部扫描任务（按入队时间倒序）与当前进行中任务 `current`（无则 `null`）。`status` 取值 `pending` / `running` / `completed` / `error`，`scan_type` 取值 `full` / `incremental`。队列以 SQLite `scan_tasks` 表为持久化真源，由单 worker 串行执行；手动/定时扫描 payload 记录库路径与类型，watcher 事件 payload 记录 `ScanChange`。服务重启时把残留 `running` 任务重置为 `pending`，从 payload 或 `library_paths` 还原执行目标后重新入队。当前 `running` 任务的 `scanned_files`/`total_files` 用实时全局扫描状态覆盖，已完成任务返回其持久化进度。前端页眉据此常驻展示进行中任务并可点开看任务列表与各自进度。

### 通用任务中心（FR2-037）

- **方法 / 路径**：`GET /api/tasks`
- **请求头**：`X-JianVideo-Space-Id` 可选；缺省为 `space-default`
- **查询参数**：`status`、`type`、`resource_type`、`resource_id`、`page`、`page_size` 可选；Space scoped 查询默认不返回 `scope=system` 任务。
- **响应**（200）：
  ```json
  {
    "items": [
      {
        "id": 12,
        "scope": "space",
        "space_id": "space-default",
        "type": "library.scan",
        "status": "running",
        "priority": 0,
        "attempts": 1,
        "max_attempts": 3,
        "progress": 0.42,
        "checkpoint": "D:/Videos/a.mp4",
        "resource_type": "library",
        "resource_id": "1",
        "error": "",
        "created_at": "2026-07-09T03:00:00Z",
        "updated_at": "2026-07-09T03:00:05Z",
        "started_at": "2026-07-09T03:00:01Z",
        "finished_at": null,
        "next_run_at": null
      }
    ],
    "page": 1,
    "page_size": 20,
    "total": 1
  }
  ```
- **方法 / 路径**：`GET /api/tasks/stats`
- **查询参数**：同列表过滤项。
- **响应**（200）：`{ "total": 3, "by_status": { "pending": 1, "running": 1, "succeeded": 1, "failed": 0, "canceled": 0 }, "by_type": { "library.scan": 2, "transcode.hls": 1 } }`
- **方法 / 路径**：`GET /api/tasks/:id`
- **响应**（200）：单个任务对象，字段同列表项；跨 Space 或不存在返回 `404`。
- **方法 / 路径**：`POST /api/tasks/:id/cancel`
- **响应**（200）：取消后的任务对象。仅 `pending` / `running` 可取消；命中旧扫描 / 转码镜像任务时，先取消旧队列真源再同步通用任务镜像。
- **方法 / 路径**：`POST /api/tasks/:id/retry`
- **响应**（200）：重试后的任务对象。仅 `failed` / `canceled` 可重试；命中旧扫描 / 转码镜像任务时，先重试旧队列真源再同步通用任务镜像。
- **说明**：通用状态只使用 `pending` / `running` / `succeeded` / `failed` / `canceled`。旧扫描 / 转码 API 仍可返回 `completed` / `error`，但统一任务 API 与 `packages/media-client` 均映射为 `succeeded` / `failed`。

### 触发媒体健康巡检（FR-73）

- **方法 / 路径**：`POST /api/library/health/scan`
- **响应**（200）：`{"status": "scanning"}`，已有巡检在跑时返回 `{"status": "already_running"}`
- **响应**（503）：未启用健康巡检服务时返回 `{"code": "HEALTH_UNAVAILABLE", ...}`
- **说明**：触发一轮后台只读巡检，遍历全部未软删媒体逐项判 0 字节 / 源文件丢失（排除 `smb://`）/ 视频损坏（ffprobe）/ 缩略图无法生成，问题写入 `media_health_issues` 表（每轮先清空再写当轮快照）。单飞执行，已有巡检在跑时不并发第二轮。**全程只读、绝不改 `deleted_at`**（软删真源归 FR-25/27）。进度经「健康巡检进度」端点查询，问题清单经「健康问题清单」端点查询。

### 健康巡检进度（FR-73）

- **方法 / 路径**：`GET /api/library/health/status`
- **响应**（200）：
  ```json
  {
    "status": "completed",
    "total": 1200,
    "checked": 1200,
    "issue_count": 7,
    "error": "",
    "started_at": "2026-06-23T20:00:00Z",
    "completed_at": "2026-06-23T20:01:30Z"
  }
  ```
- **说明**：`status` 取值 `idle` / `scanning` / `completed` / `error`。`total` 为待巡检的未软删媒体总数，`checked` 为已巡检数（巡检中渐增），`issue_count` 为本轮发现的问题数。前端据此渲染进度并在完成后刷新问题清单。

### 健康问题清单（FR-73）

- **方法 / 路径**：`GET /api/library/health/issues`
- **响应**（200）：
  ```json
  {
    "items": [
      {
        "id": 3,
        "media_id": 42,
        "issue_type": "missing",
        "detail": "stat D:/v/gone.mp4: no such file or directory",
        "checked_at": "2026-06-23T20:01:30Z",
        "file_name": "gone.mp4",
        "file_path": "D:/v/gone.mp4",
        "library_id": 1,
        "display_name": ""
      }
    ]
  }
  ```
- **说明**：返回最近一轮巡检的问题清单，按 `issue_type` 再按 `media_id` 排序。`issue_type` 取值 `broken`（视频损坏） / `zero_byte`（0 字节） / `missing`（源文件丢失） / `no_thumbnail`（缩略图无法生成），`detail` 为问题细节（如 ffprobe / 缩略图错误尾部）。每项附带媒体基本信息（`file_name`/`file_path`/`library_id`/`display_name`，媒体已被删除时可能缺省），供前端按类型分组直接展示。删除问题媒体复用「批量软删媒体文件」端点。

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

### 编码协商（FR-53）

- **方法 / 路径**：`POST /api/play/:id/negotiate`
- **请求**：客户端上报各高级编码的解码能力（来自前端 `MediaSource.isTypeSupported` 探测）。
  ```json
  {"client_caps": {"h265": true, "av1": true, "vp9": false}}
  ```
  - 请求体可缺省，缺省视为无高级编码能力（兜底 H.264）。
- **响应**（200）：播放描述符。后端按「首选优先级（FR-50）∩ 客户端能力 ∩ 实测可产出（FR-49）」协商出实际编码与播放路径。
  - H.264（含协商不出高级编码的所有情形）→ TS 路径：
    ```json
    {"codec": "h264", "path": "ts", "url": "/api/play/hls/1/master"}
    ```
  - 高级编码（h265/av1/vp9）→ 后端同步产出 fMP4（FR-51），返回 fMP4 描述符：
    ```json
    {
      "codec": "av1",
      "path": "fmp4",
      "url": "/api/play/hls/1/index.m3u8",
      "mime": "video/mp4; codecs=\"av01.0.05M.08\"",
      "fallback_url": "/api/play/1/stream"
    }
    ```
- **说明**：`url` 为相对路径，前端绝对化后交自适应播放器（H.264 走 mpegts.js、高级编码走 hls.js 原生 MSE）。非 H.264 时若 fMP4 产出失败，**降级返回 H.264/TS 描述符**（不报错，保证可播）。协商结果（实际编码与路径）记录到内存播放会话。
- **错误**：`404` 媒体文件不存在。

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
- **响应**（200）：以编码器实测为真源、per-codec 表达（见 [ADR-0033](adr/0033-hwaccel-probe-source-cache.md)）。
  ```json
  {
    "available": [
      {
        "name": "AMD AMF", "family": "amf", "device_type": "d3d11va", "available": true,
        "codecs": [
          { "codec": "h264", "encoder": "h264_amf", "compiled": true, "tested_ok": true },
          { "codec": "h265", "encoder": "hevc_amf", "compiled": true, "tested_ok": true },
          { "codec": "av1",  "encoder": "av1_amf",  "compiled": true, "tested_ok": false }
        ]
      },
      {
        "name": "软件编码", "family": "software", "device_type": "", "available": true,
        "codecs": [
          { "codec": "h264", "encoder": "libx264",   "compiled": true, "tested_ok": true },
          { "codec": "av1",  "encoder": "libsvtav1", "compiled": true, "tested_ok": true }
        ]
      }
    ],
    "preferred": "h264_amf",
    "codecs": ["h264", "h265", "av1", "vp9"],
    "intel_gpu": false,
    "intel_gpu_detail": "",
    "software_fallback": false,
    "from_cache": true,
    "ffmpeg_version": "ffmpeg version 7.1 ...",
    "tested_at": "2026-06-23T10:00:00Z"
  }
  ```
- **说明**：`available` 为各硬件/软件家族的 per-codec 实测能力，家族 `available` = 至少一编码 `tested_ok`；`preferred` 为转码默认 H.264 编码器（保证 mpegts.js 可播）；`codecs` 为系统可输出编码并集；`from_cache`/`ffmpeg_version`/`tested_at` 标示实测来源；冷态（从未实测）`available` 为空、`preferred` 为 `libx264`、`tested_at` 为空。

### 列出转码预设（FR-77）

- **方法 / 路径**：`GET /api/transcode/presets`
- **响应**（200）：`{ "items": [ { "id", "name", "codec", "width", "height", "created_at", "updated_at" } ] }`，按创建时间倒序。`width`/`height` 为 `0` 表示沿用源分辨率。未注入预设服务返回 `503`。

### 创建转码预设（FR-77）

- **方法 / 路径**：`POST /api/transcode/presets`
- **请求体**：`{ "name": "1080p HEVC", "codec": "h265", "width": 1920, "height": 1080 }`。`codec` 取 `h264`/`h265`/`av1`/`vp9`（`hevc` 视为 `h265`）。
- **响应**：`201` 返回创建的预设；空名 / 不支持编码 / 负分辨率返回 `400`（`code=INVALID_PRESET`）。

### 更新转码预设（FR-77）

- **方法 / 路径**：`PUT /api/transcode/presets/:id`
- **请求体**：同创建。**响应**：`200` 返回更新后的预设；预设不存在 `404`、校验失败 `400`。

### 删除转码预设（FR-77）

- **方法 / 路径**：`DELETE /api/transcode/presets/:id`
- **响应**：`204`；预设不存在 `404`。已预生成的切片缓存不受影响。

### 加入预生成队列（FR-77）

- **方法 / 路径**：`POST /api/transcode/tasks`
- **请求头**：`X-JianVideo-Space-Id` 可选；缺省为 `space-default`
- **请求体**：`{ "media_id": 42, "preset_id": 1 }`。
- **响应**（200）：`{ "status": "queued", "task_id": 7 }`。按预设快照编码入当前 Space 的预生成队列、单 worker 串行预转码切片预热首播。媒体或预设不存在返回 `404`；未注入预生成服务返回 `503`。

### 列出预生成任务（FR-77）

- **方法 / 路径**：`GET /api/transcode/tasks?status=`
- **请求头**：`X-JianVideo-Space-Id` 可选；缺省为 `space-default`
- **查询参数**：`status`（可选）取 `pending`/`running`/`completed`/`error`，非空时仅返回该状态任务。
- **响应**（200）：`{ "tasks": [ { "id", "space_id", "media_id", "preset_id", "codec", "width", "height", "status", "error", "created_at", "started_at", "completed_at" } ] }`，当前 Space 内按入队时间倒序。

### 系统信息

- **方法 / 路径**：`GET /api/system/info`
- **响应**（200）：
  ```json
  {
    "app_version": "0.3.0",
    "os": "linux", "arch": "amd64", "num_cpu": 8, "hostname": "nas01",
    "go_version": "go1.22.5",
    "ffmpeg": { "available": true, "path": "/opt/jianvideo/ffmpeg", "version": "ffmpeg version 6.1.1 ..." },
    "runtime": {
      "pid": 12345, "work_dir": "/opt/jianvideo", "executable": "/opt/jianvideo/jianvideo",
      "db_path": "/opt/jianvideo/data/jianvideo.db", "uptime_seconds": 3661,
      "mem_alloc": 12582912, "mem_sys": 50331648, "num_gc": 7, "gomaxprocs": 8
    },
    "hwaccel": { "available": [], "preferred": "libx264", "codecs": [], "intel_gpu": false, "intel_gpu_detail": "", "software_fallback": true, "from_cache": false, "ffmpeg_version": "", "tested_at": "" }
  }
  ```
- **说明**：`hwaccel` 复用 `GET /api/transcode/hwaccel` 的 per-codec 结构（上例为冷态，未实测）；`app_version` 由构建期 `-ldflags -X main.version` 注入。`runtime`（FR-60）为进程与 Go 运行时信息——`pid`/`work_dir`/`executable`（`os` 包）、`db_path`/`uptime_seconds`（由 main 注入数据库路径与启动时刻派生，未注入时分别为空串 / 0）、`mem_alloc`/`mem_sys`/`num_gc`（`runtime.MemStats`，字节）、`gomaxprocs`（`runtime.GOMAXPROCS(0)`）；全部来自标准库，系统级总内存 / 磁盘可用不在此列（见 `GET /api/system/metrics` 系统指标时序，FR-119）。

### 系统指标时序（FR-119）

- **方法 / 路径**：`GET /api/system/metrics`
- **查询参数**：`range`（`1h` / `24h` / `7d`，缺省 `24h`）
- **响应**（200）：
  ```json
  {
    "range": "24h",
    "points": [
      {"t": "2026-06-27T10:00:00Z", "cpu_percent": 38.2, "mem_used_bytes": 195000000, "mem_sys_bytes": 536870912, "disk_used_bytes": 1979900000000, "disk_total_bytes": 2600000000000, "transcode_active": 2, "goroutines": 120}
    ],
    "current": {"cpu_percent": 41.0, "mem_used_bytes": 198000000, "mem_sys_bytes": 536870912, "disk_used_bytes": 1979900000000, "disk_total_bytes": 2600000000000, "transcode_active": 2, "goroutines": 122}
  }
  ```
- **说明**：系统监控页（`/monitor`）的指标时序。后台采样器每 15s 采一行（系统 CPU% 经 gopsutil、进程内存经 `runtime`、数据盘用量经 gopsutil、转码并发为活跃会话数、goroutine 数）写入 SQLite `metric_samples`，按 7 天保留期裁剪。本端点按 `range` 选窗口与桶大小**下采样**（`1h`→60s、`24h`→300s、`7d`→1800s，`GROUP BY` 时间桶 + AVG/MAX）使点数有界。`current` 为最新一条原始样本（供当前值卡）；刚启动无样本时 `points` 为空数组、`current` 为 `null`。机制见 ADR-0044。

### 编解码器实测

- **方法 / 路径**：`POST /api/system/codec-test`（可选查询参数 `?force=true` 强制重测）
- **响应**（200）：
  ```json
  {
    "ffmpeg_available": true,
    "results": [
      { "encoder": "libx264", "family": "software", "codec": "h264", "compiled": true, "tested_ok": true, "detail": "" },
      { "encoder": "h264_amf", "family": "amf", "codec": "h264", "compiled": true, "tested_ok": true, "detail": "" },
      { "encoder": "av1_amf", "family": "amf", "codec": "av1", "compiled": true, "tested_ok": false, "detail": "<stderr 尾部>" }
    ],
    "from_cache": true,
    "ffmpeg_version": "ffmpeg version 7.1 ...",
    "tested_at": "2026-06-23T10:00:00Z"
  }
  ```
- **说明**：对候选编码器（软件 + QSV/VAAPI/NVENC/AMF/VideoToolbox/Vulkan 的 H.264/H.265/AV1/VP9）用外部 ffmpeg 跑一小段试编码（`-f lavfi … -f null`）。`compiled` 表示是否编入当前 ffmpeg，`tested_ok` 表示试编码是否成功。**默认读按 ffmpeg 版本持久化的缓存即时返回**（`from_cache:true`），`?force=true` 强制重测覆盖缓存（`from_cache:false`，逐个试编码可能耗时数分钟）。ffmpeg 不可用时返回 `ffmpeg_available:false` 且 `results` 为空。结果与 `GET /api/transcode/hwaccel` 同源（见 [ADR-0033](adr/0033-hwaccel-probe-source-cache.md)）。

### 查看环境变量（FR-56）

- **方法 / 路径**：`GET /api/system/env`
- **响应**（200）：
  ```json
  {
    "env": [
      { "key": "JIANVIDEO_FFMPEG_PATH", "description": "ffmpeg 可执行文件路径，未设置时回退同目录捆绑版或 PATH", "sensitive": false, "set": true, "value": "/opt/jianvideo/ffmpeg" },
      { "key": "JWT_SECRET", "description": "JWT 签名密钥，未设置时启动随机生成（重启后需重新登录）", "sensitive": true, "set": true, "value": "****（已设置）" },
      { "key": "SMB_MASTER_PASSWORD", "description": "SMB 凭据加解密主密码，未设置则 SMB 凭据功能不可用", "sensitive": true, "set": false, "value": "（未设置）" }
    ]
  }
  ```
- **说明**：只读返回项目已知环境变量清单（涵盖根 config 的 `SERVER_PORT`/`DB_PATH`/`JWT_SECRET` 与 `internal/config` 的 `JIANVIDEO_*` 两套来源）。`set` 表示是否已设置。**敏感项（`JWT_SECRET`、`SMB_MASTER_PASSWORD`）绝不回显明文**——`value` 固定为掩码（已设置 `****（已设置）`、未设置 `（未设置）`），只暴露 `set` 布尔；非敏感项 `value` 为明文。env 为进程级，本端点只查看不修改。

### 检测 FFmpeg 路径（FR-56）

- **方法 / 路径**：`POST /api/system/ffmpeg/detect`
- **请求**：
  ```json
  { "path": "D:/tools/ffmpeg.exe" }
  ```
- **响应**（200）：
  ```json
  { "ffmpeg_available": true, "ffmpeg_version": "ffmpeg version 6.1.1 ..." }
  ```
- **说明**：对指定路径跑 `path -version` 验证是否可用（`path` 可省/空 = 测当前已配置路径）。**仅探测、不改写运行期全局路径**，供用户保存前先验路径。不可用时返回 `ffmpeg_available:false` 且 `ffmpeg_version` 为空串。持久化路径走 `PUT /api/settings`（键 `ffmpeg_path`/`ffprobe_path`），保存后即时应用到运行期。

### 测试代理连通性（FR-89）

- **方法 / 路径**：`POST /api/system/proxy/test`
- **请求**：
  ```json
  { "proxy": "http://host:port" }
  ```
- **响应**（200）：
  ```json
  { "reachable": true, "detail": "HTTP 200", "latency_ms": 123, "target": "https://api.github.com" }
  ```
- **说明**：用**临时 `http.Client`** 经待测代理（`proxy` 可省/空 = 测直连）对默认目标 `https://api.github.com`（与自更新出站目标一致，FR-46）发一次轻量 GET 探测连通性。请求体可选 `target` 覆盖探测目标，供 FR2-022 下载源连通性预检。**仅探测、绝不改写运行期全局代理真源**（`netproxy.current`），与「保存即生效」的 `PUT /api/settings`（键 `network_proxy`，FR-80）解耦，供用户保存前先验。只要 HTTP 层拿到任意响应（含 4xx）即 `reachable:true`、`detail` 为 `HTTP <状态码>`；网络层错误（连不上代理 / 目标、超时）`reachable:false`、`detail` 为脱敏后的原因。**代理 URL 含 userinfo 凭据时返回与日志一律脱敏，绝不回显明文**（安全红线）。整体超时约 10s。

### 外部工具下载（FR2-022）

- **方法 / 路径**：`GET /api/system/tools`
- **响应**（200）：
  ```json
  {
    "items": [
      { "tool": "ffmpeg", "setting_key": "ffmpeg_path", "configured_path": "D:/data/tools/ffmpeg/fr2-022-e2e/bin/ffmpeg.exe", "installed": [] }
    ]
  }
  ```
- **说明**：返回 ffmpeg、ffprobe、ImageMagick `magick` 的当前配置路径与受控工具目录内已安装版本。

- **方法 / 路径**：`GET /api/system/tools/sources`
- **响应**（200）：
  ```json
  {
    "sources": [
      { "id": "ffmpeg-source-8.1.2", "tool": "ffmpeg", "platform": "source", "arch": "all", "version": "8.1.2", "url": "https://ffmpeg.org/releases/ffmpeg-8.1.2.tar.gz", "sha256": "", "size": 0, "label": "FFmpeg 官方源码包" }
    ]
  }
  ```
- **说明**：返回内置工具源元数据。缺少 `sha256` 的源只能展示，不能自动下载。

- **方法 / 路径**：`POST /api/system/tools/download`
- **请求**：
  ```json
  { "tool": "ffmpeg", "source_id": "", "custom_url": "http://127.0.0.1:18022/tool.tar.gz", "sha256": "<64位sha256>", "version": "fr2-022-e2e", "allow_insecure_http": true }
  ```
- **响应**（202）：
  ```json
  { "status": "queued", "task_id": "42" }
  ```
- **说明**：创建系统级 `tool.download` 任务。自定义 URL 必须提供合法 SHA-256；默认只接受 HTTPS，HTTP 仅允许本机测试源且需显式 `allow_insecure_http=true`。任务下载到受控临时目录后校验 SHA-256，安全解压 zip/tar.gz（拒绝路径穿越、symlink、hardlink 与非普通文件），探测 `-version` 成功后安装到数据目录 `tools/<tool>/<version>/` 并写入对应运行期设置。状态、进度、取消与重试复用 `/api/tasks`。

### 自更新下载进度（FR-90）

- **方法 / 路径**：`GET /api/system/update/progress`
- **响应**（200）：
  ```json
  { "state": "downloading", "downloaded": 6291456, "total": 12582912, "percent": 50 }
  ```
- **说明**：供前端在自更新（`POST /api/system/update/apply`，FR-46）进行中轮询展示下载进度条。`state` 取 `idle`（空闲，未在更新）/`downloading`（下载二进制中）/`verifying`（校验 sha256 中）/`done`（替换已触发，进程即将重启）/`failed`（下载或校验失败）。`downloaded`/`total` 为字节数，`total` 取响应 `Content-Length`，**为 0 表示总字节未知**（响应无 `Content-Length`），此时 `percent` 为 0、前端退化为展示已下载字节。进度为**进程内单例**（互斥量保护、不落库，自更新本就用户显式触发、单次互斥），无外部依赖恒可用；服务重启后归零为 `idle`。鉴权随 `/api/*` 的 APIGuard。

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
- **说明**：返回全部已登记的运行期设置（key → value，值统一为字符串；结构化值如每盘符回收站路径以 JSON 字符串存于单 key）。未落库 key 返回 registry 默认值；敏感 key 非空时只返回 `已设置`，不回显明文。旧库中未登记 key 不会通过该接口暴露。
- **错误**：`503` 设置服务未启用

### 读取设置定义

- **方法 / 路径**：`GET /api/settings/definitions`
- **响应**（200）：
  ```json
  {
    "definitions": [
      {
        "key": "network_proxy",
        "label": "网络代理",
        "description": "后端出站网络代理；支持 http、https、socks5、socks5h，凭据不回显。",
        "layer": "runtime",
        "value_type": "url",
        "default_value": "",
        "sensitive": true,
        "hot_apply": true,
        "consumer": "netproxy"
      }
    ]
  }
  ```
- **说明**：返回配置 registry，供前端按分层、类型、默认值、敏感性和热应用能力渲染设置页。`layer=startup` 的启动固定项只读展示，不能经 `PUT /api/settings` 修改。
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
- **说明**：批量 upsert 键值，同一 key 覆盖旧值；所有 key 必须先登记为 `runtime`，并通过 registry 类型校验。任一 key 未知、不可运行期修改或值类型非法时整体返回 `400`，不写入任何设置。提交成功后回读返回，并触发设置变更回调，使定时扫描周期（`scan_interval`）即时重排生效、无需重启（FR-28）。含 `ffmpeg_path`/`ffprobe_path`（非空）时，落库后即时应用到转码运行期（覆盖自动发现），保存即生效（FR-56）；含 `magick_path`（非空）时同理即时应用到 HEIC/RAW 转换运行期，保存即生效（FR-63）；含 `network_proxy` 时写入前校验协议和格式，落库后即时应用到后端出站 HTTP 运行期（空=直连、非空=设代理），支持 http/https/socks5/socks5h，保存即生效（FR-80）。含 `debug_log` 时落库后即时切换 GORM 日志级别（`"1"`/`"true"`=开启详细 SQL/慢查询日志、其余=安静），保存即生效（FR-110）；启动时读取该键决定初始级别，重启后保持。
- **已知运行期键**：`scan_interval`（定时扫描周期秒）、`recycle_bin_paths`（盘符→回收站目录 JSON）、`update_channel`（`stable`/`prerelease`）、`transcode_codec_priority`（首选目标编码优先级 JSON 数组）、`ffmpeg_path`/`ffprobe_path`（FR-56，可执行文件路径，非空覆盖自动发现）、`magick_path`（FR-63，ImageMagick magick 可执行文件路径，非空覆盖自动发现）、`network_proxy`（FR-80，后端出站网络代理 URL，空=直连，敏感不回显）、`debug_log`（FR-110，运行时调试日志开关，`"1"`=开启 GORM 详细日志、其余=安静）、`upload_target_dir`、`upload_naming_rule`、`open_tabs`、`last_opened_path`。
- **错误**：`400` 请求参数错误、`settings` 为空或配置校验失败（`INVALID_SETTING`），`503` 设置服务未启用，`500` 保存失败

### 查询审计事件（FR2-040）

- **方法 / 路径**：`GET /api/audit/events`
- **请求头**：Space scoped 查询可带 `X-JianVideo-Space-Id`；缺省为 `space-default`
- **查询参数**：
  - `scope`：可选，传 `system` 时查询系统级事件；缺省为 Space scoped 查询。
  - `space_id`：可选，显式查询某 Space；仅 Space scoped 查询生效。
  - `action` / `resource_type` / `resource_id`：可选，按动作与资源过滤。
  - `from` / `to`：可选，支持 RFC3339 或 `YYYY-MM-DD`。
  - `cursor` / `limit`：cursor 分页，按 `created_at desc, id desc` 排序。
- **响应**（200）：
  ```json
  {
    "items": [
      {
        "id": 1,
        "scope": "space",
        "space_id": "space-default",
        "actor_type": "system",
        "actor_id": "system",
        "action": "media.deleted",
        "resource_type": "media",
        "resource_id": "42",
        "before_json": {"file_name": "clip.mp4"},
        "after_json": {"deleted_at": "2026-07-08T08:00:00Z"},
        "metadata_json": null,
        "request_id": "",
        "created_at": "2026-07-08T08:00:00Z"
      }
    ],
    "next_cursor": null
  }
  ```
- **说明**：Space scoped 查询默认只返回 `scope=space` 且匹配当前 Space 的事件，不返回 `scope=system` 事件；系统级事件需显式 `scope=system`。响应中的 before/after/metadata 已复用后端审计脱敏策略，密码、令牌、代理凭据和含用户名路径不会明文回显。
- **错误**：`400` 查询参数无效，`404` Space 不存在，`503` 审计服务未启用

## 分享链接（FR-43）

分享分两层：**管理端点** `/api/shares`（鉴权后，受 APIGuard 保护）创建/列出/撤销；**公开端点** `/api/share/:token`（免登，APIGuard 豁免 `/api/share/` 前缀）由 token 持有者只读访问。公开端点经 `shareAuth` 校验 token + 过期，并对每个 `:mediaId` 做范围校验——不在分享范围内一律 `404`。分享可选带访问密码与访问限次（FR-78）。

### 创建分享

- **方法 / 路径**：`POST /api/shares`（鉴权后）
- **请求体**：
  ```json
  { "resource_type": "media", "resource_id": 12, "expires_in_hours": 168, "password": "可选密码", "max_uses": 0 }
  ```
  `resource_type` 为 `media` 或 `album`；`expires_in_hours` 可选，`>0` 设过期、缺省或 `0` 表示永不过期。`password` 可选（FR-78），非空则后端以 bcrypt 哈希存储、绝不明文落库/回显；`max_uses` 可选（FR-78），`>0` 设访问次数上限、缺省或 `0` 表示无限。
- **响应**（201）：`{ "token": "...", "resource_type": "media", "resource_id": 12, "expires_at": "..."|null, "max_uses": 0, "used_count": 0, "created_at": "..." }`（不含密码哈希）
- **错误**：`400` 参数错误或非法类型，`404` 被分享资源不存在，`503` 分享服务未启用，`500` 创建失败

### 列出分享

- **方法 / 路径**：`GET /api/shares`（鉴权后）
- **响应**（200）：`{ "shares": [ ... ] }`（含已过期，供管理展示）

### 撤销分享

- **方法 / 路径**：`DELETE /api/shares/:token`（鉴权后）
- **响应**：`204`

### 公开访问分享元信息

- **方法 / 路径**：`GET /api/share/:token`（免登）
- **请求头**（FR-78）：分享设密码时需带 `X-Share-Password: <密码>`。
- **响应**（200）：
  - 校验通过——媒体分享 `{ "resource_type": "media", "expires_at": ..., "requires_password": false, "media": {...} }`；相册分享 `{ "resource_type": "album", "expires_at": ..., "requires_password": false, "album": {...}, "items": [...] }`。
  - 需密码且未带/带错密码——`{ "resource_type": "media", "requires_password": true }`，**不含任何 media/album 内容**（供前端弹密码框、不泄露内容、不区分过期/撤销）。本端点不消费访问额度。
- **错误**：`404` token 不存在/已过期/已撤销

### 公开访问分享内的媒体

- **方法 / 路径**：
  - `GET /api/share/:token/media/:mediaId/raw`（图片在线查看）
  - `GET /api/share/:token/media/:mediaId/thumbnail`（缩略图）
  - `GET /api/share/:token/media/:mediaId/download`（原文件下载）
  - `GET /api/share/:token/media/:mediaId/stream`（视频渐进式在线播放，支持 Range；不开放转码/HLS）
- **请求头**（FR-78）：带 `X-Share-Password` 头时校验密码；`<img>`/`<video>` 直链无法带头时不阻断（拿到 `mediaId` 已必过 `ShareInfo` 密码门禁）。
- **说明**：`:mediaId` 必须在分享范围内（== 被分享媒体，或 ∈ 被分享相册成员），否则 `404`。`smb://` 路径不支持。每次成功访问对限次分享原子自增一次 `used_count`（FR-78），达到 `max_uses` 后再访问 `404`。
- **错误**：`400` ID 无效，`404` 不在范围/不存在/已软删/密码错/次数已用尽，`503` 播放服务未启用（stream）
