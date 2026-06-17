# ADR-0007：fsnotify 实时路径监控

## 状态
已接受

## 背景
FR-03 要求媒体库目录中的文件新增、删除、移动时自动增量更新媒体库索引。需要选择一个跨平台的文件系统事件监听方案，监听已注册目录的递归子目录变化。候选方案包括：`fsnotify/fsnotify`（跨平台 inotify/FSEvents/ReadDirectoryChangesW 封装）、`golang.org/x/exp/inotify`（仅 Linux）、定时轮询。

## 决策
采用 `github.com/fsnotify/fsnotify` v1.10.1 实现实时路径监控。

## 理由
- **跨平台**：fsnotify 统一封装了 Linux inotify、macOS FSEvents、Windows ReadDirectoryChangesW，契合项目跨平台目标。
- **递归监听**：支持对目录树递归添加监听器，子目录创建后自动纳入监听。
- **成熟稳定**：v1.10.1 是稳定版本，Go 1.24+ 兼容，社区广泛使用。
- **去抖友好**：事件驱动模型天然适合去抖策略，只需在事件处理层加计时器即可实现 500ms 写入完成检测。
- **轻量级**：单依赖，无额外系统库要求。

## 后果
- watcher 模块依赖 library.Service（单向），文件事件触发媒体库 CRUD 操作。
- SMB 网络共享路径的监听依赖操作系统挂载，未挂载的 SMB 路径暂时无法监听（留待 FR-02 处理）。
- 去抖 map 在高并发写入场景下可能增长，但单用户家庭场景规模可控。

## 备选方案
- **定时轮询**：周期性扫描目录变化，实现简单但延迟高、I/O 开销大，不符合"实时"需求。
- **golang.org/x/exp/inotify**：仅支持 Linux，不满足跨平台要求。
- **平台原生 API 自行封装**：灵活但开发维护成本高，无必要。
