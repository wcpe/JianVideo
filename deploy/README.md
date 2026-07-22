# 部署编排（deploy/）

本目录提供 JianVideo 的 Docker 部署骨架（FR2-072）。完整运维见 [`../docs/OPERATIONS.md`](../docs/OPERATIONS.md)。

## 内容

| 文件 | 用途 |
| --- | --- |
| `Dockerfile` | 多阶段：前端 → Go embed（**CGO=1** + sqlite3）→ Debian + FFmpeg 运行镜像 |
| `docker-compose.yml` | 单机 Compose（命名卷、`env_file`、healthcheck、`restart`） |
| `.env.example` | 环境变量模板；复制为 `.env` 填值。**`.env` 不入库** |

## 设计取舍（相对 JianArtifact）

| 点 | JianVideo | 说明 |
| --- | --- | --- |
| CGO | **保留 CGO=1** | 默认 `mattn/go-sqlite3`；不做全量 CGO=0 |
| 运行镜像 | Debian slim + `ffmpeg` | 转码/探测依赖外部 FFmpeg，非 distroless 纯静态 |
| 探活 | `GET /health` | 公开、无 JWT，compose healthcheck 使用 |

## 主路径（Docker Compose）

```bash
cp deploy/.env.example deploy/.env
# 编辑 deploy/.env：至少设置 JWT_SECRET（生产）
docker compose -f deploy/docker-compose.yml up -d --build
curl -fsS http://127.0.0.1:8080/health
```

数据持久化在命名卷 `jianvideo-data`，容器内路径 `/data`。

## 构建参数（可选）

离线/内网可覆盖基础镜像与 Go 代理：

```bash
docker build -f deploy/Dockerfile \
  --build-arg WEB_IMAGE=node:22-bookworm-slim \
  --build-arg BUILD_IMAGE=golang:1.26-bookworm \
  --build-arg RUNTIME_IMAGE=debian:bookworm-slim \
  --build-arg GOPROXY=https://goproxy.cn,direct \
  -t jianvideo:latest .
```

## 安全

- 密钥（`JWT_SECRET`、`SMB_MASTER_PASSWORD`）经环境变量注入，严禁硬编码、严禁入库。
- 详见 [`../SECURITY.md`](../SECURITY.md)（若有）与 OPERATIONS。

## 不做（本骨架范围）

- 多节点 HA / K8s / Helm（可后续 FR）
- 默认 S3 对象存储
- 宣称全量 CGO=0 镜像
