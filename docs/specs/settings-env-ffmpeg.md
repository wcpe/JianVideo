# 功能规格：设置功能补齐（环境变量查看 + FFmpeg 路径配置）

> 状态：开发中　·　关联 PRD：FR-56　·　分支：main

## 1. 背景与目标

「设置」tab（FR-55 合并后由 `SettingsPage` 承载）当前仅有 2 项（扫描周期、回收站路径）。本期补齐两项运维常用能力（P6）：

1. **查看全部环境变量**：让运维在 Web 端一眼看清本项目认得哪些环境变量、当前是否已设置、值是什么——免去登服务器查进程环境。敏感项（`JWT_SECRET`、`SMB_MASTER_PASSWORD`）必须脱敏，绝不回显明文。
2. **FFmpeg/FFprobe 路径运行期可配置 + 检测**：当前 ffmpeg/ffprobe 路径只能靠环境变量或同目录捆绑发现，改不了也测不了。本期让两路径成为持久化设置（保存即生效、重启保留），并提供「检测」按钮验证某路径是否真能跑出版本。

## 2. 需求（要什么）

### ① 环境变量查看（只读 + 敏感脱敏）
- 新增只读端点 `GET /api/system/env`（经全局 APIGuard 鉴权），返回本项目**已知**环境变量清单。
- 每项含 `{ key, description, sensitive, set, value }`：
  - `description`：中文用途说明。
  - `sensitive`：是否敏感。敏感 = `JWT_SECRET`、`SMB_MASTER_PASSWORD`。
  - `set`：该环境变量当前是否已设置（非空）。
  - `value`：非敏感项返回 `os.Getenv` 明文；**敏感项一律返回固定掩码**（已设置 → `****（已设置）`，未设置 → `（未设置）`），绝不返回明文。
- 范围内：只读查看。
- 不做（范围外）：通过 Web 修改环境变量（env 是进程级，改需重启/部署，本期不做）。

### ② FFmpeg/FFprobe 路径配置 + 检测
- settings 增键常量 `KeyFFmpegPath="ffmpeg_path"`、`KeyFFprobePath="ffprobe_path"`。
- **持久化设置优先于自动发现**：`main.go` 在 `resolveTool` 默认注入后，若 settings 中对应键非空，则用 `SetFFmpegPath`/`SetFFprobePath` 覆盖。
- **保存即生效**：`PUT /api/settings` 含这两个键时，落库后调用 `transcoder.SetFFmpegPath`/`SetFFprobePath` 应用到运行时（同时让 library 的 ffprobe 路径同步）。
- **检测端点** `POST /api/system/ffmpeg/detect`，body `{ "path": "..." }`（path 可空 = 测当前已配置路径）：跑 `path -version`，返回 `{ ffmpeg_available, ffmpeg_version }`，**不改全局状态**（保存前先验路径）。
- 范围内：ffmpeg + ffprobe 两路径输入、检测按钮、随既有 PUT /api/settings 保存。
- 不做（范围外）：其他 tab、FR-54 收缩导航、FR-57 页脚/协议页、FR-58 页眉提示；不通过 detect 端点持久化（持久化走 PUT settings）。

## 3. 设计（怎么做）

无新架构决策，沿用既有分层与真源（settings = SQLite settings 表；ffmpeg 全局路径 = transcoder 包内全局变量，由 main 启动注入），不写新 ADR。

### 后端
- `internal/settings/service.go`：增 `KeyFFmpegPath`、`KeyFFprobePath` 常量。
- `internal/api/system_handler.go`：
  - 维护已知 env keys 元数据表（key + 中文用途 + 敏感标志），`GET /api/system/env` 据此构造脱敏响应。
  - `POST /api/system/ffmpeg/detect`：解析 body.path（空则用当前），调 `transcoder.CheckFFmpegPath`。
