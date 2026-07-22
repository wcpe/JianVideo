# 接口契约：轻量级单用户视频媒体服务器

> 对外接口的人类可读契约。**机器可读真源**为仓库根 [`api/openapi.yaml`](../api/openapi.yaml)（FR2-071）；本文件补充完整历史端点说明，分期迁入 OpenAPI。
>
> 校验：`make openapi-check`（或 `node scripts/openapi-check.mjs`）。契约变更后：`cd apps/server && task gen` 重生成，`task gen:check` 防漂移。生成物：`apps/server/internal/openapi/api.gen.go`（不得手改）。

## 1. 通用约定

- **协议**：HTTP/HTTPS RESTful API
- **认证**：基于 Cookie 的会话认证，登录后返回 `Set-Cookie` 头部（HttpOnly `auth_token`）。除 `/api/auth/login`、`/api/auth/logout`、`/api/auth/setup-status`、`/api/auth/setup`、`/health` 及前端静态资源外，所有 `/api/*` 端点均强制校验 JWT（Cookie `auth_token` 或 `Authorization: Bearer <token>` 任一有效），未携带或无效凭据返回 `401`
- **编码**：请求/响应体使用 JSON（`Content-Type: application/json`），视频流使用 `video/mp2t`
- **分页**：列表接口支持 `page`（从 1 开始）和 `page_size`（默认 20，最大 100）参数
- **时间格式**：ISO 8601（`YYYY-MM-DDTHH:MM:SSZ`）
- **静态资源**：前端文件通过 `go:embed` 内嵌，由 `/` 路径提供服务
- **数据库迁移（FR2-017）**：不新增对外 HTTP 迁移端点。v0.20 到 v2 schema 升级在服务启动期由 `internal/migration` 执行；Runner 先做 settings/迁移只读预检，blocker 在备份和任何写入前终止，warning 不阻断。dry-run 计划包含每步影响行数、`blockers`、`warnings`、是否执行及是否已应用。CLI 使用 `jianvideo -migration-dry-run`：向 stdout 打印 JSON 计划后退出，不启动 HTTP 服务、不创建备份、不写业务表、`schema_migrations` 或审计表；仅 warning 时成功退出，存在 blocker 时仍输出计划并以非零状态退出。
- **Space 头与权限（FR2-007）**：`/api/library` 下的媒体列表、详情、目录浏览、统计、扫描、标签、回收站、上传入口，以及播放、`/api/transcode/tasks`、Space scoped `/api/tasks`、`/api/audit/events`、`/api/storage/cache`、`/api/settings/storage` 支持 `X-JianVideo-Space-Id: <space_id>`。缺失时使用默认 `space-default`；显式传入非法格式返回 `400 INVALID_SPACE`；显式传入不存在的 Space 返回 `404 SPACE_NOT_FOUND`；当前 JWT 用户不是该 Space 的 `owner_user_id` 时返回 `403 SPACE_FORBIDDEN`。审计查询显式携带 `space_id` 时以该查询值作为实际授权目标，不能用默认 Space owner 身份查询其他 Space；stream/HLS 除 owner 校验外还会确认媒体记录属于该 Space。P2 仅实现 owner-only，不暴露成员/角色矩阵；系统级 `scope=system` 任务/审计与非 Space 系统端点不走 Space owner 守卫。

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

### v2 媒体 / 任务契约（FR2-006 / FR2-071）

本节描述 `packages/media-client`、`packages/mock` 与 Go 运行时对齐的 v2 契约表面。机器可读真源见 [`api/openapi.yaml`](../api/openapi.yaml)。

- **客户端 / mock**：请求携带 `X-JianVideo-Space-Id` 表示当前 Space；client 支持 `Authorization: Bearer <token>` 与可配置 timeout / retry。
- **Go 运行时（FR2-071）**：`apps/server` 已单挂同路径薄适配（`api.ListMediaV2` / `GetMediaV2` / `GetTaskV2`），响应为契约形态，委托 `library` / `tasks`；**不**调用 `openapi.RegisterHandlers` 全量挂载。历史 `/api/library/*` 与 `/api/tasks` 仍并存。

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
- **响应**（200）：单个媒体对象，字段同列表项；若媒体不属于当前 Space 或不存在，返回 `404` + `Error`（`code`/`message`）。

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
- **说明**：`status` 对齐 ADR-0055，取 `pending` / `running` / `succeeded` / `failed` / `canceled`；client 兼容 mock 或旧队列返回的 `completed` / `error`，分别映射为 `succeeded` / `failed`。任务服务未注入时 Go 返回 `503 TASKS_UNAVAILABLE`。

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
  - `sort`：排序方式，`time_desc`（默认，按入库时间降序）/ `time_asc` / `name` / `media_time`（按媒体时间降序，缺失回退入库时间，FR-31）/ `media_time_asc`（按媒体时间升序）/ `duration` / `duration_asc` / `resolution` / `resolution_asc`（FR2-046；后四者走 offset 分页，不支持游标）
  - `page`：页码
  - `page_size`：每页条数
  - `cursor`：游标分页 token（可选）；用于时间倒序列表，旧 `page/page_size` 仍可用
  - `search`：搜索（可选）。走 everything 式表达式解析（FR-35）：裸词→文件名/显示名/相机/镜头/备注 **或同 Space 推断片名**（多词 AND，FR2-046）；`ext:jpg` 或 `ext:jpg,png`→按扩展名；`type:image`/`type:video`→按类型；`size:>10mb`/`size:<=2gb`/`size:>=500kb`（单位 b/kb/mb/gb/tb）→按大小。无法识别的 `key:val` 退化为裸词。纯文本与旧行为一致（向后兼容）。
  - `favorite`：传 `true`/`1` 时仅返回已收藏媒体（可选，FR-41）
  - `tag_id`：传标签 ID 时仅返回打了该标签的媒体（可选，FR-41）
  - 结构化筛选（可选，FR-35，显式参数优先于 `search` 表达式同名约束）：`type`（`image`/`video`）、`size_min`/`size_max`（字节）、`time_from`/`time_to`（媒体时间范围，`RFC3339` 或 `YYYY-MM-DD`，按 `COALESCE(media_time, added_at)` 比较）、`path`（目录前缀）。以上全部走参数化查询，无 SQL 注入面。
  - `duration_min` / `duration_max`：时长秒数下/上界（含，>0 生效，FR2-046）
  - `width_min` / `width_max` / `height_min` / `height_max`：分辨率像素下/上界（含，>0 生效，FR2-046）
  - `has_gps`：传 `true` 时仅返回带 GPS 坐标（`gps_lat != 0 OR gps_lon != 0`）的媒体（可选，FR-39 照片地图）。
  - `inference`：传 `inferred` 时返回自动推断与人工纠正的并集，传 `auto` 时仅返回自动推断媒体，传 `manual` 时仅返回人工纠正媒体，传 `missing` 时仅返回尚无推断记录的媒体（可选，FR2-031）。
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
        "gps_lon": 121.47,
        "inference": {
          "kind": "movie",
          "title": "电影名",
          "confidence": 0.9,
          "source": "offline_rule",
          "manual": false
        }
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

