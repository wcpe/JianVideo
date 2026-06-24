# 功能规格：播放页全屏沉浸布局

> 状态：开发中　·　关联 PRD：FR-103（扩 FR-85）　·　分支：feature/fr-103-play-immersive

## 1. 背景与目标

播放页（PlayPage）当前用 `<Stack>` 竖排：面包屑 + 头部 + **16:9 固定比例视频** + 下方「媒体信息」`Paper`。视频高度被锁在 16:9，下方信息卡又把内容撑出视口，导致播放页**可向下滚动、视频未铺满**——这是用户最在意的体验缺陷。属第九期（P9）播放体验大改。

目标：让播放页**正好铺满视口、不可纵向滚动**，视频吃满头部/控件之外的全部高度，媒体信息收纳出文档流不再撑高页面。

## 2. 需求（要什么）

- **播放路由专属全屏布局**：播放页容器 `height:100dvh`（用 `dvh` 避开移动端地址栏抖动）+ `display:flex; flex-direction:column; overflow:hidden`，锁纵向滚动。
- **视频填满剩余高度**：视频区由固定 `aspectRatio:16/9` 改为 `flex:1; min-height:0` + 视频 `object-fit:contain`（letterbox 黑边），吃满头部/控件之外的全部高度。
  - 给 `VideoPlayer` 加「填充模式」prop `fill`，播放页传 `fill`；**灯箱（MediaDetailPanel）/分享页等默认用法保持 16:9 不变**（FR-102 灯箱内嵌播放器不受影响）。
- **媒体信息移出文档流**：把「媒体信息」`Paper` 从视频下方移出，收进头部「更多」菜单的「详情」项 → 右侧可收起抽屉（Mantine `Drawer`），不再撑高页面。
- **控件/头部不撑高**：头部保留 FR-85 的返回/影院/更多/字幕，整体高度受控（`flex-shrink:0`）。
- **与外层滚动协调**：播放路由下让 `AppShell.Main`（`AppLayout`）不产生额外 padding/滚动——播放页用 effect 给 `body` 加 `play-immersive` 类、`index.css` 据此去掉 main 的 padding 并锁高，卸载清理；**不改 `AppLayout.tsx`**，不破坏其它页布局。

- 范围内：纯前端、PlayPage 布局重排、VideoPlayer `fill` 模式、index.css 播放路由专属类。
- 不做（范围外）：碰后端、引入新依赖、改 VideoPlayer 默认 16:9 行为、改其它页布局、控件自动隐藏的复杂交互（FR-104 范畴，本期最小保留常显控件）。

## 3. 设计（怎么做）

- **VideoPlayer `fill` prop**（默认 `false`）：
  - `false`（默认）：外层 `Box` 与视频容器维持现状（`width:100%` + `aspectRatio:16/9`），灯箱/分享零改动。
  - `true`：外层 `Box` 改 `flex:1; height:100%; minHeight:0`，视频容器由 `aspectRatio:16/9` 改 `flex:1; minHeight:0`，`<video>` 用 `object-fit:contain` letterbox 填充。
- **PlayPage 布局**：根容器由 `<Stack>` 改 `<Box>` flex 列：`height:100dvh; display:flex; flexDirection:column; overflow:hidden`。
  - 面包屑（非影院态）+ 头部为 `flex-shrink:0` 不撑高。
  - 播放器外层包一层 `flex:1; minHeight:0` 容器，VideoPlayer 传 `fill`。
- **媒体信息抽屉**：原「媒体信息」`Paper` 内容搬进 Mantine `Drawer`（右侧、`position="right"`），头部「更多」菜单新增「详情」`Menu.Item` 打开；内容（真实文件名/路径/格式/编码/分辨率）不变。
- **AppShell.Main 协调**：`index.css` 加 `body.play-immersive .mantine-AppShell-main { padding:0; height:100dvh; overflow:hidden; }` 与 `body.play-immersive { overflow:hidden; }`；PlayPage `useEffect` 挂载加类、卸载移除（仅作用播放路由，不动 AppLayout）。
- 无新 ADR：仅前端组件内布局与 CSS，未触及架构不变量；播放内核（mpegts.js）与协商（FR-52/53）路径不变。

## 4. 任务拆分
- [ ] VideoPlayer 新增 `fill` prop：默认维持 16:9，`fill` 时 flex 填充 + `object-fit:contain`
- [ ] PlayPage 根布局改 100dvh flex 列 + overflow hidden，视频区 `flex:1`
- [ ] 媒体信息搬进右侧 Drawer，「更多」菜单加「详情」入口
- [ ] index.css 加 `body.play-immersive` 播放路由专属类，PlayPage effect 加/卸类
- [ ] 测试：播放页容器 100dvh+overflow hidden、VideoPlayer fill 非固定 16:9、媒体信息不在视频下方文档流（移入抽屉）；既有 PlayPage/VideoPlayer 测试全绿
- [ ] 文档同步：PRD 状态、CHANGELOG

## 5. 验收标准
- 播放页根容器 `height:100dvh` 且 `overflow:hidden`（不可纵向滚动）。
- VideoPlayer 在 `fill` 模式下视频容器非固定 `aspectRatio:16/9`，而是 `flex:1` 填充剩余高度；默认（无 `fill`）仍 16:9（灯箱/分享不受影响）。
- 「媒体信息」不再渲染在视频下方文档流，改由「更多」菜单的「详情」项打开右侧抽屉查看。
- 回归：FR-85 影院模式与「更多」菜单全部操作、FR-102 静音自动播、续播（FR-44）/字幕等既有行为不变。
- 自动化：vitest 断言上述布局与抽屉收纳；既有 PlayPage/VideoPlayer 测试全绿。
- 真机（待真机验）：**桌面 + 移动端播放页正好铺满、无纵向滚动条**、视频吃满高度、信息收纳可查看、影院/自动播不回退。

## 6. 风险 / 待定
- `100dvh` 在个别旧浏览器无支持时回退为视口高度近似；目标浏览器（现代 Chromium/WebKit/Firefox）均支持。
- `body.play-immersive` 依赖 PlayPage 卸载清理移除类；若未来 PlayPage 不再整页卸载需复查清理时机（与 FR-85 影院态同此约束）。
