# ADR-0027: 跨平台分发与打包模型

## 状态
已接受

## 背景
需要把应用交付到目标机（含具备 Intel/NVIDIA 硬件、SMB 共享的环境）直接运行，以完成 FR-10/FR-11/FR-02 的真机验收。涉及三个现实约束：
- SQLite 采用 `mattn/go-sqlite3`（CGO），跨平台交叉编译需要目标平台 C 工具链；早期 ADR（如 ADR-0025 的理由段）曾假设"纯 Go、交叉编译简单"，与当前驱动现状不符。本决策按**代码现状**（CGO）制定，不修改既往已接受 ADR 正文。
- FFmpeg 是**外部进程依赖**（`exec` 调用 `ffmpeg`/`ffprobe`），无法编入 Go 二进制。
- 前端经 `go:embed` 内嵌，构建二进制前需先产出 `frontend/dist`。

## 决策
- **单 Go 二进制 + 随包附带 ffmpeg/ffprobe**：发布包内含主二进制（内嵌前端）与对应平台的 ffmpeg/ffprobe 可执行文件及运行说明。
- **各平台原生构建**：在对应操作系统上用本机 C 工具链构建（CGO_ENABLED=1），不做一机交叉编译。
- **启动期工具解析顺序**：`JIANVIDEO_FFMPEG_PATH`/`JIANVIDEO_FFPROBE_PATH` 环境变量 → 可执行文件同目录的捆绑版 → PATH 查找，使随包 ffmpeg 开箱即用。
- **Makefile 一键打包**：`frontend → build（-ldflags 注入 VERSION）→ package（组装并打 zip/tar）`；ffmpeg 由用户放入约定目录，Make 仅拷贝，不自动下载。
- 硬件加速**检测**（CGO/libav）保持现状，仅在 `-tags ffmpeg` 构建时启用；编解码器**诊断实测**（FR-21）独立走外部 ffmpeg CLI，普通 `make build` 即可用，不需 libav 开发库。

## 理由
- 用户明确选择保留现状 CGO + 随包附带 ffmpeg（见本期需求决策），原生构建是 CGO 下最稳的路径。
- 同目录自动发现让"单二进制 + 随包 ffmpeg"真正开箱即用，避免每台机器手工配置环境变量。
- 不内置下载，避免许可证（FFmpeg LGPL/GPL）、体积与网络问题。
- 诊断实测与 CGO 检测解耦：真机验收只需普通构建产物即可跑试编码，降低验收机的构建门槛。

## 后果
- 要产出 Windows 与 Linux 两套产物，需在两类机器（或 WSL）上各跑一次 make。
- 目标机需具备 C 工具链才能本机构建；若仅运行预构建产物，则只需具备 ffmpeg（随包提供）。
- Makefile 依赖 GNU Make，Windows 侧需 WSL/Git-Bash/scoop 提供。
- 发布包体积增大（含 ffmpeg/ffprobe，每平台数十~上百 MB）；分发时须遵守 FFmpeg 授权。

## 备选方案
- **纯 Go 化（modernc.org/sqlite + 检测改 CLI）实现一机交叉编译**：可一台机器产出多平台静态单二进制，但需迁移 SQLite 驱动并改造硬件检测，改动面大；用户选择保留现状，故不采用。
- **Docker 交叉构建**：架构红线禁止容器化，不采用。
- **要求目标机自行安装 ffmpeg（不随包）**：二进制更精简，但开箱即用性差，用户选择随包附带。
</content>
