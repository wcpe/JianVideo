# 功能规格：系统诊断页与跨平台打包

> 状态：开发中　·　关联 PRD：FR-21、FR-22　·　分支：feature/system-diagnostics-packaging

## 1. 背景与目标

FR-02（SMB）、FR-10（Intel QSV/VAAPI）、FR-11（NVIDIA NVENC）的代码已就绪，但本机无对应硬件/网络，真机验收受限。需要一个**自带的诊断手段**：把应用打包成可在目标机直接运行的产物，运行后在页面上看到系统情况与编解码器实测结果，复制后回传，由用户在有相应硬件的机器上完成 FR-10/FR-11 验收。属 P2 配套（语义上偏 P3 运维工具，提前实现以支撑本期验收，经用户确认）。

## 2. 需求（要什么）

### FR-21 系统诊断页
- 范围内：
  - 后端暴露系统信息：操作系统/架构、CPU 核数、主机名、Go 运行时版本、应用版本、ffmpeg 是否可用 + 路径 + 版本号；复用现有硬件加速检测结果（`BuildHWAccelInfo`）。
  - 后端提供「编解码器实测」：对候选编码器（软件 libx264/libx265，Intel QSV，VAAPI，NVIDIA NVENC，AMD AMF，VideoToolbox 的 H.264/H.265）逐个用**外部 ffmpeg 跑一小段试编码**，报告「是否编入当前 ffmpeg / 试编码是否成功 / 失败时的错误尾部」。
  - 前端新增受保护页面 `/system`，展示系统信息 + 编解码器实测结果，提供「测试编解码器」按钮触发实测、「复制结果」按钮复制纯文本报告。
- 不做（范围外）：
  - 不替换/不重构现有 CGO/libav 硬件检测（用户决定保留现状）；编解码器实测仅走外部 ffmpeg CLI，独立于 CGO 检测。
  - 不采集 CPU 型号/总内存等需新增依赖或平台特定代码的指标（避免引入 gopsutil 等依赖）。
  - 不在本页做 SMB 连接测试（FR-02 验收走正常添加 SMB 路径流程）。

### FR-22 跨平台打包
- 范围内：
  - 根目录 Makefile，一键完成：构建前端（go:embed 产物）→ 编译单二进制（注入 VERSION）→ 组装发布包（二进制 + 随包 ffmpeg/ffprobe + 运行说明）→ 打包 zip/tar。
  - 应用启动时按「环境变量 → 可执行文件同目录捆绑版 → PATH」顺序解析 ffmpeg/ffprobe，使随包 ffmpeg 开箱即用。
  - 支持 Windows 与 Linux：因 SQLite 用 mattn/go-sqlite3（CGO），采用**各平台原生构建**（在对应 OS 上 make），不做交叉编译。
- 不做（范围外）：
  - 不自动下载 ffmpeg（许可证/体积/网络）；ffmpeg/ffprobe 由用户放到约定目录，Make 仅负责拷贝进包。
  - 不引入 Docker/容器化（架构红线）。
  - 不把 ffmpeg 编进 Go 二进制（技术上不可行——ffmpeg 是外部进程依赖）。

## 3. 设计（怎么做）

分发与打包的架构决策见 [ADR-0027](../adr/0027-cross-platform-packaging.md)，此处不重复决策正文。

### 3.1 后端

- 新增 `internal/transcoder/encoder_probe.go`（编解码器实测，归属 ffmpeg 域）：
  - `candidateEncoders() []EncoderCandidate`：静态候选表（family/codec/encoder）。
  - `parseCompiledEncoders(out string) map[string]bool`：解析 `ffmpeg -encoders` 输出（纯函数，可测）。
  - `buildProbeArgs(c EncoderCandidate) []string`：按 family 生成试编码参数（纯函数，可测；vaapi 特殊：`-init_hw_device vaapi=va:/dev/dri/renderD128 -filter_hw_device va ... -vf format=nv12,hwupload`，其余通用 `-f lavfi -i color=...:s=256x256:r=5 -frames:v 5 -an -c:v <enc> -f null -`）。
  - `ProbeEncoders(ctx) []EncoderProbeResult`：先取一次 `-encoders` 解析编入集合；对编入的候选逐个带超时（每个约 20s）试编码，捕获退出码与 stderr 尾部。**高风险区（FFmpeg 进程管理）**：用 `exec.CommandContext` + 超时 context，限制 stderr 缓冲，杜绝僵尸/泄露，ffmpeg 不可用时立即返回不阻塞。
  - `FFmpegVersion(ctx) string`：`ffmpeg -version` 首行。
- 新增 ffprobe 路径支持（与 ffmpeg 对称）：`SetFFprobePath/GetFFprobePath`，`codec.go` 改用 `GetFFprobePath()` 替代硬编码 `"ffprobe"`。
- 新增 `internal/api/system_handler.go`：
  - `GET /api/system/info`（`Handler.SystemInfo`）：聚合 `runtime`/`os` 信息 + `transcoder` 的 ffmpeg 状态/版本 + `BuildHWAccelInfo`，应用版本取自 `Handler.version`（main 经 ldflags 注入）。
  - `POST /api/system/codec-test`（`Handler.CodecTest`）：调用 `transcoder.ProbeEncoders`，返回结果列表；ffmpeg 不可用时 200 + `ffmpeg_available:false`。
  - 路由在 `RegisterRoutes` 中与 `/api/transcode/hwaccel` 并列注册（同样的鉴权处理）。
