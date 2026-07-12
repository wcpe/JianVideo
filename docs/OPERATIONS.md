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

SMB 凭据通过 API 管理，加密存储在 `data/smb_credentials.enc`。
加解密主密码由服务端通过 `SMB_MASTER_PASSWORD` 环境变量统一配置，**必须显式设置**——未设置（或为空串）时服务端拒绝保存/加载 SMB 凭据（返回 `503`），不再回退弱默认主密码；请求体不包含主密码：

```bash
# 启动服务前必须设置主密码环境变量，否则 SMB 凭据功能不可用
export SMB_MASTER_PASSWORD="请替换为强随机主密码"

# 保存 SMB 凭据
curl -X POST http://localhost:8080/api/smb/credentials \
  -H "Content-Type: application/json" \
  -d '{"host":"192.168.1.100","username":"user","password":"pass","share":"ShareName","domain":"WORKGROUP"}'
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

### 升级前检查

1. 查看 `CHANGELOG.md`，确认目标版本包含的 schema 与配置变化。
2. 确认数据库文件路径及其所在磁盘有足够空间；迁移会在同盘额外生成一个完整 SQLite 副本，索引重建还需要临时空间。
3. 确认数据库目录可写，且数据库同目录下可创建 `backups/`。
4. 先运行只读预检：

```bash
jianvideo -migration-dry-run
```

> `-migration-dry-run` 会向 stdout 输出 JSON 计划，包含迁移步骤、预计影响行数、blocker 和 warning，然后退出；它不启动 HTTP 服务、不创建备份、不写数据库。仅 warning 时退出码为 0；存在 blocker 时仍输出计划并以非零退出码结束。

settings 预检中，已登记但值非法的 key，以及命名涉及密钥/口令/token、数据库路径、监听端口的未知高风险 key，会成为 blocker；普通未知历史 key 只产生 warning 并原样保留。必须先处理 blocker，不能靠跳过预检强行升级。

### 正式升级

1. 停止旧版本进程，保留旧二进制，不要删除旧数据库。
2. 使用新版本二进制启动服务。
3. 若存在待执行 migration，程序先在数据库文件同目录的 `backups/` 创建迁移前备份并校验完整性，再执行版本化迁移。
4. 观察日志中的备份路径与迁移结果；迁移失败时不要反复替换文件或删除备份。
5. 启动成功后检查核心页面、媒体数量、库路径、任务与 Space 归属。

Runner 对每一步都把 `Up`、`Validate` 和成功状态写入放在同一事务中；单步失败时该步写入自动回滚。失败状态会保留在 `schema_migrations`，可安全重试的步骤在问题修复后可重入，不可安全重试的步骤会阻断启动并要求人工处理。

## 3. 数据备份与恢复

### 备份内容

| 数据 | 位置 | 说明 |
|---|---|---|
| 元数据数据库 | 由 `DB_PATH` 指定，如 `data/jianvideo.db` | SQLite 主库，含媒体库、文件索引、用户、settings、任务与审计 |
| 自动迁移备份 | `<数据库目录>/backups/<数据库名>-before-v2-<UTC时间戳>.sqlite` | 迁移前一致性快照；重名时追加序号 |
| 配置文件 | `config.yml` 及部署环境变量 | 服务启动配置；敏感环境变量需由运维系统另行安全备份 |
| SMB 凭据 | `data/smb_credentials.enc` | 必须与对应 `SMB_MASTER_PASSWORD` 一并安全保管 |
| HLS/缩略图等缓存 | 数据目录下缓存子目录 | 可重建，不是迁移恢复的必要可信源 |

### 为什么不能在运行时直接复制 WAL 数据库

**运行时只复制 `jianvideo.db` 并不安全。** SQLite 处于 WAL 模式时，最新已提交事务可能仍在 `jianvideo.db-wal` 中；单独复制主 `.db` 可能得到缺少近期提交、无法代表同一时点的副本。分别复制 `.db`、`-wal`、`-shm` 也可能跨越不同写入时刻，不能自动保证三者一致。

FR2-017 自动迁移备份通过当前 SQLite 连接执行 `VACUUM INTO`，生成单文件一致快照；随后用独立连接打开备份并执行：

```sql
PRAGMA integrity_check;
```

只有结果为 `ok` 才继续迁移。备份失败或完整性校验失败时，迁移不会创建 `schema_migrations`/审计记录，也不会修改业务 schema。

人工在线备份应使用 SQLite 官方备份能力，例如 `.backup`；不要使用普通文件复制代替：

```bash
sqlite3 data/jianvideo.db ".backup 'data/manual-backup.sqlite'"
sqlite3 data/manual-backup.sqlite "PRAGMA integrity_check;"
```

如必须使用文件复制，应先完全停止服务，确认没有数据库写入连接，再同时处理主库及遗留 WAL/SHM；优先采用停服后对主库执行 checkpoint，再复制并校验，而不是在运行中复制。

### 迁移失败后的处置

1. 保留日志中记录的自动备份路径；Runner 不会因迁移失败删除该文件。
2. 不要把失败后的当前数据库直接交给旧二进制启动；新 schema 可能已完成若干前置 migration。
3. 若失败步骤标记 `SafeToRetry=true`，先修复 blocker、磁盘空间或权限问题，再用同一新版本启动重试。已成功且校验通过的步骤会跳过，不会重复破坏数据。
4. 若需要回退旧版本，执行下方“完整恢复/回滚”，恢复**迁移前备份**，而不是只替换二进制。

### 完整恢复/回滚

以下示例中的路径必须替换为日志记录的实际数据库和备份路径：

```bash
# 1. 完全停止 JianVideo，确认进程已退出

