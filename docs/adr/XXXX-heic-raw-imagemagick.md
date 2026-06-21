# ADR-XXXX：HEIC/RAW 图片经外部 ImageMagick 转 JPEG 显示

> 占位编号 XXXX：真实编号由主控统一分配，落地时改本文件名与标题，禁止自行算 max+1。

## 状态
已接受

## 背景
FR-37 要求让浏览器显示 HEIC（iPhone 默认格式）与相机 RAW（cr2/nef/arw/dng/rw2 等）。
浏览器原生无法渲染这些格式，需在服务端转成 JPEG。实现路径有两类：

- **CGO 绑定库**（libheif 处理 HEIC、libraw 处理 RAW）：编入 Go 二进制，但需各平台 C 开发库与交叉编译适配，构建门槛高。
- **外部进程调用 ImageMagick（`magick`）**：与本项目既有的 ffmpeg/ffprobe 外部进程范式（见 ADR-0027）完全一致，零 CGO 改动。

PRD FR-37 行最初写「libheif（HEIC）+ libraw（RAW）」，是方案描述而非已定决策；用户最终决策走外部 ImageMagick。

## 决策
- **用外部 ImageMagick（`magick` 命令行）把 HEIC/RAW 转成 JPEG**，覆盖原图预览（`/raw` 端点）与缩略图两条链路；不引入 libheif/libraw 的 CGO 绑定。
- **路径解析顺序**镜像 `main.go` 的 `resolveTool`：环境变量 `JIANVIDEO_MAGICK_PATH` → 可执行文件同目录捆绑版 → PATH（`magick`/`magick.exe`）。
- **转换结果缓存**到数据目录下 `image_cache/`，按「源路径 + 源修改时间」hash 命名，二次命中不重转；源文件 mtime 变化即自然失效重转。
- magick 不可用或转换失败时**优雅降级**（端点返回明确错误、缩略图仅记日志），不 panic、不返回半成品字节，日志为中文分级。

## 理由
- 与 ffmpeg/ffprobe 同为外部进程依赖，复用「启动期路径解析 + 随包附带」的成熟范式，认知与运维成本最低。
- 避免 CGO 绑定带来的各平台 C 开发库依赖与交叉编译复杂度（与 ADR-0027 的原生构建取向不冲突，且不新增构建期 C 依赖）。
- ImageMagick 一个工具同时覆盖 HEIC 与多种 RAW（经 libheif / libraw / dcraw delegate），无需分别集成两套库。
- 结果缓存避免每次请求重复转换（RAW 解码开销大），命中后等价于直出 JPEG。

## 后果
- 目标机需安装 **带 HEIC（libheif）与 RAW（libraw/dcraw）delegate 的 ImageMagick**；缺 delegate 时对应格式转换失败并降级。
- 对 FR-22 跨平台打包：`magick` 作为外部工具，可随发布包附带（放入可执行文件同目录即被自动发现）或要求目标机自行安装；附带时须选用含 HEIC + RAW delegate 的构建，并遵守 ImageMagick 授权（Apache-2.0 风格，但其 HEIC delegate libheif 等含 LGPL/其他授权，分发时需一并核对）。打包脚本仅拷贝用户自备的 magick，不自动下载（与 ffmpeg 处理一致）。
- 数据目录新增 `image_cache/` 子目录，随源文件增多而增长；删除源文件不会自动清理缓存（缓存键含 mtime，不会误命中，磁盘占用为可接受代价）。
- 未安装 magick 的开发/验收机（如本机）无法做真实转换，相关验收为真机维度。

## 备选方案
- **CGO 绑定 libheif + libraw**：可编入单二进制、无外部进程依赖，但需各平台 C 开发库与交叉编译适配，构建面大；用户选择外部进程方案，不采用。
- **纯 Go 解码库**（如 RAW 的纯 Go 实现）：当前生态对 HEIC/众多 RAW 覆盖不全、维护度参差，不采用。
- **转 WebP 而非 JPEG**：体积更小，但 magick 的 WebP delegate 非默认必备，且 JPEG 兼容性最广；本期只做 JPEG（YAGNI），WebP 留待需要时再加。
