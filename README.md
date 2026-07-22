# JianVideo

> 自托管视频媒体服务器——浏览器直连全格式播放，单二进制交付。

## 状态

开发中 · v0.25.0（见根目录 `VERSION`）。v2 能力与分期见 [`docs/PRD.md`](docs/PRD.md) / [`docs/ROADMAP.md`](docs/ROADMAP.md)。

## 架构一览

- **后端** `apps/server`：Go + Gin + GORM/SQLite（CGO），FFmpeg 外部进程转码，HLS/TS 流。
- **前端** `apps/web`：React + TypeScript + Vite + Mantine；构建产物同步到 `apps/server/web/dist` 后经 `go:embed` 内嵌。
- **工作区**：根 `go.work` → `apps/server`；前端 pnpm workspace + Turborepo（`apps/*` + `packages/*`）。

```
浏览器 ──HTTP/HLS──► apps/server（API · 媒体库 · 转码 · embed web/dist）
                              │
                         SQLite(WAL) + FFmpeg
```

真貌细节见 [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md)。

## 能力

- 多目录 / SMB 媒体库，扫描与文件监听
- 全格式兼容：能直连则直连，否则转码 HLS
- 硬件加速（NVENC / QSV / AMF / VAAPI / VideoToolbox / Vulkan 等）
- 自适应码率、边下边播、精准 Seek
- 单二进制跨平台部署

## 结构

```
JianVideo/
├── go.work                 # use ./apps/server
├── Makefile                # 顶层入口（委托 task + pnpm）
├── apps/
│   ├── server/             # Go 服务端 + Taskfile + embed web/dist
│   ├── web/                # 生产主端
│   ├── wiki/               # UI 博物馆
│   └── mock-studio/        # Mock / Benchmark
├── packages/               # 共享 UI / client / pixi / …
├── e2e/                    # Playwright
├── docs/                   # PRD · ARCHITECTURE · ADR · specs
├── scripts/                # 质量门 · root-hygiene
└── VERSION
```

## 快速开始

### 前置

- Go 1.22+（需 **CGO** 工具链）
- [go-task](https://taskfile.dev)（`go install github.com/go-task/task/v3/cmd/task@latest`）
- Node.js 18+、pnpm
- FFmpeg（硬件加速另装驱动）

### 常用命令

```bash
make install          # pnpm install
make frontend         # 构建 apps/web → apps/server/web/dist
make build            # 前端 + 单二进制 → dist/jianvideo
cd apps/server && task --list
go run -C apps/server .          # 开发跑服务（默认 DB：data/jianvideo.db）
make check            # 全仓 pnpm quality
```

### 配置

环境变量优先（详见 [`docs/OPERATIONS.md`](docs/OPERATIONS.md)）：

| 变量 | 说明 | 默认 |
|---|---|---|
| `SERVER_PORT` | 端口 | `8080` |
| `DB_PATH` | 数据库路径（缓存与库同父目录） | `data/jianvideo.db` |
| `JWT_SECRET` | 会话密钥 | 未设则随机 |
| `JIANVIDEO_FFMPEG_PATH` / `FFPROBE` | 工具路径 | PATH / 同目录捆绑 |

## 文档导航

- 需求：[`docs/PRD.md`](docs/PRD.md)
- 架构：[`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md)
- 接口：[`docs/API.md`](docs/API.md)
- 运维：[`docs/OPERATIONS.md`](docs/OPERATIONS.md)
- 安全：[`SECURITY.md`](SECURITY.md)
- 决策：[`docs/adr/`](docs/adr/)
- 演进：[`docs/CONTRIBUTING.md`](docs/CONTRIBUTING.md)
- 变更：[`CHANGELOG.md`](CHANGELOG.md)

## 约定

- 贡献与提交见 [`docs/CONTRIBUTING.md`](docs/CONTRIBUTING.md)
- 注释与提交说明使用简体中文
- 架构不变量见 [`.claude/rules/architecture-invariants.md`](.claude/rules/architecture-invariants.md)

## 许可

[MIT](LICENSE)。
