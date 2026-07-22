# 功能规格：后端分层硬化（保留 GORM）

> 状态：首切 + Tx 抽象 + ListSpaces DBProvider + tags/albums + covers/view + watch + inference + health + media 写路径全切片 + library path/MediaExtension + Space + 扫描对账 + summary + next_episode + dedup + file_hash + media_type_rules + bookmark + watch_state_source + recycle_cleanup repository 已交付；library 生产路径无 `s.db`　·　关联 PRD：FR2-070　·　阶段：对齐-B

## 1. 背景与目标

落地 [ADR-0058](../adr/0058-data-layer-abstraction.md)：api 禁直连 db，repository 接口 + GORM 实现；**不**引入 sqlx。

## 2. 需求（要什么）

- 范围内：目标高耦合模块抽 repository；invariants 硬规则；单测；api 入队/跨域查询签名去 `*gorm.DB` 裸传。
- 不做：换 ORM；改对外 API 语义；全量重写所有包。

## 3. 设计（怎么做）

- 依赖：`api → service/domain → repository → gorm`。
- 首切模块：**settings**（运行期键值设置真源）。
- library 已有 MediaQuery / chapter / bookmark / metadata repository，本轮补充推断回填 space 查询下沉。
- **续1～7**：`tasks.Tx`、`DBProvider`、tag/album、cover/view、watch、inference、health repository。
- **续8～14**：`MediaQueryRepository` 最小写/软删/BatchReassign/Rename·Move/元数据回写/扫描 missing·硬删/批量 index 路径查询。
- **续15～16**：`libraryPathRepository`（目录 CRUD + MediaExtension List/Add/Delete）。
- **续17**：`spaceRepository`（Exists/GetByID/HasTable）；`ListActiveForReconcile`；`service.go` 生产路径无 `s.db`。
- **续18**：`pathRepo.CountEnabledBySpace`；`mediaRepo.ListSummaryFormatRows`；summary 规则口径改委托。
- **续19**：`inferenceRepository.FindNextEpisode`；`findNextInference` 改委托。
- **续20**：`MediaQueryRepository` 扩展 `ListMissingDHash` / `SetDHashIfZero` / `ListWithDHash`；`ComputeMissingDHashesInSpace` / `FindDuplicateGroupsInSpace` 改委托；缩略图生成与汉明聚类仍在 service。
- **续21**：`MediaQueryRepository` 扩展 `CountContentHashBackfill` / `ListContentHashBackfillBatch` / `UpdateContentHash` / `ListExactDuplicateMedia` / `RefreshContentHashGroups`；file_hash 回填/精确重复组/快照重建改委托；哈希计算与任务编排仍在 service。
- **续22**：新建 `mediaTypeRuleRepository`（HasTable/ListBySpace/RunInTx/GetByIDTx/FindByKeyTx/CreateTx/UpdateFieldsTx/ReloadTx/DeleteTx）；`pathRepo.HasMediaExtensionTable`；media_type_rules 与 MediaExtension 兼容路径改委托；审计与规则合并仍在 service。
- **续23**：`bookmarkRepository` 扩展 `RunInTx` / `Get`；Create/Update/Delete CAS 事务入口改委托；busy 后冲突探测用非事务 `Get`；审计仍在 service。
- **续24**：`watchStateRepository` 非事务 `GetWatchableMedia`/`LoadState` 与事务 `*Tx` 分离；`GetWatchStateInSpace` 不再传 `s.db`。
- **续25**：`MediaQueryRepository` 扩展 `UpdateFileStateCAS`/`UpdateFileStateCASTx`/`GetByIDAndDeletedAtTx`；recycle claim/delete/restore/release 经 `mediaRepo.RunInTx`；library 生产路径无 `s.db`（测试文件可直连 db）。

## 4. 任务拆分