- `api.Handler` 增 `version` 字段 + `WithVersion(v string)` 构造选项；`NewHandler(...).WithVersion(version)`。
- `main.go`：新增 `var version = "dev"`（ldflags 覆盖）；新增 `resolveTool(env, name)` 按「环境变量→同目录捆绑→PATH」解析，分别 `SetFFmpegPath`/`SetFFprobePath`。

### 3.2 API 契约

`GET /api/system/info` → 200：
```json
{
  "app_version": "0.3.0",
  "os": "linux", "arch": "amd64", "num_cpu": 8, "hostname": "nas01",
  "go_version": "go1.22.x",
  "ffmpeg": { "available": true, "path": "/opt/jianvideo/ffmpeg", "version": "ffmpeg version 6.1.1 ..." },
  "hwaccel": { /* 现有 HWAccelInfo 结构原样 */ }
}
```

`POST /api/system/codec-test` → 200：
```json
{
  "ffmpeg_available": true,
  "results": [
    { "encoder": "libx264", "family": "software", "codec": "h264", "compiled": true, "tested_ok": true, "detail": "" },
    { "encoder": "h264_qsv", "family": "qsv", "codec": "h264", "compiled": true, "tested_ok": false, "detail": "<stderr 尾部>" }
  ]
}
```

### 3.3 前端

- `src/pages/SystemPage.tsx`：挂载请求 `/api/system/info`；「测试编解码器」按钮 POST `/api/system/codec-test`（带 loading）；「复制结果」用 Mantine `useClipboard` 复制纯文本报告。
- `src/api/system.ts`（real + mock 双实现，遵循现有 `VITE_USE_MOCK` 范式）、`src/types/index.ts` 增类型。
- `src/App.tsx` 加受保护路由 `/system`；`src/components/AppLayout.tsx` 加导航项。
- 测试：`SystemPage.test.tsx` + `mocks/handlers.ts`/`mocks/data.ts` 增系统接口 mock。

### 3.4 打包

- 根 `Makefile`（GNU Make；Windows 经 WSL/Git-Bash/scoop 提供）目标：`frontend`、`build`、`build-hwaccel`（加 `-tags ffmpeg`，需 libav 开发库）、`package`、`clean`。VERSION 经 `-ldflags "-X main.version=$(VERSION)"` 注入。
- 发布包结构：`jianvideo-<版本>-<os>-<arch>/` 内含二进制、`ffmpeg`/`ffprobe`（来自 `FFMPEG_DIR`，用户自备）、运行说明。

## 4. 任务拆分
- [ ] 后端 `encoder_probe.go`（纯函数 + ProbeEncoders）+ 单测/集成测试
- [ ] ffprobe 路径支持（SetFFprobePath/GetFFprobePath + codec.go 改造）
- [ ] `system_handler.go` 两个端点 + 路由注册 + Handler.WithVersion
- [ ] main.go version 注入 + ffmpeg/ffprobe 同目录解析
- [ ] 前端 SystemPage + api/system + 类型 + 路由 + 导航 + 测试 + mock
- [ ] 根 Makefile + 发布说明
- [ ] 文档同步：PRD 状态、ARCHITECTURE（新模块/端点/分发模型）、API、CHANGELOG、README（修正 ffmpeg 调用方式表述）

## 5. 验收标准
- 编解码器实测纯函数（解析 `-encoders`、按 family 构参）有单测覆盖；`ProbeEncoders` 集成测试在有 ffmpeg 的环境下对 `libx264` 报告「编入 + 试编码成功」（无 ffmpeg 时跳过）。
- `GET /api/system/info` 返回结构完整、含应用版本与 ffmpeg 版本；`POST /api/system/codec-test` 返回候选编码器实测结果。
- 前端 `/system` 页测试：渲染系统信息、点击「测试编解码器」展示结果、「复制结果」写入剪贴板（MSW mock）。
- **（真机验收，需用户确认）**：在装有 Intel/NVIDIA 的机器上运行打包产物，`/system` 页对 `h264_qsv`/`hevc_qsv`（或 `*_nvenc`）报告「试编码成功」；复制报告回传。
- **（真机验收，需用户确认）**：`make package` 在 Windows 与 Linux 各自产出单二进制发布包，随包 ffmpeg 可被自动发现、无需手动配置即可启动播放。

## 6. 风险 / 待定
- VAAPI 试编码依赖 `/dev/dri/renderD128` 设备路径，不同发行版可能不同；失败时 stderr 尾部供用户判断，必要时后续做成可配置。
- 各平台原生构建要求目标机有 C 工具链（mattn/go-sqlite3 CGO）；硬件转码（非诊断）还需 `-tags ffmpeg` + libav 开发库——诊断页本身不需要，普通 `make build` 即可。
- Makefile 依赖 GNU Make，Windows 需 WSL/Git-Bash/scoop 提供（在发布说明中写明）。
</content>
