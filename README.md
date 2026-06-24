# JianVideo

> 轻量级单用户视频媒体服务器——个人家庭影音中心

## 状态

开发中 · v0.15.0

## 架构一览

Go 后端以**外部进程**方式调用 FFmpeg（`ffmpeg`/`ffprobe`），提供 RESTful API 和 HLS/TS 视频流服务；CGO 仅用于 SQLite 驱动与可选的硬件编码器检测。React + TypeScript 前端通过 `go:embed` 内嵌于单个可执行文件中。元数据使用 SQLite（WAL 模式）本地存储，支持全部硬件加速编码器（NVIDIA NVENC、Intel QSV、AMD AMF、VAAPI、VideoToolbox、Vulkan），必须同时支持 H.264 和 H.265，启动时自动检测并在 `GET /api/transcode/hwaccel` 接口暴露。

```
┌──────────────┐     HTTP/HLS      ┌──────────────────┐
│   浏览器      │ ◄──────────────► │   Go 后端         │
│  (mpegts.js) │                   │  ┌────────────┐  │
└──────────────┘                   │  │ 媒体库管理  │  │
                                   │  │ 转码管理器  │  │
                                   │  │ Web 服务    │  │
                                   │  └────────────┘  │
                                   │  ┌────────────┐  │
                                   │  │ SQLite(WAL) │  │
                                   │  └────────────┘  │
                                   └──────────────────┘
                                          │
                                   ┌──────┴──────┐
                                   │   FFmpeg    │
                                   │ (硬件加速)   │
                                   └─────────────┘
```

## 能力

- 多目录聚合管理，将分散在多个硬盘的视频汇聚到统一媒体库
- 原生 SMB/CIFS 网络共享支持，直接添加 NAS 路径
- 实时文件监听，新增/删除/移动自动增量更新
- 全格式兼容播放（MP4/MKV/AVI/MOV/WebM/RMVB/TS/HEVC/AV1）
- 智能播放策略：兼容格式直出，不兼容自动转码
- HLS/TS 强制输出，确保复杂网络环境平滑播放
- 自适应码率（ABR），根据带宽动态切换
- CGO 绑定 FFmpeg，高性能硬件加速转码（NVIDIA NVENC、Intel QSV、AMD AMF、VAAPI、VideoToolbox、Vulkan）
- 必须同时支持 H.264 和 H.265 硬件编码，含 Intel 核显检测
- 硬件加速能力自动检测并通过 API 暴露
- 边下边播，实时追随转码/写入进度
- 精准进度拖拽，GOP 对齐毫秒级 Seek
- 双视图浏览：时间轴视图 + 文件目录视图
- 单文件部署，跨平台（Windows/Linux/macOS）

## 结构

```
JianVideo/
├── main.go                    # 入口
├── config/                    # 配置加载与管理
├── internal/
│   ├── web/                   # HTTP API 服务、静态文件服务
│   ├── auth/                  # 单用户认证
│   ├── library/               # 媒体库管理（目录注册、文件索引）
│   ├── watcher/               # 文件系统事件监听（fsnotify）
│   ├── transcoder/            # FFmpeg 进程管理、硬件加速
│   ├── db/                    # SQLite 数据库初始化与 CRUD
│   └── static/                # go:embed 前端静态资源
├── frontend/                  # React + TypeScript 前端源码
│   ├── src/
│   └── dist/                  # 编译产物（被 go:embed 内嵌）
├── docs/                      # 项目文档
├── VERSION                    # 版本号
├── config.yml                 # 配置文件
└── go.mod
```

## 文档导航

- 需求：[`docs/PRD.md`](docs/PRD.md)
- 架构：[`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md)
- 接口：[`docs/API.md`](docs/API.md)
- 运维：[`docs/OPERATIONS.md`](docs/OPERATIONS.md)
- 安全：[`SECURITY.md`](SECURITY.md)
- 决策：[`docs/adr/`](docs/adr/)
- 演进与维护：[`docs/CONTRIBUTING.md`](docs/CONTRIBUTING.md)
- 变更史：[`CHANGELOG.md`](CHANGELOG.md)

## 快速开始

### 前置依赖

- Go 1.22+
- FFmpeg（支持硬件加速需安装对应驱动）
- Node.js 18+（仅构建前端时需要）

### 构建与运行

```bash
# 构建前端
cd frontend
npm install
npm run build

# 运行后端（开发模式）
go run main.go

# 编译为单文件
go build -o jianvideo main.go
```

### 配置

通过 `config.yml` 或环境变量配置：

```yaml
# 服务端口
server_port: 8080

# FFmpeg 路径
ffmpeg_path: /usr/bin/ffmpeg
ffprobe_path: /usr/bin/ffprobe

# 媒体库目录
library_paths:
  - path: /media/movies
    type: local
    label: 电影
  - path: /media/tv
    type: local
    label: 电视剧
```

## 约定

- 贡献与提交规范见 [`docs/CONTRIBUTING.md`](docs/CONTRIBUTING.md)
- 所有注释使用简体中文
- Git 提交信息标题与正文使用简体中文

## 许可

MIT License，详见 [LICENSE](LICENSE)。
