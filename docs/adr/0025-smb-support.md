# ADR-0025: SMB 网络共享支持架构决策

## 状态
已接受

## 背景
用户需要播放存储在 NAS 或 Windows SMB/CIFS 网络共享上的视频文件。需要决定 SMB 客户端库的选型以及凭据存储方案。

## 决策
- 使用 `github.com/cloudsoda/go-smb2` 作为 SMB 客户端库（纯 Go，无需 CGO）
- 凭据使用 AES-256-GCM 加密存储到本地文件，密钥由用户主密码通过 PBKDF2 派生
- SMB 路径跳过 fsnotify 监听，改用 5 分钟轮询

## 理由
- **纯 Go 实现**：cloudsoda/go-smb2 不需要 CGO，与项目其他纯 Go 模块（如 go-sqlite）一致，保持交叉编译简单
- **活跃维护**：cloudsoda 是 hirochachacha 的活跃 fork，持续接收 bug 修复和功能更新
- **io/fs 接口**：Share 类型通过 `DirFS()` 方法提供 `fs.FS` 接口，与 Go 标准库无缝集成
- **凭据安全**：AES-GCM 提供认证加密，PBKDF2 密钥派生抵抗暴力破解，不引入额外 CGO 依赖
- **轮询兜底**：SMB 网络共享的 fsnotify 不可靠，5 分钟轮询是稳定的降级方案

## 后果
- 引入新的间接依赖（gokrb5、sddl 等），但均为纯 Go 实现
- SMB 连接需要处理超时和重连，增加了错误处理复杂度
- 视频播放时每次打开文件都需要建立 SMB 连接，可能有延迟
- 轮询间隔 5 分钟意味着新文件发现最多延迟 5 分钟

## 备选方案
- **hirochachacha/go-smb2**：原始库，但维护不活跃（已归档）
- **系统挂载 + 本地路径**：要求用户在操作系统层面挂载 SMB，用户体验差
- **NFS 协议**：Linux/macOS 原生支持好，但 Windows 需要额外安装
- **WebDAV**：协议开销大，NAS 端需要额外配置
