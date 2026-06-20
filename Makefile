# JianVideo 跨平台打包 Makefile（GNU Make）
#
# 用途：一键完成「构建前端 → 编译单二进制（注入版本号）→ 组装发布包（二进制 + 随包 ffmpeg/ffprobe + 运行说明）→ 压缩」。
# 决策依据见 docs/adr/0027-cross-platform-packaging.md，范围见 docs/specs/system-diagnostics-and-packaging.md。
#
# 重要前提：
#   1. 各平台原生构建，不交叉编译。后端用 mattn/go-sqlite3（CGO），需本机 C 工具链；
#      要产出 Windows 与 Linux 两套产物，须分别在对应系统（或 WSL）上各跑一次 make。
#   2. ffmpeg/ffprobe 是外部进程依赖，无法编入二进制；由用户自备放入 $(FFMPEG_DIR)，
#      Make 只负责拷贝进发布包，绝不自动下载（避免许可证/体积/网络问题）。
#   3. Windows 上需经 WSL / Git-Bash / scoop 等提供 GNU Make 与 tar/zip 命令。
#
# 常用命令：
#   make build           # 构建前端 + 编译单二进制到 dist/
#   make build-hwaccel   # 同 build，额外启用 libav 硬件检测（需 libavcodec 等开发库）
#   make package         # 在 build 基础上组装并压缩发布包
#   make clean           # 删除 dist/
#   make help            # 查看所有目标

# 版本号唯一真源：根目录 VERSION 文件。
VERSION := $(shell cat VERSION)

# 当前构建平台：取本机 go 环境，不交叉编译。
GOOS  := $(shell go env GOOS)
GOARCH := $(shell go env GOARCH)

# 二进制名与可执行后缀（Windows 加 .exe）。
BIN_NAME := jianvideo
ifeq ($(GOOS),windows)
	EXE := .exe
else
	EXE :=
endif

# 输出目录与发布包目录名。
DIST_DIR := dist
BIN_PATH := $(DIST_DIR)/$(BIN_NAME)$(EXE)
PKG_NAME := $(BIN_NAME)-$(VERSION)-$(GOOS)-$(GOARCH)
PKG_DIR  := $(DIST_DIR)/$(PKG_NAME)

# 用户自备的 ffmpeg/ffprobe 存放目录（按平台区分）。Make 仅从此处拷贝，不下载。
# 可在命令行覆盖，例如：make package FFMPEG_DIR=/path/to/ffmpeg
FFMPEG_DIR ?= third_party/ffmpeg/$(GOOS)

# 链接参数：去符号表与调试信息（-s -w）减小体积，并注入版本号。
LDFLAGS := -s -w -X main.version=$(VERSION)

.PHONY: help frontend build build-hwaccel package clean

# 默认目标：打印帮助。
help:
	@echo "JianVideo 打包目标（当前平台 $(GOOS)/$(GOARCH)，版本 $(VERSION)）："
	@echo "  make frontend       构建前端产物（go:embed 所需 frontend/dist）"
	@echo "  make build          构建前端 + 编译单二进制到 $(BIN_PATH)"
	@echo "  make build-hwaccel  同 build，额外启用 libav 硬件检测（需 libavcodec 等开发库）"
	@echo "  make package        在 build 基础上组装并压缩发布包到 $(DIST_DIR)/"
	@echo "  make clean          删除 $(DIST_DIR)/"
	@echo ""
	@echo "ffmpeg/ffprobe 请自备放入 $(FFMPEG_DIR)/（package 时拷入发布包）。"

# 构建前端：产出 frontend/dist，供后端 go:embed 内嵌。
frontend:
	cd frontend && npm ci && npm run build

# 编译单二进制（含内嵌前端）。CGO 必开（mattn/go-sqlite3 依赖）。
build: frontend
	CGO_ENABLED=1 go build -ldflags "$(LDFLAGS)" -o $(BIN_PATH) .
	@echo "已生成二进制：$(BIN_PATH)"

# 启用 libav 硬件检测的构建：额外加 -tags ffmpeg。
# 注意：仅硬件转码所需的 CGO/libav 检测路径需要此构建，目标机须装有 libavcodec/libavutil 等开发库；
#       系统诊断页（FR-21）的编解码器实测走外部 ffmpeg CLI，普通 make build 即可，无需本目标。
build-hwaccel: frontend
	CGO_ENABLED=1 go build -tags ffmpeg -ldflags "$(LDFLAGS)" -o $(BIN_PATH) .
	@echo "已生成二进制（含 libav 硬件检测）：$(BIN_PATH)"

