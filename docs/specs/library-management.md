# 功能规格：多目录聚合管理

> 状态：开发中　·　关联 PRD：FR-01　·　分支：feature/fr-01-library

## 1. 背景与目标
解决家庭用户多个硬盘/目录中视频文件分散的问题。用户需要将多个本地根目录统一汇聚到一个媒体库中，以便集中浏览和管理。属于第一期（MVP）P1 能力。

## 2. 需求（要什么）
- 支持添加多个本地根目录作为媒体库源
- 支持为每个目录设置类型（local/smb）、标签
- 支持目录的增删，删除时级联删除关联的媒体文件记录和该目录绑定的自定义后缀
- 支持递归扫描目录下的内置视频与图片文件；内置后缀包含 `mp4/mkv/avi/mov/webm/flv/wmv/ts/m4v/mpg/mpeg/3gp/rmvb/rm` 与 `jpg/jpeg/png/gif/webp/bmp/tif/tiff/heic/heif`
- 支持按媒体库目录追加自定义媒体后缀，追加后手动扫描与 watcher 增量入库使用同一套识别规则
- 支持媒体文件的 CRUD：添加记录、列表查询（分页/排序/搜索）、详情查看、删除、图片 raw 预览
- 列表查询支持按 media库过滤、按时间/名称排序、关键词搜索

范围内：目录注册、图片/视频文件索引、媒体文件 CRUD API、自定义后缀 API
不做（范围外）：SMB 网络共享挂载、实时文件监听（fsnotify）、FFmpeg 元数据提取

## 3. 设计（怎么做）

### 模块
- `internal/config`：应用配置（端口、FFmpeg 路径、DB 路径）
- `internal/db`：数据库初始化、连接管理
- `internal/db/models`：LibraryPath、MediaFile、MediaExtension 数据模型
- `internal/library`：媒体库业务逻辑（目录管理、文件索引、扫描）
- `internal/api`：HTTP API handler 和路由注册
- `main.go`：应用入口，组装各模块

### 数据模型
- `library_paths`：id, path(UNIQUE), type, label, enabled, created_at
- `media_files`：id, library_id(FK), file_path, file_name, file_size, format, video_codec, audio_codec, duration, width, height, bitrate, subtitle_tracks, added_at, modified_at
- `media_extensions`：id, library_id(FK), extension, type(video/image), is_builtin, created_at；唯一键为 `(library_id, extension)`

### API 端点
| 方法 | 路径 | 说明 |
|---|---|---|
| GET | /api/library/paths | 目录列表 |
| POST | /api/library/paths | 添加目录 |
| DELETE | /api/library/paths/:id | 删除目录 |
| GET | /api/library/media | 媒体文件列表 |
| GET | /api/library/media/:id | 媒体文件详情 |
| DELETE | /api/library/media/:id | 删除媒体文件 |
| GET | /api/library/media/:id/raw | 读取图片原始内容用于预览 |
| GET | /api/library/extensions?library_id=:id | 查询指定目录的内置与自定义后缀 |
| POST | /api/library/extensions | 为指定目录追加自定义后缀 |
| POST | /api/library/scan/:id | 按目录类型扫描并索引媒体文件 |

### 关键机制
- SQLite WAL 模式，DSN: `file:jianvideo.db?cache=shared&_journal_mode=WAL&_busy_timeout=5000`
- 删除目录使用事务：先删关联媒体文件和自定义后缀，再删目录记录
- 本地路径校验必须存在且为目录，路径转为绝对路径和正斜杠后存储；SMB 路径统一规范化为 `host/share/path` 以便扫描器解析
- 扫描时递归遍历子目录，按 `library_id + file_path` 去重
- 自定义后缀绑定到 `LibraryPath`，不得污染其他目录

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
- 删除目录后，关联的媒体文件记录和自定义后缀一并删除
- 扫描目录后，内置图片/视频文件与该目录自定义后缀文件正确入库，非媒体文件被忽略
- 重复扫描不重复入库，Windows 路径下的目录浏览不会生成 `/D:` 面包屑
- 媒体文件列表支持分页、排序（time_desc/time_asc/name）、关键词搜索
- 图片文件可通过 raw 接口在前端预览，视频文件仍进入播放页
- 所有 API 返回符合 docs/APP.md 约定的 JSON 格式

## 6. 风险 / 待定
- 当前扫描 API 仍为同步返回，前端提供不确定加载态；大目录后续可改为异步进度接口
- 媒体格式识别仅靠扩展名，后续可结合 ffprobe 做真实格式检测