### 查询媒体封面与候选（FR2-059）

- **方法 / 路径**：`GET /api/library/media/:id/covers`
- **请求头**：`X-JianVideo-Space-Id` 可选；缺省为 `space-default`
- **响应**（200）：
  ```json
  {
    "cover": {
      "media_id": 42,
      "space_id": "space-default",
      "selected_asset_id": 101,
      "selected_source": "video_frame",
      "selected_timestamp_seconds": 3,
      "selected_fingerprint": "0123456789abcdef0123456789abcdef",
      "manual": true,
      "updated_at": "2026-07-12T08:00:00Z"
    },
    "candidates": [
      {
        "id": 7,
        "media_id": 42,
        "space_id": "space-default",
        "asset_id": 101,
        "source": "video_frame",
        "timestamp_seconds": 3,
        "fingerprint": "0123456789abcdef0123456789abcdef",
        "score": 1,
        "image_url": "/api/library/media/42/covers/7/image",
        "created_at": "2026-07-12T08:00:00Z",
        "updated_at": "2026-07-12T08:00:00Z"
      }
    ],
    "cover_url": "/api/library/thumbnail/42"
  }
  ```
- **说明**：视频候选按时长 10% / 30% / 50% / 70% / 90% 规则化抽帧，极短视频会去重且不越过结尾；图片生成单个自身缩略图候选。`cover` 可为 `null`，`candidates` 为空数组。查询、候选图片与选择均校验当前 Space 和媒体归属，跨 Space 返回 `404`。

### 生成或刷新媒体封面候选（FR2-059）

- **方法 / 路径**：`POST /api/library/media/:id/covers/generate`
- **请求**：`{"refresh":false}`；`refresh=true` 使用独立 `cover.refresh` 任务重新生成候选，缺省为 `cover.generate`。
- **响应**（202）：`{"status":"pending","task_id":123}`
- **说明**：任务经 FR2-037 通用队列执行并最多尝试 3 次。产物写入 `covers/{space_id}/{media_id}/{fingerprint}.jpg`，登记为 `cache_assets(kind=cover)`；客户端通过 `GET /api/tasks/:id` 等待终态。源媒体 missing、不可访问或非本地文件时任务失败，不会改写当前人工选择。

### 选择媒体封面（FR2-059）

- **方法 / 路径**：`PUT /api/library/media/:id/cover`
- **请求**：`{"candidate_id":7}`
- **响应**（200）：更新后的 `MediaCover` 对象。
- **说明**：只能选择当前 Space、当前媒体所属候选。人工选择同时保存 `selected_source`、`selected_timestamp_seconds`、`selected_fingerprint` 与 `manual=true`，不只依赖可清理的 `selected_asset_id`；封面缓存清理后语义保留，重建时按指纹恢复新的资产 ID。生成与选择分别写 `cover.generated` / `cover.selected` 审计事件。
- **错误**：`400` 请求或封面任务无效，`404` 媒体/候选不存在或归属不匹配，`503` 封面服务未启用。

### 获取媒体封面候选图片（FR2-059）

- **方法 / 路径**：`GET /api/library/media/:id/covers/:candidate_id/image`
- **响应**（200）：候选 JPEG 二进制内容。
- **说明**：候选记录仍在但对应可重建缓存已被清理时，需先重新生成候选；不会跨 Space 或跨媒体读取其他候选文件。

### 查询文件自带元数据（FR2-030）

- **方法 / 路径**：`GET /api/library/media/:id/metadata`
- **请求头**：`X-JianVideo-Space-Id` 可选；缺省为 `space-default`
- **响应**（200）：
  ```json
  {
    "items": [
      {
        "id": 1,
        "media_id": 42,
        "space_id": "space-default",
        "source": "ffprobe",
        "tool": "ffprobe",
        "tool_version": "7.1",
        "raw_json": "{...}",
        "normalized_json": "{...}",
        "parsed_at": "2026-07-13T03:00:00Z",
        "stale": false
      }
    ]
  }
  ```
- **说明**：`source` 当前为视频 `ffprobe` 或图片 `image`；同一 Space、媒体、来源只保留一条当前结果。`normalized_json` 包含容器、视频/音频/字幕流、EXIF/IPTC/XMP、内嵌标签以及解析时记录的文件大小、mtime 和已有可信内容哈希。跨 Space 或媒体不存在返回 `404 NOT_FOUND`。

### 刷新单文件自带元数据（FR2-030）

- **方法 / 路径**：`POST /api/library/media/:id/metadata/refresh`
- **响应**（202）：`{"status":"pending","task_id":123}`
- **说明**：先把当前结果标记为 stale，再幂等入队 `metadata.parse`；任务默认最多尝试 3 次，后台 worker 完成后覆盖同来源当前结果，不修改原媒体文件。

### 批量回填文件自带元数据（FR2-030）

- **方法 / 路径**：`POST /api/library/metadata/backfill`
- **请求**（可选）：`{"library_id":1}`；省略或传 0 表示当前 Space 全部媒体。
- **响应**（202）：`{"status":"pending","task_id":124}`
- **说明**：幂等入队 `metadata.backfill`，按媒体 ID checkpoint 分批推进并更新任务进度；失败由通用任务队列自动重试，已完成媒体不会因重试重复追加元数据记录。

### 查询媒体章节（FR2-060）

- **方法 / 路径**：`GET /api/library/media/:id/chapters`
- **请求头**：`X-JianVideo-Space-Id` 可选；缺省为 `space-default`
- **响应**（200）：`{"items":[MediaChapter...],"stale":false,"parsed_at":"2026-07-17T00:00:00Z"}`
- **说明**：章节只读，来自媒体内嵌章节元数据，按 `start_ms, source_index` 稳定排序。媒体文件大小或 mtime 与最近解析指纹不一致时返回 `stale=true`；跨 Space、软删或 missing 媒体返回 `404 MEDIA_NOT_FOUND`。

### 查询与创建媒体书签（FR2-060）

- **列表**：`GET /api/library/media/:id/bookmarks`，响应 `{"items":[MediaBookmark...]}`，按位置和创建时间稳定排序。
- **创建**：`POST /api/library/media/:id/bookmarks`
- **请求**：`{"position_ms":123000,"title":"关键论点","note":"稍后复看"}`
- **响应**（201）：创建后的 `MediaBookmark`，初始 `revision=1`。
- **说明**：`position_ms` 必须非负，标题必填且不超过 120 字符，备注不超过服务端限制；所有操作同时校验当前 Space 与媒体有效状态。

### 更新与删除媒体书签（FR2-060）

- **更新**：`PUT /api/library/media/:id/bookmarks/:bookmark_id`
- **请求**：`{"position_ms":125000,"title":"关键论点","note":null,"revision":1}`
- **删除**：`DELETE /api/library/media/:id/bookmarks/:bookmark_id?revision=2`
- **响应**：更新成功返回新的 `MediaBookmark`；删除成功返回 `204`。
- **冲突**（409）：`{"code":"BOOKMARK_CONFLICT","message":"...","current":MediaBookmark,"deleted":false}`；记录已删除时 `deleted=true`。客户端必须以服务端当前值重新基线化。
- **错误**：非法位置、标题或备注返回结构化 `400 BOOKMARK_*`；媒体或书签不存在、归属不匹配返回 `404 MEDIA_NOT_FOUND`。

