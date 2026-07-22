# JianVideo 顶层入口：前端 pnpm/turbo + apps/web；后端委托 apps/server Taskfile。
# 决策：docs/specs/fr2-068-toolchain-entrypoint.md · 打包细节见 ADR-0027。
#
# 重要前提：
#   1. 后端 mattn/go-sqlite3 需 CGO；各平台原生构建，不交叉编译。
#   2. ffmpeg/ffprobe 为外部进程；package 时从 FFMPEG_DIR 拷贝，不自动下载。
#   3. Go 模块在 apps/server；go.work 在仓库根。需安装 go-task（task）。

VERSION := $(shell cat VERSION)
GOOS    := $(shell go env GOOS)
GOARCH  := $(shell go env GOARCH)

BIN_NAME := jianvideo
ifeq ($(GOOS),windows)
	EXE := .exe
else
	EXE :=
endif

DIST_DIR := dist
BIN_PATH := $(DIST_DIR)/$(BIN_NAME)$(EXE)
PKG_NAME := $(BIN_NAME)-$(VERSION)-$(GOOS)-$(GOARCH)
PKG_DIR  := $(DIST_DIR)/$(PKG_NAME)
FFMPEG_DIR ?= third_party/ffmpeg/$(GOOS)

# 在 apps/server 执行 task；Windows/Git Bash 下 task 已在 PATH 时可用。
TASK := task
SERVER_DIR := apps/server

.DEFAULT_GOAL := help
.PHONY: help install dev frontend build build-hwaccel package clean \
	lint vet vuln test coverage quality check openapi-check gen gen-check

help: ## 显示可用目标
	@echo "JianVideo（$(GOOS)/$(GOARCH)，版本 $(VERSION)）— make 目标："
	@echo "  install        安装前端 workspace 依赖（pnpm）"
	@echo "  dev            本地前端 dev（apps/web；后端请另开 go run -C apps/server .）"
	@echo "  frontend       构建 apps/web 并同步 embed 到 apps/server/web/dist"
	@echo "  build          frontend + 后端单二进制 → $(BIN_PATH)"
	@echo "  build-hwaccel  同 build，额外 -tags ffmpeg"
	@echo "  package        组装发布包（二进制 + 可选 ffmpeg）"
	@echo "  lint/vet/vuln  Go 静态检查（委托 task）"
	@echo "  test/coverage  Go 测试 / 覆盖率门"
	@echo "  quality        Go lint+test+coverage"
	@echo "  openapi-check  OpenAPI 契约结构门禁（FR2-071）"
	@echo "  gen            从 api/openapi.yaml 生成 Go 接口（task gen）"
	@echo "  gen-check      生成物与契约防漂移（task gen:check）"
	@echo "  check          根质量门 pnpm quality（含 root/openapi/workspace/frontend/go/e2e/release）"
	@echo "  clean          删除 $(DIST_DIR)/"
	@echo ""
	@echo "后端任务也可直接：cd apps/server && task --list"
	@echo "ffmpeg 自备：$(FFMPEG_DIR)/"

install: ## 安装前端依赖
	pnpm install

dev: ## 前端开发服务器
	npm --prefix apps/web run dev

frontend: ## 构建前端并写入 go:embed 目录
	cd apps/web && npm ci && npm run build

# 后端：委托 Taskfile（需先 frontend，保证 web/dist）
build: frontend ## 前端 + 后端二进制
	cd $(SERVER_DIR) && $(TASK) build
	@echo "已生成二进制：$(BIN_PATH)"

build-hwaccel: frontend ## 前端 + 后端（ffmpeg 硬件检测 tag）
	cd $(SERVER_DIR) && $(TASK) build:hwaccel
	@echo "已生成二进制（含 libav 硬件检测）：$(BIN_PATH)"

package: build ## 发布包
	@echo "组装发布包：$(PKG_DIR)"
	rm -rf "$(PKG_DIR)"
	mkdir -p "$(PKG_DIR)"
	cp "$(BIN_PATH)" "$(PKG_DIR)/"
	@if [ -f "$(FFMPEG_DIR)/ffmpeg$(EXE)" ]; then \
		cp "$(FFMPEG_DIR)/ffmpeg$(EXE)" "$(PKG_DIR)/"; \
	else \
		echo "[警告] 未找到 $(FFMPEG_DIR)/ffmpeg$(EXE)，发布包不含 ffmpeg"; \
	fi
	@if [ -f "$(FFMPEG_DIR)/ffprobe$(EXE)" ]; then \
		cp "$(FFMPEG_DIR)/ffprobe$(EXE)" "$(PKG_DIR)/"; \
	else \
		echo "[警告] 未找到 $(FFMPEG_DIR)/ffprobe$(EXE)，发布包不含 ffprobe"; \
	fi
	@printf '%s\n' \
		'JianVideo 运行说明' \
		'==================' \
		'' \
		'版本：$(VERSION)' \
		'平台：$(GOOS)/$(GOARCH)' \
		'' \
		'一、如何运行' \
		'  - 本目录内含主程序 $(BIN_NAME)$(EXE)，以及随包的 ffmpeg/ffprobe（若已拷贝）。' \
		'  - 启动后在浏览器打开 http://localhost:8080' \
		'' \
		'二、环境变量' \
		'  - SERVER_PORT / JWT_SECRET / DB_PATH / SMB_MASTER_PASSWORD' \
		'  - JIANVIDEO_FFMPEG_PATH / JIANVIDEO_FFPROBE_PATH' \
		> "$(PKG_DIR)/运行说明.txt"
ifeq ($(GOOS),windows)
	cd $(DIST_DIR) && zip -r "$(PKG_NAME).zip" "$(PKG_NAME)"
	@echo "已生成发布包：$(DIST_DIR)/$(PKG_NAME).zip"
else
	cd $(DIST_DIR) && tar -czf "$(PKG_NAME).tar.gz" "$(PKG_NAME)"
	@echo "已生成发布包：$(DIST_DIR)/$(PKG_NAME).tar.gz"
endif

lint: ## Go lint（task）
	cd $(SERVER_DIR) && $(TASK) lint
	@echo "Go 静态检查通过"

vet:
	cd $(SERVER_DIR) && $(TASK) vet
	@echo "Go vet 通过"

vuln:
	cd $(SERVER_DIR) && $(TASK) vuln
	@echo "Go 漏洞扫描通过"

test:
	cd $(SERVER_DIR) && $(TASK) test
	@echo "Go 测试通过"

coverage:
	cd $(SERVER_DIR) && $(TASK) coverage

quality: ## Go 质量门（task quality）
	cd $(SERVER_DIR) && $(TASK) quality
	@echo "Go 质量门禁通过"

openapi-check: ## OpenAPI 契约结构门禁（FR2-071；用 node 直跑，避免 pnpm 装依赖门）
	node --test scripts/openapi-check.test.mjs
	node scripts/openapi-check.mjs
	@echo "OpenAPI 契约门禁通过"

gen: ## 从 api/openapi.yaml 生成 Go ServerInterface（task gen）
	cd $(SERVER_DIR) && $(TASK) gen

gen-check: ## 生成物与契约防漂移（task gen:check）
	cd $(SERVER_DIR) && $(TASK) gen:check

check: ## 全仓 pnpm quality（含 root/openapi 与前后端门）
	pnpm quality

clean:
	rm -rf $(DIST_DIR)
	@echo "已删除 $(DIST_DIR)/"
