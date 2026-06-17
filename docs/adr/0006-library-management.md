# ADR-0006: 多目录聚合管理

## 状态
已接受

## 背景
家庭用户的视频文件分散在多个硬盘/目录中，需要统一汇聚到一个媒体库进行浏览和管理。系统需要支持注册多个本地根目录，自动扫描索引其中的视频文件，并提供媒体文件的 CRUD API。这是第一期（MVP）P1 能力。

## 决策
在 `internal/library` 包中实现多目录聚合管理业务逻辑，通过 `internal/api` 暴露 RESTful HTTP 接口。数据模型新增 `library_paths`（目录注册）和 `media_files`（媒体文件元数据）两张表，使用 GORM 自动迁移管理 Schema。

## 理由
- 遵循架构不变量中的模块分层：`web`（api）→ `media-library`（library）→ `db`，依赖方向严格单向
- 目录和媒体文件分开管理，删除目录时事务级联删除关联媒体文件，保持数据一致性
- 扫描时按扩展名过滤视频格式，去重逻辑基于 file_path 唯一性检查，避免重复入库
- 不影响后续 SMB 支持，`type` 字段预留了 `local`/`smb` 两种类型

## 后果
- 新增 `internal/library` 包，包含目录和媒体文件的完整 CRUD
- 新增 `LibraryPath` 和 `MediaFile` 两个 GORM 模型
- API 路由新增 `/api/library` 分组，包含 paths 和 media 两个子资源
- 后续 SMB 支持、文件监听（fsnotify）可在此基础上扩展

## 备选方案
- 在 `internal/db` 中直接编写业务逻辑：违反架构不变量，db 模块应仅提供纯数据读写
- 引入独立的 `media-library` 模块（与 `db` 平级）：当前阶段不需要额外抽象，`library` 包已足够
