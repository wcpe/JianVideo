# ADR-0008：全格式兼容播放——ffprobe 探测 + 字幕解析方案

## 状态
已接受

## 背景
用户视频文件涵盖多种容器格式（MP4、MKV、AVI、MOV、WebM、RMVB、TS）和编码格式（H.264、H.265、AV1），浏览器仅原生支持 H.264+AAC 的 MP4。需要一套格式探测和字幕解析机制，为 FR-05 智能播放策略提供决策依据。

## 决策
采用 ffprobe（通过 exec.Command 调用）进行容器/编码探测，SRT/ASS 字幕解析为纯文本后转为 WebVTT 格式，SUP 图片字幕本期占位不实现。

## 理由
- ffprobe 是 FFmpeg 生态标准工具，输出 JSON 格式易于解析，无需引入 CGO 依赖
- exec.Command 调用方式简单可靠，ffprobe 进程生命周期短，不会长期占用资源
- WebVTT 是浏览器原生支持的字幕格式，前端无需额外解析库
- SRT/ASS 文本解析逻辑简单，纯 Go 实现无需外部依赖
- SUP 为图片字幕，OCR 依赖外部工具（tesseract），复杂度高，留待后续版本

## 后果
- 格式探测依赖系统安装 ffprobe（与 FFmpeg 一并分发）
- 探测结果缓存到 media_files 表，避免重复调用
- ffprobe 调用失败时降级为不兼容，触发转码
- SUP 字幕本期不可用

## 备选方案
- CGO 绑定 libavformat（ffmpeg-go）：直接调用 C API 探测，性能更好但引入 CGO 复杂度和编译依赖。当前项目架构已规划 CGO 转码管道，但格式探测用 ffprobe 更简单，两者可共存。
- 文件扩展名判断：不可靠，容器内编码可能不一致。
