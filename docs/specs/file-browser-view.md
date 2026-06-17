# 功能规格：文件目录视图

> 状态：开发中　·　关联 PRD：FR-15　·　分支：feature/fr-15-filebrowser-v2

## 1. 背景与目标
解决用户在媒体库中以传统文件资源管理器风格浏览视频文件的问题。用户需要看到完整的文件夹层级结构，能够像操作文件管理器一样逐目录导航浏览视频文件。属于第一期（MVP）P1 能力。

## 2. 需求（要什么）
- 后端提供 `GET /api/media/browse` 接口，支持按目录路径浏览该目录下的文件和子目录
- 支持面包屑导航（从根目录到当前目录的路径分段）
- 支持按 `library_id` 过滤（可选参数），只浏览指定媒体库下的内容
- 前端左侧显示目录树形结构（侧边栏），右侧显示当前目录下的文件列表
- 点击文件跳转播放页，点击目录进入该目录
- 顶部面包屑导航，支持点击跳转到任意上级目录
- 文件列表展示文件名、大小、格式、时长、分辨率等信息

范围内：后端目录浏览 API、前端文件目录视图页面（侧边栏 + 内容区 + 面包屑）
不做（范围外）：文件上传/下载/重命名/删除、拖拽排序、缩略图预览、多选操作

## 3. 设计（怎么做）

### 后端模块
- `internal/library/service.go`：新增 `BrowseDirectory` 方法，读取指定目录下的文件和子目录
  - 扫描文件系统获取子目录列表
  - 从 SQLite 查询该目录下的已索引媒体文件
  - 组装面包屑路径分段
- `internal/api/handler.go`：新增 `BrowseDirectory` handler
  - 参数：`path`（必填，目录绝对路径），`library_id`（可选，int64）
  - 返回：面包屑分段、子目录列表、媒体文件列表
- `internal/api/router.go`：注册 `GET /api/media/browse` 路由

### 前端模块
在 `frontend/` 下创建完整的 React + TypeScript 项目：
- `src/pages/FileBrowserPage.tsx`：文件目录视图主页面
  - 左侧固定侧边栏：目录树形结构
  - 右侧内容区：当前目录下的文件列表（复用 MediaCard 组件模式）
  - 顶部面包屑导航
- `src/store/fileBrowser.ts`：zustand store，管理当前路径、面包屑、目录树
- `src/components/DirectoryTree.tsx`：左侧目录树组件
- `src/components/Breadcrumb.tsx`：面包屑导航组件
- `src/components/FileListItem.tsx`：文件列表行组件
- `src/api/client.ts`：API 客户端，封装 browse 接口调用
- `src/types/media.ts`：类型定义（扩展现有类型，含 BrowseResponse）
- `src/App.tsx`：路由入口

### API 契约
`GET /api/media/browse?path=/media/movies&library_id=1`

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
- 目录浏览：后端通过 `os.ReadDir` 读取文件系统获取子目录，从 SQLite 按 `file_path` 前缀匹配获取已索引的媒体文件
- 面包屑：将当前路径按 `filepath.Segments` 拆分，逐段累加构建完整路径
- 前端目录树：懒加载模式，点击展开时才加载子目录
- 媒体文件跳转：点击文件跳转 `/play/:id`，点击目录更新当前路径

## 4. 任务拆分
- [x] 后端 Service：BrowseDirectory 方法（文件系统扫描 + SQLite 查询 + 面包屑构建）
- [x] 后端 API：GET /api/media/browse handler + 路由注册
- [x] 后端测试：Service 层 + API 层测试用例
- [x] 前端项目脚手架：package.json、vite.config.ts、tsconfig.json、index.html
- [x] 前端类型定义与 API 客户端
- [x] 前端页面与组件（FileBrowserPage、DirectoryTree、Breadcrumb、FileListItem）
- [x] App.tsx 路由注册
- [x] 文档同步：PRD 状态、ARCHITECTURE、API、CHANGELOG

## 5. 验收标准
- `GET /api/media/browse?path=/some/dir` 返回该目录下的子目录列表和已索引的媒体文件列表
- 返回的面包屑数组正确反映从根到当前目录的路径层级
- 传入 `library_id` 时，只返回该媒体库下的媒体文件
- 不存在的目录返回空列表，不返回 500 错误
- 前端文件目录视图页面展示左侧目录树、右侧文件列表、顶部面包屑
- 点击文件跳转播放页，点击目录进入该目录
- 面包屑支持点击跳转到任意上级目录
- 所有新增测试用例通过

## 6. 风险 / 待定
- 目录路径中的特殊字符（如空格、中文）需正确处理 URL 编码
- 大目录（含数千文件）的文件列表可能较长，当前不做虚拟滚动，后续可优化
- 目录树懒加载需要递归扫描文件系统，深层目录可能耗时，后续可加缓存
- 前端目录树组件与 FR-14 的时间轴视图共用 App.tsx，rebase 时需注意冲突