### 读取观看状态（FR2-045）

- **方法 / 路径**：`GET /api/play/:id/watch-state`
- **请求头**：`X-JianVideo-Space-Id` 可选；缺省为 `space-default`
- **响应**（200）：返回当前 `WatchState`；尚无记录时返回该 Space 与媒体 ID、`revision=0`、`position_seconds=0`、`completed=false` 的初始状态。
- **说明**：媒体必须属于当前 Space、未软删且源文件状态有效，否则返回 `404 NOT_FOUND`。

### 更新观看状态（FR2-045）

- **方法 / 路径**：`PUT /api/play/:id/watch-state`
- **请求**：
    ```json
    {
        "position_seconds": 1234.5,
        "duration_seconds": 7200,
        "expected_revision": 7,
        "session_id": "web-01J2ABC",
        "event_seq": 18,
        "event_type": "progress",
        "reason": "user"
    }
    ```
- **枚举**：`event_type` 为 `progress | pause | seek | ended`；`reason` 为 `user | ab_loop | restore | system`。`duration_seconds` 可省略，仅在媒体元数据缺少时辅助完成判定。
- **响应**（200）：`{"applied":true,"current":WatchState}`；当前会话重复或倒序事件返回 `applied=false`，`current` 保持不变。
- **冲突**（409）：`{"code":"WATCH_STATE_CONFLICT","message":"观看状态已被其他会话更新","applied":false,"current":WatchState}`。客户端必须采用 `current` 重新基线化，不得按错误文本判断或无条件重试。
- **说明**：更新以 `expected_revision` 比较交换；成功时在同一事务内更新 `watch_states`、`media_files` 兼容投影与完成转换的 `view_count`。

### 观看历史（FR2-045）

- **方法 / 路径**：`GET /api/library/watch-history`
- **查询参数**：`cursor` 为上一页 `next_cursor`；`limit` 默认 `20`、上限 `50`
- **响应**（200）：`{"items":[{"media":MediaFile,"watch_state":WatchState}],"next_cursor":"..."}`
- **说明**：按 `last_watched_at DESC, media_id DESC` 稳定分页，包含已完成和未完成状态；只返回当前 Space 内未软删且有效的媒体。无效游标返回 `400 INVALID_CURSOR`。

### 继续观看列表（FR2-045）

- **方法 / 路径**：`GET /api/library/continue-watching`
- **查询参数**：`limit` 默认 `12`、上限 `50`
- **响应**（200）：`{"items":[{"media":MediaFile,"watch_state":WatchState}]}`
- **说明**：从 `watch_states` 真源返回 `completed=false AND position_seconds>1` 的媒体，按 `last_watched_at DESC, media_id DESC` 排序，并隔离其他 Space、软删和失效媒体。旧前端 API 适配层可把真源状态投影回 `last_position`、`watched`、`last_watched_at`，但不得反向以兼容字段覆盖真源。

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

### 影视信息推断（FR2-031）

- **方法 / 路径**：`GET /api/library/media/:id/inference`
- **请求头**：`X-JianVideo-Space-Id` 可选；缺省为 `space-default`
- **响应**（200）：
  ```json
  {
    "inference": {
      "id": 1,
      "media_id": 42,
      "space_id": "space-default",
      "kind": "series",
      "title": "剧名",
      "year": 0,
      "season": 1,
      "episode": 2,
      "episode_title": "标题",
      "confidence": 0.95,
      "source": "offline_rule",
      "rule_version": "fr2-031-v1",
      "manual": false,
      "created_at": "2026-07-09T10:00:00Z",
      "updated_at": "2026-07-09T10:00:00Z"
    },
    "display_name": "剧名"
  }
  ```
- **说明**：返回当前媒体的本地离线影视信息推断；无推断时 `inference` 为 `null`，`display_name` 仍按展示优先级返回。展示优先级为：人工纠正推断 > `media_files.display_name` > 高置信自动推断（`confidence >= 0.75`）> 原始文件名。低置信自动候选不替换显示名。
- **错误**：`400` ID 无效，`404` 媒体记录不存在，`500` 查询失败

- **方法 / 路径**：`PUT /api/library/media/:id/inference`
- **请求**：
  ```json
  {
    "kind": "series",
    "title": "人工剧名",
    "year": 2024,
    "season": 1,
    "episode": 2,
    "episode_title": "人工集标题"
  }
  ```
- **响应**（200）：更新后的 `MediaInference` 对象，`manual=true`、`source="manual"`、`confidence=1`。
- **说明**：仅修改库内影视推断信息，不改磁盘文件名。人工纠正写 `media.inference.updated` 审计事件，并优先于后续自动推断和 backfill。
- **错误**：`400` 请求体无效或 `kind` 非法，`404` 媒体记录不存在，`500` 保存失败

### 查询下一集（FR2-047）

- **方法 / 路径**：`GET /api/library/media/:id/next-episode`
- **响应**（200）：
  ```json
  {
    "media": { "...": "MediaFile 或 null" },
    "current": { "...": "MediaInference 或 null" },
    "next": { "...": "下一集 MediaInference 或 null" }
  }
  ```
- **说明**：在当前 Space 内按推断 `title` + `season`/`episode` 定位下一集：优先同季更大 `episode`，否则下一季最小 `episode`。跳过已软删媒体；无推断、标题为空、无下一集时 `media` 为 `null`。不跨 Space。
- **错误**：`400` ID 无效，`404` 媒体不存在，`500` 查询失败

- **方法 / 路径**：`POST /api/library/inference/backfill`
- **请求**：
  ```json
  {"library_id": 1}
  ```
- **响应**（202）：
  ```json
  {"status": "pending", "task_id": 12}
  ```
- **说明**：手工发起时批量重跑当前 Space 内的离线影视信息推断。`library_id` 可省，省略则扫描全部库；任务以当前 Space、`library_id` 和执行模式组成的幂等键入队为 `library.inference.backfill`，跳过 `home_video`、关闭的库和已有人工纠正的媒体。保存推断总开关或按库关闭配置时，若保存后总开关开启，后端自动入队 `missing` 模式，仅为尚无推断记录的已有媒体补齐结果，不重算已有自动结果且不覆盖人工结果。媒体记录落库后若即时推断失败，后端会为该媒体持久化同类型 `media` 补偿任务并唤醒 worker；worker 重读当前开关与人工值，关闭或按库禁用时仍不产生自动推断。客户端须使用 `GET /api/tasks/:task_id` 轮询 `pending` / `running`，直到 `succeeded` / `failed` / `canceled` 终态；批量任务的 `progress` 按逐媒体完成比例更新，`checkpoint` 为最近处理的媒体 ID。取消 `running` 任务会向 worker 处理器传递 context 取消信号。
- **错误**：`503` 通用任务服务或 worker 未启用，`500` 入队失败

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

