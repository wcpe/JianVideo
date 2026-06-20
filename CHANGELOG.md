# 变更日志

本项目所有重要变更记录于此。

格式遵循 [Keep a Changelog](https://keepachangelog.com/zh-CN/1.1.0/)，版本号遵循 [语义化版本](https://semver.org/lang/zh-CN/)。

## 未发布版本

### 安全
- **SMB 主密码强制显式配置（FR-02）**：`smb.MasterPassword()` 不再在 `SMB_MASTER_PASSWORD` 未设置时回退公开弱默认值，改为返回错误；显式空串同样视为未配置。未配置时全部 Save/Load 调用点拒绝以弱密钥加解密——`POST /api/smb/credentials` 返回 `503`，添加 SMB 库路径仅记录 ERROR 不落弱密钥，SMB 扫描/播放返回明确错误。消除「拿到 `smb_credentials.enc` 即可用公开常量离线解出明文密码」的风险。同步更新 `docs/API.md`、`docs/OPERATIONS.md`。

## 0.3.0（2026-06-21）

### 修复
- **SMB 凭据（FR-02）主密码一致性与密码持久化**：修复保存时用 `SMB_MASTER_PASSWORD`/默认主密码、加载时却用空串，导致加密与解密主密码不一致、凭据永远无法读回的缺陷；同时修复 `Credentials.Password` 因 `json:"-"` 未写入加密文件、SMB 连接始终使用空密码的问题。新增 `smb.MasterPassword()` 作为加解密主密码唯一来源，统一全部 Save/Load 调用点（`saveSMBConfig`/`SaveSMBCredentials`/`library`/`playback`）。

### 变更
- **ABR 自适应码率（FR-07）真机验收交付**：多码率 master.m3u8 + hls.js ABR 链路（代码自 v0.1.0 已落地、未变更）经真机验证（`blob:` MSE 播放、多码率清单生效），状态由开发中转为已交付。
- **双进度条（FR-20）真机验收交付**：VideoPlayer 缓冲进度 + 播放进度双滑块（代码自 v0.2.0 已落地、未变更）经真机验证，状态由开发中转为已交付。
- **接口契约（FR-02）**：`POST /api/smb/credentials` 请求体移除 `master_password` 字段，加解密主密码改由服务端 `SMB_MASTER_PASSWORD` 环境变量统一管理；同步更新 `docs/API.md`、`docs/OPERATIONS.md`。

## 0.2.0（2026-06-21）

### 新增
- **播放器（FR-18）末端缓冲等待**：VideoPlayer 监听 mpegts.js `error` 事件 + 原生 `<video>` `waiting`/`stalled` 事件，进入等待态展示「等待新数据…」横幅；mpegts.js 报错后 1s 自动 `unload + load` 重载，新数据可用时 `canplay`/`playing` 自动复位。

### 修复
- **转码（FR-10/11/12）**：挂载硬件加速能力查询端点 `GET /api/transcode/hwaccel`。此前 HWAccelHandler 已实现但未注册路由，请求被前端 SPA 回退（NoRoute）接管返回 HTML 而非硬件能力 JSON，与 ARCHITECTURE §1/§4 不符；现已注册并返回 available/preferred/h264_supported 等字段。
- **监听器（FR-03）**：修复 main.go 未启动文件监听——watcher 包齐备且单测全绿，但程序入口从未实例化或 Start，导致新增/删除文件不自动入库（真机 35s 不入库）。修复后实测加文件 1s 入库（spec ≤30s）、删文件 1s 移除（spec ≤10s）。

### 变更
- **文档对齐**（期验收 P1 发现）：PRD AC-13 由 `/timeline` 改为根路由 `/`（FR-A 已重排）；FR-10/11 状态注「检测/单测就绪；本机无 Intel/NVIDIA，硬件真机待验」；`docs/specs/smart-playback.md`（FR-05）改为前端探测 `/api/play/hls/:id/master` 不可用即降级 `/api/play/:id/stream`，不再单提 `playinfo` 端点；`docs/specs/mpegts-player.md`（FR-16）澄清 mpegts.js 仅约束 TS/HLS 转码流，直出 MP4 经 `/stream` 允许原生 `<video>`。

## 0.1.0（2026-06-20）

### 新增
- 外挂字幕支持（FR-04）：后端 SRT/ASS→WebVTT 转换 API、前端字幕 overlay 渲染与轨道选择
- 实现 SMB/CIFS 网络共享支持（FR-02）：原生 SMB 连接管理、凭据 AES-GCM 加密存储、SMB 目录扫描与视频播放流
- **ABR 自适应码率（FR-07）**：后端 MultiPipeline 单进程多输出 1080p/720p/480p 三档 HLS 切片，码率阶梯根据源分辨率自动裁剪；新增 master.m3u8 生成与路由；前端 VideoPlayer 支持 hls.js ABR 模式（动态 import），自动回退 mpegts.js
- 初始化项目结构与 SDD 治理文档
- 实现 fsnotify 实时路径监控（FR-03）：递归监听媒体库目录，视频文件自动入库/移除，500ms 去抖机制
- 前端全面重构：引入 Mantine UI v9 暗色主题，替换手写 Tailwind 原子类
- 完善登录页（FR-13）：Mantine 表单 + zustand 认证状态管理 + 路由守卫
- 完善媒体库管理页（FR-01/FR-14）：路径增删、扫描、媒体列表/搜索/分页
- 新增文件目录浏览 API（FR-15）：`GET /api/library/browse` 按目录层级浏览媒体文件，支持面包屑导航、子目录列表、媒体文件列表，前端 Tab 切换（时间轴 | 文件目录）
- 完善视频播放页（FR-16）：VideoPlayer 接入 HLS 流
- 路由改造：修复 catch-all 通配符冲突，统一 BrowserRouter 模式
- 后端异步扫描 + SSE 进度推送（FR-C）：扫描改为后台 goroutine 异步执行，新增 `GET /api/library/scan/progress` SSE 端点实时推送已扫描数/总数/状态，前端展示扫描进度条并在完成后自动刷新
- 缩略图系统（FR-D）：后端扫描时通过 ffmpeg 异步生成 320px 缩略图（视频取第 2 秒帧、图片缩放），新增 `GET /api/library/thumbnail/:id` 端点，前端媒体卡片改用缩略图加载（图片预览弹窗仍用原图）
- 页面流程重设计（FR-A）：原综合媒体库页拆分为存储库管理 `/library-manager`、时间轴 `/`、目录浏览 `/browse` 三个独立页面，AppLayout 导航在三者间切换；管理页支持媒体文件删除与重命名（新增 `PUT /api/library/media/:id/rename` 磁盘改名端点）
- 时间轴视图重做（FR-B）：按 `added_at` 日期分组，左侧竖向日期轴 + 右侧缩略图网格，视频与图片均展示缩略图
- 虚拟列表 + 懒加载（FR-E）：时间轴与目录浏览改用 `@tanstack/react-virtual` 窗口虚拟滚动，只渲染可见区 + overscan；时间轴改为滚动到底自动加载更多（替代分页），缩略图 `loading="lazy"`
- 暗色模式 + 路由守卫 + 全局错误处理（FR-G）：MantineProvider 接入 `localStorageColorSchemeManager` 持久化主题，顶栏新增明暗切换按钮；新增 ProtectedRoute/RequireAnon 路由守卫（未认证跳登录、已认证访问登录页跳首页）；新增 `handleApiError` 工具，Axios 拦截器对网络错误统一 toast

### 变更
- 前端代码结构拆分（FR-F）：LibraryPage 拆分为 LibraryPathManager / MediaTimeline / DirectoryBrowser 等子组件与 useLibraryPaths / useMediaList / useDirectoryBrowse hooks，VideoPlayer 改用 Tabler Icons 与 Mantine 样式，删除 App.css 模板代码
- 扫描接口改为异步（FR-C）：`POST /api/library/scan/:id` 不再等待扫描完成，立即返回 `{"status":"scanning"}`，实际进度经扫描进度 SSE 端点获取

### 修复
- **时间轴（FR-E）**：修复无限滚动失效——滚到底不再加载更多、永远停在首页 60 条。改用 IntersectionObserver 底部哨兵触发加载（替代依赖虚拟化 lastIndex 变化的脆弱判定），并在 useInfiniteMedia 用同步标记防止重复拉取。
- **扫描进度（FR-C）**：修复扫描完成后 SSE 重连风暴——浏览器 EventSource 每 ~3 秒重连并重复触发列表重拉。后端 SSE 改为保持连接打开、仅在状态变化时推送；前端按 `completed_at` 变化判定「新完成」才刷新，避免重复刷新且兼容快扫描。
- **媒体库**：修复本地扫描只读取第一层目录的问题，改为递归扫描并按目录类型分发 local/SMB。
- **媒体库**：统一图片/视频后缀识别策略，新增按 `LibraryPath` 绑定的自定义后缀，删除目录时同步清理，避免污染全局。
- **媒体库**：新增图片 raw 预览接口，前端时间轴与目录视图点击图片打开预览，视频仍跳转播放页。
- **前端**：补齐独立 `/timeline` 路由和侧边栏/移动抽屉入口，时间轴默认显式请求 `sort=time_desc`。
- **前端**：修复 Windows 路径下目录浏览面包屑出现 `/D:` 的问题，并为扫描提供可感知加载态。
- **安全**：移除硬编码默认 JWT 密钥，改为启动时生成随机密钥并提示用户设置 `JWT_SECRET` 环境变量
- **安全**：HLS 切片路由增加路径遍历防护（过滤 `..` 和 `/`）
- **安全**：Cookie 添加 `SameSite=Strict` 属性，防止 CSRF 攻击
- **安全**：Content-Disposition 文件名使用 `url.PathEscape` 转义，防止 HTTP 头注入
- **并发**：修复 `GetProgress` 中 `BufferedRanges` 的锁外反序列化数据竞争
- **并发**：客户端断开后流式传输 goroutine 主动 cancel context，防止 goroutine 泄露
- **并发**：HLS m3u8 writer 确保 Close() 总是被调用并写入 `EXT-X-ENDLIST`
- **并发**：播放会话 map 增加 30 分钟 TTL 清理机制，防止内存泄露
- **CGO**：修复 `findEncoderByName`/`findQSVEncoder` 违反 CGO 指针规则的问题（改为返回 `bool`）
- **基础设施**：硬件检测失败时降级继续而非阻断整个流程
- **基础设施**：`detectOnce` 增加 `recover()` 防止 panic 后返回 nil
- **基础设施**：`TranscodeSession.Start()` 增加重复启动检查
- **基础设施**：移除冗余的 `killProcessGroup` 调用（`exec.CommandContext` 已自动处理）
- **基础设施**：`isIntelGPU()` 添加 `//go:build linux` 平台构建标签
- **基础设施**：`setupTestDB` 从业务代码迁移到测试工具文件
- **性能**：`ScanLibrary` N+1 查询改为批量查询 + map 查找
- **性能**：流式传输从逐包 Flush 改为 `io.CopyBuffer`
- **性能**：HLS `GetM3U8`/`GetSegment` 锁内不再执行磁盘 I/O
- **健壮性**：ffprobe 增加 10 秒超时，防止永久阻塞
- **健壮性**：`envInt` 无效值增加警告日志
- **健壮性**：搜索参数中 `%` 和 `_` 通配符转义
- **健壮性**：DB 插入失败增加 WARN 日志
- **测试**：`play_handler_test.go` 宽松断言改为精确状态码断言
- **测试**：`handler_test.go`、`jwt_test.go`、`library/service_test.go` 补充错误路径和边界条件测试
- **测试**：`watcher_test.go` 增加超时时间减少 flaky 风险
- **前端**：Mock 数据统一为单一数据源（`mocks/data.ts`）
- **前端**：VideoPlayer 事件监听器在 `destroyPlayer` 中清理
- **前端**：`LibraryPage` 工具函数提取为模块级
- **前端**：auth store 移除硬编码 `'cookie_auth'` 字面量
- **前端**：mock 模式从运行时 `localStorage` 判断改为构建时 `VITE_USE_MOCK` 环境变量
- **规则**：`architecture-invariants.md` ORM 规则修正为允许 GORM（ADR-0023）
- **规则**：`architecture-invariants.md` 删除 Bukkit/Spigot 残留内容

### 移除
（无）

### 修复（二期 FR 审查修复）
- **CRITICAL**：SMB 流式播放中 `smbReadSeeker.Close()` 不再调用 `client.Disconnect()`，避免 HTTP 分块传输中途断开 SMB 会话
- **CRITICAL**：`GetProgress` 的 `exists` 检查移入 `RLock` 内，消除 nil 指针解引用风险
- **HIGH**：`openSMBFile` 加载凭据后检查 `creds == nil`，给出明确错误提示而非 panic
- **HIGH**：`saveSMBConfig` 主密码改为从 `SMB_MASTER_PASSWORD` 环境变量读取
- **HIGH**：`router.go` 所有播放路由添加 `parseMediaID` 错误处理，与 HLS 路由一致
- **HIGH**：`MultiPipeline.RunMulti` 添加 `dsts` 参数被忽略的 WARN 日志
- **HIGH**：`Credentials` 增加 `Domain` 字段，支持企业 Windows AD 环境
- **MEDIUM**：`Service.smbCreds` 添加 `smbCredsMu` 读写锁保护并发安全
- **MEDIUM**：`Watcher.pathToLib` 读取加 `RLock` 保护
- **MEDIUM**：`HLSSegmentWriter.Close` 添加 `closed` 标志防止重复关闭
- **MEDIUM**：`WriteSegment` 移除每次 `Sync()`，改为 `Close()` 时一次性 sync
- **MEDIUM**：`BrowseDirectory` 入口添加 `filepath.Clean` + `..` 路径遍历校验
- **MEDIUM**：`smbfs.normalize` 添加 `..` 过滤
- **MEDIUM**：`smb.Client.EnsureConnected` 使用 `sync.Once` 消除竞态窗口
- **MEDIUM**：`GetSubtitles` 对 SMB 路径返回空列表
- **MEDIUM**：`GetSubtitleContent` 空内容返回 204
- **MEDIUM**：`LibraryPage` catch 块不再静默吞错，添加错误状态和 UI 提示
- **MEDIUM**：`LibraryPage.activeTab` 同步到 URL query 参数
- **MEDIUM**：`LibraryPage` paths 变化时使用 `useRef` 追踪初始化，不重置浏览状态
- **MEDIUM**：`SubtitleEntry` 接口统一到 `types/index.ts`，消除重复定义
- **MEDIUM**：`VideoPlayer` 自动播放被阻止时显示"点击播放"提示
- **MEDIUM**：`VideoPlayer.hlsRef` 类型从 `unknown` 改为 `Hls`
- **LOW**：`master_test.go` 使用 `strconv.Itoa` 替代自定义 `itoa`
- **LOW**：`credentials.go` 添加 `saltLen`/`nonceLen` 常量
- **LOW**：`Watcher.Stop` 先关闭 watcher 再关闭 done 通道
- **LOW**：`Credentials.Password` 添加 `json:"-"` 标签
- **LOW**：新增 `credentials_test.go` 覆盖加解密 roundtrip、错误密码、空输入

> 发版时把"未发布版本"段切成 `## [X.Y.Z] - YYYY-MM-DD`，再新建空的"未发布版本"段。
