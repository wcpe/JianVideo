# 变更日志

本项目所有重要变更记录于此。

格式遵循 [Keep a Changelog](https://keepachangelog.com/zh-CN/1.1.0/)，版本号遵循 [语义化版本](https://semver.org/lang/zh-CN/)。

## 未发布版本

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

### 变更
（无）

### 修复
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

> 发版时把"未发布版本"段切成 `## [X.Y.Z] - YYYY-MM-DD`，再新建空的"未发布版本"段。
