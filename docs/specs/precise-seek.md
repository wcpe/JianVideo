# 功能规格：精准进度拖拽

> 状态：开发中　·　关联 PRD：FR-19、FR-20　·　分支：feature/fr-19-seek

## 1. 背景与目标

用户拖动进度条后，画面需要快速更新（< 2 秒）。当前仅具备媒体库管理 API，缺少播放流控接口。本功能实现后端 HTTP Range 请求支持、Seek 操作 API，并为前端双进度条提供数据支撑。

属于第一期（MVP）P1 能力。

## 2. 需求（要什么）

- **FR-19 后端**：全面支持 HTTP Range（Content-Range / 206 Partial Content），对媒体文件按字节范围响应
- **FR-19 Seek API**：`POST /api/play/:id/seek` 接收目标时间戳（秒），后端按需 Range 请求定位
- **FR-19 播放流 API**：`GET /api/play/:id/stream` 返回媒体文件流，支持 `Range` 请求头
- **FR-20 进度查询 API**：`GET /api/play/:id/progress` 返回当前播放进度和缓冲信息
- 范围内：HTTP Range 解析、Seek 操作、播放进度查询、缓冲区间上报
- 不做（范围外）：前端 UI 组件（由前端分支负责）、mpegts.js 播放器集成（FR-16）、转码管道（FR-08/09）

## 3. 设计（怎么做）

### 3.1 模块改动

| 模块 | 改动 |
|---|---|
| `internal/api` | 新增 `PlayHandler`，注册 `/api/play/:id/stream`、`/api/play/:id/seek`、`/api/play/:id/progress` |
| `internal/playback` | 新增播放流控服务：Range 请求解析、文件流输出、Seek 定位、进度追踪 |

### 3.2 接口契约

#### GET /api/play/:id/stream

- 接收 `Range: bytes=START-END` 请求头
- 解析 Range，返回 `206 Partial Content` + `Content-Range` 响应头
- 无 Range 请求头时返回 `200 OK` + 完整文件
- `Content-Type` 根据文件扩展名确定（MIME）
- `Accept-Ranges: bytes` 始终在响应头中

#### POST /api/play/:id/seek

- 请求体：`{"position": <float>}`, 单位秒
- 返回：`{"status": "ok", "position": <float>}`
- 实际 Seek 由播放器客户端通过 Range 请求实现，本 API 记录 Seek 位置并返回确认

#### GET /api/play/:id/progress

- 返回：`{"current_position": <float>, "buffered_ranges": [[start, end], ...], "duration": <float>, "file_size": <int64>}`
- 缓冲区间由播放器前端上报（POST），后端存储最近的缓冲状态

### 3.3 Range 请求处理流程

```
客户端请求 Range: bytes=1000-2000
  → 解析 Range header
  → 打开文件，Seek 到 1000
  → 设置响应头：
    Content-Range: bytes 1000-2000/5000000
    Content-Length: 1001
    Accept-Ranges: bytes
  → 返回 206 Partial Content
  → 流式写入 1001 字节
```

### 3.4 数据模型

新增 `playback_sessions` 表：

| 字段 | 类型 | 说明 |
|---|---|---|
| id | INTEGER PK | 自增主键 |
| media_id | INTEGER FK | 关联媒体文件 |
| client_ip | TEXT | 客户端 IP |
| current_position | REAL | 当前播放位置（秒） |
| duration | REAL | 媒体总时长（秒） |
| file_size | INTEGER | 文件大小（字节） |
| buffered_ranges | TEXT | 已缓冲区间 JSON |
| created_at | DATETIME | 创建时间 |
| updated_at | DATETIME | 最后更新时间 |

## 4. 任务拆分

- [x] 创建规格文档 `docs/specs/precise-seek.md`
- [x] 更新 PRD FR-19 状态 → 开发中
- [x] 写 ADR-0022
- [ ] 新增 `internal/playback` 模块：Range 请求解析、Seek、进度追踪
- [ ] 新增 `internal/api/play_handler.go`：播放 API 处理器
- [ ] 更新 `internal/db/models`：新增 PlaybackSession 模型
- [ ] 更新 `internal/api/router.go`：注册播放路由
- [ ] 更新 `main.go`：初始化 playback 服务
- [ ] 编写测试用例（红→绿）
- [ ] 更新 CHANGELOG
- [ ] 文档同步自检

## 5. 验收标准

- `GET /api/play/:id/stream` 带 `Range: bytes=0-1023` 返回 206 + `Content-Range` 头
- `GET /api/play/:id/stream` 无 Range 返回 200 + 完整文件
- `POST /api/play/:id/seek` 返回确认 JSON
- `GET /api/play/:id/progress` 返回进度 JSON
- 所有新增和已有测试通过

## 6. 风险 / 待定

- 大文件（>4GB）的 int64 范围处理：使用 `http.ServeContent` 可自动处理
- MIME 类型覆盖：建立常见视频扩展名映射表
- 并发 Seek：每个 Seek 独立 HTTP 请求，无状态竞争
- 缓冲区间上报频率：前端每 5 秒上报一次即可
