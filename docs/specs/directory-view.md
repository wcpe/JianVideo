# 功能规格：文件目录视图（FR-15）

> 状态：开发中　·　关联 PRD：FR-15　·　分支：feature/fr-15-directory-view

## 1. 背景与目标
解决用户在媒体库中以传统文件资源管理器风格浏览视频文件的问题。用户需要看到完整的文件夹层级结构，能够像操作文件管理器一样逐目录导航浏览视频文件。属于第二期 P1 能力，无依赖。

## 2. 需求（要什么）
- 后端新增 `GET /api/library/browse` 接口，支持按 `library_id` + `parent_path` 浏览该目录下的文件和子目录
- 目录树通过 `file_path` 前缀聚合（一次 SQL 查询 + Go 层 map 分组）
- 面包屑导航（从根目录到当前目录的路径分段）
- 前端 LibraryPage 添加 Tab 切换：「时间轴」|「文件目录」
- 文件目录视图：面包屑导航 + 当前目录文件列表（复用现有卡片样式）
- 为 `media_files` 表添加 `file_path` 索引以加速前缀查询

范围内：后端目录浏览 API、前端 Tab 切换 + 面包屑 + 文件列表
不做（范围外）：侧边栏目录树懒加载（V2 延后）、文件上传/下载/重命名/删除、拖拽排序

## 3. 设计（怎么做）

### 后端模块
- `internal/library/service.go`：新增 `BrowseDirectory(libraryID int64, parentPath string) (*models.BrowseResponse, error)`
  - SQL: `SELECT * FROM media_files WHERE file_path LIKE ? AND library_id = ? ORDER BY file_path`
  - Go 层聚合：遍历结果，按第一级子目录分组，区分文件和目录
  - 面包屑：按 `string(filepath.Separator)` 拆分 parentPath，逐段累加
- `internal/db/models/browse_response.go`：新增 `BrowseResponse`、`DirInfo`、`BreadcrumbItem` 数据模型
- `internal/api/handler.go`：新增 `BrowseDirectory` handler
  - 参数：`library_id`（必填，int64），`parent_path`（必填，string）
  - 返回：面包屑分段、子目录列表、媒体文件列表
- `internal/api/router.go`：注册 `GET /api/library/browse` 路由
- 数据库迁移：为 `media_files.file_path` 添加索引

### 前端模块
- `frontend/src/types/index.ts`：新增 `BrowseResponse`、`DirInfo`、`BreadcrumbItem` 类型
- `frontend/src/api/library.ts`：新增 `browseDirectory(libraryID, parentPath)` 函数
- `frontend/src/components/DirectoryBreadcrumb.tsx`：面包屑导航组件
- `frontend/src/pages/LibraryPage.tsx`：添加 Tabs 组件（时间轴 | 文件目录），集成目录浏览视图

### API 契约
`GET /api/library/browse?library_id=1&parent_path=/media/movies`

响应（200）：
```json
{
  "breadcrumbs": [
    {"name": "media", "path": "/media"},
    {"name": "movies", "path": "/media/movies"}
  ],
  "directories": [
    {"name": "动作片", "path": "/media/movies/动作片"},
    {"name": "喜剧片", "path": "/media/movies/喜剧片"}
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

### 关键机制
- 目录浏览：一次 SQL 查询获取所有 `file_path` 以 `parentPath` 为前缀的媒体文件，Go 层按第一级子目录聚合分组
- 面包屑：后端按路径分隔符拆分 `parentPath`，逐段累加构建
- 前端 Tab 切换：Mantine Tabs 组件，时间轴视图和文件目录视图共存于 LibraryPage
- 点击目录：更新 `parentPath` 状态，重新请求 browse API
- 点击面包屑：跳转到对应路径
- 点击文件：跳转 `/play/:id`

## 4. 任务拆分
- [ ] 后端：BrowseResponse 数据模型
- [ ] 后端：file_path 索引迁移
- [ ] 后端：Service BrowseDirectory 方法 + 测试（红→绿）
- [ ] 后端：Handler + Router + 测试（红→绿）
- [ ] 前端：类型定义 + API 函数
- [ ] 前端：DirectoryBreadcrumb 组件
- [ ] 前端：LibraryPage Tab 集成
- [ ] 文档同步：ARCHITECTURE、API、CHANGELOG

## 5. 验收标准
- `GET /api/library/browse?library_id=1&parent_path=/media` 返回该目录下的子目录列表和已索引的媒体文件列表
- 返回的面包屑数组正确反映从根到当前目录的路径层级
- 不存在的目录返回空列表，不返回 500 错误
- 前端 LibraryPage 支持 Tab 切换时间轴/文件目录视图
- 面包屑支持点击跳转到任意上级目录
- 点击子目录进入该目录，点击文件跳转播放页
- 所有新增测试用例通过

## 6. 风险 / 待定
- 目录路径中的特殊字符（如空格、中文）需正确处理 URL 编码
- 大目录（含数千文件）的文件列表可能较长，当前不做虚拟滚动，后续可优化
- 侧边栏目录树懒加载延后到 V2
