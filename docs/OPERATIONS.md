# 运维手册：轻量级单用户视频媒体服务器

> 部署、升级、备份恢复、回滚、排障的操作指南。运维方式变化时更新。

## 1. 部署

### 前置依赖

- **FFmpeg**：需安装 FFmpeg 并确保在系统 PATH 中，或通过环境变量 `FFMPEG_PATH` / `FFPROBE_PATH` 指定路径。
- **硬件加速驱动**（可选）：
  - Intel QSV：安装 Intel Media SDK / VAAPI 驱动
  - NVIDIA NVENC：安装 NVIDIA 驱动 + CUDA
  - VAAPI：安装 Mesa VAAPI 驱动

### 配置

通过环境变量或命令行参数配置（优先级：环境变量 > 命令行参数 > 默认值）：

| 环境变量 | 说明 | 默认值 |
|---|---|---|
| `SERVER_PORT` | 服务端口 | `8080` |
| `JWT_SECRET` | JWT 签名密钥（未设置时自动生成随机值） | 随机生成 |
| `JWT_EXPIRES_IN` | JWT 过期时间 | `72h` |
| `DB_PATH` | 数据库文件路径 | `jianvideo.db` |
| `FFMPEG_PATH` | FFmpeg 可执行文件路径 | 从 PATH 查找 |
| `FFPROBE_PATH` | ffprobe 可执行文件路径 | 从 PATH 查找 |
| `SMB_MASTER_PASSWORD` | SMB 凭据加密主密码 | `default-master-password`（不推荐生产使用） |

### SMB 凭据管理

SMB 凭据通过 API 管理，加密存储在 `data/smb_credentials.enc`：

```bash
# 保存 SMB 凭据
curl -X POST http://localhost:8080/api/smb/credentials \
  -H "Content-Type: application/json" \
  -d '{"host":"192.168.1.100","username":"user","password":"pass","share":"ShareName","domain":"WORKGROUP","master_password":"your-master-password"}'
```

### 启动

```bash
# 直接运行
./jianvideo

# 指定配置文件路径
./jianvideo -config /path/to/config.yml

# 指定端口（覆盖配置文件）
./jianvideo -port 9090
```

### 健康检查

启动后访问 `http://localhost:8080/api/config`，返回 JSON 即表示服务正常。

## 2. 升级

1. 下载新版本可执行文件替换旧文件。
2. 重启服务。
3. SQLite 数据库自动兼容（WAL 模式向前兼容）。
4. 查看 `CHANGELOG.md` 确认是否有配置变更。

## 3. 数据备份与恢复

### 备份内容

| 数据 | 位置 | 说明 |
|---|---|---|
| 元数据数据库 | `data/jianvideo.db` | SQLite 文件，含媒体库目录、文件索引、用户信息 |
| 配置文件 | `config.yml` | 服务配置 |
| HLS 切片缓存 | `data/hls_cache/` | 转码产生的临时切片文件（可重建） |

### 备份方法

```bash
# 直接复制数据库文件（服务运行时安全，WAL 模式支持热备份）
cp data/jianvideo.db data/jianvideo.db.bak

# 或使用 SQLite 内置备份
sqlite3 data/jianvideo.db ".backup 'data/jianvideo.db.bak'"
```

### 恢复方法

```bash
# 停止服务后替换数据库文件
cp data/jianvideo.db.bak data/jianvideo.db
# 重启服务
```

### 恢复演练

建议每月执行一次恢复演练，验证备份文件可用。

## 4. 回滚

1. 停止当前服务。
2. 用旧版本可执行文件替换新版本。
3. 如果数据库 Schema 有变化，用备份的数据库文件恢复。
4. 重启服务。

## 5. 排障

| 现象 | 排查路径 | 处置 |
|---|---|---|
| 服务无法启动 | 检查端口是否被占用、FFmpeg 路径是否正确 | 修改 `config.yml` 中的 `server_port` 或 `ffmpeg_path` |
| 视频无法播放 | 检查 FFmpeg 是否安装、文件路径是否可访问 | 运行 `ffmpeg -version` 验证安装 |
| 转码失败 | 检查硬件加速驱动是否正确安装 | 查看日志中的 FFmpeg 错误输出，尝试关闭硬件加速 |
| 播放卡顿 | 检查网络带宽、CPU 占用 | 降低转码码率或启用硬件加速 |
| Seek 后黑屏 | 检查 GOP 设置是否正确 | 确认 FFmpeg 使用了固定 GOP 参数 |
| 媒体库不更新 | 检查目录权限、fsnotify 是否正常工作 | 查看日志中的 watcher 错误，尝试重启服务 |
| SMB 路径无法访问 | 检查网络连通性、凭据是否正确 | 通过 `POST /api/smb/credentials` 配置凭据，验证 SMB 路径可达 |
| SMB 凭据丢失 | 凭据加密密钥（`SMB_MASTER_PASSWORD`）更换后无法解密 | 使用旧主密码重新加密或删除 `data/smb_credentials.enc` 后重新配置 |
| 字幕不显示 | 检查字幕文件是否与视频同名同目录 | 通过 `GET /api/play/:id/subtitles` 验证字幕轨道列表 |
| ABR 不生效 | 检查浏览器是否支持 hls.js、master.m3u8 是否可访问 | 验证 `GET /api/play/hls/:id/master.m3u8` 返回有效的多码率索引 |