- `internal/transcoder/multi_pipeline.go`（或就近）：新增纯函数 `CheckFFmpegPath(ctx, path) (available bool, version string)`——对给定路径跑 `-version`，不触碰全局 `ffmpegPath`。
- `internal/api/settings_handler.go`：`UpdateSettings` 落库后，若本次包含 ffmpeg_path/ffprobe_path，应用到运行时（api→transcoder 依赖方向允许）。
- `internal/api/router.go`：注册 `GET /api/system/env`、`POST /api/system/ffmpeg/detect`。
- `main.go`：`resolveTool` 注入后，读 settings 覆盖。

### 前端
- `frontend/src/api/system.ts`：增 `getEnvVars()`、`detectFFmpeg(path?)`；`frontend/src/types`：增 `EnvVar`、`FFmpegDetectResult`。
- `frontend/src/api/settings.ts`：增 `SETTING_KEY_FFMPEG_PATH`、`SETTING_KEY_FFPROBE_PATH` 常量。
- `frontend/src/pages/SettingsPage.tsx`：加「环境变量」表格区块（敏感显掩码徽标、非敏感显明文，只读）+「FFmpeg 路径」区块（ffmpeg/ffprobe 输入 + 检测按钮 + 随设置保存）。
- `frontend/src/mocks/handlers.ts`：增 `GET /api/system/env`、`POST /api/system/ffmpeg/detect` mock。

## 4. 任务拆分
- [ ] spec + PRD FR-56 状态改「开发中」
- [ ] 后端测试先行：env 脱敏、CheckFFmpegPath、保存路径生效、detect 端点
- [ ] 后端实现：常量 + env 端点 + CheckFFmpegPath + detect 端点 + PUT 应用 + main 注入 + 路由
- [ ] 前端测试先行 + 实现：api/types/settings 常量/SettingsPage 区块/mock
- [ ] 文档同步：PRD 状态、ARCHITECTURE、API、CHANGELOG

## 5. 验收标准
- 后端单测：
  - `GET /api/system/env` 返回全部已知 keys；**即便 `JWT_SECRET`/`SMB_MASTER_PASSWORD` 环境变量设了真实值，响应 value 也不等于真实值、仅 `set=true` + 固定掩码**（安全红线，显式断言）；非敏感项（如 `JIANVIDEO_DEBUG`）返回明文。
  - `transcoder.CheckFFmpegPath` 对当前可用 ffmpeg 返回 available=true 且版本非空；对不存在路径返回 available=false。
  - 保存 `ffmpeg_path` 设置后 `transcoder.GetFFmpegPath()` 变更为新值。
  - `POST /api/system/ffmpeg/detect` 响应含 `ffmpeg_available`、`ffmpeg_version` 字段。
  - `go test ./internal/...` 全绿、`go vet ./...` 干净。
- 前端单测（vitest）：设置 tab 渲染环境变量区块（敏感显掩码、非敏感显值）；ffmpeg 路径输入 + 检测按钮（mock detect 返回可用性/版本并展示）；保存路径走 PUT settings。现有 SettingsPage 测试保持绿。
- `npx tsc --noEmit` + `npx vitest run` 全量绿；eslint 改动文件干净。
- 真机（用户复验）：设置 tab 看到环境变量（`JWT_SECRET` 只显「已设置」不显明文）；填 ffmpeg 路径点检测出版本；保存后转码用新路径。

## 6. 风险 / 待定
- 项目存在两套 config 包：根 `config`（`SERVER_PORT`/`DB_PATH`/`JWT_SECRET`）与 `internal/config`（`JIANVIDEO_SERVER_PORT`/`JIANVIDEO_DB_PATH`/`JIANVIDEO_FFMPEG_PATH` 等）。两套环境变量在运行时均生效，env 元数据表需把两套都列出，避免遗漏。
- 敏感脱敏是安全红线：响应序列化的任何字段都不得携带 secret 明文，测试须对此显式断言。