# 2. 保留失败现场，便于排障
mv data/jianvideo.db data/jianvideo.db.failed

# 3. 移走失败库对应的 WAL/SHM，禁止与备份混用
mv data/jianvideo.db-wal data/jianvideo.db-wal.failed 2>/dev/null || true
mv data/jianvideo.db-shm data/jianvideo.db-shm.failed 2>/dev/null || true

# 4. 恢复迁移前一致性备份
cp data/backups/jianvideo-before-v2-YYYYMMDDTHHMMSSZ.sqlite data/jianvideo.db

# 5. 恢复后再次校验
sqlite3 data/jianvideo.db "PRAGMA integrity_check;"
```

只有完整性结果为 `ok` 才能继续：

1. 用升级前保留的旧版本二进制启动。
2. 检查用户、媒体库、媒体数量、settings、旧扫描/转码任务和原媒体访问。
3. 若校验不通过，停止操作，保留失败现场和所有备份，禁止继续启动服务写库。

自动备份只包含数据库，不包含原媒体；FR2-017 迁移本身不移动、不重命名、不修改原媒体文件。

### 恢复演练

建议在正式升级前用数据库副本演练一次“新二进制迁移 → 启动 smoke → 停服 → 恢复迁移前备份 → 旧二进制启动”。至少每月对常规备份执行一次恢复演练和 `PRAGMA integrity_check`。

FR2-017 规模基准已在 `.tmp/benchmark/fr2-017/` 完成：

| 数据规模 | 一致性备份 | Runner 完整迁移 | 完整性与数据保持 |
|---|---:|---:|---|
| 1m | 2.526s | 57.197s | 备份/主库 integrity、计数、指纹、关键索引均通过 |
| 5m | 8.540s | 216.203s | 备份/主库 integrity、计数、指纹、关键索引均通过 |
| 10m | 18.245s | 596.426s | 备份/主库 integrity、计数、指纹、关键索引均通过 |

以上是验收机实测，不是所有硬件的升级时限承诺。生产升级仍应按实际数据库大小、磁盘吞吐与可用空间预留维护窗口。

## 4. 回滚决策

- **单步 migration 失败**：Runner 自动回滚该步事务；优先修复原因并按 `SafeToRetry` 重入。
- **需要继续使用新版本但迁移不可重试**：停止服务，保留当前库与自动备份，先人工分析，不得跳过 migration 状态。
- **需要退回旧版本**：停止服务，恢复迁移前备份，移走失败库的 WAL/SHM，再用旧二进制启动。
- **备份完整性不通过**：不得用于恢复；保留现场并改用其他已验证备份。

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
