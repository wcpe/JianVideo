# 功能规格：收藏与标签

> 状态：开发中　·　关联 PRD：FR-41　·　分支：feature/fr-41-tags

## 1. 背景与目标

为媒体文件提供「收藏」与「自定义标签」两类组织能力，属于 P2 媒体管理增强。
用户可对单个媒体标星收藏、可创建标签并给媒体打/去标签，并能按「仅收藏」或「指定标签」筛选媒体列表，
方便在大量媒体中快速定位关注内容。基础数据模型（`media_files.Favorite` 字段、`tags`/`tag_mappings` 表）已由 foundation 提交备好，本特性只补业务逻辑与 UI。

## 2. 需求（要什么）

- 收藏切换：对单个媒体设置/取消收藏标记（幂等，重复设置同值不报错）。
- 标签管理：创建标签（按名唯一）、列出全部标签。
- 媒体打标签：给媒体绑定已有或新建标签；去标签解除绑定；列出某媒体的标签。
- 筛选：媒体列表支持「仅收藏」过滤与「按标签 ID」过滤，可与既有 `library_id`/`search`/分页/排序组合。
- 范围内：本地与已索引媒体；标签为扁平结构（无层级/无颜色）。
- 不做（范围外）：标签重命名/删除标签本体（仅去除媒体上的绑定）、标签层级/分组、按多标签 AND/OR 组合筛选、收藏夹独立视图（沿用现有列表 + 筛选）。

## 3. 设计（怎么做）

复用既有分层：`api`（HTTP）→ `library.Service`（业务）→ `db`（GORM）。无新架构决策，不写 ADR。

### 数据模型（foundation 已建，不改结构）

- `media_files.favorite`（bool）：收藏标记。
- `tags`：`id` / `name`（唯一）/ `created_at`。
- `tag_mappings`：`id` / `tag_id` / `media_id`，`(tag_id, media_id)` 唯一索引去重。

### 后端服务（`internal/library/service.go` 新增方法）

- `SetMediaFavorite(id int64, favorite bool) (*MediaFile, error)`：更新收藏标记并回读返回。
- `CreateTag(name string) (*Tag, error)`：名规整后 `FirstOrCreate`，空名报错。
- `ListTags() ([]Tag, error)`：按 `name` 升序。
- `ListMediaTags(mediaID int64) ([]Tag, error)`：联表查某媒体的标签。
- `AddMediaTag(mediaID int64, tagID int64) error`：校验媒体与标签存在后建映射（已存在则幂等）。
- `RemoveMediaTag(mediaID int64, tagID int64) error`：删除映射。
- `ListMediaFiles` 扩展：新增 `favorite *bool` 与 `tagID int64` 过滤参数；`tagID>0` 时按 `tag_mappings` 子查询限定。

### 后端端点（`internal/api`）

- `PUT  /api/library/media/:id/favorite`，体 `{"favorite": true}`，返回更新后的媒体对象。
- `GET  /api/library/tags`，返回 `{"items": [...]}`。
- `POST /api/library/tags`，体 `{"name": "..."}`，返回创建/已存在的标签（201）。
- `GET  /api/library/media/:id/tags`，返回 `{"items": [...]}`。
- `POST /api/library/media/:id/tags`，体 `{"tag_id": 1}` 或 `{"name": "..."}`（名时先建/取标签再绑定），返回绑定后的标签（201）。
- `DELETE /api/library/media/:id/tags/:tag_id`，返回 204。
- `GET /api/library/media` 增查询参数：`favorite=true`、`tag_id=N`。

### 前端（`frontend/src`）

- `api/library.ts` + `api/library.mock.ts`：补 real + mock 双实现与导出（沿用 `VITE_USE_MOCK` 切换）。
- 类型：`MediaFile` 增 `favorite`；新增 `Tag` 类型。
- 时间轴媒体卡片加标星按钮（点击切换收藏，阻止冒泡不触发打开）。
- 时间轴顶部加「仅收藏」开关与「标签」筛选下拉，接入列表查询。
- 标签管理：在媒体卡片/筛选区提供创建标签、给媒体打/去标签的入口（轻量 UI，沿用 Mantine）。

## 4. 任务拆分

- [x] service 层方法 + 单测（收藏切换、标签建/列、打/删标签、按收藏/标签过滤）
- [x] handler + 路由 + 端点测试
- [x] 前端 api 双实现 + 类型 + MSW handler
- [x] 前端标星按钮 + 标签筛选/管理 UI + 组件测试
- [x] 文档同步：PRD 状态、API.md、ARCHITECTURE.md、CHANGELOG

## 5. 验收标准

- 对媒体标星与取消标星，`media_files.favorite` 正确更新，重复设同值幂等不报错（service + handler 测试覆盖）。
- 创建标签按名唯一；给媒体打标签后可查到，去标签后查不到，重复打同标签幂等（service + handler 测试覆盖）。
- `GET /api/library/media?favorite=true` 仅返回收藏项；`?tag_id=N` 仅返回打了该标签的项；可与分页/搜索组合（service + handler 测试覆盖）。
- 前端：媒体卡片标星按钮可切换并反映收藏态；可按「仅收藏」「指定标签」筛选列表；可创建标签并给媒体打/去标签（组件 + MSW 测试覆盖）。
- 后端 `go test ./internal/library/... ./internal/api/...` 全绿；前端 `npm run build` 与 `npm run test` 全绿。

## 6. 风险 / 待定

- 标签名规整规则取最小：去首尾空白、非空即可，不限字符集（与媒体后缀的严格校验不同，标签是用户自由文本）。
- 不提供删除标签本体端点，避免牵连已绑定媒体；范围外，后续如需再加。
