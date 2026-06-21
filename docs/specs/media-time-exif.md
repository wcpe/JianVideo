# 功能规格：媒体时间与 EXIF 提取

> 状态：开发中　·　关联 PRD：FR-31　·　分支：feature/fr-31-exif

## 1. 背景与目标

时间轴视图（FR-B）当前按入库时间 `added_at` 组织媒体，与照片真实拍摄时间脱节——同一天拍摄、不同时间扫描的照片会被打散。本特性在扫描入库时为每个媒体解析出「媒体时间」`media_time` 并记录其来源，时间轴改按媒体时间排序，贴近相册体验。同时提取图片完整 EXIF（相机/镜头/光圈/快门/ISO/GPS），存入字段供 EXIF 详情展示（FR-38）与照片地图（FR-39）消费。属于 P2 媒体管理增强。

承载字段（`MediaTime/MediaTimeSource/Camera/Lens/Aperture/Shutter/ISO/GPSLat/GPSLon`）已由 foundation 提交建好，本特性只补提取与排序逻辑。

## 2. 需求（要什么）

- 图片 EXIF 提取：扫描入库时用 imagemeta 读取拍摄时间与相机/镜头/光圈/快门/ISO/GPS。
- 视频媒体时间：用 ffprobe 读 `format.tags.creation_time`。
- 媒体时间多层降级：拍摄时间（exif）→ 文件名日期解析（filename）→ 文件创建时间（created）→ 文件修改时间（modified），并记录命中来源 `media_time_source`。
- 文件名日期解析：识别常见相机/截图/导出命名，如 `IMG_20230101_120000`、`2023-01-01`、`Screenshot_2023...`、`VID_20230101` 等。
- 时间轴排序：列表排序改用 `media_time`（缺失回退 `added_at`）；`ListMediaFilesFiltered` 新增 `media_time` 排序选项。
- 范围内：本地图片与视频；EXIF 提取失败或非图片不报错，按降级链兜底。
- 不做（范围外）：EXIF 详情展示 UI（FR-38）、照片地图（FR-39）、HEIC/RAW 转码显示（FR-37）、时间轴前端缩放拖动（FR-32）、对已入库存量记录的回填迁移（仅新入库与重扫未入库项生效）。

## 3. 设计（怎么做）

复用既有分层：扫描/入库在 `library.Service`，EXIF 由新增依赖 `imagemeta` 提取，视频时间复用 `transcoder.ProbeMetadata` 同款 ffprobe 调用。无新架构决策，不写 ADR（新增纯 Go 依赖 imagemeta 经用户批准，记入 ARCHITECTURE 依赖说明）。

### 数据模型（foundation 已建，不改结构）

- `media_files.media_time`（*time.Time，带索引）：多层降级解析出的媒体时间，供时间轴排序。
- `media_files.media_time_source`（string）：`exif` / `filename` / `created` / `modified`。
- `media_files.camera` / `lens` / `aperture` / `shutter`（string）、`iso`（int）、`gps_lat` / `gps_lon`（float64）：EXIF 明细。

### 纯函数（`internal/library/metadata.go`，便于穷举单测）

- `ParseFilenameDate(name string) (time.Time, bool)`：从文件名解析日期时间，命中返回 `true`。按一组正则有序匹配常见模式，无法解析返回 `false`。
- `ResolveMediaTime(exifTime, filenameTime *time.Time, createdAt, modifiedAt time.Time) (time.Time, string)`：按 exif → filename → created → modified 优先级选第一个有效值，返回时间与来源标识。纯函数无 IO。

### EXIF 提取（`internal/library/metadata.go`）

- `ExtractImageEXIF(path string) *ImageEXIF`：打开文件用 `imagemeta.Decode` 解析，映射出拍摄时间（`SelectedDate`）、相机（`CameraMake`+`Model`）、镜头（`LensModel`）、光圈（`FNumber`）、快门（`ExposureTime`）、ISO（`ISOSpeedRatings`）、GPS（`Latitude`/`Longitude`）。解析失败或无 EXIF 返回 `nil`（不报错，交降级链兜底）。
- 视频时间复用 `transcoder.ProbeMetadata` 输出新增的 `CreationTime` 字段（ffprobe `-show_format` 已含 tags，扩展解析 `format.tags.creation_time`）。

### 接入入库（`internal/library/service.go` 的 `CreateMediaFile`）

`CreateMediaFile` 是扫描与 watcher 共用的唯一入库点，在此统一富化媒体元数据：

1. `os.Stat` 取文件 created/modified（Windows 用 `ChangeTime`/`ModTime`，跨平台取 `ModTime` 与 birthtime 兜底）。
2. 图片走 `ExtractImageEXIF`，视频走 ffprobe `creation_time`，得到 `exifTime`。
3. `ParseFilenameDate` 解析文件名时间。
4. `ResolveMediaTime` 选出 `media_time` 与 `media_time_source`，连同 EXIF 明细写入记录。
- 远程（SMB）路径不做本地文件 stat 的 EXIF 提取，按 modified 兜底（范围外不展开）。

### 时间轴排序（`internal/library/favorites_tags.go` 的 `ListMediaFilesFiltered`）

- `Sort` 新增 `media_time`（降序）与 `media_time_asc`（升序）：`ORDER BY COALESCE(media_time, added_at) DESC/ASC`，缺失媒体时间回退入库时间。

## 4. 任务拆分

- [ ] 纯函数单测（文件名日期解析、降级选择）先行（红）
- [ ] 实现纯函数 ParseFilenameDate / ResolveMediaTime
- [ ] go get imagemeta；实现 ExtractImageEXIF + ffprobe creation_time；接入 CreateMediaFile
- [ ] 时间轴 media_time 排序 + 排序单测
- [ ] 文档同步：PRD 状态、ARCHITECTURE、CHANGELOG

## 5. 验收标准

- 有 EXIF 的照片：`media_time` 等于 EXIF 拍摄时间，`media_time_source = exif`；相机/镜头/光圈/快门/ISO/GPS 正确写入（单测覆盖纯函数与 EXIF 映射；真实样片解析为手动/集成验收）。
- 逐层降级正确：无 EXIF 但文件名含日期 → `filename`；都没有 → `created`；再无 → `modified`（`ResolveMediaTime` 表驱动单测覆盖）。
- 文件名日期解析覆盖 `IMG_20230101_120000` / `2023-01-01` / `Screenshot_2023...` 等常见模式（`ParseFilenameDate` 表驱动单测覆盖）。
- 时间轴：`Sort=media_time` 时按 `COALESCE(media_time, added_at)` 排序，缺失媒体时间项回退入库时间参与排序（service 单测覆盖）。
- 后端 `go test ./internal/library/...`（及受影响的 `./internal/transcoder/...`）全绿；`go build ./...` 通过。
- 真机维度（真实带 EXIF 照片端到端入库校验）：本机如无样片，标「待真机验」，由用户确认通过，不以单测替代。

## 6. 风险 / 待定

- imagemeta 为新增纯 Go 依赖（用户已批准），仅在 library 包引用，不破坏模块依赖方向。
- 文件创建时间跨平台语义不一：Windows 有 birthtime，Linux 多数 `Stat` 无 birthtime，取不到时该层降级跳过，落到 modified；不引第三方 birthtime 库，保持简单。
- 不回填存量记录：仅对新入库 / 重扫尚未入库的文件生效；存量回填留待后续显式迁移任务，避免本特性扩面。