### 图片编辑导出（FR2-038）

- **方法 / 路径**：`POST /api/library/media/:id/image-export`
- **请求体**：
  ```json
  {
    "exposure": 0,
    "contrast": 0,
    "saturation": 0,
    "temperature": 0,
    "format": "jpeg"
  }
  ```
  参数范围均为 `[-100, 100]`；`format` 白名单：`jpeg`/`jpg`/`png`/`webp`。
- **响应**（202）：`{"status":"queued","task_id":"123"}`。
- **说明**：不修改原文件。入队任务类型 `media.image.export`；worker 使用 ImageMagick 生成产物到 `exports/{space}/{media_id}/{task_id}.{ext}`；任务 `checkpoint` 含下载元数据。幂等键含 Space/Media/参数指纹。
- **错误**：`400` 参数非法 / SMB 路径，`404` 媒体不存在，`503` 任务中心未启用

### 视频片段粗剪导出（FR2-039）

- **方法 / 路径**：`POST /api/library/media/:id/clip-export`
- **请求体**：
  ```json
  {
    "start_sec": 0,
    "end_sec": 30,
    "format": "mp4"
  }
  ```
  要求 `0 <= start < end`；默认最大片段 2 小时；`format` 白名单：`mp4`/`mkv`/`mov`。
- **响应**（202）：`{"status":"queued","task_id":"124"}`。
- **说明**：不修改原文件。入队任务类型 `media.video.clip`；优先 ffmpeg stream copy，失败回退 H.264/AAC 重编码；产物路径同导出目录约定。
- **错误**：`400` 起止非法 / 超时长 / 格式不支持，`404` 媒体不存在，`503` 任务中心未启用

### 导出产物下载（FR2-038 / FR2-039）

- **方法 / 路径**：`GET /api/library/exports/:task_id/download`
- **响应**（200）：附件流。
- **说明**：仅成功完成的 `media.image.export` / `media.video.clip` 任务可下载；需归属当前 Space。
- **错误**：`404` 任务/产物不存在，`409` 任务未完成，`503` 任务中心未启用

### 批量转码（FR2-053）

- **方法 / 路径**：`POST /api/library/media/batch-transcode`
- **请求体**：`{"ids":[101,102,...],"preset_id":1}`；`ids` 最多 100 项，`preset_id` 必填且为正。
- **响应**（200）：`{"queued":N,"skipped":M,"failed":K,"task_ids":[...]}`。
- **说明**：对当前 Space 内选中媒体入队转码（复用 HLS preview / 预生成队列）。图片格式与不存在/已软删项计入 `skipped`；入队失败计入 `failed`。未启用预设或转码服务时返回 `503`。
- **错误**：`400` 请求体无效 / 超限 / `preset_id` 非法，`404` 预设不存在，`503` 服务未启用

### 批量移动到媒体库（FR2-053，索引层）

- **方法 / 路径**：`POST /api/library/media/batch-move`
- **请求体**：`{"ids":[101,102,...],"target_library_id":2}`；`ids` 最多 100 项，`target_library_id` 必填且为正。
- **响应**（200）：`{"moved":N,"skipped":M}`。
- **说明**：**仅更新** `media_files.library_id`，**不搬移磁盘原文件**。目标库必须同 Space；不存在/已软删/已在目标库的 id 计入 `skipped`。写审计 `media.library_reassigned`。
- **错误**：`400` 请求体无效 / 超限，`404` 目标库不存在，`500` 更新失败

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
- **查询参数**：`size`（可选）缩略图宽度，受支持白名单 `160` / `320` / `640`，缺省或非白名单值回落默认 `320`；`probe=1` 表示前端状态探测（FR2-028）
- **响应**（200）：存在当前 FR2-059 封面缓存时优先返回该 JPEG；否则返回按 `size` 生成的普通缩略图（视频取第 2 秒帧、图片缩放；带透明区的源已合成中性灰底，FR-81 P1）
- **响应**（202）：普通缩略图回退尚未就绪时返回 `{"code":"GENERATING","message":"缩略图生成中","task_id":123,"sizes":[320]}`；同一 Space、媒体和尺寸集合的未完成任务按幂等键复用
- **说明**：该稳定 URL 是列表、详情和播放器 poster 的统一封面入口。当前智能封面存在时忽略 `size` 返回封面原始 640px JPEG；封面缓存缺失或被清理时保持 FR2-028 向后兼容，按需经 `thumbnail.generate` 生成 `thumbnails/{spaceID}/{mediaID}/{size}.jpg` 并登记 `cache_assets(kind=thumbnail, variant=size)`。前端选择或重建封面后会给请求追加缓存版本参数以主动刷新列表图片，其他未知查询参数均可安全忽略。
- **错误**：`202` 缩略图生成中，`404` 媒体记录不存在

### 批量预生成缩略图（FR2-028）

- **方法 / 路径**：`POST /api/library/thumbnails/backfill`
- **请求头**：`X-JianVideo-Space-Id` 可选；缺省为 `space-default`
- **请求**：`{"sizes":[160,320,640]}`；`sizes` 为空或省略时使用全部三档，非法尺寸返回 `400`
- **响应**（202）：`{"status":"queued","task_id":124,"sizes":[160,320,640]}`
- **说明**：创建 `thumbnail.backfill` 任务，按媒体 ID checkpoint 分批扫描当前 Space；只处理具备缩略图能力的图片/视频，生成成功后逐档登记缓存资产。任务进度与终态通过 `GET /api/tasks/:id` 查询。

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

### 存储与缓存管理（FR2-048）

- **方法 / 路径**：`GET /api/storage/cache/summary`
- **请求头**：`X-JianVideo-Space-Id` 可选；缺省为 `space-default`
- **查询参数**：`kind`、`library_id`、`media_id` 可选。
- **响应**（200）：
  ```json
  {
    "total_size_bytes": 1024,
    "total_file_count": 3,
    "total_assets": 2,
    "by_kind": {
      "thumbnail": {"kind": "thumbnail", "size_bytes": 512, "file_count": 1, "asset_count": 1, "rebuildable": true},
      "hls": {"kind": "hls", "size_bytes": 512, "file_count": 2, "asset_count": 1, "rebuildable": true}
    }
  }
  ```
- **方法 / 路径**：`GET /api/storage/cache/assets`
- **查询参数**：`kind`、`library_id`、`media_id`、`page`、`page_size` 可选。
- **响应**（200）：
  ```json
  {
    "items": [
      {
        "id": 1,
        "space_id": "space-default",
        "library_id": 1,
        "media_id": 88,
        "kind": "hls",
        "asset_level": "directory",
        "profile_id": "h264",
        "relative_path": "hls/88",
        "size_bytes": 512,
        "file_count": 2,
        "rebuildable": true,
        "created_at": "2026-07-09T10:00:00Z",
        "updated_at": "2026-07-09T10:00:00Z"
      }
    ],
    "page": 1,
    "page_size": 20,
    "total": 1
  }
  ```
