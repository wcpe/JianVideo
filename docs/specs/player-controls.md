# 功能规格：播放器内核与控件增强（FR-104）

> 状态：开发中　·　关联 PRD：FR-104（扩 FR-16）　·　分支：feature/fr-104-player-controls

## 1. 背景与目标
播放器（`VideoPlayer.tsx`）此前仅有基础的播放/暂停、进度条、音量与字幕，缺少现代播放器的常用控件与交互。本 FR 在 FR-102（静音自动播）、FR-103（fill 全屏沉浸布局）之上，补齐内核与控件能力，提升观看体验。属第九期（P9）界面体验增强。

## 2. 需求（要什么）
范围内：
- 全屏：控件栏全屏按钮（`requestFullscreen` / `exitFullscreen`），双击视频区切换全屏。
- 画中画（PiP）：`requestPictureInPicture` / `exitPictureInPicture` 按钮，浏览器不支持时隐藏该按钮。
- 键盘快捷键（仅播放器/播放页聚焦、且焦点不在输入框时生效）：空格 播放/暂停、←/→ 退/进 10s、↑/↓ 音量、F 全屏、M 静音、数字 0-9 跳转百分比。
- 倍速：0.5×–2× 菜单，改 `playbackRate`。
- ±10s 快退/快进按钮。
- 进度条 hover 时间预览气泡 + 加大热区 + 显示 thumb。
- 控件自动隐藏：播放中鼠标静止数秒后控件淡出，鼠标移动/暂停时重新显示。
- 记忆音量/静音：音量与静音偏好持久化 localStorage，跨视频/会话保留；与 FR-102 协调——首次仍默认静音自动播，用户取消静音后记忆其音量。
- 字幕样式：字幕字号档可调（小/中/大）。
- 修偏：进度条/音量条颜色由蓝改品牌紫（FR-93，Mantine 命名色 `purple`）；音量图标在亮色主题下 `color="white"` 不可见 → 改语义灰。

不做（范围外）：
- 不动后端、不引新依赖（全屏/PiP 用浏览器原生 API）。
- 不改续播/追播/编码协商/静音自动播/fill 等既有行为。
- 不做字幕位置/背景的完整自定义面板（仅给字号档，YAGNI）。

## 3. 设计（怎么做）
全部改动集中在 `frontend/src/components/VideoPlayer.tsx`，复用既有 video ref 与状态结构：
- 全屏：对最外层容器调原生 Fullscreen API；监听 `fullscreenchange` 同步按钮态。
- PiP：用 `document.pictureInPictureEnabled` 探测能力，调 video 元素的 `requestPictureInPicture`。
- 快捷键：在容器上挂 `onKeyDown`（容器 `tabIndex=0`），命中前判断 `target` 是否为输入控件以避让。
- 倍速：状态 `rate`，菜单选择后置 `video.playbackRate`。
- ±10s：直接增减 `video.currentTime`（夹取 [0, duration]）。
- 进度条 hover 预览：进度条容器 `onMouseMove` 算出 hover 位置百分比 → 时间气泡。
- 控件自动隐藏：`controlsVisible` 状态 + 鼠标移动重置定时器，播放中静止数秒淡出。
- 记忆音量：纯函数 `loadVolumePref` / `saveVolumePref` 读写 localStorage（独立可测）；用户主动调音量/切静音时保存，初始化时读取（不破坏 FR-102 首次静音自动播）。
- 字幕字号：状态 `subtitleScale`，映射到字幕文本 `fontSize`。
- 颜色：进度/缓冲/音量 Slider 由 `color="blue"/"cyan"` 改 `color="purple"`；音量图标 `color="white"` 改 `color="gray"`。

无新 ADR（不触架构决策、不引依赖）。

## 4. 任务拆分
- [x] 测试先行：倍速改 playbackRate、±10s 改 currentTime、快捷键（空格/M/方向键）、记忆音量持久化纯函数、进度条品牌紫。
- [x] 实现控件增强（全屏/PiP/快捷键/倍速/±10s/hover 预览/自动隐藏/记忆音量/字幕字号/配色修偏）。
- [x] 文档同步：PRD 状态、CHANGELOG 未发布段。

## 5. 验收标准
- `npm run build`（tsc -b）与 `npm run test`（vitest）全绿，既有 VideoPlayer 测试（含 directplay/fill/adaptive/watchstate/bufferwait）不回归。
- 新增单测覆盖：倍速、±10s、空格/M/方向键快捷键、记忆音量纯函数、进度条品牌紫颜色。
- 待真机验（需用户确认）：全屏 / 画中画 / 键盘快捷键整体 / 倍速实际生效 / 进度条 hover 时间气泡 / 控件静止自动隐藏 / 音量记忆跨会话——浏览器原生 API 与定时器/指针交互无法在 jsdom 充分覆盖，单测不替代真机。

## 6. 风险 / 待定
- 全屏/PiP 为浏览器原生 API，jsdom 不实现，相关分支仅能做存在性/降级单测，主路径靠真机。
- 控件自动隐藏的定时器交互在测试中以行为近似覆盖，淡出动画观感需真机确认。