- [x] 选定首切模块与接口（settings `Repository` / `TxRepository`）
- [x] 迁移 service + settings handler 依赖（读写经 repo）
- [x] 测试与 invariants（settings/library/api 包测绿；architecture-invariants 补 repository 规则）
- [x] tasks `Tx` / `EnqueueTx`；api `enqueueInferenceTask` 去 `*gorm.DB` 签名
- [x] library `DBProvider`；`ListSpaces…` 与 settings 钩子无 `tx.DB()` 裸传
- [x] library 域 tag/album/cover/view/watch/inference/health repository
- [x] MediaQueryRepository 写路径全切片（Create～扫描 index/对账）
- [x] libraryPathRepository + MediaExtension
- [x] spaceRepository + ListActiveForReconcile
- [x] summary 聚合：CountEnabledBySpace + ListSummaryFormatRows
- [x] next_episode：`FindNextEpisode` 经 inferenceRepo
- [x] dedup：`ListMissingDHash` / `SetDHashIfZero` / `ListWithDHash` 经 mediaRepo
- [x] file_hash：`CountContentHashBackfill` / `ListContentHashBackfillBatch` / `UpdateContentHash` / `ListExactDuplicateMedia` / `RefreshContentHashGroups` 经 mediaRepo
- [x] media_type_rules：`mediaTypeRuleRepository` + pathRepo `HasMediaExtensionTable`；读写/事务经 repo
- [x] bookmark：`RunInTx` / `Get`；Create/Update/Delete CAS 事务入口经 bookmarkRepo
- [x] watch_state_source：非事务 `GetWatchableMedia`/`LoadState`；事务路径 `GetWatchableMediaTx`/`LoadStateTx`
- [x] recycle_cleanup：`RunInTx` + `UpdateFileStateCAS`/`GetByIDAndDeletedAtTx`；claim/delete/restore/release 经 mediaRepo

## 5. 验收标准

- [x] 首切包：api 生产代码对 settings 表无直接 GORM 业务读写；推断缺失 space 查询在 library。
- [x] `settings` repository 接口测试 + settings/library/api 回归绿。
- [x] 不引入 sqlx；GORM 仍为唯一 ORM。
- [x] api 生产入队路径签名为 `tasksvc.Tx`，不再出现 `enqueue*(…, *gorm.DB, …)`。
- [x] api 生产路径对 `ListSpaces…` 不再写 `tx.DB()`；经 `DBProvider` / `TxRepository` 传入。
- [x] library 已下沉域相关单测绿（含 tags/albums/covers/view/watch/inference/health/media 写/软删/BatchReassign/Rename·Move/Writeback/扫描/path/MediaExtension/对账/summary/next_episode/dedup/file_hash/media_type_rules/bookmark/watch/recycle）。
- [x] library 生产路径无 `s.db` 裸拼；业务读写经各域 repo（测试文件可直连 db 造数）。

## 6. 交付摘要

| 落点 | 说明 |
|---|---|
| `internal/settings/repository.go` | `Repository` / `TxRepository` + GORM 实现 |
| `internal/tasks/tx.go` | `Tx` + `AsTx`；`EnqueueTx(ctx, Tx, …)` |
| `internal/library/*_repository.go` | tag/album/cover/view/watch/inference/health/media/path/space |
| `internal/library/summary.go` | 规则口径经 pathRepo/mediaRepo |
| `internal/library/next_episode.go` | 下一集定位经 `inferenceRepo.FindNextEpisode` |
| `internal/library/dedup.go` | dHash 补算/聚类查询经 mediaRepo |
| `internal/library/file_hash.go` | 内容哈希回填/精确重复组/快照经 mediaRepo；计算与任务编排仍在 service |
| `internal/library/media_type_rule_repository.go` | 媒体类型规则 CRUD/事务；`pathRepo.HasMediaExtensionTable` 旧库兼容 |
| `internal/library/media_type_rules.go` | 规则合并/校验/审计；无 `s.db` |
| `internal/library/bookmark_repository.go` | `RunInTx`/`Get` + CAS Tx；事务入口下沉 |
| `internal/library/bookmark_service.go` | 校验/CAS 编排/审计；无 `s.db` |
| `internal/library/watch_state_repository.go` | 非事务读 + `*Tx`；Persist/Project 仍走 tx |
| `internal/library/watch_state_source.go` | Get/Apply/列表经 watchRepo；无 `s.db` |
| `internal/library/recycle_cleanup.go` | claim/delete/restore/release 经 mediaRepo；无 `s.db` |
| `internal/library/service.go` | 业务规则 + 审计 + 委托 repo；生产路径无 `s.db` |
| `internal/api/*` | 入队/ListSpaces 经 Tx/DBProvider；错误映射可保留 |

## 7. 后续（非本 FR 阻塞）

- library 生产路径 `s.db` 已清空；其它包（playback/subtitle 等）若仍有直连可按同模式续抽。
- api 层仍可对 `gorm.ErrRecordNotFound` 做错误映射；测试文件允许直连 db 造数。

## 8. 风险 / 待定

- 与 067 包路径耦合：已随 067 完成，本 FR 在 `apps/server` 落地。