# 组装并压缩发布包：二进制 + 随包 ffmpeg/ffprobe + 运行说明。
package: build
	@echo "组装发布包：$(PKG_DIR)"
	rm -rf "$(PKG_DIR)"
	mkdir -p "$(PKG_DIR)"
	cp "$(BIN_PATH)" "$(PKG_DIR)/"
	@# 拷贝用户自备的 ffmpeg/ffprobe；缺文件时打印中文警告但继续（仍可手动配置环境变量运行）。
	@if [ -f "$(FFMPEG_DIR)/ffmpeg$(EXE)" ]; then \
		cp "$(FFMPEG_DIR)/ffmpeg$(EXE)" "$(PKG_DIR)/"; \
	else \
		echo "[警告] 未找到 $(FFMPEG_DIR)/ffmpeg$(EXE)，发布包不含 ffmpeg，请自行放入或配置 JIANVIDEO_FFMPEG_PATH"; \
	fi
	@if [ -f "$(FFMPEG_DIR)/ffprobe$(EXE)" ]; then \
		cp "$(FFMPEG_DIR)/ffprobe$(EXE)" "$(PKG_DIR)/"; \
	else \
		echo "[警告] 未找到 $(FFMPEG_DIR)/ffprobe$(EXE)，发布包不含 ffprobe，请自行放入或配置 JIANVIDEO_FFPROBE_PATH"; \
	fi
	@# 内联生成中文运行说明（不单独入库模板文件）。
	@printf '%s\n' \
		'JianVideo 运行说明' \
		'==================' \
		'' \
		'版本：$(VERSION)' \
		'平台：$(GOOS)/$(GOARCH)' \
		'' \
		'一、如何运行' \
		'  - 本目录内含主程序 $(BIN_NAME)$(EXE)，以及随包的 ffmpeg/ffprobe。' \
		'  - 直接运行主程序即可启动（Windows 双击或命令行执行，Linux 在终端执行 ./$(BIN_NAME)）。' \
		'  - 启动后在浏览器打开 http://localhost:8080 （端口可经环境变量调整，见下）。' \
		'' \
		'二、ffmpeg 随包说明' \
		'  - 程序按「环境变量 → 可执行文件同目录捆绑版 → 系统 PATH」顺序查找 ffmpeg/ffprobe。' \
		'  - 本目录内的 ffmpeg/ffprobe 与主程序同目录，开箱即用，无需手动配置。' \
		'  - 请勿移动或改名这两个文件；如需替换为系统已装版本，可用下方环境变量指定路径。' \
		'  - ffmpeg 受其自身授权（LGPL/GPL）约束，分发时请遵守相应许可证。' \
		'' \
		'三、可用环境变量（仅列名称，按需设置真实值）' \
		'  - SERVER_PORT              服务监听端口（默认 8080）' \
		'  - JWT_SECRET               登录令牌签名密钥（生产环境务必设置为随机长字符串，否则每次重启需重新登录）' \
		'  - SMB_MASTER_PASSWORD      SMB 凭据加密主密码（用于加密保存的共享凭据）' \
		'  - JIANVIDEO_FFMPEG_PATH    指定 ffmpeg 可执行文件路径（覆盖同目录捆绑版）' \
		'  - JIANVIDEO_FFPROBE_PATH   指定 ffprobe 可执行文件路径（覆盖同目录捆绑版）' \
		> "$(PKG_DIR)/运行说明.txt"
	@# 压缩：Linux 用 tar.gz，Windows 用 zip。
ifeq ($(GOOS),windows)
	cd $(DIST_DIR) && zip -r "$(PKG_NAME).zip" "$(PKG_NAME)"
	@echo "已生成发布包：$(DIST_DIR)/$(PKG_NAME).zip"
else
	cd $(DIST_DIR) && tar -czf "$(PKG_NAME).tar.gz" "$(PKG_NAME)"
	@echo "已生成发布包：$(DIST_DIR)/$(PKG_NAME).tar.gz"
endif

# 清理构建产物。
clean:
	rm -rf $(DIST_DIR)
	@echo "已删除 $(DIST_DIR)/"
