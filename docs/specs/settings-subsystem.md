# 功能规格：设置子系统

> 状态：开发中　·　关联 PRD：FR-24　·　分支：feature/fr-24-settings

## 1. 背景与目标

目前应用没有运行期可配置项的统一存储。后续的回收站清理（FR-26）需要「每盘符回收站路径」，定时扫描（FR-28）需要「扫描周期」。本功能（P2）为这些后续能力提供配置真源：一个持久化的键值设置表 + 读写端点 + 设置页，使运行期可改、重启后保留。本功能只负责**存与取**键值，不实现回收站、定时扫描的具体行为（那是 FR-25/26/28）。

## 2. 需求（要什么）

- 范围内：
  - 后端 `settings` 服务：按 key 读单项、读全部、写单项、批量写。
  - 端点 `GET /api/settings`（返回全部键值，map 形式）、`PUT /api/settings`（批量 upsert 键值），与其它 `/api` 路由同级注册。
  - 设置写入 SQLite `settings` 表持久化，重启后保留。
  - 前端设置页 `/settings`（Mantine）：加载现有设置、编辑「每盘符回收站路径」与「扫描周期」并保存。
  - 前端 `api/settings.ts`（real + mock，受 `VITE_USE_MOCK` 控制）、路由、导航项。
- 不做（范围外）：
  - 回收站软删/还原/清理行为（FR-25/26）。
  - 定时扫描调度逻辑（FR-28）。
  - 设置项的强校验/类型系统（键值统一按字符串存取，「每盘符回收站路径」以 JSON 字符串存于单个 key）。

## 3. 设计（怎么做）

- 数据模型：复用 foundation 已加的 `models.Setting{Key(主键), Value, UpdatedAt}`（`settings` 表），不改结构。
- 后端新增 `internal/settings/service.go`：`Service{db}`，方法 `Get(key)`、`GetAll()`、`Set(key,value)`、`SetMany(map)`；写操作 upsert（`OnConflict` 更新 value 与 updated_at），批量写在单事务内原子完成。
- API：`internal/api/settings_handler.go` 新增 `GetSettings`/`UpdateSettings`，在 `RegisterRoutes` 注册 `GET/PUT /api/settings`；Handler 持有 settings 服务（经 `WithSettings` 注入，未注入时返回 503，保持既有链式注入风格）。`main.go` 与 `web.NewRouter` 注入 settings 服务实例。
- 已知键以常量集中定义（`recycle_bin_paths`、`scan_interval`），避免魔法字符串散落。
- 前端：`api/settings.ts` real+mock 双实现；`pages/SettingsPage.tsx` 表单读写；`App.tsx` 加受保护路由 `/settings`；`AppLayout.tsx` 导航加「设置」项；MSW handlers 加 `GET/PUT /api/settings`。
- 架构决策：设置持久化选用 SQLite `settings` 表（而非新配置文件），写一条 ADR（见 `docs/adr/XXXX-settings-persistence.md`）。

## 4. 任务拆分

- [ ] 后端 settings 服务 + 单测（红→绿）：Get/GetAll/Set/SetMany 持久化与 upsert
- [ ] 后端 API handler + 路由 + 测试：GET/PUT /api/settings
- [ ] 前端 api/settings.ts（real+mock）
- [ ] 前端 SettingsPage + 测试（读、改、存）
- [ ] 路由 /settings + 导航项 + MSW handlers
- [ ] ADR：设置持久化
- [ ] 文档同步：PRD 状态、ARCHITECTURE、API、CHANGELOG

## 5. 验收标准

- 调 `PUT /api/settings` 写入键值后，`GET /api/settings` 能读回；重新打开数据库（新建 service 实例）后仍能读到（持久化）。
- 同一 key 重复 PUT 覆盖旧值（upsert），不报主键冲突。
- 设置页 `/settings` 加载后展示现有「每盘符回收站路径」「扫描周期」，修改并保存后再次进入仍为新值（前端经 mock/MSW 验证读写链路）。
- 后端 `go build ./...` 通过，受影响包 `go test` 全绿；前端 `npm run build` 与 `npm run test` 全绿。

## 6. 风险 / 待定

- 「每盘符回收站路径」结构：本期以单 key `recycle_bin_paths` 存 JSON 字符串（盘符→路径），消费方（FR-26）落地时再决定是否拆分；本期不为其预留额外表/字段（避免镀金）。
- 设置项校验：本期不做强类型校验，仅做空值/JSON 基本处理；强校验留到消费方需要时再加。
