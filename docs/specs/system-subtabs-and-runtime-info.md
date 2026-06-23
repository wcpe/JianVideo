# 功能规格：系统诊断页拆子 tab + 运行环境补齐 + 更新交互完善 + 运行期配置热更新

> 状态：开发中　·　关联 PRD：FR-59 / FR-60 / FR-62 / FR-63　·　分支：feature/fr-59-system-tabs

## 1. 背景与目标

第七期（P7）界面交互与运维完善。`SystemPage`（控制台「系统信息」tab）此前把运行环境 / FFmpeg / 硬件加速 / 应用更新 / 编解码测试五块单列平铺，页面过长需大量滚动；硬件加速卡片纵向单列、信息密度低；页眉「更新可用」点击只能跳到 tab、锚点滚动在 `keepMounted={false}` 下失效（FR-58 遗留）。运行环境信息字段偏少（仅 6 项标量）。应用更新「检查更新」走缓存无强制刷新入口、确认用原生 `window.confirm`。设置页 env 表纯只读，magick 路径无可热更入口。

本规格覆盖 FR-59/60/62/63 四个 FR，统一记录其需求、设计与边界。

## 2. 需求（要什么）

### FR-59 系统诊断页拆子 tab + 硬件加速网格 + 页眉精确跳转
- SystemPage 内部拆 Mantine 子 tab：运行环境（含 FFmpeg）/ 硬件加速 / 编解码测试 / 应用更新，消除长滚动。
- 子 tab 由 URL query 控制（`?tab=system&sys=<env|hwaccel|codec|update>`），便于深链。
- 硬件加速 per-family 卡片由 Stack 单列改 `SimpleGrid` 一行 2-3 列。
- 页眉 `UpdateIndicator` 点击导航到 `?tab=system&sys=update`，SystemPage 读 query 自动选中应用更新子 tab（修复 FR-58 跳转）。

### FR-60 运行环境信息补齐
- 后端 `GET /api/system/info` 增字段：PID、工作目录、可执行文件路径、数据库路径、运行时内存（Alloc/Sys/NumGC）、GOMAXPROCS、运行时长。
- 前端 `SystemInfo` 类型与「运行环境」子 tab 同步展示。
- 范围内：仅进程级、Go runtime 可得信息（不引新依赖）。
- 不做（范围外）：系统级总内存 / 磁盘可用（需 gopsutil 等新依赖，禁止）。

### FR-62 应用更新交互完善
- 进入默认用缓存展示（确认现状）。
- 「检查更新」改「获取更新」走 `force=true` 强制刷新。
- 更新 / 回滚确认由原生 `window.confirm` 改 Mantine `<Modal>` 确认（`@mantine/modals` 未装，用 `@mantine/core` 的 `<Modal>` 自建，不引新依赖）。

### FR-63 运行期配置设置页可编辑热更新
- 设置页新增「运行期配置」可编辑区，仅放有 settings 键 + 运行期 apply 钩子的项。
- 最小集：magick 路径——新增 settings 键 `magick_path`，仿 `applyFFmpegPathSettings` 加 apply 钩子调 `library.SetMagickPath`。
- env 只读表保留不动（env 进程级不可改）。
- 不做（范围外）：敏感项（JWT/SMB）与启动期项（端口 / DB 路径）严禁可编辑。

## 3. 设计（怎么做）

### FR-59
- `SystemPage` 引入 `useSearchParams` 读 `sys` query（缺省 / 非法落回 `env`）。切子 tab 用 `setSearchParams` 函数式更新，保留外层 `tab=system`。
- 四子 tab：env（运行环境 + FFmpeg 两卡片）、hwaccel、codec、update。原各卡片内容原样移入对应 Panel。
- 硬件加速 per-family 卡片包裹层 `Stack` → `SimpleGrid cols={{ base:1, sm:2, lg:3 }}`。
- `UpdateIndicator` 常量 `UPDATE_ROUTE` 改 `/system?tab=system&sys=update`。`id="update"` 锚点可保留兜底。

### FR-60
- `system_handler.go` SystemInfo 增 runtime/process 字段。运行时长需 `main.go` 记录启动时刻并经 handler 注入——评估：`main.go` 已有 handler builder 模式，加 `WithStartTime(time.Time)` 注入即可，开销低、可行。
- `runtime.ReadMemStats` 取 Alloc/Sys/NumGC；`runtime.GOMAXPROCS(0)` 取并行度；`os.Getpid/Getwd/Executable` 取进程信息。
- 数据库路径：由 `main.go` 经 `WithDBPath(cfg.DBPath)` 注入（handler 不依赖 config 包，守依赖方向）。

### FR-62
- 前端 `handleCheckUpdate` 改 `force=true`；按钮文案「检查更新」→「获取更新」。
- 自建 `<ConfirmModal>`（基于 `@mantine/core` `Modal`）替换 `window.confirm`，承载更新 / 回滚二次确认。

### FR-63
- `settings` 包增常量 `KeyMagickPath = "magick_path"`。
- `applyFFmpegPathSettings` 同文件增 `applyMagickPathSettings`（或并入统一 apply），保存含 `magick_path` 且非空时调 `library.SetMagickPath`。
- `main.go` 启动时持久化 `magick_path` 非空则覆盖自动发现（仿 ffmpeg_path）。
- 路径全局变量并发：`library.magickPath` / `transcoder.ffmpegPath` 为普通 `string`，启动期与运行期 PUT 都可能写、转码读。评估加 `sync/atomic` 或互斥——本期最小改：见 §6 风险，按确认结论实施。
- 前端设置页「运行期配置」区加 magick 路径输入，随保存即生效。

## 4. 任务拆分
- [x] FR-59：SystemPage 子 tab + 硬件加速网格 + UpdateIndicator 跳转；vitest 覆盖
- [x] FR-60：后端 SystemInfo 字段补齐 + 前端类型与展示；go test 断言新字段
- [ ] FR-62：force 刷新 + Mantine Modal 确认；vitest 覆盖
- [ ] FR-63：magick_path settings 键 + apply 钩子 + 设置页可编辑；go test + vitest 覆盖
- [ ] 文档同步：PRD 状态、ARCHITECTURE、API、CHANGELOG

## 5. 验收标准
- `npx tsc --noEmit` + `npx vitest run` 前端全绿。
- 受影响后端包 `go build` + `go vet` + `go test` 绿。
- FR-59：子 tab 切换 / URL 同步 / UpdateIndicator 导航目标断言通过。
- FR-60：`go test` 断言 SystemInfo 含 pid/work_dir/executable/db_path/mem/gomaxprocs/uptime 等新字段。
- FR-62：vitest 断言 force=true 透传、模态框确认流程触发 apply/rollback。
- FR-63：go test 断言 PUT magick_path 后 `library.GetMagickPath` 更新；vitest 断言设置页 magick 输入保存。
- 真机维度（浏览器目视子 tab / 网格 / 跳转 / 模态框）标「待真机验」。

## 6. 风险 / 待定
- FR-60 运行时长：需 main 记录启动时刻并注入 handler（已评估可行，低开销）。
- FR-63 路径全局变量并发：`SetMagickPath`/`SetFFmpegPath` 等为无锁普通字符串写。运行期 PUT 与转码读并发下存在数据竞争（理论上单字符串赋值在 Go 内存模型下非原子可见）。本期对 magick 路径热更新最小引入，**按最小改**：若加保护则统一用 `atomic.Value`/`sync.RWMutex` 包裹路径变量；若改动面过大则仅就 magick 路径加保护并在 spec 标注 ffmpeg 既有变量沿用现状（已随 FR-56 上线）。开工前与主控确认范围。
