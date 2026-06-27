# 功能规格：系统/设置页改一级 tab + 每 tab 左侧锚点导航

> 状态：开发中　·　关联 PRD：FR-113（取代 FR-55 两级结构，扩 FR-59）　·　分支：feature/fr-b-console-settings

## 1. 背景与目标

当前控制台页（`ConsolePage`）是**两级 tab**：外层「系统信息 | 设置」，进「系统信息」后内层还有「运行环境 / 硬件加速 / 编解码 / 应用更新」四个子 tab（`SystemPage`），而「设置」（`SettingsPage`）是一条长滚动分区页。两级 tab 层级深、可发现性差。本变更把控制台拍平成**一级 tab**，并在每个 tab 内提供左侧锚点导航分区，提升信息可达性。属于 P11（界面交互打磨）。

## 2. 需求（要什么）

- 范围内：
  - 去掉外层「系统信息 / 设置」父级，控制台改为一级 tab：`[运行环境 | 硬件加速 | 编解码 | 应用更新 | 设置]`，「设置」并入为同级 tab。
  - 每个 tab 内若含多个区块，提供**左侧锚点导航**：点击锚点平滑滚动定位到对应区块，滚动时高亮当前可视区块。
    - 设置 tab 锚点：账户安全 / 扫描 / 网络 / 工具路径 / 回收站 / 诊断 / 环境变量。
    - 运行环境 tab 锚点：运行环境 / FFmpeg。
    - 硬件加速 / 编解码 / 应用更新 tab 仅单区块，可不显示锚点列（或仅一项）。
  - **向后兼容深链**：旧 URL `?tab=system&sys=update`、`?tab=settings`、`?tab=system&sys=hwaccel` 等仍能正确定位到对应一级 tab（兼容映射）。页眉 `UpdateIndicator` 仍跳「应用更新」。
- 不做（范围外）：
  - 不改各区块自身的业务行为 / 接口 / 数据模型（纯前端结构重组）。
  - FR-112 的应用更新面板交互重构单独走（本 spec 不动更新按钮/缓存逻辑）。
  - 不新增依赖。

## 3. 设计（怎么做）

- 纯前端重组，无后端 / API / 数据模型改动，无新增依赖（Mantine `Tabs`、`react-router-dom` `useSearchParams`、原生 `IntersectionObserver` 与 `scrollIntoView` 均可用）。不涉及架构决策，无新 ADR。
- 一级 tab 由 `ConsolePage` 承载，`value` 取自 URL query `?tab=`，取值 `env|hwaccel|codec|update|settings`，缺省 `env`。
- **兼容映射**：解析时把旧式 `?tab=system&sys=<x>` 归一为新一级 tab（`sys` 值直接作一级 tab key：`env/hwaccel/codec/update`，缺省 `env`）；`?tab=settings` → `settings`；`?tab=system` 无 `sys` → `env`。切 tab 写回新式 `?tab=<key>`（同时清掉遗留的 `sys`）。
- `SystemPage` 拆分：原四子 tab 内容（运行环境/FFmpeg、硬件加速、编解码、应用更新）抽为各自可独立渲染的区块组件，由 `ConsolePage` 直接作为一级 tab 面板渲染；`SystemPage` 保留为「运行环境 tab」内容（运行环境 + FFmpeg 两区块 + 左侧锚点），其余区块各自独立。共享的更新逻辑随「应用更新」区块迁移。
- 左侧锚点导航抽为通用组件 `AnchorNav`：接收 `[{id, label}]`，渲染锚点列；用 `IntersectionObserver` 观察各区块、滚动时高亮当前；点击调用 `scrollIntoView({behavior:'smooth'})`。无依赖、可复用于设置 tab 与运行环境 tab。
- 页面级 `<Title>`（「系统诊断」「设置」）保留以兼容旧测试断言。

## 4. 任务拆分

- [x] 复制模板写本 spec；PRD FR-113 状态「计划」→「开发中」
- [x] 测试先行：`ConsolePage.test.tsx` 改为断言一级 tab（含设置）、旧深链兼容映射；`AnchorNav` 单测（高亮 / 点击滚动）
- [x] 实现 `AnchorNav` 通用锚点导航组件
- [x] 重构 `ConsolePage`（一级 tab + 兼容映射）、拆 `SystemPage`、`SettingsPage` 接入锚点
- [x] 文档同步：PRD 状态、ARCHITECTURE（控制台描述）、CHANGELOG 未发布段
- [x] 前端验证门：`npm run build` + 相关 vitest 全绿

## 5. 验收标准

- 控制台只剩一级 tab，且含「设置」（`getByRole('tab', {name})` 五项齐全）。
- 每个含多区块的 tab 有左侧锚点列；点锚点滚动定位、滚动时高亮当前区块。
- 旧深链仍能定位：`?tab=system&sys=update` → 应用更新 tab；`?tab=settings` → 设置 tab；`?tab=system` → 运行环境 tab。
- 页眉 `UpdateIndicator` 跳转仍命中应用更新 tab（不报错）。
- `npm run build`（tsc -b + vite build）通过；相关 vitest 绿。

## 6. 风险 / 待定

- `IntersectionObserver` 在 jsdom 缺失，测试需注入 stub（参考既有测试对 `navigator.clipboard` 的注入）。高亮逻辑下沉为可单测的纯逻辑，滚动行为只做 smoke。
