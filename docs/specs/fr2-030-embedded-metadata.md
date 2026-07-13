# 功能规格：文件自带元数据解析

> 状态：已交付@v0.23.0　·　关联 PRD：FR2-030　·　阶段：P2 `0.23.x`

## 1. 背景与目标

当前入库会提取部分视频 ffprobe 信息、图片 EXIF 和 `media_time`，但缺少完整 stream/audio/subtitle 轨、帧率、码率细节、IPTC/XMP、嵌入标签、来源记录、批量 backfill 和前端完整展示基础。P2 需要把原文件自带元数据解析为库内可信派生数据，默认只存库、不写回原文件。

目标：

- 建立结构化技术元数据模型，覆盖视频流、音频流、字幕流、图片 EXIF/IPTC/XMP 和嵌入标签。
- 扫描入库和文件修改后可刷新元数据。
- 支持批量 backfill 任务，进度可查、可重试。
- 元数据只写 SQLite，不修改原媒体文件。

前置依赖：FR2-007（Space/索引）、FR2-017（迁移框架）、FR2-037（任务队列）、FR2-040（审计核心）、FR2-027（扫描变更 hook）。

## 2. 需求（要什么）

- 视频：容器、编码、分辨率、时长、码率、帧率、色彩信息、视频流、音频流、字幕流。
- 图片：EXIF、IPTC、XMP、相机/镜头/曝光、GPS、拍摄时间。
- 标签：内嵌标题、作者、描述等按来源保存。
- 存储：保留原始摘要 JSON 与规范化字段，记录来源、解析时间、解析工具版本。
- 元数据刷新：文件大小/mtime 变化后标记过期并入队刷新。
- 批量 backfill：对已有媒体补齐新字段，走 FR2-037 任务队列。
- API 能返回技术元数据详情；前端完整展示由 FR2-032 承接，本期只提供基础面板或调试入口。
- 范围内：解析、存储、刷新、backfill、API、测试素材。
- 不做（范围外）：联网刮削、AI 内容理解、写回原文件、复杂编辑 UI。

## 3. 设计（怎么做）

Schema：

- `media_metadata`：`media_id`、`space_id`、`source`、`tool`、`tool_version`、`raw_json`、`normalized_json`、`parsed_at`、`stale`。
- 唯一键：`UNIQUE(space_id, media_id, source)`；刷新同一 source 时覆盖当前记录，历史追踪交给审计事件和任务日志，不在元数据表无限追加。
- 可将常用筛选字段同步到 `media_files` 或后续索引表：帧率、主音轨语言、分辨率、时长等。

解析：

- 视频使用 ffprobe JSON 输出，统一超时与错误摘要。
- 图片优先用现有纯 Go EXIF 能力，IPTC/XMP 如现有依赖不支持则先保存可解析子集，并在风险中标注。
- 所有外部工具路径从 FR2-024/022 配置获取，不硬编码。

任务：

- `metadata.parse`：单文件解析。
- `metadata.backfill`：批量生成子任务或 checkpoint 分批。

API：

- `GET /api/library/media/:id/metadata`
- `POST /api/library/media/:id/metadata/refresh`

## 4. 任务拆分

- [x] 定义元数据 schema 与规范化 JSON 结构。
- [x] 实现 ffprobe 解析和图片元数据解析扩展。
- [x] 扫描入库时幂等入队解析，文件变更时标记 stale 并刷新。
- [x] 实现单文件刷新与批量 backfill 任务。
- [x] 新增元数据 API 与基础前端展示入口。
- [x] 补固定测试素材清单：多音轨视频、多字幕视频、可变帧率视频、带 EXIF 图片、带 IPTC/XMP 图片。
- [x] 补单元测试：解析器、规范化、错误摘要、工具版本记录。
- [x] 补集成测试：真实 ffprobe 样本、文件修改后刷新、backfill checkpoint、源文件 hash/mtime 不变、Go 单二进制扫描与查询。
- [x] 补 SQLite 体积 benchmark：10,000 条、单条 raw JSON 4,135 bytes 时，数据库净增长 46,727,168 bytes，平均每条 4,672.72 bytes。
- [x] 文档同步：PRD 状态、ARCHITECTURE、API、CHANGELOG。

## 5. 验收标准

- 视频样本能解析帧率、码率、视频/音频/字幕流并持久化。
- 图片样本能解析 EXIF，IPTC/XMP 能解析的字段持久化；不支持字段有明确测试说明。
- 同一媒体同一来源刷新后只保留一条当前元数据记录，`UNIQUE(space_id, media_id, source)` 有集成测试覆盖。
- 文件变化后元数据标记 stale，并可通过任务刷新。
- 批量 backfill 可查看进度、失败重试，不阻塞浏览播放。
- 原媒体文件不被修改。
- `go test`、元数据集成测试、`pnpm run quality` 全绿。
- Go 单二进制 serve 后，测试素材入库并查询元数据详情通过。

## 6. 风险 / 待定

- 已确认：IPTC/XMP 首版不新增第三方解析依赖，优先复用现有工具与标准库可得信息。
- 已量化：`packages/benchmark/scripts/fr2-030-metadata-size-benchmark.go` 记录 raw/normalized JSON 与索引的 SQLite 体积；当前 10,000 条基准平均每条净增长约 4.56 KiB，报告写入 `.tmp/benchmark/fr2-030/`。
