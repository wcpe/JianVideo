# 功能规格：自更新下载进度与重试（FR-90）

> 状态：开发中　·　关联 PRD：FR-90（扩 FR-46）　·　分支：feature/fr-90-update-progress

## 1. 背景与目标
FR-46 的自更新「下载→校验→替换→重启」全程对用户不可见：点「立即更新」后只有一个无反馈的等待，几十 MB 产物在慢网络下载时用户无从判断是卡死还是在进行；下载/校验失败也只弹一句错误、无显式重试入口（只能再点主按钮）。FR-90 在不改动 FR-46 替换/重启正常逻辑的前提下，给下载链路加**进度上报**与前端**失败重试**入口，提升可观测性与可恢复性。属 P8（扩 FR-46）。

## 2. 需求（要什么）
- 范围内：
  - 后端：`downloadToTemp` 用计数 `io.Writer` 包装 `io.Copy`，按字节累进上报「已下载 / 总字节」。总字节取响应 `Content-Length`，未知（≤0）时报已下载字节 + 不确定态（total=0）。
  - 后端：新增进度查询端点 `GET /api/system/update/progress`（轮询），复用 FR-13 鉴权，返回 `{state, downloaded, total, percent}`。进度状态以进程内单例（`Service` 上的互斥量保护字段）维护，不落库。
  - 前端：更新进行中轮询进度端点并以 `Progress` 展示百分比；下载/更新失败后展示显式「重试」按钮重新触发 apply。
- 不做（范围外）：
  - 不引入 SSE / WebSocket / 任何外部消息队列或缓存中间件（架构不变量禁 Redis/MQ）——只用标准库 + 现有内存状态单例，轮询最简。
  - 不动 `replaceAndRestart`（备份 `.old` → 落地 → spawn → 延时退出）这段已正确处理 Windows 覆盖运行中 exe / spawn 失败回退的代码。
  - 不改下载 client「不设整体 Timeout、靠 context」的既有设计（FR-46 真机教训，commit eb28aa9）。
  - 不做断点续传、不做下载速率/剩余时间估算（YAGNI）。

## 3. 设计（怎么做）
本功能不涉及新架构决策（无新中间件、无新机制、无依赖方向变化），故**不写 ADR**——轮询 + 进程内状态单例复用既有 update 状态管理范式（与 FR-46 的 TTL 缓存单例同构）。

### 后端 `internal/update`
- **进度状态**：`Service` 新增互斥量保护的进度字段 `progress`（`progressState{State, Downloaded, Total}`）。`State` 取值 `idle`/`downloading`/`verifying`/`done`/`failed`。提供 `Progress()` 读取副本（并发安全）、内部 `setProgress*` 更新。
- **计数下载**：`downloadToTemp` 接受一个 `func(downloaded, total int64)` 回调，包装 `io.Copy` 的 writer（`countingWriter`）在每次写入后回调累计已下载字节；总字节取 `resp.ContentLength`（≤0 视为未知）。`Apply` 把回调接到 `setProgressDownloading`，并在进入校验 / 失败 / 完成时切 `State`。
- **端点**：`GET /api/system/update/progress` → `h.UpdateProgress`，读 `updateSvc.Progress()` 组装 `{state, downloaded, total, percent}`（`percent` 在 `total>0` 时为 `downloaded*100/total` 取整，否则 0）。鉴权随 `/api/*` 的 APIGuard。

### 前端（系统诊断页 `/system` 应用更新子 tab）
- `applyUpdate` 触发后启动进度轮询（每 ~800ms 调 `getUpdateProgress`），用 Mantine `<Progress>` 展示 `percent`（total 未知时展示已下载字节 + 不确定文案）；轮询在请求结束 / 进入重启等待时停止。
- 失败（apply 抛错或进度 state=failed）时展示「重试」按钮，点击重新走 `handleApplyUpdate`。

## 4. 任务拆分
- [x] 后端：进度状态单例 + 计数下载（红→绿单测：httptest 带 Content-Length，断言回调按字节累进）
- [x] 后端：`GET /api/system/update/progress` 端点 + handler 单测
- [x] 前端：进度轮询 + `Progress` 渲染 + 失败「重试」按钮（vitest）
- [x] 文档同步：PRD 状态、ARCHITECTURE、API、CHANGELOG

## 5. 验收标准
- 后端：`go vet ./internal/update/... ./internal/api/...`；`go test ./internal/update/... ./internal/api/...` 全绿。新增 `download` 进度计数单测（本地 httptest 提供带 Content-Length 的响应、断言进度回调按字节累进且终值 = 总字节）；新增 `GET /api/system/update/progress` 端点单测。
- 前端：`npm run build`（tsc -b）+ `npm run test`（vitest）全绿；新增进度渲染 + 重试按钮的 vitest 用例。
- **真机（待验）**：完整「检测→下载→替换→自动重启→版本变化 + 回滚 + checksum 篡改拒绝」是 FR-46 既有验收项。**本机直连 GitHub 下载 CDN 不可达**（已知限制），需配 FR-80 代理在可达环境才能跑通进度条端到端实测——标「待真机验」，本 FR 交付进度+重试代码与单测。

## 6. 风险 / 待定
- 进度状态为单例：同一时刻只支持一次 apply（自更新本就互斥，符合 FR-46「用户显式触发、不并发」语义）。
- `Content-Length` 未知（GitHub 资产一般有，但代理 / 重定向场景可能缺）→ 退化为不确定态，前端展示已下载字节而非百分比，不报错。
