# 功能规格：感知哈希去重（FR-70）

> 状态：开发中　·　关联 PRD：FR-70　·　分支：feature/fr-70-dedup

## 1. 背景与目标

媒体库长期积累后常有重复或近似重复的照片/视频（同一张图多次导入、不同压缩比例的副本、视频与其截图等）。需要一种不依赖文件名、不依赖精确字节相等的「内容相似」检出手段，把疑似重复的媒体聚成组，供用户批量清理（软删进回收站，可还原）。

属于第七期（P7，影像智能与体验增强）。本期为「检出 + 批量清理候选」，不做自动删除、不做跨库特殊策略。

## 2. 需求（要什么）

- 为每个非软删媒体计算并持久化一个 64 位感知哈希（dHash，差分哈希）。
- 提供端点触发「为缺哈希的媒体计算 dHash」（已算过的跳过，幂等）。
- 提供端点返回「重复组」：按汉明距离阈值聚类，组内成员两两相似，排除已软删项，单成员组不返回。
- 前端新增「重复项」页与导航入口：展示每个重复组及成员缩略图，用户勾选组内多余项后批量软删（复用 FR-69 批量软删端点），删除后刷新。

- 范围内：
  - 纯 Go 自实现 dHash（图像缩放 + 灰度 + 相邻差分），不引入第三方图像哈希库。
  - 哈希来源统一为缩略图（320 宽 JPEG）；缩略图缺失则先按现有惰性生成逻辑生成再算。图片与视频都算（视频用其缩略图单帧）。
  - 汉明距离聚类（默认阈值 10），重复组查询。
  - 「重复项」页 + 批量软删（复用 `POST /api/library/media/batch-delete`）。

- 不做（范围外）：
  - 不引入图像哈希第三方库、不引入新依赖。
  - 不做自动删除/自动选择保留项（仅给候选，用户决定）。
  - 不为去重新建持久表（哈希落 `media_files` 加列即可，遵循 YAGNI 与真源不变量）。
  - 不做跨库去重的特殊处理（统一按全库非软删媒体参与聚类）。
  - 不做 pHash/aHash 等其它算法（dHash 足够，单一实现）。
  - 不做扫描进度 SSE（同步端点，见 §6）。

## 3. 设计（怎么做）

### 3.1 数据模型（media_files 加列）

`models.MediaFile` 新增字段 `DHash int64`（`json:"dhash,omitempty"`，默认 0 表示未计算）。`main.go` 已对 `MediaFile` 执行 `AutoMigrate`，启动时自动 `ADD COLUMN dhash`，无需改 `db.go`、无需手写迁移。

选 `int64` 而非字符串：dHash 恰为 64 位，`int64` 存储紧凑、汉明距离用 `bits.OnesCount64(uint64(a^b))` 直接算，无解析开销。

### 3.2 dHash 纯函数（library 模块）

新增 `internal/library/dhash.go`，纯函数、无副作用、可穷举测试：

- `computeDHash(img image.Image) uint64`：把图像缩放到 9×8（宽×高）灰度网格，逐行比较相邻像素亮度（左 > 右 置 1），得 8×8=64 位。缩放用最近邻采样（纯 Go，避免引库）。灰度用 `0.299R+0.587G+0.114B`。
- `hammingDistance(a, b uint64) int`：`bits.OnesCount64(a ^ b)`。
- `dHashFromThumbnail(path string) (uint64, error)`：读缩略图 JPEG（`image/jpeg`，Go 标准库已隐式注册）解码后调用 `computeDHash`。

聚类（重复组）也做成纯函数便于测试：`clusterByHamming(items []dhashItem, threshold int) [][]int64`，并查集/逐对比较把距离 ≤ 阈值的连成组，返回成员 id 组（仅 size ≥ 2 的组）。常量 `dedupHammingThreshold = 10`。

### 3.3 service 层（library.Service）

- `ComputeMissingDHashes() (int, error)`：查全部 `deleted_at IS NULL AND dhash = 0` 的媒体，有界并发（`min(4, NumCPU)`，复用缩略图并发上限语义）逐个：缩略图缺失则先 `GenerateThumbnail` 并等待（同步生成一次，不走异步队列）→ `dHashFromThumbnail` → `UPDATE dhash`。返回成功计算条数。单条失败仅记 WARN 日志、跳过，不中断整体。
- `FindDuplicateGroups(threshold int) ([][]models.MediaFile, error)`：查全部 `deleted_at IS NULL AND dhash != 0` 的媒体，调用 `clusterByHamming` 聚类，把每组 id 还原为 `MediaFile` 切片返回（组内按 id 升序，组间按首成员 id 升序，稳定可测）。

为同步等待缩略图，新增不阻塞队列的 `generateThumbnailSync(filePath string)`（library 内部），直接调用对应生成实现一次（图片/视频/magick 分派），与异步 `GenerateThumbnail` 区分。

### 3.4 端点（api 层，/api/library 组）

