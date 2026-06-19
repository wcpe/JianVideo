# 功能规格：自适应码率（ABR）

> 状态：开发中　·　关联 PRD：FR-07　·　分支：feature/abr

## 1. 背景与目标

当前系统仅支持单码率 HLS 输出，无法根据客户端网络带宽动态调整播放质量。本功能引入多码率阶梯转码，通过 FFmpeg 单进程多输出（filter_complex split）同时生成 1080p/720p/480p 三档切片，并由 HLS master playlist 调度，实现客户端自适应码率切换。属于第二期功能，无依赖。

## 2. 需求（要什么）

- 后端同时输出多码率 HLS 切片（1080p/720p/480p），码率阶梯根据源分辨率自动裁剪
- 生成 master.m3u8 索引文件，包含各码率流的 BANDWIDTH/RESOLUTION 信息
- 前端支持 ABR 模式：请求 master.m3u8 时自动使用 hls.js 播放，回退时用 mpegts.js
- hls.js 动态 import，不增加主 bundle 体积
- 切片文件名包含码率标识（如 `1080p_segment_000.ts`）

**范围内**：
- MultiPipeline 管理多个 Pipeline 实例
- FFmpeg filter_complex split 单进程多输出
- 码率阶梯自动裁剪（<720p 只输出 480p+720p，<1080p 不输出 1080p）
- 所有码率共享同一 GOP（-g 48 -keyint_min 48 -sc_threshold 0）
- master.m3u8 生成
- 前端 ABR 模式（hls.js + mpegts.js 回退）

**不做（范围外）**：
- 不实现自定义 ABR 算法（依赖 hls.js 内置切换逻辑）
- 不实现 DRM
- 不实现音频独立码率切换
- 不实现竖屏/超宽屏特殊处理

## 3. 设计（怎么做）

### 后端改动

1. **`internal/transcoder/multi_pipeline.go`**（新增）
   - `MultiPipeline` 结构体，管理多个 `Pipeline` 实例
   - `RunMulti` 方法：构建 filter_complex split 命令，单进程输出多路 HLS
   - 码率阶梯自动裁剪逻辑

2. **`internal/player/hls.go`**（改造）
   - `HLSSegmentWriter` 增加 `quality` 字段
   - 切片文件名改为 `{quality}_segment_xxx.ts`
   - m3u8 文件名改为 `{quality}.m3u8`

3. **`internal/player/hls_manager.go`**（改造）
   - `writers` 改为 `map[int64]map[string]*HLSSegmentWriter`
   - 新增 `GetMasterM3U8(mediaID)` 方法

4. **`internal/player/master.go`**（新增）
   - `GenerateMasterM3U8` 函数：生成 master.m3u8 内容

5. **`internal/api/router.go`**（改造）
   - 新增 `GET /api/play/hls/:id/master.m3u8` 路由

### 前端改动

1. **`frontend/package.json`**：添加 `hls.js` 依赖
2. **`frontend/src/components/VideoPlayer.tsx`**：新增 ABR 模式
3. **`frontend/src/pages/PlayPage.tsx`**：请求 URL 改为 master.m3u8

### 架构决策

详见 [ADR-0026](adr/0026-abr-adaptive-bitrate.md)。

## 4. 任务拆分

- [ ] 写 ADR-0026
- [ ] 更新 ARCHITECTURE.md（新增 ABR 模块描述）
- [ ] 更新 API.md（新增 master.m3u8 端点）
- [ ] 测试先行：MultiPipeline 单元测试
- [ ] 测试先行：master.m3u8 生成测试
- [ ] 实现 multi_pipeline.go
- [ ] 改造 hls.go（quality 维度）
- [ ] 改造 hls_manager.go（嵌套 map）
- [ ] 实现 master.go
- [ ] 改造 router.go（新增 master.m3u8 路由）
- [ ] 前端：安装 hls.js
- [ ] 前端：改造 VideoPlayer.tsx（ABR 模式）
- [ ] 前端：改造 PlayPage.tsx（URL 更新）
- [ ] 运行全部测试验证
- [ ] 更新 CHANGELOG

## 5. 验收标准

- 后端能同时生成 1080p/720p/480p 三档 HLS 切片（或根据源分辨率裁剪）
- master.m3u8 包含正确的 EXT-X-STREAM-INF 标签
- 切片文件名包含码率标识
- 前端 VideoPlayer 能根据 URL 自动选择 hls.js 或 mpegts.js
- 所有现有测试通过
- 新增测试覆盖 MultiPipeline 和 master.m3u8 生成逻辑

## 6. 风险 / 待定

- FFmpeg 集成测试需要 CGO 环境，重点确保编译通过和逻辑正确
- hls.js 动态 import 可能受 bundler 配置影响，需验证 Vite 兼容性
- 码率阶梯参数（1080p=5000k, 720p=2500k, 480p=1000k）为初始值，后续可能需要可配置化
