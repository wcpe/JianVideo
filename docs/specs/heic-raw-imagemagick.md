# 功能规格：HEIC/RAW 图片显示（经外部 ImageMagick 转 JPEG）

> 状态：开发中　·　关联 PRD：FR-37　·　分支：feature/fr-37-heicraw

## 1. 背景与目标

iPhone 默认拍摄 HEIC，相机出片多为 RAW（cr2/nef/arw/dng/rw2 等）。浏览器无法直接渲染这些格式，
直接把原始字节返回给前端会显示为损坏图片。本功能在服务端把 HEIC/RAW 转成浏览器可显示的 JPEG，
覆盖原图预览与缩略图两条链路，属 P2 阶段（图片体验完善）。

转换走**外部 ImageMagick（`magick` 命令行）进程**，与项目既有 ffmpeg/ffprobe 外部进程范式一致（见 ADR-0030），
不引入 CGO 绑定（libheif/libraw）。

## 2. 需求（要什么）

- 识别常见 RAW 扩展名（cr2/nef/arw/dng/rw2/raf/orf/srw/pef），归入 image 类，使其能被扫描入库。
- HEIC/HEIF（已在内置后缀）与 RAW 经 `magick` 转 JPEG 后供浏览器显示。
- 转换结果缓存到数据目录专用子目录，按「源路径 + 源修改时间」hash 命名，二次命中不重复转换。
- `GET /api/library/media/:id/raw` 对 HEIC/RAW 返回转换后的 JPEG（而非原始字节）；普通 JPEG/PNG 等保持原样直出。
- 缩略图：HEIC/RAW 经 `magick` 直接生成缩略图（缩放 320px 宽 JPEG）。
- `magick` 路径解析镜像 `main.go` 的 `resolveTool`：环境变量 `JIANVIDEO_MAGICK_PATH` → 可执行文件同目录捆绑版 → PATH。
- 转换失败优雅降级（不 panic、不返回半成品），记中文分级日志。

- 范围内：HEIC/RAW → JPEG（原图预览 + 缩略图）+ 结果缓存 + magick 路径解析。
- 不做（范围外）：转 WebP（PRD 原行提及 JPEG/WebP，本期只做 JPEG，YAGNI）；RAW EXIF 旋转/白平衡精修；SMB 远程 HEIC/RAW（与现有 raw 端点一致，SMB 暂不支持）；前端改动（前端已用同一 `/raw` 与 `/thumbnail` 端点，URL 不变，无需改）。

## 3. 设计（怎么做）

涉及模块：

- `internal/library/service.go`：`builtInMediaExtensions` 增加 RAW 扩展名（image 类）。HEIC/HEIF 已存在。
- `internal/library/imageconvert.go`（新增）：
  - 纯函数 `isMagickConvertExt(ext)`：判断某扩展名是否需要经 magick 转换（heic/heif + RAW 集合）。
  - 纯函数 `buildMagickConvertArgs(src, dst)` / `buildMagickThumbnailArgs(src, dst, width)`：构建 magick 命令行参数。
  - 纯函数 `convertCacheKey(srcPath, modUnix)`：按「源路径 + 源修改时间」算缓存键（hash）。
  - `magick` 路径全局变量 + `SetMagickPath/GetMagickPath/IsMagickAvailable`（镜像 transcoder 的 ffmpeg 范式）。
  - `ConvertToJPEG(srcPath)`：解析缓存目录命中 → 命中直接返回；未命中调 magick 转换并写缓存；返回缓存 JPEG 路径。
  - `InitConvertCacheDir(baseDir)`：初始化缓存目录（数据目录下 `image_cache/`）。
- `internal/library/thumbnail.go`：`GenerateThumbnail` 的 switch 增加 HEIC/RAW 分支，走 magick 生成缩略图。
- `internal/api/handler.go`：`GetRawImage` 对需转换的扩展名调用 `ConvertToJPEG`，返回 JPEG；转换不可用/失败时降级（503/404 + 中文日志）。
- `main.go`：启动期 `library.SetMagickPath(resolveTool("JIANVIDEO_MAGICK_PATH", "magick"))` + `library.InitConvertCacheDir(数据目录)`，并打印可用性日志。

架构决策（外部 ImageMagick、不走 CGO）见 ADR-0030，此处不重复决策正文。

## 4. 任务拆分

- [x] 写规格（本文件）+ ADR + PRD FR-37 状态改「开发中」
- [ ] 纯函数单测（扩展名识别 / magick 参数构建 / 缓存键）先红
- [ ] service.go 增 RAW 扩展名
- [ ] imageconvert.go：magick 路径解析 + 转换 + 缓存
- [ ] thumbnail.go：HEIC/RAW 缩略图分支
- [ ] handler.go：GetRawImage 返回转换后 JPEG + 降级
- [ ] main.go：注入 magick 路径 + 初始化缓存目录
- [ ] 集成测试：有 magick 验证 HEIC→JPEG，无 magick 跳过
- [ ] 文档同步：PRD 状态、ARCHITECTURE、API、CHANGELOG、ADR 对 FR-22 打包影响

## 5. 验收标准

- 纯函数单测全绿：RAW 扩展名被识别为 image；magick 参数按预期构建；同源同修改时间缓存键稳定、不同源/不同时间键不同。
- 集成测试：在装有 `magick` 的环境用真实 HEIC 样本验证转换产物为合法 JPEG（magic bytes / 可被 ffprobe/标准库识别）；无 `magick` 时 `t.Skip`，不算失败。
- 缓存：同一文件二次请求不重复调用 magick（命中缓存返回既有 JPEG）。
- 降级：magick 不可用或转换失败时，端点返回明确错误而非 panic 或半成品字节，日志为中文分级。
- 真机维度（**待真机验**）：在装有带 HEIC + RAW delegate 的 ImageMagick 的机器上，前端能正常显示 HEIC/RAW 图片与其缩略图。本机未安装 magick，自动化只能覆盖纯函数与「无 magick 跳过」路径，真实转换需用户在目标机确认。

## 6. 风险 / 待定

- ImageMagick 默认编译可能不含 HEIC（libheif）或 RAW（libraw/dcraw）delegate，转换会失败；属部署约束，已在 ADR 的「后果」与 FR-22 打包说明中注明（随包/要求安装带对应 delegate 的版本）。
- 缓存键含源修改时间：源文件被替换（mtime 变化）后自动失效重转，避免显示旧内容。
