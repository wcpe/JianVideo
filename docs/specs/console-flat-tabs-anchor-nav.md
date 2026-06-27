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

## 7. 真机走查后续修复（v0.18.0 之后）：锚点列 sticky 常驻

> v0.18.0 发布后真机走查反馈：在某 tab 内点左侧锚点滚到下方后，锚点列本身随内容一起滚走、滚到底即不可见。属本 FR 后续修复（不改 PRD FR-113 状态行，记 CHANGELOG）。

- **根因**：滚动容器为 `AppShell.Main`（页面整体滚动）；锚点列容器（`SettingsPage`/`SystemPage` 内 `Box w={160}`）此前为普通流内元素，随内容一起滚出视口。
- **修复**：
  - 锚点列容器改 `position: sticky`（内联 `position/top`，便于 jsdom 单测断言）+ `.anchor-nav-sticky` 类补 `max-height: calc(100vh - 72px)` + `overflow-y: auto`（锚点项过多时自滚动不溢出）。`top` 取 56px（略大于一级 tab 头部，使锚点列落在 tab 之下不被遮挡）。
  - 一并把控制台一级 tab 头部（`Tabs.List`）改为吸顶：`ConsolePage` 的 `<Tabs className="console-tabs">`，`.console-tabs > .mantine-Tabs-list` 设 `position: sticky; top: 0`（补 `--mantine-color-body` 背景遮挡其下滚动内容），避免锚点定位后一级 tab 被滚走。
- **验收硬指标**：设置/系统各 tab 内左侧锚点列滚动时常驻可见、点击定位与当前区块高亮正常。
- 测试：`SettingsPage.test.tsx` 加「锚点列容器带 sticky 定位」断言（`getComputedStyle(...).position === 'sticky'`）。真机维度（滚到底锚点仍可见、吸顶不遮挡）标「待真机验」。

## 8. 真机走查第二批后续修复（v0.18.0 之后）

> v0.18.0 第二批真机走查反馈。属本 FR 后续修复（不改 PRD FR-113 状态行，记 CHANGELOG）。

### 8.1 锚点点击高亮被平滑滚动抢回（bug，含死区根因）
- **根因（深层·死区）**：原实现用 `IntersectionObserver`（`rootMargin: '0px 0px -66% 0px'`，有效观测带仅视口顶部 ~34%）+ `pickActiveSection`「可见集为空时回退首个」。当滚动到某些位置（真机证据：点「工具路径」滚到 `scrollY≈973`），恰无任何区块目标落在窄带内 → 可见集为空 → 回退首项「账户安全」，高亮 snap 回去。这是**死区 bug**，仅加 700ms 点击锁治标——锁过期后必然复现。
- **修复（健壮 scroll-spy，消除死区）**：放弃「窄带 + 空集回退首个」，改为**基于滚动位置**判定。纯函数 `pickActiveByScroll(offsets, scrollPos, currentActive)`：取「顶部已越过 `scrollPos + 阈值` 的**最后一个**区块」（即视口顶部当前所在区块）；滚到最顶取第一个、滚到最底取最后一个；`offsets` 为空（DOM 未就绪）时**保留传入的 `currentActive`**——**任何位置都有确定结果、绝不回退到无关首项**。组件在 `scroll`（rAF 节流）+ `resize` 时用 `getBoundingClientRect + window.scrollY` 实测各区块绝对偏移并重算。
- **点击锚点**：点击即高亮被点项并加 700ms 锁（屏蔽平滑滚动途中跳变），落定后解锁由 scroll-spy 接管；落定后其结果**正好等于被点项**（滚动已停在被点区块，一致无跳变）。
- **测试**：`AnchorNav.test.tsx` 改为覆盖新纯函数的死区场景——`scrollY=973` 取工具路径（不回退账户安全）、中部空隙归属上一已越过区块、空 `offsets` 保留当前 active；组件级模拟滚动到工具路径区后高亮稳定、点击锁定窗内滚动不被抢回、落定后一致。

### 8.2 运行环境区块加「复制」按钮（增强）
- 运行环境 tab 的「运行环境」卡片头部新增**复制按钮**（与编解码「复制结果」同风格：`useClipboard`、复制后绿勾 + 「已复制」文案）。
- 点击把当前展示的运行环境字段（应用版本/操作系统/架构/CPU/主机名/Go 版本/运行时信息/FFmpeg 等）拼成纯文本报告（`buildEnvReport`）写入剪贴板。
- **测试**：`SystemPage.test.tsx` 新增——按钮存在、点击调用 `clipboard.writeText` 且报告含关键字段。

### 8.3 验收补充
- 点「工具路径」后高亮稳定停在「工具路径」、不被抢回「账户安全」。
- 运行环境区块「复制」按钮可把运行环境信息复制到剪贴板。
- `npm run build` + vitest 全绿；真机维度（点击锚点高亮稳定、复制内容完整）标「待真机验」。

## 9. 真机走查第三批后续修复（v0.19.0 之后）

> v0.19.0 真机走查反馈两处对齐问题。属本 FR 后续修复（不改 PRD FR-113 状态行，记 CHANGELOG）。

