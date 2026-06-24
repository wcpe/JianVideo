# 功能规格：直接播放（FR-102）

> 状态：开发中　·　关联 PRD：FR-102（扩 FR-16/FR-34）　·　分支：feature/fr-102-direct-play

## 1. 背景与目标

用户最在意的体验之一：点开视频应「直接播放」，去掉中间多余的一步点击。当前存在两处卡点：

1. 图片灯箱（`MediaDetailPanel`）遇视频时只显示「缩略图 + 打开播放按钮」，要再点一次才跳转播放页。
2. 播放页 / `VideoPlayer` 带声 autoplay 被浏览器自动播放策略拦截，落到 `autoPlayBlocked` 显示「点击播放」遮罩，又要再点一次。

目标：进入播放即出画面（静音自动播，浏览器允许），灯箱内视频直接内嵌播放，全程少点一次。属第九期（P9）。

## 2. 需求（要什么）

范围内：
- **VideoPlayer 静音自动播**：autoplay 时先 `video.muted=true` 再 `.play()`（浏览器允许 muted autoplay），进页即出画面；在播放器角落显著「🔇 点击取消静音」按钮，点击后恢复音量（`muted=false`）。原「点击播放」遮罩仅作极端兜底（muted 仍被拦时）。
- **灯箱内视频直接播**：`MediaDetailPanel` 遇视频时内嵌 `<VideoPlayer>` 直接播放（autoplay muted），去掉「缩略图 + 打开播放按钮」中间步骤；播放器尺寸适配预览区。

不做（范围外）：
- 不动后端、不引新依赖。
- 不改播放路径选择 / 协商逻辑（descriptor 解析、ABR、fMP4 回退保持不变）。
- 灯箱图片预览逻辑不动（仍走原 raw 预览 + 滚轮缩放）。
- 续播 seek（FR-44）、追播缓冲（FR-18）、字幕、编码协商（FR-52）等既有行为不变。

## 3. 设计（怎么做）

无新 ADR（不引入新技术 / 新模式，仅扩既有组件行为）。

### VideoPlayer
- 新增内部辅助：自动播放统一走「先 `v.muted=true`，再 `.play()`，失败 → `setAutoPlayBlocked(true)`」，替换原三处（mpegts `loadeddata`、hls `MANIFEST_PARSED`、mp4 直载）各自重复的 `play().catch(setAutoPlayBlocked)`，消除重复。
- 新增 `autoMuted` 状态：autoplay 触发的静音播放期间为 true；当 `isMuted && autoMuted` 时，视频区角落渲染「🔇 点击取消静音」按钮。
- 点击「取消静音」：`v.muted=false`、清 `autoMuted`，恢复有声。用户主动调音量 / 静音切换时也清 `autoMuted`（避免按钮误留）。
- `autoPlayBlocked` 遮罩逻辑保留作兜底：muted 仍被拦时才出现；muted 自动播成功后不应常驻挡画面。

### MediaDetailPanel
- 左侧预览：`isImage` 分支不变；非图片分支由「缩略图 + 打开播放按钮」改为内嵌 `<VideoPlayer autoPlay muted 协商>`，URL 走既有播放流地址（与播放页一致的 `/api/play/:id/stream` 形态），尺寸适配预览区。
- 移除 `IconPlayerPlay`「打开播放」按钮与 `navigate` 跳转（该组件内不再需要）。

## 4. 任务拆分
- [ ] 测试先行：VideoPlayer muted 自动播后开始播放且不显「点击播放」遮罩、取消静音按钮点击后 `muted=false`；MediaDetailPanel 遇视频渲染内嵌 VideoPlayer（无「打开播放」按钮）。
- [ ] 实现 VideoPlayer muted-autoplay + 取消静音 UI。
- [ ] 实现 MediaDetailPanel 内嵌 VideoPlayer。
- [ ] 文档同步：PRD 状态（开发中→已交付随发版）、CHANGELOG 未发布段末尾追加。

## 5. 验收标准
- `frontend/` 下 `npm run build`（tsc -b）与 `npm run test`（vitest）全绿，含本 FR 新增用例与既有 VideoPlayer/MediaDetailPanel/PlayPage 测试。
- 新增测试覆盖：muted 自动播开始播放（无「点击播放」遮罩）、取消静音后 `muted=false`、灯箱视频渲染内嵌播放器且无「打开播放」按钮。
- 真机（需用户确认，主控真机抽查）：浏览器点视频直接出画面播放（静音）、点取消静音有声、灯箱内视频直接播。

## 6. 风险 / 待定
- jsdom 无原生 `HTMLMediaElement.play`/`muted`，测试需对 video 元素打桩（参考既有 bufferwait/adaptive 测试的 mpegts/hls mock 模式）。
- 极端浏览器策略下 muted autoplay 仍可能被拦——保留「点击播放」兜底遮罩覆盖该情形。