- **方法 / 路径**：`POST /api/storage/cache/inventory`
- **响应**（202）：`{"task_id":101}`
- **说明**：请求只将 `cache.inventory` 写入通用任务队列，不在 HTTP handler 内扫描磁盘。客户端通过 `GET /api/tasks/101` 查询进度与终态；worker 扫描数据目录下的缓存白名单，补齐历史缓存资产并把磁盘缺失的登记标记 `missing_at`，HLS 只登记媒体目录，不为 segment 逐行建资产。
- **方法 / 路径**：`POST /api/storage/cache/clean`
- **请求**：
  ```json
  {
    "dry_run": true,
    "kinds": ["thumbnail", "hls"]
  }
  ```
- **响应**（dry-run 200）：
  ```json
  {
    "dry_run": true,
    "candidate_count": 1,
    "total_size_bytes": 512,
    "total_file_count": 1,
    "deleted_count": 0,
    "deleted_size_bytes": 0,
    "failed_count": 0
  }
  ```
- **响应**（真实清理 202）：`{"dry_run":false,"task_id":102,"candidate_count":0,"total_size_bytes":0,"total_file_count":0,"deleted_count":0,"deleted_size_bytes":0,"failed_count":0}`
- **说明**：`kinds` 为空表示全部白名单类型；非法类型返回 `400`。dry-run 同步计算影响范围并写 `cache.clean.preview` 审计；真实请求只将 `cache.clean` 入队，客户端通过 `GET /api/tasks/102` 查询进度与终态。worker 会再次校验 Space、payload、类型白名单和删除路径，只删除 `thumbnails/`、`hls/`、`image_cache/`、`covers/`、`metadata_temp/` 下登记的可重建缓存，写 `cache.clean.executed` 审计，不删除原媒体、数据库、WAL/SHM、审计或备份。

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

### 查询合集邻项（FR2-047）

- **方法 / 路径**：`GET /api/albums/:id/neighbor?media_id=&dir=next|prev`
- **查询参数**：
  - `media_id`（必填）：当前媒体 ID
  - `dir`（可选，默认 `next`）：`next` 下一首，`prev` 上一首
- **响应**（200）：`{"media": MediaFile | null}`，越界时 `media` 为 `null`
- **说明**：按相册成员加入顺序定位相邻媒体；当前媒体不在合集时返回 `404`。
- **错误**：`400` 相册 ID / `media_id` / `dir` 非法，`404` 相册不存在或媒体不在合集，`500` 查询失败

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

### 查询 HLS 预览 / ABR / 音轨重载状态（FR2-008 / FR2-026 / FR2-044）

- **方法 / 路径**：`GET /api/play/:id/hls-status`
- **请求头**：`X-JianVideo-Space-Id` 可选；缺省为 `space-default`
- **查询参数**：
  - `profile_id` 可选；缺省为兼容 profile `h264`，多码率 H.264 使用 `abr-h264`。查询 FR2-044 音轨任务时必须显式携带创建响应中的完整 canonical profile ID，不能只传 `task_id` 或使用大小写别名。
  - `task_id`：普通 preview 不要求；`abr-h264` 不接受；FR2-044 音轨重载 profile **必填**，值必须是 `POST /api/play/:id/audio-reload` 返回的字符串任务 ID。
- **普通 preview 响应**（200）：
  ```json
  {
    "available": true,
    "profile_id": "h264",
    "url": "/api/play/hls/42/master.m3u8",
    "task": {
      "id": "7",
      "type": "transcode.hls.preview",
      "status": "succeeded",
      "priority": 10,
      "progress": 1
    }
  }
  ```
- **音轨重载响应**（200）：
  ```json
  {
    "available": true,
    "profile_id": "audio-h264-aac-0123456789abcdef01234567",
    "url": "/api/play/hls/42/profiles/audio-h264-aac-0123456789abcdef01234567/tasks/91/master.m3u8",
    "effective_track_id": "emb-0123456789abcdef01234567",
    "task": {
      "id": "91",
      "type": "transcode.hls.preview",
      "status": "succeeded",
      "progress": 1
    }
  }
  ```
- **说明**：普通 preview 的 `task` 仍是当前 Space/media/profile 最近一条统一任务；默认 H.264/TS profile 返回兼容 URL `master.m3u8`，`abr-h264` 返回 `/api/play/hls/:id/profiles/abr-h264/master.m3u8`。音轨重载不按 profile 猜“最新任务”，而是按 `task_id` 精确交叉校验 Space、media、profile、任务类型和 payload；其 `available`、`url` 与 `effective_track_id` 只对应这一任务。只有任务成功且任务目录内清单存在时才返回 `available=true` 和目标 `effective_track_id`。
- **错误**：`400 INVALID_ID`（媒体 ID 不是整数）、`400 INVALID_HLS_TASK_ID`（任务 ID 非正整数，或 ABR 错带 `task_id`）、`400 HLS_TASK_ID_REQUIRED`（音轨 profile 缺少 `task_id`）、`404 HLS_TASK_NOT_FOUND`、`404 NOT_FOUND`（媒体不存在）、`500 HLS_STATUS_FAILED`（任务信封、payload、媒体/profile/task 身份不一致，profile 未使用创建响应中的 canonical 值，或状态读取失败；响应不泄露内部细节）、`503 HLS_PREVIEW_UNAVAILABLE`、`503 HLS_ABR_UNAVAILABLE`。

### 查询或生成时间轴预览（FR2-029）

- **方法 / 路径**：`GET /api/play/:id/timeline-preview`
- **查询参数**：`profile` 可选；缺省使用服务端当前默认 profile。
- **响应**：已有可用 generation 时返回 `200` 与 `status=available`、`profile_id`、`source_fingerprint`、`generation_id`、`duration`、`version`、`vtt_url`、`sprite_urls`；缺失或过期时幂等入队并返回 `202`、`status=pending` 与 `task_id`。
- **说明**：查询不会同步执行 ffmpeg；同一 Space/media/profile/source generation 的未完成任务复用同一幂等身份。媒体不存在或不属于当前 Space 返回 `404 NOT_FOUND`。

### 重建时间轴预览（FR2-029）

- **方法 / 路径**：`POST /api/play/:id/timeline-preview/rebuild`
- **请求**：`{"profile_id":"timeline-v1-..."}`
- **响应**（202）：返回新的 pending generation 与 `task_id`。
- **说明**：显式重建创建新 generation，旧 generation 在新产物原子发布前仍可读取；发布成功后当前指针切换，新旧缓存由安全清理与补偿机制收口。

### 获取时间轴预览资源（FR2-029）

- **方法 / 路径**：`GET /api/play/:id/timeline-preview/resources/:profile/:fingerprint/:generation/:resource`
- **资源**：`:resource` 仅允许 `index.vtt` 或服务端返回的 `.jpg/.webp/.png` sprite 文件名。
- **响应**：VTT 返回 `text/vtt`，sprite 返回对应 `image/*`；按完整 Space/media/profile/source fingerprint/generation 身份受限读取，不回退到其他 generation。
- **错误**：非法 profile 或资源身份返回 `400 INVALID_PROFILE/INVALID_RESOURCE/INVALID_PREVIEW`，资源不存在返回 `404 NOT_FOUND`，服务未启用返回 `503 TIMELINE_PREVIEW_UNAVAILABLE`。

