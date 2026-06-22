# 功能规格：回收站清理

> 状态：开发中　·　关联 PRD：FR-26　·　分支：feature/fr-26-recycle

## 1. 背景与目标

FR-25 已实现软删除与回收站：删除媒体仅置 `media_files.deleted_at`，源文件不动，回收站页可查看并还原。本功能（P2）补上「清理」这条链：把回收站里的软删项真正移出媒体库——将每个软删项的**磁盘源文件**移动到其所在盘符对应的回收站目录（按删除日期分子目录），移动成功后删除该 `media_files` 记录。清理依赖 FR-24 设置提供的「每盘符回收站路径」配置：**未配置该盘符路径则拒绝清理**，避免误把文件移到未知位置。

## 2. 需求（要什么）

- 范围内：
  - 复用 FR-24 设置键 `recycle_bin_paths`（单 key，JSON 字符串：盘符 → 回收站目录），不新增设置键。盘符大小写不敏感（`D` 与 `d` 视为同一盘符）。
  - 新增清理端点 `POST /api/library/recycle/cleanup`：对当前全部软删项执行清理。
  - **未配置校验**：清理前先校验每个软删项所在盘符是否都有配置的回收站路径；只要有任一项的盘符未配置（或路径为空），整体拒绝清理（不移动任何文件），返回明确错误指出缺哪个盘符。
  - 清理动作：把每个软删项的源文件移动到 `<该盘符回收站目录>/<删除日期 YYYY-MM-DD>/<原文件名>`，删除日期取 `deleted_at` 的日期；移动成功后删除该 `media_files` 记录。
  - 前端 `/recycle` 回收站页加「清理回收站」按钮（二次确认 Modal）；未配路径（被后端拒绝）时提示去设置页配置。
- 不做（范围外）：
  - 不动 FR-25 的软删 / 还原 / 回收站列表逻辑。
  - 不动扫描逻辑、不做定时自动清理（FR-28）。
  - 不实现「逐项清理」「彻底物理删除（不移动）」；本期只做「全部软删项整体移动到回收站目录」一种清理方式。
  - SMB 远程软删项无盘符、无法按盘符回收站移动，本期视为「不可清理」并在校验阶段拒绝（与未配置盘符同等处理）。

## 3. 设计（怎么做）

模块：`internal/library`（服务：移动文件 + 删记录）、`internal/api`（端点 + 路由 + 读设置并解析）、`frontend`（按钮 / 二次确认 / 接口 / mock）。复用 FR-24 `settings` 服务与 `recycle_bin_paths` 键、FR-25 `ListDeletedMediaFiles`。

- 依赖方向：`settings` 服务解析（读 JSON）放在 `api` 层完成，`library` 服务只接收已解析好的 `盘符(大写)→目录` 映射，保持 `library` 不依赖 `settings`、不碰 JSON 解析（职责单一）。

- 服务层 `internal/library/recycle_cleanup.go`：
  - 新增 `CleanupRecycle(drivePaths map[string]string) (CleanupResult, error)`：
    1. 查全部软删项（复用 `ListDeletedMediaFiles`）；空则返回零结果。
    2. 校验阶段：逐项取盘符（从 `file_path` 解析，如 `D:/a/b.mp4` → `D`；SMB / 无盘符 → 视为缺失），若该盘符在 `drivePaths` 无非空配置 → 收集缺失盘符；**只要有缺失即整体返回 `ErrRecycleBinPathUnset`（不移动任何文件）**。
    3. 移动阶段：对每项，目标目录 `<drivePaths[盘符]>/<deleted_at 日期>`，`os.MkdirAll` 后 `os.Rename` 移动；目标已存在同名文件则加序号避免覆盖。移动成功后 `Delete` 该记录（先移动成功、后删记录，保证「记录还在 = 文件还在库内或回收站」一致）。
    4. 返回 `CleanupResult{Moved, Failed}` 统计。
  - 盘符解析与目标命名为无副作用纯函数，便于穷举测试。
- 端点 `internal/api/handler.go` + 路由 `internal/api/router.go`：
  - `POST /api/library/recycle/cleanup`：读 `settings.Get(recycle_bin_paths)` → 解析 JSON（map[string]string，键统一大写）→ 调 `library.CleanupRecycle`。
  - 未注入 settings → 503；JSON 非法 → 500；`ErrRecycleBinPathUnset` → 409 + 明确 message（含缺失盘符）；成功 → 200 + `{"moved":N,"failed":M}`。
- 前端：`api/library.ts` 增 `cleanupRecycle()`（real + mock）；`RecyclePage.tsx` 加「清理回收站」按钮 + 二次确认 Modal，成功后刷新列表（清空），失败（409）提示「请先到设置页配置回收站路径」；MSW 增 cleanup handler。

数据模型与依赖方向不变，无新增 ADR（复用既有 settings 表与 media_files 软删字段）。

## 4. 任务拆分

- [x] 后端测试先行：未配路径拒绝、配置后真实移动文件 + 按日期分目录 + 删记录、SMB/无盘符拒绝、空回收站（红→绿）
- [x] 服务层 `CleanupRecycle` + 纯函数（盘符解析 / 目标命名）实现
- [x] 端点 + 路由：读设置解析 JSON、错误码映射
- [x] 前端测试先行：清理按钮 + 二次确认 + 成功清空 + 未配路径提示（红→绿）
- [x] 前端按钮 / Modal / 接口 / mock / MSW 实现
- [x] 文档同步：PRD 状态、ARCHITECTURE、API、CHANGELOG

## 5. 验收标准

- 未配置对应盘符回收站路径时，调清理端点返回 409 且**不移动任何文件、不删任何记录**（集成测试：临时目录 + 软删项，断言文件仍在原位、记录仍在）。
- 配置好盘符路径后清理：源文件被移动到 `<回收站目录>/<删除日期>/<文件名>`、对应 `media_files` 记录被删除、回收站列表清空（集成测试用临时目录验证真实文件移动）。
- 前端 `/recycle` 页有「清理回收站」按钮，点击弹二次确认；确认后调用清理接口；未配路径时给出去设置页配置的提示（前端经 mock/MSW 验证）。
- 自动化：`internal/library` 与 `internal/api` 相关测试、前端 `RecyclePage` 测试全绿；后端 `go build ./...`、前端 `npm run build` 通过。

## 6. 风险 / 待定

- 跨盘符移动：`os.Rename` 在跨卷时可能失败。本期约定回收站目录与源文件同盘符（按盘符配置即天然同卷），故用 `os.Rename`；若运维把某盘回收站配到别的卷，移动会失败并计入 `failed`，不影响其他项。
- 部分失败：移动阶段单项失败（如文件已被外部删除）仅计入 `failed` 并跳过，不回滚已成功项（已移动的文件 + 已删记录保持一致），返回统计供前端展示。
