# 功能规格：命令面板（Ctrl+K）

> 状态：开发中　·　关联 PRD：FR-74　·　分支：feature/fr-74-cmdk

## 1. 背景与目标

为应用提供全局命令面板（FR-74，P7）。用户在任意页面按 `Ctrl+K`（macOS `Cmd+K`）即可弹出命令面板，输入关键字模糊检索命令，键盘上下移动、回车执行，快速完成「页面跳转 / 切主题 / 收展导航 / 退出登录」等高频操作，无需用鼠标在导航间往返。

挂载点为全局壳层 `AppLayout`（唯一全局组件、已持有 `useNavigate` / `toggleColorScheme` / `toggleNavCollapsed` / `logout` 等全部命令所需能力）。

## 2. 需求（要什么）

- 全局快捷键：任意页面 `mod+K`（Windows/Linux 为 Ctrl+K、macOS 为 Cmd+K）打开命令面板。
- 面板交互：弹出后输入框自动聚焦、查询清空；按命令中文 label 模糊过滤；键盘 `↑`/`↓` 移动高亮、`Enter` 执行选中项、`Esc` 关闭；鼠标点击命令项亦可执行。执行任一命令后关闭面板。
- 命令清单：
  - 直接执行：切换主题、收起/展开导航、退出登录（退出后跳 `/login`）。
  - 跳转（`navigate(path)`）：`navItems` 的 9 项（管理 / 时间轴 / 目录 / 相册 / 地图 / 回收站 / 巡检 / 重复项 / 系统）+ 开源协议（`/licenses`）+「扫描媒体库」（跳 `/library-manager`）+「搜索」（跳 `/` 时间轴）。
- 移动端入口（可选）：header 增一个图标按钮，点击同样打开命令面板，便于无物理键盘的移动端触发。
- 不做（范围外）：
  - 面板内**直接**执行扫描 / 加库 / 搜索（这些依赖具体页面级上下文与表单，属范围扩张）——命令面板只负责「跳到对应页面」，由页面承接后续操作。
  - 命令历史 / 最近使用排序、模糊评分排序、命令分组标题、自定义快捷键、可扩展命令注册中心等（YAGNI，需要时再加）。
  - 不引入 `@mantine/spotlight`、`cmdk` 等第三方命令面板库；用现有依赖自建。

## 3. 设计（怎么做）

纯前端展示/交互层增强，无后端改动、无数据模型改动、无新架构决策、不写 ADR、不引新依赖。

### 命令模型

命令面板渲染一份命令列表，每条命令为 `{ id, label, icon, run }`，`run` 是无参回调。命令清单在 `AppLayout` 内构造（此处才能拿到 `navigate` / `toggleColorScheme` / `toggleNavCollapsed` / `logout` 闭包），以 props 传入 `CommandPalette`：

- 跳转类命令：`run = () => navigate(path)`，数据源复用 `navItems` 9 项 + `/licenses` + 「扫描媒体库」(`/library-manager`) + 「搜索」(`/`)。
- 直接执行命令：切主题 `toggleColorScheme`、收展导航 `toggleNavCollapsed`、退出登录 `() => { logout(); navigate('/login') }`。

把命令清单作为 props 注入而非在组件内写死，便于组件用纯展示逻辑独立单测（注入 spy 断言执行）。

### 组件 `components/CommandPalette.tsx`

受控弹窗范式参照 `ConfirmModal` / `NameEditModal`：

- props：`opened: boolean`、`onClose: () => void`、`commands: Command[]`。
- 用 `@mantine/core` 的 `Modal` + `TextInput`（`data-autofocus`，打开时由 `useEffect(opened)` 清空查询并重置高亮）+ 命令列表（`Stack`/`Group`/`Text`/`UnstyledButton` 承载，每项可点击）。
- 过滤：按查询字符串对命令 `label` 做大小写无关的 `includes` 子串匹配（中文无大小写，沿用统一小写化以兼容潜在英文）。
- 键盘：`TextInput` 上 `onKeyDown` 处理 `ArrowDown`/`ArrowUp`（移动高亮，边界取模回绕）、`Enter`（执行当前高亮命令）、`Escape`（关闭，交由 Modal 默认 `onClose` 亦可，统一显式处理）。执行命令后调用 `onClose`。
- 高亮项以背景色 + `data-active` 标识，便于测试与无障碍定位。

### 快捷键注册（`AppLayout`）

用 `@mantine/hooks` 的 `useHotkeys`（与既有 `useDisclosure` 同包、已是依赖）注册 `[['mod+K', open]]`；`open`/`close` 由 `useDisclosure(false)` 提供。`mod+K` 当前全仓无冲突（已确认零 `useHotkeys` 命中）。

### 移动端入口（`AppLayout` header，可选）

header 左侧（汉堡按钮旁）增一个 `ActionIcon`（`IconSearch` / `IconCommand`），`onClick={open}`，`aria-label="命令面板"`，触发同一 `open`。

## 4. 任务拆分

- [x] 新增 `components/CommandPalette.tsx`（受控 Modal + 输入过滤 + 键盘导航 + 点击执行）
- [x] `AppLayout` 接入：`useDisclosure` + `useHotkeys('mod+K')` + 构造命令清单 + 挂载组件 + header 入口按钮
- [x] 组件测试 `CommandPalette.test.tsx`：输入过滤、↑↓+Enter 执行、Esc 关闭、点击执行
- [x] 布局测试补充：`mod+K` 打开命令面板、header 入口按钮打开（在 `AppLayout.test.tsx`）
- [x] 文档同步：PRD 状态、CHANGELOG

## 5. 验收标准

- 任意页面按 `Ctrl/Cmd+K` 弹出命令面板，输入框自动聚焦、查询为空（布局测试覆盖快捷键打开）。
- 输入关键字按命令 label 过滤命令列表；`↑`/`↓` 移动高亮、`Enter` 执行高亮命令（断言对应 `navigate` / `toggleColorScheme` 等被调用）；`Esc` 关闭；点击命令项执行（组件测试覆盖）。
- 命令清单含 9 个导航项 + 开源协议 + 扫描媒体库 + 搜索的跳转命令，以及切主题 / 收展导航 / 退出登录三条直接执行命令。
- 前端 `npx tsc --noEmit` + `npx vitest run` + `npm run build` 全绿；改动文件 eslint 干净。
- 手动验收（待真机验）：实机任意页面 Ctrl+K 打开面板、输入过滤、键盘选中跳转、Esc 关闭符合预期。

## 6. 风险 / 待定

- `mod+K` 在部分浏览器可能与浏览器内置快捷键存在潜在冲突（如某些浏览器的地址栏聚焦）；`@mantine/hooks` 的 `useHotkeys` 默认对匹配事件 `preventDefault`，可拦截多数情况，真机验证为准。
- jsdom 下 `useHotkeys` 监听 `document` 的 `keydown`，测试中用 `fireEvent.keyDown(document.body, ...)` 或 userEvent 键盘事件触发；需确保事件目标与监听一致。