### 9.1 内容标题与一级 tab 不一致（bug）
- **现象**：`SystemPage` 内容区 order-2 标题对四个系统 tab（env/hwaccel/codec/update）恒显「系统诊断」——点「应用更新」tab 内容标题却是「系统诊断」，与上方 tab 名不符（设置 tab 标题为「设置」，正确）。
- **修复**：抽出 `SECTION_TITLES: Record<SystemSection, string>`（env→运行环境、hwaccel→硬件加速、codec→编解码、update→应用更新），`SystemPage` 的 order-2 标题改为 `SECTION_TITLES[section]`；`ConsolePage` 的 `Tabs.Tab` 文案亦复用同一映射（与各区块内容标题单一真源、不再各写一份硬编码）。设置 tab 仍为「设置」。
- **测试**：`SystemPage.test.tsx` 加各 section 内容区标题断言（update section 标题为「应用更新」而非「系统诊断」，codec/hwaccel 同理）；`ConsolePage.test.tsx` env tab 内容断言改为命中内容区 order-2「运行环境」标题。

### 9.2 锚点高亮不准 + 点击定位偏移 + 滚动后 tab 条被页眉遮住（吸顶布局根因，bug）

> 注：本节先有一版「运行时实测吸顶偏移取 `tabsList.bottom`」的修复，真机复验仍不对——根因在更底层的布局（sticky tab 条被固定页眉遮住、且测量随 stuck 状态变化而点击/滚动取值不一致）。下文为修正后的最终方案。

- **真机 inspect 实测（scrollY=609）**：固定页眉 `.mantine-AppShell-header` position:fixed、top:0、高 56px、占 y=0..56；sticky tab 条 `.console-tabs > .mantine-Tabs-list` 原 `top:0`、stuck 时 rect y=0..38——正好被固定页眉盖住（滚动后整条 tab 不可见）。`tabsList.bottom` 读得 38（其实被页眉遮），而实际可读区顶部应是 页眉 56 + tab 38 = 94；点击时（未 stuck）测得 ~94、滚动高亮判定用 38 → 点「网络」落 y=109、高亮却判成「扫描」。
- **根因**：sticky tab 条 `top:0` 让它 stuck 在固定页眉下方被遮；且吸顶偏移测量随 stuck 状态变化，点击瞬间与滚动后取值不一致。
- **修复（让 sticky 元素让开固定页眉 + 测量改为稳定 stuck 偏移）**：
  - **sticky tab 条让开页眉**：`.console-tabs > .mantine-Tabs-list` 的 `top` 由 `0` 改为 `var(--app-shell-header-height, 56px)`（Mantine AppShell 在根元素暴露的页眉高度变量，由 `header={{height:56}}` 经 `rem()` 算出 `3.5rem`，随配置/安全区变化；缺省兜底 56px、不写死）。tab 条 stuck 在页眉正下方、滚动时可见。
  - **左侧锚点列让开页眉 + tab 条**：`.anchor-nav-sticky` 的 `top` 改为 `calc(var(--app-shell-header-height,56px) + 2.375rem)`（tab 条高度无 Mantine 变量，取实测近似 2.375rem≈38px），`max-height` 同步扣除，使锚点列 stuck 在 tab 条下方不被遮。原内联 `top:56` 从 `SystemPage`/`SettingsPage` 移除（仅保留 `position:sticky` 内联供 jsdom 单测断言）。
  - **`measureStickyOffset` 返回稳定 stuck 偏移**：= 固定页眉 `offsetHeight` + tab 条 `offsetHeight`（均与当前滚动无关），不再读随 stuck 变化的 `getBoundingClientRect().bottom`。保证「点击时（页面或在顶部、tab 尚未 stuck）」与「滚动后（tab 已 stuck）」测得同一值——点击设的 `scroll-margin-top` 与 scroll-spy 判定线用同一值、点哪个锚点就高亮哪个且区块落在 tab 条正下方完整可见。
  - **scroll-spy 判定线**：`pickActiveByScroll` 的可注入 `stickyOffset` 参数不变，判定线 = `scrollPos + stickyOffset + 小提前量(SPY_LEAD=8)`；死区/触底/空集语义不回归（空 offsets 保留 active、触底钳末项、绝不回退首项）。
- **验收硬指标**：① 滚动后一级 tab 条可见（不再被页眉盖住）；② 点「网络」→「网络」标题落在 tab 条正下方、完整可见，且左侧「网络」锚点 `data-active=true`、不再高亮「扫描」；③ 任意位置高亮 = 可读区顶部所见区块；④ 死区/触底场景仍正确。
- **测试**：`AnchorNav.test.tsx` 的 `measureStickyOffset` 用例改为桩定固定页眉 + tab 条 `offsetHeight` 验「= 二者之和的稳定值」；组件级吸顶高亮（绝对顶部仍在上一区块时不偏前一个）、点击设 `scroll-margin-top` 用例随之以 `offsetHeight` 桩定，保留死区/触底/空集用例。真机维度（tab 条可见、点击落点对齐、高亮与肉眼一致）标「待真机验」。

### 9.3 验收补充
- `npm run build` + vitest 全绿；真机维度（tab 条不被遮、高亮对齐可读区、点击落点完整可见、死区/触底正确）标「待真机验」。
