# 功能规格：运行期配置设置页可编辑热更新（magick 路径）

> 状态：开发中　·　关联 PRD：FR-63　·　分支：feature/fr-63-settings-hot

## 1. 背景与目标

「设置」tab（`SettingsPage`）当前已把若干运行期配置做成可编辑、保存即生效：扫描周期（FR-28）、回收站路径（FR-24）、ffmpeg/ffprobe 路径（FR-56）。但 ImageMagick（`magick`）路径（HEIC/RAW 转 JPEG，FR-37）此前**只能靠环境变量 `JIANVIDEO_MAGICK_PATH` 注入**，Web 端改不了、改了也要重启。

本期（P7）补齐最后一项**安全可热更**的运行期工具路径：让 `magick` 路径成为持久化设置，保存即应用到运行期、重启保留，复用 ffmpeg 路径的现成范式。敏感项（JWT/SMB）与启动期项（端口/DB 路径/debug 模式）保持只读，不做成可编辑。

## 2. 需求（要什么）

- settings 增键常量 `KeyMagickPath="magick_path"`。
- **持久化设置优先于自动发现**：`main.go` 在 `resolveTool("JIANVIDEO_MAGICK_PATH", "magick")` 默认注入后，若 settings 中 `magick_path` 非空，则用 `library.SetMagickPath` 覆盖（与 ffmpeg/ffprobe 启动覆盖一致）。
- **保存即生效**：`PUT /api/settings` 含 `magick_path` 且非空时，落库后调用 `library.SetMagickPath` 应用到运行期全局路径（HEIC/RAW 转换后续即时采用新路径）。
- 前端「运行期配置」编辑区新增 magick 路径输入框，随既有「保存设置」按钮一并 PUT 保存。
- 范围内：magick 路径输入 + 随 PUT /api/settings 保存 + 保存即生效 + 启动覆盖。
- 不做（范围外）：
  - magick 检测端点（`IsMagickAvailable` 已提供可用性判断；FR-63 核心是「可编辑 + 热更」，独立 detect 端点属镀金，本期不加）。
  - debug 模式（`JIANVIDEO_DEBUG` 在启动期设 gin mode，**启动期项不可热更**，保持 env 只读展示）。
  - 端口 / DB 路径 / JWT / SMB（启动期项与敏感项，保持 env 只读）。
  - env 只读表本身（保持不动，env 进程级不可 Web 改）。

## 3. 设计（怎么做）

无新架构决策，沿用既有分层与真源（settings = SQLite settings 表；magick 全局路径 = `library` 包内全局变量 `magickPath`，由 main 启动注入、运行期可经 `SetMagickPath` 切换）。`library.SetMagickPath` 已存在且每次转换经 `runMagick`/`IsMagickAvailable` 现读该全局，**运行期可切换已具备**，不写新 ADR。

### 后端
- `internal/settings/service.go`：增 `KeyMagickPath` 常量。
- `internal/api/settings_handler.go`：新增 `applyMagickPathSettings`（仿 `applyFFmpegPathSettings`），`UpdateSettings` 落库后调用；`magick_path` 出现且非空时 `library.SetMagickPath`。空串不覆盖（保留自动发现/捆绑版结果），由 `SetMagickPath` 自身空值守卫保证。
- `main.go`：`resolveTool` 注入后，读 settings `magick_path` 非空则覆盖（与 ffmpeg/ffprobe 同处）。

### 前端
- `frontend/src/api/settings.ts`：增 `SETTING_KEY_MAGICK_PATH` 常量。
- `frontend/src/pages/SettingsPage.tsx`：「运行期配置」区新增 magick 路径输入框，纳入 `handleSave` 的 PUT 载荷与回读。
- `frontend/src/mocks/handlers.ts`：现有 PUT /api/settings mock 已回显任意 settings，无需改（如需可补默认值，非必须）。

### 并发说明
`library.magickPath` 与 `transcoder.ffmpegPath/ffprobePath` 同为无锁包级全局，写入点仅限启动期注入与 PUT settings 应用，与既有 ffmpeg 路径并发模型完全一致；本期未新增并发写入点，沿用既有处理（不加锁，精准修改）。

## 4. 任务拆分
- [ ] spec + PRD FR-63 状态改「开发中」
- [ ] 后端测试先行：保存 `magick_path` 后 `library.GetMagickPath()` 变更
- [ ] 后端实现：常量 + `applyMagickPathSettings` + PUT 应用 + main 启动覆盖
- [ ] 前端测试先行 + 实现：settings 常量 / SettingsPage magick 输入框
- [ ] 文档同步：PRD 状态、ARCHITECTURE、API、CHANGELOG

## 5. 验收标准
- 后端单测：
  - 保存 `magick_path` 设置后 `library.GetMagickPath()` 变更为新值；空串保存不覆盖既有路径。
  - 受影响包 `go test ./internal/api/... ./internal/settings/... ./internal/library/...` 全绿、`go vet ./internal/...` 干净、`go build ./...` 通过。
- 前端单测（vitest）：运行期配置区渲染 magick 路径输入框；编辑后点「保存设置」走 PUT /api/settings 且载荷含 `magick_path`；现有 SettingsPage 测试保持绿。
- `npx tsc --noEmit` + `npx vitest run` 全量绿；eslint 改动文件干净。
- 真机（用户复验，待真机验）：设置 tab 填 magick 路径保存后，HEIC/RAW 缩略图/预览即时采用新路径转换，无需重启。

## 6. 风险 / 待定
- magick 路径错误时不致命：`runMagick` 内 `IsMagickAvailable` 守卫会让 HEIC/RAW 显示降级并记日志，不影响其他功能（既有行为）。
- 安全红线：本期只新增 `magick_path`（非敏感、非启动期），不触碰 JWT/SMB/端口/DB 路径/debug 的只读状态。
