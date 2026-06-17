# 功能规格：多目录聚合管理

> 状态：开发中　·　关联 PRD：FR-01　·　分支：feature/fr-01-library

## 1. 背景与目标
解决家庭用户多个硬盘/目录中视频文件分散的问题。用户需要将多个本地根目录统一汇聚到一个媒体库中，以便集中浏览和管理。属于第一期（MVP）P1 能力。

## 2. 需求（要什么）
- 支持添加多个本地根目录作为媒体库源
- 支持为每个目录设置类型（local/smb）、标签
- 支持目录的增删，删除时级联删除关联的媒体文件记录
- 支持扫描目录下的视频文件（mp4/mk4/avi/mov/webm/flv/wmv/ts/m4v/mpg/mpeg/3gp/rmvb/rm）
- 支持媒体文件的 CRUD：添加记录、列表查询（分页/排序/搜索）、详情查看、删除
- 列表查询支持按 media库过滤、按时间/名称排序、关键词搜索

范围内：目录注册、视频文件索引、媒体文件 CRUD API
不做（范围外）：SMB 网络共享挂载、实时文件监听（fsnotify）、FFmpeg 元数据提取

## 3. 设计（怎么做）

### 模块
- `internal/config`：应用配置（端口、FFmpeg 路径、DB 路径）
- `internal/db`：数据库初始化、连接管理
- `internal/db/models`：LibraryPath、MediaFile 数据模型
- `internal/library`：媒体库业务逻辑（目录管理、文件索引、扫描）
- `internal/api`：HTTP API handler 和路由注册
- `main.go`：应用入口，组装各模块

### 数据模型
- `library_paths`：id, path(UNIQUE), type, label, enabled, created_at
- `media_files`：id, library_id(FK), file_path, file_name, file_size, format, video_codec, audio_codec, duration, width, height, bitrate, subtitle_tracks, added_at, modified_at

### API 端点
| 方法 | 路径 | 说明 |
|---|---|---|
| GET | /api/library/paths | 目录列表 |
| POST | /api/library/paths | 添加目录 |
| DELETE | /api/library/paths/:id | 删除目录 |
| GET | /api/library/media | 媒体文件列表 |
| GET | /api/library/media/:id | 媒体文件详情 |
| DELETE | /api/library/media/:id | 删除媒体文件 |
| POST | /api/library/scan/:id | 扫描目录索引视频 |

### 关键机制
- SQLite WAL 模式，DSN: `file:jianvideo.db?cache=shared&_journal_mode=WAL&_busy_timeout=5000`
- 删除目录使用事务：先删关联媒体文件，再删目录记录
- 路径转为绝对路径后存储，避免相对路径歧义
- 扫描时自动去重（按 file_path 判重）

## 4. 任务拆分
- [x] 项目基础结构（go.mod、main.go、config、db）
- [x] 数据模型定义（LibraryPath、MediaFile）
- [x] Library 业务逻辑（CRUD + 扫描）
- [x] API Handler + 路由注册
- [x] 服务层测试 + API 层测试
- [ ] 文档同步：PRD 状态、ARCHITECTURE、CHANGELOG

## 5. 验收标准
- 添加本地目录后返回完整记录（含 ID、绝对路径、类型、标签）
- 查询目录列表返回所有已注册目录
- 删除目录后，关联的媒体文件记录一并删除
- 扫描目录后，视频文件正确入库，非视频文件被忽略
- 重复扫描不重复入库
- 媒体文件列表支持分页、排序（time_desc/time_asc/name）、关键词搜索
- 所有 API 返回符合 docs/APP.md 约定的 JSON 格式

## 6. 风险 / 待定
- 当前扫描为同步阻塞，大目录可能耗时较长，后续可改为异步
- 视频格式识别仅靠扩展名，后续可结合 ffprobe 做真实格式检测
