# 功能规格：Wiki UI 博物馆与 Mockup 先行

> 状态：已完成@v0.22.0　·　关联 PRD：FR2-005　·　阶段：P0.5 / P1 `0.22.x`　·　分支：`feature/fr2-005-wiki-ui-museum-mockup`

## 1. 背景与目标

P0.5 要在进入 P1 前冻结前端协作方式：先在 wiki UI 博物馆和 mockup 中验证组件、状态、密度、主题、HLS 预览、任务队列和 PixiJS 样例，再拼正式页面。这样可以避免继续在主应用页面里边做边改，导致组件、主题和 mock 数据重复分叉。

目标：

- `apps/wiki` 作为 UI 博物馆与 mockup 入口。
- UI 控件源码放 `packages/ui`，wiki 只展示和治理，不作为运行时依赖被 `apps/web` 引用。
- `apps/web` 与 `apps/wiki` 从 `packages/ui`、`packages/theme`、`packages/mock`、`packages/render-pixi` 引用同源能力。
- Mock 先行，后端实现前也能预览 HLS 卡片、转码任务、AI 任务、Space 权限和错误状态。
- 每个可复用控件必须有交互预览和使用代码片段。

## 2. 范围

范围内：

- wiki 导航、搜索、组件分组和状态切换。
- `packages/ui` 控件预览：按钮、输入、菜单、表格、媒体卡、HLS 预览卡、任务状态、空态、错误态、权限态。
- `packages/theme` 主题 token、密度、亮暗色和响应式预览。
- `packages/render-pixi` 的媒体网格、时间轴、纹理池和拖拽定位样例。
- `packages/mock` 的 MSW handlers、seed 数据和场景切换。
- 展示真实 import 路径与最小使用代码片段。

范围外：

- 不把 wiki 当主应用运行时依赖。
- 不在本规格里决定是否引入 Storybook；默认先按 Vite 应用实现，后续如需引入额外依赖必须单独确认。
- 不在 wiki 内实现正式业务 API、真实上传、真实删除或真实转码。

## 3. 结构约束

```text
apps/
  wiki/          UI 博物馆与 mockup 站
packages/
  ui/            可复用 UI 控件源码
  theme/         主题 token 与密度
  mock/          MSW handlers、seed 与场景定义
  render-pixi/   PixiJS 高密度渲染样例与核心封装
```

约束：

- `apps/wiki` 可以 import `packages/*`。
- `apps/web` 不得 import `apps/wiki`。
- 代码片段中的 import 路径必须指向 `packages/*`，不能指向 wiki 内部文件。
- 所有 mock 数据必须可 seed 重建，不把用户真实路径、密钥或隐私数据写进 mock。

## 4. 任务拆分

- [x] 盘点当前自定义 UI 控件，确定首批进入 `packages/ui` 的组件清单。
- [x] 定义 wiki 页面分组：基础控件、媒体控件、任务队列、Space 权限、PixiJS 样例、主题与密度。
- [x] 建立 mock 场景：空库、正常库、百万素材压力场景、缩略图缺失、HLS 生成中、转码失败、权限不足、AI 审核待处理。
- [x] 为首批控件补交互预览：默认、loading、disabled、empty、error、selected、dense、mobile。
- [x] 为每个可复用控件展示最小使用代码片段。
- [x] 增加 PixiJS 样例入口，输出纹理数量、可见窗口和请求数量指标的基础入口。
- [x] 确认 wiki 从 `packages/mock` 读取共享 mock 场景；本轮没有新增真实 API 或 MSW handler。

## 5. 验收标准

- `apps/wiki` 能作为独立前端应用运行，且不依赖真实后端。
- `packages/ui` 首批控件均能在 wiki 中交互预览，并显示使用代码片段。
- HLS 预览卡、缩略图状态、转码任务、AI 任务、Space 权限状态都有 mock 场景。
- PixiJS 样例能展示 100 万素材压力场景的基础指标入口；正式性能阈值以 `fr2-003-performance-budget.md` 为准。
- `apps/web` 不直接依赖 `apps/wiki`；两者只通过 `packages/*` 共享能力。
- wiki 中不得出现用户真实路径、密钥、账号、内网地址或不可公开隐私数据。

## 6. 风险

- 组件过早抽象会拖慢 P1；首批只抽复用明确的控件，页面专用布局不强行入库。
- PixiJS 样例不能只做静态演示，必须能接入 Benchmark 指标，否则无法支撑 P1/P3 验收。
- 代码片段需要从真实组件导入路径生成或维护，避免展示和可用代码漂移。