- `POST /api/library/duplicates/scan` → `{"computed": N}`：调用 `ComputeMissingDHashes`，返回本次新算条数。
- `GET /api/library/duplicates` → `{"groups": [[MediaFile,...], ...]}`：调用 `FindDuplicateGroups(dedupHammingThreshold)`。

### 3.5 前端（重复项页）

- `frontend/src/api/library.ts` 增 `scanDuplicates()` 与 `getDuplicateGroups()`（real + mock + 导出），类型 `DuplicateGroup = MediaFile[]`。
- 新增 `frontend/src/pages/DuplicatesPage.tsx`：进入先 `getDuplicateGroups`；提供「扫描重复项」按钮触发 `scanDuplicates` 后刷新。每组一行/一块展示成员缩略图（复用 `MediaThumbnail`），成员可勾选；「删除选中项」调用 `batchDeleteMediaFiles` 软删后刷新。空态提示。
- `App.tsx` 加路由 `/duplicates`；`AppLayout.tsx` navItems 加「重复项」项（图标 `IconCopy`）。

### 3.6 为何不写 ADR

未引入新技术栈、新中间件、新架构模式，也未推翻任何已接受 ADR：仅在既有 `media_files` 表加一列、用 Go 标准库实现纯函数、复用既有缩略图与批量软删能力。按 `doc-sync` 与 `decision-alignment`，此类改动 spec + ARCHITECTURE 数据模型同步即足，无需 ADR。

## 4. 任务拆分

- [x] `models.MediaFile` 加 `DHash int64` 字段（显式列名 `dhash`）
- [x] `internal/library/dhash.go`：computeDHash / hammingDistance / dHashFromThumbnail / clusterByHamming + 阈值常量
- [x] dhash 纯函数单测（相同图距离 0、近似图小、迥异图大；汉明距离；聚类分组/链式/有序）
- [x] service：ComputeMissingDHashes / FindDuplicateGroups + generateThumbnailSync（抽 resolveThumbnailJob 共用派发）
- [x] service 集成测试（扫描算哈希、幂等、聚类成组、排除软删）
- [x] 端点 scan + groups + 路由注册（含 DedupThreshold 暴露阈值常量，避免 api 层魔法值）
- [x] 端点测试
- [x] 前端 api（real/mock/导出）+ 类型 DuplicateGroup
- [x] 前端 DuplicatesPage + 路由 + 导航项（IconCopy）
- [x] 前端页测试（渲染组 + 扫描 + 选择 + 批量删除 + 空态/禁用）
- [x] 文档同步：PRD 状态（开发中）、ARCHITECTURE（media_files 加 dhash 列 + 去重机制说明）、API、CHANGELOG

## 5. 验收标准

- dHash 纯函数单测：同一图像 dHash 不变且与自身距离 0；同图轻微缩放/重编码后距离小（≤ 阈值）；内容迥异图像距离大（> 阈值）。
- 聚类纯函数：距离 ≤ 阈值的媒体聚为同组，单成员不成组，分组稳定有序。
- `POST /api/library/duplicates/scan` 为缺哈希媒体计算并持久化 dHash，二次调用 computed=0（幂等）。
- `GET /api/library/duplicates` 返回的组排除软删项；组内成员两两相似。
- 前端「重复项」页可展示重复组缩略图、勾选并批量删除（软删进回收站），删除后列表刷新。
- 完成判据三项全绿：后端 `go build ./...` + `go vet ./internal/...` + 受影响包 `go test`；前端 `npx tsc --noEmit` + `npx vitest run`；`npm run build` 过。
- 真机维度（待真机验）：用真实重复照片/视频验证检出准确度（dHash 对视频单帧代表性弱，可能漏检/误检，spec 已注明，属可接受局限）。

## 6. 风险 / 待定

- **视频代表性弱**：视频 dHash 取自第 2 秒单帧缩略图，不同视频若首帧相似可能误判、同视频不同剪辑可能漏判。属已知局限，本期接受。
- **同步扫描时长**：扫描对缺哈希媒体逐个解码缩略图，量大时端点响应较慢。当前选同步 + 有界并发（择简，不引入后台任务/SSE）。缩略图已惰性预生成时，dHash 计算（解码 320 宽 JPEG + 64 次比较）开销很小；仅当缩略图缺失需现场生成时较慢。后续若实测过慢再评估后台化（届时另立 FR/ADR）。
- **阈值取值**：默认汉明距离 10 为经验值（64 位中约 16% 不同仍算相似），定义为常量便于后续调整。
- **featureless 图哨兵歧义**：以 `dhash=0` 兼作「未计算」哨兵。featureless 纯色 / 无横向亮度变化的图像，其真实 dHash 恰为 0，与「未计算」无法区分，故每次扫描会被重算一次。重算开销极小（解码缩略图 + 64 次比较）且写回同值无害，不影响重复组结果；本期接受，不为此引入额外的「已计算」标志列（YAGNI）。