### 显式创建多码率 HLS 任务（FR2-026）

- **方法 / 路径**：`POST /api/play/:id/hls-abr`
- **请求头**：`X-JianVideo-Space-Id` 可选；缺省为 `space-default`
- **请求体**：
  ```json
  { "priority": 8, "force_rebuild": false }
  ```
- **响应**（202）：
  ```json
  {
    "task_id": 19,
    "profile_id": "abr-h264",
    "url": "/api/play/hls/42/profiles/abr-h264/master.m3u8"
  }
  ```
- **说明**：仅显式调用时入队，扫描入库不会自动创建高成本 ABR 任务。任务类型固定为 `transcode.hls.abr`，最大尝试 3 次；`priority` 进入通用任务队列，取消、重试与详情使用 `/api/tasks/:id`。实际 ladder 从运行期设置 `transcode_abr_ladder` 读取，默认 `1080p/720p/480p`，高于源分辨率的档位会跳过，源低于 480p 时仅生成原尺寸 `source` 档。`force_rebuild=true` 会先经缓存安全边界清理当前 Space/media 的 `abr-h264` profile，再生成并登记 master 与各 variant。原文件直连仍是播放页首选，只有直连加载失败且本 profile 已可用时才回退该 URL。
- **错误**：服务未启用返回 `503`，媒体不存在或不属于当前 Space 返回 `404`，请求或入队参数非法返回 `400`。

### 获取 HLS 清单与切片

- **默认兼容路径**：
  - `GET /api/play/hls/:id/master` 或 `GET /api/play/hls/:id/master.m3u8`：默认 `h264` profile 的 TS master 清单；FR2-008 单档任务只含一个 `EXT-X-STREAM-INF`，不等同于 FR2-026 多码率 ABR。
  - `GET /api/play/hls/:id/index.m3u8`：默认 profile 的 fMP4/CMAF 清单兼容路径。
- **普通显式 profile 路径**：`GET /api/play/hls/:id/profiles/:profile_id/:file`；`abr-h264` 的 `:file` 可为 `master.m3u8` 或 `:variant/index.m3u8`、`:variant/segment_NNN.ts`。
- **FR2-044 音轨任务路径**：`GET /api/play/hls/:id/profiles/:profile_id/tasks/:task_id/:file`。`POST /api/play/:id/audio-reload` 和同一任务的 `hls-status` 均返回该受鉴权 URL；读取前必须确认数据库中存在同 Space/media/profile/payload 的 `succeeded` `transcode.hls.preview` 任务，master、variant 清单与切片只从对应任务目录读取，不回退到同 profile 的其他任务。音轨 profile 通过不带 `tasks/:task_id` 的普通显式 profile 路径直连会被拒绝；任务不存在、身份不匹配、未成功或资产不存在均返回受控 `404`。
- **旧文件布局兼容**：仅默认兼容路径在 Space/profile 新目录中不存在目标文件时，继续回退读取历史 `hls/:media_id/:file` 产物；任务级音轨路径不参与旧布局回退。
- **响应类型**：`.m3u8` 为 `application/vnd.apple.mpegurl`，`.ts` 为 `video/mp2t`，`.m4s` 为 `video/iso.segment`，fMP4 初始化段 `.mp4` 为 `video/mp4`。
- **安全边界**：请求必须通过当前 Space owner 校验，媒体必须属于该 Space；路径必须位于受控 HLS 根目录内。HLS 文件通过受限根打开的同一普通文件句柄完成校验与 Range 响应，避免路径检查与实际打开之间的符号链接竞态。任务级路径还必须匹配当前 Space/media/profile 下已成功的 `transcode.hls.preview` 任务身份。

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
- **硬件策略**：默认转码硬件策略不内嵌在本响应中，使用 `GET/PUT /api/settings` 的 `transcode_hwaccel_mode`（`auto/software/nvenc/qsv/amf/vaapi/videotoolbox`）与 `transcode_hwaccel_fallback`（`"1"`/`"0"`）读取和保存。

### 列出转码预设（FR-77）

- **方法 / 路径**：`GET /api/transcode/presets`
- **响应**（200）：`{ "items": [ { "id", "name", "codec", "width", "height", "created_at", "updated_at" } ] }`，按创建时间倒序。`width`/`height` 为 `0` 表示沿用源分辨率；FR2-008 HLS 任务会把两者快照到统一任务 payload，并在 H.264 单档输出时用其选择目标质量档。未注入预设服务返回 `503`。

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

### 加入 HLS 预览任务队列（FR2-008；兼容旧转码入口）

- **方法 / 路径**：`POST /api/transcode/tasks`
- **请求头**：`X-JianVideo-Space-Id` 可选；缺省为 `space-default`
- **请求体**：
  ```json
  {
    "media_id": 42,
    "preset_id": 1,
    "profile_id": "h264",
    "priority": 10,
    "force_rebuild": false
  }
  ```
- **说明**：`profile_id` 缺省时使用预设 codec；`priority` 进入通用队列优先级；`force_rebuild=true` 时先通过缓存安全边界仅清理目标 Space/media/profile，再重建。任务类型固定为 `transcode.hls.preview`，最大尝试次数为 3，worker 默认单并发。H.264 生成单档 TS HLS，非 H.264 生成单档 fMP4/CMAF；**FR2-008 不生成 1080p/720p/480p 多码率阶梯，ABR 属 FR2-026**。
- **响应**（200）：`{ "status": "queued", "task_id": 7 }`。相同 Space/media/profile 存在未完成任务时按统一任务幂等键复用。媒体或预设不存在返回 `404`，参数或入队失败返回 `400`。
- **取消 / 重试 / 详情**：使用通用任务端点 `GET /api/tasks/:id`、`POST /api/tasks/:id/cancel`、`POST /api/tasks/:id/retry`；取消会传播到 ffmpeg context，重试复用同一任务记录并重新调度。

### 列出 HLS 预览任务（旧响应形状兼容）

