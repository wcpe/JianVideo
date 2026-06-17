# 运维手册：轻量级单用户视频媒体服务器

> 部署、升级、备份恢复、回滚、排障的操作指南。运维方式变化时更新。

## 1. 部署

### 前置依赖

- **FFmpeg**：需安装 FFmpeg 并确保在系统 PATH 中，或通过 `config.yml` 的 `ffmpeg_path` 指定路径。
- **硬件加速驱动**（可选）：
  - Intel QSV：安装 Intel Media SDK / VAAPI 驱动
  - NVIDIA NVENC：安装 NVIDIA 驱动 + CUDA
  - VAAPI：安装 Mesa VAAPI 驱动

### 配置

首次运行前创建 `config.yml`：

```yaml
# 服务端口
server_port: 8080

# FFmpeg 路径（留空则从 PATH 查找）
ffmpeg_path: ""
ffprobe_path: ""

# 媒体库目录
library_paths:
  - path: /media/movies
    type: local
    label: 电影
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
| SMB 路径无法访问 | 检查网络连通性、凭据是否正确 | 验证 SMB 路径在操作系统中可正常访问 |
