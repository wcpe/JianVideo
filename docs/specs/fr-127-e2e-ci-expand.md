# 功能规格：前端 E2E 接入 CI 并扩关键场景

> 状态：开发中　·　关联 PRD：FR-127　·　分支：claude/suspicious-snyder-718ae6

## 1. 背景与目标

已有 Playwright E2E：`playwright.config.ts`（webServer 构建前端 + `go run .`、隔离 `.tmp/e2e.db`、SW 禁用、CI retries:2）+ `e2e/browser_e2e.spec.ts`（登录/守卫/导航/库增删）+ `e2e/share_e2e.spec.ts`，本地可跑但未进 CI。本 FR **扩关键流程 spec**（CI 接线落在 FR-128）。属第十三期（P13）。

## 2. 需求（要什么）
- 新增**适度**关键流程 E2E spec（不堆量），覆盖现有 spec 未覆盖的关键用户流程：
  - 目录浏览：进入 `/browse`、与目录树/列表交互（点击下钻或视图切换）。
  - 设置页：进入设置、读取/保存一项运行期配置（如调试日志开关或某路径），断言保存成功反馈。
  - 播放页加载：对一条媒体进入 `/play/:id`，断言播放器容器渲染（jsdom 外真实浏览器，HLS 可不真正解码，断言到「进入播放页 + 播放器挂载/协商发起」即可）。
- 复用 `browser_e2e.spec.ts` 的 `login()` 辅助与 `serviceWorkers:'block'`。
- 范围内：`e2e/*.spec.ts`。
- 不做（范围外）：CI workflow（FR-128）、改后端、改前端业务。

## 3. 设计（怎么做）
- **首跑前置核实（关键）**：FR-109 取消了 admin/admin 默认账户，全新 `.tmp/e2e.db` 首访为「初始化引导」而非登录。须先核实现有 E2E 如何登录成功（可能：①webServer/测试启动已播种 admin；②首个 spec 走 setup 流程建号）。据实让新 spec 的登录前置可靠——若需先初始化，则复用/补一个 setup 辅助。**先把现有 2 个 spec 在本地跑通、确认登录前置**，再写新 spec。
- 新 spec 选择器对照实际页面（LoginPage/BrowsePage/SettingsPage/PlayPage）编写，断言用 role/text/label，避免易朽。
- 媒体相关场景若依赖库中有媒体：用 `page.request` 经已登录上下文准备最小数据或选择不依赖具体媒体的断言（如空态/页面骨架）。

## 4. 任务拆分
- [ ] 本地跑通现有 2 个 spec，确认登录/初始化前置（`npm run e2e`）
- [ ] 写目录浏览、设置、播放页加载关键流程 spec（适度）
- [ ] 本地 `npm run e2e` 新 + 旧全绿
- [ ] 文档同步：PRD 状态、CHANGELOG（按需）

## 5. 验收标准（AC-31，部分为真机/集成维度）
- 新增关键流程 spec 本地 `npm run e2e` 通过（headless Chromium）。
- 覆盖目录浏览、设置、播放页加载等关键流程。
- CI 中 E2E job 通过、失败挡合并——**此项在 FR-128 接线后由 CI 实跑确认**（本机不代表 CI；标 CI 待验）。

## 6. 风险 / 待定
- E2E 重（构建前端 + 起 go 服务 + 浏览器），偶发抖动已由 config retries:2 缓解。
- admin/admin 与 FR-109 初始化引导的张力：必须先核实登录前置，否则新 spec 登录步骤不可靠。
- 播放真解码依赖真实编解码/MSE，E2E 仅断言到「进入播放页 + 播放器挂载/协商发起」，不强求真播。
- 本机能否稳定跑通完整 E2E 取决于环境（端口/ffmpeg/go run）；跑不通则记录并以 FR-128 的 CI 实跑为准。
