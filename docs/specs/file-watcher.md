# 功能规格：实时路径监控（File Watcher）

> 状态：开发中　·　关联 PRD：FR-03　·　分支：feature/fr-03-watcher

## 1. 背景与目标

用户添加本地目录作为媒体库后，需要自动感知目录中的文件变化（新增、删除、重命名），实时增量更新媒体库索引，免去手动扫描的等待。

属于 P1（MVP）第一期核心功能。

## 2. 需求（要什么）

- 利用 `github.com/fsnotify/fsnotify` v1.10.1 对所有已注册的媒体库目录进行递归监听
- 识别视频文件：后缀名白名单 `.mp4 .mkv .avi .mov .webm .rmvb .ts .flv .wmv .m4v`
- 文件 Create/Write 事件触发元数据提取（写入 media_files 表），采用去抖策略：连续 500ms 无新事件后才处理，避免读取不完整文件
- 文件 Remove/Rename 事件移除对应的 media_files 记录
- 启动时从 `internal/library` 获取所有已注册的目录列表，逐一添加监听
- 提供 `Start()` / `Stop()` 生命周期方法，由 main.go 在启动/关闭时调用

范围内：
- 本地目录的 fsnotify 递归监听
- 视频文件过滤（后缀名白名单）
- 去抖机制（500ms）
- Create/Write → 入库，Remove/Rename → 删除记录

不做（范围外）：
- SMB 网络共享路径的监听（FR-02 范畴，SMB 挂载后路径视同本地处理）
- 文件内容变化后的元数据更新（如视频被覆盖写入，视为删除后重新创建）
- 目录子目录的创建/删除监听（fsnotify 递归监听已覆盖）

## 3. 设计（怎么做）

### 模块：`internal/watcher/watcher.go`

**依赖方向**：`watcher` → `library.Service`（单向，watcher 调用 library 的 CRUD 方法）

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
2. `Start()` — 从 library 获取所有目录路径，逐一 `Add()` 递归监听，启动事件循环 goroutine
3. `Stop()` — 关闭 fsnotify，停止事件循环
4. 事件循环：
   - 收到 Create/Write 事件 → 检查后缀名白名单 → 启动/重置 500ms 去抖计时器 → 计时器到期后调用 `library.CreateMediaFile()` 入库
   - 收到 Remove/Rename 事件 → 根据 file_path 查找并删除 media_files 记录

**去抖机制**：
- 使用 `map[string]*time.Timer`，key 为文件路径
- 每次事件重置对应计时器，500ms 无新事件后触发处理
- 计时器回调中调用 `time.Sleep(500ms)` 等待写入完成，再入库

**视频文件白名单**：

```go
var videoExts = map[string]bool{
    "mp4": true, "mkv": true, "avi": true, "mov": true,
    "webm": true, "rmvb": true, "ts": true, "flv": true,
    "wmv": true, "m4v": true,
}
```

### 与 library 模块协作

- watcher 持有 `*library.Service` 引用
- Create/Write → 调用 `library.CreateMediaFile(libraryID, filePath, fileSize)`
- Remove/Rename → 通过 file_path 查询后调用 `library.DeleteMediaFile(id)`
- 需要 library 提供 `ListLibraryPaths()` 获取目录列表，以及根据 file_path 查找 MediaFile 的方法

### 对 library.Service 的扩展

为支持 watcher 的删除逻辑，需要在 `library.Service` 中新增方法：
- `DeleteMediaFileByPath(filePath string) error` — 根据文件路径删除记录

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

- 创建视频文件到已注册目录 → 500ms 内 media_files 表出现对应记录
- 删除已索引的视频文件 → media_files 表对应记录被移除
- 非视频文件（.txt/.jpg 等）→ 不触发入库
- 快速连续写入同一文件 → 只入库一次（去抖生效）
- Start/Stop 可正常启停，无 goroutine 泄露

## 6. 风险 / 待定

- **SMB 路径**：fsnotify 对 SMB 挂载路径的支持取决于操作系统挂载方式，当前只做本地目录监听，SMB 路径留待 FR-02 处理
- **大量文件同时写入**：去抖 map 在高并发场景下的内存占用需要关注，但单用户场景下规模可控
