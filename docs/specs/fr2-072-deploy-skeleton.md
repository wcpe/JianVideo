# 功能规格：deploy/ 骨架

> 状态：首切已交付　·　关联 PRD：FR2-072　·　阶段：对齐-D

## 1. 背景与目标

补齐 `deploy/`：多阶段 Dockerfile、compose、`.env.example`，与运维手册衔接，语义贴合 JianVideo（数据卷、FFmpeg、**CGO 现实**）。

## 2. 需求（要什么）

- 范围内：Dockerfile、compose、env 示例、公开探活、OPERATIONS 入口。
- 不做：多节点 HA；默认 S3；宣称全量 CGO=0 镜像。

## 3. 设计（怎么做）

- 构建阶段：前端 `apps/web` → 同步 `apps/server/web/dist` → Go embed 编译（`CGO_ENABLED=1`）。
- 运行镜像：`debian:bookworm-slim` + `ffmpeg` + `curl`（healthcheck）。
- 探活：`GET /health`（非 `/api`，免 JWT）。
- 数据卷：`/data`，`DB_PATH=/data/jianvideo.db`。

## 4. 任务拆分

- [x] Dockerfile / compose / `.env.example` / `deploy/README.md`
- [x] 公开 `/health` + 单测
- [x] OPERATIONS 入口与规格验收说明

## 5. 验收标准

- [x] `deploy/` 可 `docker compose -f deploy/docker-compose.yml up --build`（需本机 Docker）。
- [x] healthcheck 命中 `/health`；OPERATIONS 有 Docker 节。
- [x] 文档写明 CGO=1 + FFmpeg，不宣称 CGO=0。

## 6. 风险 / 待定

- 完整镜像构建耗时长（前端 + CGO）；本机无 Docker 时以文件与单测验收，compose 冒烟由运维环境执行。
- 后续可加 systemd 单元或 Helm（非本 FR）。
