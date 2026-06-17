# 功能规格：全格式兼容播放

> 状态：开发中　·　关联 PRD：FR-04　·　分支：feature/full-codec-support

## 1. 背景与目标

用户视频文件容器格式各异（MP4、MKV、AVI、MOV、WebM、RMVB、TS 等），编码格式也包括 H.264、H.265/HEVC、AV1 等。浏览器仅原生支持 H.264+AAC 的 MP4，其余格式需要后端转码。

本功能通过 ffprobe 探测视频流信息，判断浏览器兼容性，并解析外挂字幕（SRT/ASS/SUP）为 WebVTT 供前端渲染。属于第一期 MVP。

## 2. 需求（要什么）

- 通过 ffprobe 获取视频文件的容器格式、视频编码、音频编码、分辨率、时长、码率
- 判断浏览器兼容性：H.264+AAC 的 MP4 → 直出；其余 → 需转码
- 外挂字幕解析：SRT → WebVTT、ASS/SSA → WebVTT、SUP → OCR 为文本再转 WebVTT（SUP 为图片字幕，本期仅做占位，返回空轨道列表并记录日志）
- API 端点：
  - `GET /api/play/:id` → 返回播放信息（直出 URL 或转码 URL + 格式信息）
  - `GET /api/play/:id/subtitles` → 返回字幕轨道列表（WebVTT 格式）
- 格式探测结果缓存到 media_files 表（video_codec、audio_codec、duration、width、height、bitrate）

范围内：
- ffprobe 探测与格式判断
- SRT/ASS 字幕解析转换
- 播放信息 API
- 字幕轨道 API

不做（范围外，属于 FR-05）：
- 直出/转码决策逻辑（FR-05 负责）
- 实际转码执行
- SUP 图片字幕 OCR（本期返回空占位）

## 3. 设计（怎么做）

### 3.1 模块结构

```
internal/transcoder/
├── codec.go      # ffprobe 探测 + 格式兼容性判断
└── subtitle.go   # 外挂字幕解析（SRT/ASS → WebVTT）
```

### 3.2 ffprobe 探测流程

1. 调用 `ffprobe -v quiet -print_format json -show_format -show_streams <path>`
2. 解析 JSON 输出，提取：
   - `format.format_name` → 容器格式
   - 视频流 `codec_name` → 视频编码
   - 音频流 `codec_name` → 音频编码
   - 视频流 `width`/`height` → 分辨率
   - `format.duration` → 时长
   - `format.bit_rate` → 码率

### 3.3 浏览器兼容性判断规则

```
容器 = mp4 AND 视频编码 = h264 AND 音频编码 = aac → 直出
其他情况 → 需转码
```

### 3.4 字幕解析流程

1. 扫描视频同目录下的字幕文件（同名不同扩展名）
2. SRT → 解析时间轴和文本，转换为 WebVTT 格式
3. ASS/SSA → 解析事件部分，转换为 WebVTT 格式
4. SUP → 图片字幕，本期返回空占位

### 3.5 API 设计

**GET /api/play/:id**

响应体：
```json
{
  "id": 1,
  "file_path": "/videos/movie.mkv",
  "format": "mkv",
  "video_codec": "hevc",
  "audio_codec": "aac",
  "width": 1920,
  "height": 1080,
  "duration": 7200.0,
  "bitrate": 8000000,
  "compatible": false,
  "play_url": "/api/play/1/stream"
}
```

- `compatible` = true 时 `play_url` 直出原文件
- `compatible` = false 时 `play_url` 为转码流地址（FR-05 实现）

**GET /api/play/:id/subtitles**

响应体：
```json
{
  "tracks": [
    {
      "index": 0,
      "language": "zh",
      "label": "中文",
      "format": "webvtt",
      "url": "/api/play/1/subtitles/0"
    }
  ]
}
```

## 4. 任务拆分

- [x] 编写 spec 文档 `docs/specs/full-codec-support.md`
- [ ] 编写 ADR `docs/adr/0008-full-codec-support.md`
- [ ] 更新 PRD FR-04 状态 → 开发中
- [ ] 实现 `internal/transcoder/codec.go`：ffprobe 探测 + 兼容性判断
- [ ] 实现 `internal/transcoder/subtitle.go`：SRT/ASS → WebVTT
- [ ] 扩展 `internal/api/router.go` 注册播放 API
- [ ] 扩展 `internal/api/handler.go` 添加播放信息 + 字幕 API
- [ ] 编写单元测试并确保全绿
- [ ] 文档同步：ARCHITECTURE、CHANGELOG

## 5. 验收标准

- ffprobe 能正确探测 MP4/MKV/AVI/MOV/WebM/TS 容器格式
- H.264+AAC 的 MP4 判定为兼容（`compatible: true`）
- H.265/MKV、AV1/WebM 等非 H.264+AAC 组合判定为不兼容
- SRT 字幕正确转换为 WebVTT 格式
- ASS 字幕正确转换为 WebVTT 格式
- SUP 字幕返回空轨道列表（占位）
- `GET /api/play/:id` 返回完整播放信息
- `GET /api/play/:id/subtitles` 返回字幕轨道列表
- 所有单元测试通过（红→绿）

## 6. 风险 / 待定

- ffprobe 未安装时降级：codec 探测返回未知编码，compatible 默认为 false
- SUP 图片字幕 OCR 依赖 tesseract 等外部工具，本期不做
- 大文件 ffprobe 调用可能较慢，考虑异步探测+缓存
