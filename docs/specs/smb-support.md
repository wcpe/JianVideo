# 功能规格：SMB/CIFS 网络共享支持

> 状态：开发中　·　关联 PRD：FR-02　·　分支：feature/fr-02-smb

## 1. 背景与目标
解决用户无法播放存储在 NAS（网络附加存储）或 Windows 共享上的视频文件的问题。用户需要直接将 SMB/CIFS 网络共享添加为媒体库源，无需在操作系统层面手动挂载。属于第二期 P1 能力。

## 2. 需求（要什么）
- 支持添加 SMB 网络共享路径作为媒体库源（type=smb）
- SMB 路径格式：`smb://host/share/path` 或 UNC 路径
- 支持 SMB 凭据管理：用户名、密码加密存储（AES-256-GCM）
- 支持 SMB 共享目录扫描，索引视频文件
- 支持 SMB 视频播放（带 Range 请求）
- SMB 路径跳过 fsnotify 监听，改用 5 分钟轮询
- 凭据通过主密码派生密钥加密（PBKDF2 + AES-GCM）

范围内：SMB 连接管理、凭据加密存储、目录扫描、视频播放流
不做（范围外）：SMB 写入/修改、Kerberos 认证、SMB1 协议、符号链接跟随、文件上传

## 3. 设计（怎么做）

### 新增模块
- `internal/smb/client.go`：SMB 连接管理（连接池、重连、超时）
- `internal/smb/smbfs.go`：实现 io/fs.FS 接口的 SMB 文件系统抽象
- `internal/smb/credentials.go`：AES-GCM 加密配置文件存储/读取 SMB 凭据

### 模块改动
- `internal/library/service.go`：ScanLibrary 根据 LibraryPath.Type 分发到本地扫描或 SMB 扫描
- `internal/watcher/watcher.go`：SMB 路径跳过 fsnotify，改用 5 分钟轮询
- `internal/playback/service.go`：StreamFile 根据路径前缀选择 os.Open 或 smb 打开
- `internal/api/handler.go`：CreateLibraryPath 接受 smb_host/smb_username/smb_password 参数
- `internal/api/router.go`：增加 POST /api/smb/credentials 凭据管理端点

### 凭据存储
- 配置文件：`data/smb_credentials.enc`
- 密钥派生：PBKDF2（100,000 次迭代，SHA-256）
- 加密：AES-256-GCM（随机 salt + nonce）
- 格式：`salt(16B) || nonce(12B) || ciphertext+tag`

### 架构决策
- 详见 [ADR-0025](adr/0025-smb-support.md)

## 4. 任务拆分
- [x] 添加 go-smb2 依赖
- [x] 实现 internal/smb/ 模块（client、smbfs、credentials）
- [x] 修改 library 服务支持 SMB 扫描分发
- [x] 修改 watcher 支持 SMB 轮询
- [x] 修改 playback 支持 SMB 文件流播放
- [x] 修改 API handler 接受 SMB 参数
- [x] 修改 API router 增加凭据管理端点
- [ ] 文档同步：ADR-0025、CHANGELOG
- [ ] 编译验证

## 5. 验收标准
- 添加 SMB 类型路径后，凭据加密存储到 `data/smb_credentials.enc`
- SMB 共享目录扫描正确索引视频文件（与本地扫描逻辑一致）
- SMB 视频播放支持 Range 请求（Seek 正常）
- SMB 路径每 5 分钟轮询一次，发现新文件自动入库
- 凭据管理 API 正确设置/更新凭据
- 所有现有测试继续通过
- `go build ./internal/...` 编译通过

## 6. 风险 / 待定
- SMB 连接超时时间（15 秒）和重连策略（3 次，间隔 2 秒）可能需要根据实际网络环境调整
- 主密码当前使用默认值，生产环境应要求用户设置独立主密码
- SMB 视频播放性能取决于网络带宽，大文件可能有缓冲
- 凭据文件权限为 0600，但主密码强度依赖用户输入
