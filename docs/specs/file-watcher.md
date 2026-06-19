# 功能规格：实时路径监控（File Watcher）

> 状态：开发中　·　关联 PRD：FR-03　·　分支：feature/fr-03-watcher

## 1. 背景与目标

用户添加本地目录作为媒体库后，需要自动感知目录中的文件变化（新增、删除、重命名），实时增量更新媒体库索引，免去手动扫描的等待。

属于 P1（MVP）第一期核心功能。

## 2. 需求（要什么）

- 利用 `github.com/fsnotify/fsnotify` v1.10.1 对所有已注册的媒体库目录进行递归监听
- 识别媒体文件：复用 `library.Service` 的统一后缀策略，支持内置视频/图片后缀，并支持当前 `LibraryPath` 绑定的自定义后缀
- 文件 Create/Write 事件触发媒体文件入库（写入 media_files 表），采用去抖策略：连续 500ms 无新事件后才处理，避免读取不完整文件
- 文件 Remove/Rename 事件移除对应的 media_files 记录
- 启动时从 `internal/library` 获取所有已注册的目录列表，逐一添加监听
- 提供 `Start()` / `Stop()` 生命周期方法，由 main.go 在启动/关闭时调用

范围内：
- 本地目录的 fsnotify 递归监听
- 媒体文件过滤（统一后缀策略）
- 去抖机制（500ms）
- Create/Write → 入库，Remove/Rename → 删除记录
- SMB 目录通过定时轮询触发 library 扫描

不做（范围外）：
- SMB 网络共享路径的原生 fsnotify 事件监听（SMB 使用轮询兜底）
- 文件内容变化后的元数据更新（如视频被覆盖写入，视为删除后重新创建）
- 目录子目录的创建/删除监听（fsnotify 递归监听已覆盖）

## 3. 设计（怎么做）

### 模块：`internal/watcher/watcher.go`

**依赖方向**：`watcher` → `library.Service`（单向，watcher 调用 library 的 CRUD/扫描方法；watcher 不直接操作 DB）

**核心结构**：

```go
type Watcher struct {
    watcher  *fsnotify.Watcher
    library  *library.Service
    debounce map[string]*time.Timer  // 路径 → 去抖计时器
    mu       sync.Mutex
}
```

**关键流程**：

1. `New(library *library.Service) *Watcher` — 创建 fsnotify 实例
2. `Start()` — 从 library 获取所有目录路径，本地目录逐一 `Add()` 递归监听，SMB 目录加入轮询列表，启动事件循环 goroutine
3. `Stop()` — 关闭 fsnotify，停止事件循环
4. 事件循环：
   - 收到 Create/Write 事件 → 根据路径匹配所属 `library_id` → 调用 `library.IsMediaFileForLibrary()` 检查后缀 → 启动/重置 500ms 去抖计时器 → 计时器到期后调用 `library.CreateMediaFile()` 入库
   - 收到 Remove/Rename 事件 → 根据 file_path 委托 library 删除 media_files 记录
5. SMB 轮询：定时调用 `library.ScanLibraryWithType(libraryID, path, "smb")`，不直接写 DB

**去抖机制**：
- 使用 `map[string]*time.Timer`，key 为文件路径
- 每次事件重置对应计时器，500ms 无新事件后触发处理
- 计时器回调中调用 `time.Sleep(500ms)` 等待写入完成，再入库

**媒体后缀策略**：

- 内置视频后缀：`mp4/mkv/avi/mov/webm/flv/wmv/ts/m4v/mpg/mpeg/3gp/rmvb/rm`
- 内置图片后缀：`jpg/jpeg/png/gif/webp/bmp/tif/tiff/heic/heif`
- 自定义后缀存于 `media_extensions`，按 `library_id` 绑定到单个媒体库目录
- watcher 不维护独立白名单，统一调用 `library.Service` 判断，避免扫描和监听规则漂移

### 与 library 模块协作

- watcher 持有 `*library.Service` 引用
- `ListLibraryPaths()` 获取目录列表，并维护 `path → library_id` 映射
- Create/Write → 调用 `library.IsMediaFileForLibrary(libraryID, filePath)` 判定，再调用 `library.CreateMediaFile(libraryID, filePath, fileSize)`
- Remove/Rename → 委托 `library.DeleteMediaFileByPath(filePath)` 删除记录
- SMB 目录 → 委托 `library.ScanLibraryWithType(libraryID, path, "smb")` 轮询扫描

### 对 library.Service 的要求

- `DeleteMediaFileByPath(filePath string) error` — 根据文件路径删除记录
- `IsMediaFileForLibrary(libraryID int64, path string) bool` — 统一媒体后缀判断
- `ScanLibraryWithType(libraryID int64, path, type string) (int, error)` — 按目录类型分发扫描

## 4. 任务拆分

- [x] 复制 FR-01 骨架代码到 watcher worktree
- [ ] 编写规格文档 `docs/specs/file-watcher.md`
- [ ] 更新 PRD §4 FR-03 状态为「开发中」
- [ ] 编写 ADR-0007
- [ ] 添加 fsnotify 依赖
- [ ] 扩展 library.Service 增加 DeleteMediaFileByPath
- [ ] 测试先行：编写 watcher_test.go（红）
- [ ] 实现 watcher.go（绿）
- [ ] 运行测试确认全绿
- [ ] 更新 CHANGELOG
- [ ] 中文 commit

## 5. 验收标准

- 创建内置视频/图片文件到已注册目录 → 500ms 去抖后 media_files 表出现对应记录
- 创建当前媒体库目录自定义后缀文件 → 500ms 去抖后 media_files 表出现对应记录
- 删除已索引的媒体文件 → media_files 表对应记录被移除
- 非媒体文件（如 `.txt`）→ 不触发入库
- 快速连续写入同一文件 → 只入库一次（去抖生效）
- Start/Stop 可正常启停，无 goroutine 泄露

## 6. 风险 / 待定

- **SMB 路径**：不依赖 fsnotify 事件，当前通过 5 分钟轮询触发 library 扫描；实时性弱于本地目录
- **大量文件同时写入**：去抖 map 在高并发场景下的内存占用需要关注，但单用户场景下规模可控
