# 功能规格：外挂字幕提取、转换和前端渲染

> 状态：草拟　·　关联 PRD：FR-04　·　分支：feature/subtitle-enhancement

## 1. 背景与目标

FR-04（全格式兼容播放）要求支持外挂字幕（SRT/ASS/SUP 等）。后端已具备 SRT→WebVTT、ASS→WebVTT 的转换能力（internal/transcoder/subtitle.go）和同目录字幕查找能力（FindSubtitleFiles），但前端尚无字幕渲染机制，后端也无对外暴露的字幕 API。

本功能补齐"后端字幕 API + 前端字幕渲染"的缺口，属于第二期"字幕支持增强"。

## 2. 需求（要什么）

- 后端新增 `GET /api/play/:id/subtitles`，返回媒体文件同目录下的外挂字幕轨道列表（文件名、格式、转换后的 WebVTT URL）
- 后端新增 `GET /api/play/:id/subtitles/:index`，返回指定字幕轨道的 WebVTT 内容
- 前端播放页加载字幕轨道列表
- 前端 VideoPlayer 组件内嵌字幕 overlay 层，根据当前播放时间显示对应字幕文本
- 控制栏添加字幕轨道选择菜单（Mantine Menu），支持"关闭字幕"选项
- 字幕样式：白字、黑色半透明背景、位于视频底部

范围内：
- SRT 和 ASS/SSA 字幕（已有转换器）
- SUP 占位返回空 WebVTT（已有占位逻辑）
- 前端纯 WebVTT 解析（不依赖浏览器原生 `<track>` 标签，因为 mpegts.js 方案不兼容）

不做（范围外）：
- SUP 图片字幕的 OCR 渲染
- 字幕上传/管理 UI
- 字幕同步/偏移调节
- 多音轨切换（与字幕无关）
- 字幕搜索/下载

## 3. 设计（怎么做）

### 3.1 后端

**新增 API：**

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | /api/play/:id/subtitles | 返回字幕轨道列表 JSON |
| GET | /api/play/:id/subtitles/:index | 返回 WebVTT 内容 |

**GetSubtitles 响应结构：**
```json
{
  "tracks": [
    {
      "index": 0,
      "file_name": "电影名.srt",
      "format": "srt",
      "url": "/api/play/1/subtitles/0"
    }
  ]
}
```

**GetSubtitles 流程：**
1. 解析 media_id，查询 media_files 表获取 file_path
2. 调用 `transcoder.FindSubtitleFiles(filePath)` 查找同目录字幕
3. 返回字幕轨道元信息列表

**GetSubtitleContent 流程：**
1. 解析 media_id 和 index
2. 查询 media_files 获取 file_path
3. 调用 `FindSubtitleFiles` 获取字幕文件列表
4. 校验 index 范围
5. 调用 `ConvertSubtitleFile` 转换为 WebVTT
6. 返回 `text/vtt` 内容

**路由注册：**
在 `router.go` 的播放路由组中添加两个新端点，走 `PlayHandler` 的方法。

**字幕转换缓存：**
每次请求实时转换（字幕文件通常很小，转换毫秒级），不做磁盘缓存。

### 3.2 前端

**WebVTT 解析器（轻量内联）：**
- 约 30 行，解析 WebVTT 文本为 `{start, end, text}` 数组
- 不依赖第三方库

**SubtitleOverlay 组件：**
- 绝对定位覆盖在 video 元素上方
- 接收 `currentTime` 和 `entries` 数组
- 根据当前时间匹配显示的字幕文本
- CSS：白色文字 + `rgba(0,0,0,0.6)` 背景 + `bottom: 10%` 定位

**VideoPlayer 改动：**
- 新增 props：`subtitleEntries`（可选）、`subtitleVisible`（可选，默认 false）
- 在 video 元素外层增加 `position: relative` 容器
- 条件渲染 SubtitleOverlay

**PlayPage 改动：**
- 新增 `loadSubtitles(mediaId)` 方法调用 `/api/play/:id/subtitles`
- 选中的字幕轨道调用 `/api/play/:id/subtitles/:index` 获取 VTT 内容
- 解析后传递给 VideoPlayer
- 控制栏增加字幕选择按钮（Mantine Menu + Menu.Item）

### 3.3 数据流

```
PlayPage
  ├─ loadSubtitles(mediaId) → GET /api/play/:id/subtitles → tracks[]
  ├─ 用户选择轨道 → GET /api/play/:id/subtitles/:index → VTT text
  ├─ parseWebVTT(vttText) → SubtitleEntry[]
  └─ <VideoPlayer subtitleEntries={entries} subtitleVisible={true} />
        └─ <SubtitleOverlay currentTime={currentTime} entries={entries} />
```

## 4. 任务拆分

- [ ] 后端：play_handler.go 新增 GetSubtracks + GetSubtitleContent 方法
- [ ] 后端：router.go 注册字幕路由
- [ ] 后端：编写字幕 API 测试
- [ ] 前端：实现 parseWebVTT 工具函数
- [ ] 前端：VideoPlayer.tsx 添加字幕 overlay 支持
- [ ] 前端：PlayPage.tsx 加载字幕轨道 + 选择菜单
- [ ] 文档同步：PRD 状态更新、ARCHITECTURE 补充字幕模块、API 文档更新、CHANGELOG 追加
- [ ] 运行测试套件验证

## 5. 验收标准

- **AC-1**：后端 `GET /api/play/:id/subtracks` 返回字幕轨道列表，包含正确的文件名和格式
- **AC-2**：后端 `GET /api/play/:id/subtitles/:index` 返回有效的 WebVTT 内容
- **AC-3**：无字幕时返回空列表（不报错）
- **AC-4**：前端播放页加载字幕轨道列表，用户可切换轨道
- **AC-5**：字幕在视频底部正确显示，与播放时间同步
- **AC-6**："关闭字幕"选项可隐藏字幕 overlay
- **AC-7**：后端单元测试全部通过
- **AC-8**：前端 VideoPlayer 测试全部通过

## 6. 风险 / 待定

- SUP 字幕目前返回空 WebVTT，前端选择 SUP 轨道后不会有任何显示，但不会报错——这是预期行为
- 字幕转换每次请求都实时执行，如果字幕文件很大（罕见）可能有延迟，当前可接受
- 前端 WebVTT 解析器仅处理基本格式，不支持 WebVTT 的样式/位置/对齐元数据（本期不需要）