- **方法 / 路径**：`GET /api/transcode/tasks?status=`
- **请求头**：`X-JianVideo-Space-Id` 可选；缺省为 `space-default`
- **查询参数**：`status` 可使用通用状态 `pending`/`running`/`succeeded`/`failed`/`canceled`；响应为兼容旧前端，仍把 `succeeded` 映射为 `completed`、`failed` 映射为 `error`。
- **响应**（200）：`{ "tasks": [ { "id", "space_id", "media_id", "preset_id", "profile_id", "codec", "width", "height", "status", "priority", "progress", "error", "created_at", "started_at", "completed_at" } ] }`。`progress` 为 0–1，列表仅返回当前 Space 的 `transcode.hls.preview` 任务。

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
- **说明**：对候选编码器（软件 + QSV/VAAPI/NVENC/AMF/VideoToolbox/Vulkan 的 H.264/H.265/AV1/VP9）用外部 ffmpeg 跑一小段试编码（`-f lavfi … -f null`）。`compiled` 表示是否编入当前 ffmpeg，`tested_ok` 表示试编码是否成功。**默认读按 FFmpeg 可执行文件身份（版本、实际路径、文件元数据与内容摘要）持久化的缓存即时返回**（`from_cache:true`），`?force=true` 强制重测覆盖缓存（`from_cache:false`，逐个试编码可能耗时数分钟）并写系统级 `codec_probe.retested` 审计事件。ffmpeg 不可用时返回 `ffmpeg_available:false` 且 `results` 为空。结果与 `GET /api/transcode/hwaccel` 同源（见 [ADR-0033](adr/0033-hwaccel-probe-source-cache.md)）。

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
      { "id": "ffmpeg-tools-v1.0.0-windows-amd64", "tool": "ffmpeg", "platform": "windows", "arch": "amd64", "version": "tools-v1.0.0", "url": "https://github.com/wcpe/JianVideo/releases/download/tools-v1.0.0/jianvideo-tools-windows-x86_64.zip", "sha256": "ac4ddd58f077b9cf27058a5c5149a800113ec6e092c58e08176408b63167944f", "size": 34160498, "label": "JianVideo 工具包 tools-v1.0.0" }
    ]
  }
  ```
- **说明**：返回当前运行平台的内置工具源元数据。Linux、Windows、macOS 的 amd64/arm64 分别映射到 `tools-v1.0.0` GitHub Release 六个平台 ZIP；每个平台为 ffmpeg、ffprobe、magick 返回独立 source，三者共享该平台 ZIP 的固定 URL、大小与 SHA-256。下载请求会拒绝错配当前平台或架构的运行期绑定源。

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

### 获取统一字幕与音轨列表（FR2-044）

- **方法 / 路径**：`GET /api/play/:id/tracks`
- **请求头**：`X-JianVideo-Space-Id` 可选；缺省为 `space-default`
- **响应**（200）：
  ```json
  {
    "tracks": [
      {
        "id": "emb-0123456789abcdef01234567",
        "kind": "audio",
        "label": "日语 2.0",
        "source": "embedded",
        "codec": "aac",
        "language": "jpn",
        "title": "日语",
        "channels": 2,
        "channel_layout": "stereo",
        "is_default": false,
        "is_forced": false,
        "available": true,
        "capability": "reload",
        "stream_index": 2
      },
      {
        "id": "sid-fedcba9876543210fedcba98",
        "kind": "subtitle",
        "label": "电影名.zh.srt",
        "source": "sidecar",
        "format": "srt",
        "language": "zh",
        "title": "zh",
        "is_default": false,
        "is_forced": false,
        "available": true,
        "capability": "seamless"
      }
    ],
    "selection": {
      "audio": {
        "selected_track_id": "emb-0123456789abcdef01234567",
        "effective_track_id": null
      },
      "subtitle": {
        "selected_track_id": null,
        "effective_track_id": null
      }
    },
    "sources": {
      "embedded": {"available": true, "capability": "seamless"},
      "sidecar": {"available": true, "capability": "seamless"},
      "uploaded": {"available": true, "capability": "seamless"}
    },
    "backend": {
      "audio": {"available": true, "capability": "reload"},
      "subtitle": {"available": true, "capability": "seamless"}
    }
  }
  ```
- **说明**：统一返回容器内嵌音轨/字幕、媒体同目录外挂字幕与用户上传字幕。`id` 是同一 Space/media/source/来源引用下的稳定 `track_id`，客户端不得把数组下标当身份。`selected_track_id` 表示用户选择意图，`effective_track_id` 表示播放后端已确认的实际轨道；切换中二者可以不同，初始音轨尚未由当前播放源确认时 `effective_track_id` 为 `null`。能力只取 `seamless` / `reload` / `unsupported`：`available` 表示轨道或来源本身是否可读取/枚举，存在但当前播放后端无法切换的音轨可以是 `available=true + capability=unsupported`；`unsupported_reason` 解释来源不可用或切换能力不支持的原因。SMB 媒体的外挂来源返回 `SMB_SIDECAR_UNSUPPORTED`，不以空列表伪装为“没有字幕”。
- **错误**：`400 INVALID_ID`（媒体 ID 不是整数）、`400 INVALID_SUBTITLE`、`404 SUBTITLE_NOT_FOUND`、`503 SUBTITLE_SERVICE_UNAVAILABLE`。

### 上传字幕（FR2-044）

- **方法 / 路径**：`POST /api/play/:id/subtitles`
- **请求头**：`Content-Type: multipart/form-data`；Space 头同统一轨道列表。
- **请求**：恰好一个名为 `file` 的文件字段；支持 SRT、ASS、SSA、VTT，单文件最大 10 MiB。
- **响应**（201）：返回新增的统一字幕轨对象，`id` 即后续内容读取和删除使用的稳定 `track_id`。
- **说明**：文件保存到应用数据目录 `subtitles/{space_id}/{media_id}/{track_id}.{format}`，文件名由服务端生成；不写媒体原目录、不登记 `cache_assets`。扩展名、文本结构、空文件、二进制伪装、路径穿越和 multipart 多文件都会在提交前拒绝，失败不留下部分文件。
- **错误**：`400 INVALID_SUBTITLE`、`404 SUBTITLE_NOT_FOUND`、`413 SUBTITLE_TOO_LARGE`、`422 SUBTITLE_UNPROCESSABLE`、`503 SUBTITLE_SERVICE_UNAVAILABLE`。

### 获取稳定字幕内容（FR2-044）

- **方法 / 路径**：`GET /api/play/:id/subtitles/:track_id/content`
- **响应**（200）：`Content-Type: text/vtt; charset=utf-8`，返回规范化 WebVTT。
- **说明**：外挂、上传和受支持的内嵌文本字幕统一按稳定 `track_id` 读取。SRT/ASS/SSA 转换为 WebVTT，VTT 重新解析并规范化；内嵌文本字幕按 `stream_index` 通过配置的 ffmpeg 临时提取。转换按请求执行，临时文件在响应后清理，不持久化 WebVTT 缓存。外挂字幕仅以媒体目录为受限根、以已枚举 basename 打开，枚举时跳过 symlink，打开后还会复验普通文件；路径逃逸、链接、文件消失或非普通文件统一返回不存在且不泄露内容。上传字幕则只按数据库中与服务端生成文件名一致的受控应用数据相对路径读取。图片字幕或不支持 codec 返回结构化不支持错误，不返回空 WebVTT 伪成功。
- **错误**：`400 INVALID_SUBTITLE`、`404 SUBTITLE_NOT_FOUND`、`422 IMAGE_SUBTITLE_UNSUPPORTED` / `SUBTITLE_CODEC_UNSUPPORTED` / `SUBTITLE_UNPROCESSABLE`、`503 SUBTITLE_SERVICE_UNAVAILABLE`。

### 删除上传字幕（FR2-044）

- **方法 / 路径**：`DELETE /api/play/:id/subtitles/:track_id`
- **响应**（204）：空。
- **说明**：仅接受当前 Space/media 下 `source=uploaded` 的稳定 `track_id`。服务端先隔离应用数据文件，再在事务中删除 `media_subtitle_tracks` 记录并写 `subtitle.deleted` 审计；最终文件删除失败时恢复记录、审计和原路径。外挂与内嵌轨道没有删除源文件入口。
- **错误**：`400 INVALID_ID`（媒体 ID 不是整数）、`400 INVALID_SUBTITLE`、`404 SUBTITLE_NOT_FOUND`、`503 SUBTITLE_SERVICE_UNAVAILABLE`。

### 创建音轨 HLS 重载任务（FR2-044）

- **方法 / 路径**：`POST /api/play/:id/audio-reload`
- **请求头**：`X-JianVideo-Space-Id` 可选；缺省为 `space-default`
- **请求**：
  ```json
  {"track_id":"emb-0123456789abcdef01234567"}
  ```
- **响应**（202）：
  ```json
  {
    "task_id": "91",
    "profile_id": "audio-h264-aac-0123456789abcdef01234567",
    "requested_track_id": "emb-0123456789abcdef01234567",
    "space_id": "space-default",
    "url": "/api/play/hls/42/profiles/audio-h264-aac-0123456789abcdef01234567/tasks/91/master.m3u8"
  }
  ```
- **说明**：只为本地、至少两个可确认内嵌音频 stream、目标轨具有有效 `stream_index` 且当前 ffmpeg/硬件策略可执行的媒体创建任务。任务固定输出单音轨 H.264/AAC HLS，并按 `task_id` 使用独立目录和 URL；客户端必须携同一个 `task_id` 查询 `hls-status`，不得按 profile 或媒体猜测最新任务。重复的未完成同身份请求可复用同一任务 ID。
- **错误**：`400 INVALID_ID`（媒体 ID 不是整数）、`400 INVALID_AUDIO_RELOAD_REQUEST`、`404 NOT_FOUND`、`422 AUDIO_RELOAD_UNSUPPORTED`（响应含 `reason`，例如 `SMB_AUDIO_RELOAD_UNSUPPORTED`、`AUDIO_STREAM_INDEX_UNAVAILABLE`、`AUDIO_RELOAD_FFMPEG_UNAVAILABLE`、`AUDIO_RELOAD_HARDWARE_UNAVAILABLE`）、`503 HLS_PREVIEW_UNAVAILABLE`、`500 AUDIO_RELOAD_ENQUEUE_FAILED`。

### 旧字幕路径兼容

- **列表**：`GET /api/play/:id/subtitles` 继续返回 `{tracks:[{index,file_name,format,url}]}`，仅表示旧式本地外挂字幕数组；SMB 响应附 `sources.sidecar.available=false` 与 `SMB_SIDECAR_UNSUPPORTED`。
- **内容**：`GET /api/play/:id/subtitles/:index` 继续按旧列表数组索引返回 `text/vtt`。该路径只服务旧客户端，不提供稳定身份；新客户端必须使用 `/tracks` 与 `/:track_id/content`。
- **错误**：索引非法返回 `400 INVALID_SUBTITLE`，越界或媒体不存在返回 `404 SUBTITLE_NOT_FOUND`；不支持/转换失败使用与稳定内容端点相同的 `422`/`503` 错误。

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

### 读取存储与 Space 信息（FR2-007）

- **方法 / 路径**：`GET /api/settings/storage`
- **请求头**：`X-JianVideo-Space-Id` 可选；缺省为 `space-default`
- **响应**（200）：
  ```json
  {
    "space": {"id": "space-default", "name": "默认 Space", "owner_user_id": 1},
    "data_dir": "D:/JianVideo/data",
    "database_path": "D:/JianVideo/data/jianvideo.db",
    "library_count": 3
  }
  ```
- **说明**：仅 Space owner 可读。`data_dir` 是 SQLite 索引库、缩略图、HLS 与可重建缓存所在的数据目录；原媒体仍保留在媒体库注册目录。Web 设置页展示这些信息，并跳转现有媒体库管理页完成目录增删改。
- **错误**：`400 INVALID_SPACE`、`401 UNAUTHORIZED`、`403 SPACE_FORBIDDEN`、`404 SPACE_NOT_FOUND`

### 读取运行期设置

- **方法 / 路径**：`GET /api/settings`
- **响应**（200）：
  ```json
  {
    "settings": {
      "scan_interval": "3600",
      "recycle_bin_paths": "{\"D\":\"D:/.recycle\"}",
      "transcode_hwaccel_mode": "auto",
      "transcode_hwaccel_fallback": "1"
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
- **说明**：批量 upsert 键值，同一 key 覆盖旧值；所有 key 必须先登记为 `runtime`，并通过 registry 类型校验。任一 key 未知、不可运行期修改或值类型非法时整体返回 `400`，不写入任何设置。提交成功后回读返回，并触发设置变更回调，使定时扫描周期（`scan_interval`）即时重排生效、无需重启（FR-28）。含 `ffmpeg_path`/`ffprobe_path`（非空）时，落库后即时应用到转码运行期（覆盖自动发现），保存即生效（FR-56）；含 `magick_path`（非空）时同理即时应用到 HEIC/RAW 转换运行期，保存即生效（FR-63）；含 `network_proxy` 时写入前校验协议和格式，落库后即时应用到后端出站 HTTP 运行期（空=直连、非空=设代理），支持 http/https/socks5/socks5h，保存即生效（FR-80）。含 `debug_log` 时落库后即时切换 GORM 日志级别（`"1"`/`"true"`=开启详细 SQL/慢查询日志、其余=安静），保存即生效（FR-110）；启动时读取该键决定初始级别，重启后保持。含 `media_inference_enabled` 或 `media_inference_disabled_libraries` 时即时刷新推断配置；若保存后总开关开启，自动为已有媒体入队缺失项增量推断任务，人工推断保持不变（FR2-031）。
- **已知运行期键**：`scan_interval`（定时扫描周期秒）、`recycle_bin_paths`（盘符→回收站目录 JSON）、`update_channel`（`stable`/`prerelease`）、`transcode_codec_priority`（首选目标编码优先级 JSON 数组）、`transcode_hwaccel_mode`（硬件转码策略：`auto/software/nvenc/qsv/amf/vaapi/videotoolbox`）、`transcode_hwaccel_fallback`（硬件失败软件回退，`"1"`=开启、`"0"`=关闭）、`ffmpeg_path`/`ffprobe_path`（FR-56，可执行文件路径，非空覆盖自动发现）、`magick_path`（FR-63，ImageMagick magick 可执行文件路径，非空覆盖自动发现）、`network_proxy`（FR-80，后端出站网络代理 URL，空=直连，敏感不回显）、`debug_log`（FR-110，运行时调试日志开关，`"1"`=开启 GORM 详细日志、其余=安静）、`media_inference_enabled`（FR2-031，本地影视信息推断总开关，`"1"`/`"true"`=开启）、`media_inference_disabled_libraries`（FR2-031，按库关闭推断的库 ID JSON 数组）、`upload_target_dir`、`upload_naming_rule`、`open_tabs`、`last_opened_path`。
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
