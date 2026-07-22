# 多阶段构建：前端 dist → Go embed（CGO + sqlite3）→ 含 FFmpeg 的运行镜像。
# 见 docs/specs/fr2-072-deploy-skeleton.md、docs/OPERATIONS.md。
#
# 策略说明（与 JianArtifact 差异，刻意保留）：
# - 运行时 CGO=1：默认 SQLite 驱动为 mattn/go-sqlite3，不做全量 CGO=0。
# - 运行镜像带 ffmpeg/ffprobe：转码/探测为外部进程，非 distroless 纯静态。
# - 构建参数可覆盖，便于内网/离线缓存镜像。
ARG WEB_IMAGE=node:22-bookworm-slim
ARG BUILD_IMAGE=golang:1.26-bookworm
ARG RUNTIME_IMAGE=debian:bookworm-slim

# ---- Stage 1：前端构建并同步 embed 目录 ----
FROM ${WEB_IMAGE} AS web
WORKDIR /src
RUN corepack enable
COPY pnpm-workspace.yaml package.json pnpm-lock.yaml* .npmrc* ./
COPY apps/web/package.json apps/web/
COPY packages/ui/package.json packages/ui/
COPY packages/theme/package.json packages/theme/
COPY packages/media-client/package.json packages/media-client/
COPY packages/player-core/package.json packages/player-core/
COPY packages/render-pixi/package.json packages/render-pixi/
COPY packages/mock/package.json packages/mock/
COPY packages/benchmark/package.json packages/benchmark/
COPY packages/eslint-config/package.json packages/eslint-config/
COPY packages/typescript-config/package.json packages/typescript-config/
RUN pnpm install --frozen-lockfile || pnpm install
COPY . .
RUN pnpm --filter @jianvideo/web run build

# ---- Stage 2：Go 编译（CGO + 内嵌 web/dist）----
FROM ${BUILD_IMAGE} AS build
WORKDIR /src
ARG GOPROXY=https://goproxy.cn,direct
ENV GOPROXY=${GOPROXY}
ENV GOTOOLCHAIN=local
ENV CGO_ENABLED=1
RUN apt-get update && apt-get install -y --no-install-recommends \
    gcc libc6-dev \
    && rm -rf /var/lib/apt/lists/*
COPY go.work ./
COPY apps/server/go.mod apps/server/go.sum* apps/server/
RUN cd apps/server && go mod download || true
COPY . .
COPY --from=web /src/apps/server/web/dist/ apps/server/web/dist/
RUN cd apps/server && go build -trimpath \
    -ldflags="-s -w -X main.version=$(cat /src/VERSION 2>/dev/null || echo dev)" \
    -o /out/jianvideo .

# ---- Stage 3：运行镜像（FFmpeg + 二进制）----
FROM ${RUNTIME_IMAGE} AS runtime
RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    ffmpeg \
    curl \
    && rm -rf /var/lib/apt/lists/*
WORKDIR /app
COPY --from=build /out/jianvideo /app/jianvideo
# 数据目录经卷挂载到 /data（库、hls、thumbnails 等同父目录）
ENV DB_PATH=/data/jianvideo.db
ENV SERVER_PORT=8080
VOLUME ["/data"]
EXPOSE 8080
# 非 root 运行；卷权限由 compose user/volume 约定
USER 65532:65532
ENTRYPOINT ["/app/jianvideo"]
