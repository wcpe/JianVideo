# 功能规格：页脚版本号 + 开源协议页

> 状态：开发中　·　关联 PRD：FR-57　·　分支：main

## 1. 背景与目标

第六期（界面与运维完善）需要在界面底部常驻展示当前版本，并提供一个集中查看本项目及其依赖开源协议的页面，满足开源合规与用户知情。属于 P6。

## 2. 需求（要什么）

- 全局页脚：在 `AppLayout` 底部显示**当前版本号** + 「开源协议」链接（指向 `/licenses`）。桌面与移动端均可见。
- 开源协议页 `/licenses`：
  - 顶部展示项目自身（JianVideo）+ MIT 协议 + 作者 + 协议全文（可展开）。
  - 搜索框过滤依赖包（按包名）。
  - 「前端依赖（N）」表：包名 / 版本 / 协议 / 作者，点击展开协议全文。
  - 「后端依赖（M）」表：包名 / 版本 / 协议；有全文则可展开，无全文则给 `pkg.go.dev/<module>?tab=licenses` 外链（`target=_blank rel=noopener noreferrer`）。
- 依赖清单**构建期生成、嵌入仓库、运行时不联网**：前端全部生产依赖 + 后端 `go.mod` 直接依赖（非 `// indirect`）+ 项目自身 MIT。
- 范围内：footer + 协议页 + 生成脚本（含 devDep `license-checker`）。
- 不做（范围外）：FR-54 收缩导航、FR-56 设置 tab、FR-58 页眉更新提示；运行时联网拉取协议；后端依赖协议全文的复杂联网补全（拿不到给链接即可，YAGNI）。

## 3. 设计（怎么做）

无新架构决策，不写 ADR（沿用既有前端页面 + go:embed 真源约束）。

- **生成脚本** `frontend/scripts/gen-licenses.mjs`（Node ESM，手动 / 可选 prebuild 运行）：
  - 前端：调 `license-checker --production --json` 取每个生产依赖的 `name@version`、`licenses`、`publisher`、`licenseFile`（读全文）。
  - 后端：读根 `go.mod`，解析 `require` 块剔除 `// indirect`，取 module 路径 + 版本；尝试从 `go env GOMODCACHE` 下 `<module>@<version>/LICENSE*` 读协议全文与协议名，读不到则给 `pkg.go.dev/<module>?tab=licenses` 链接。
  - 项目自身：读根 `LICENSE`（MIT 全文）+ 作者（取版权行）。
  - 输出 `frontend/src/data/licenses.json`（入库）：`{ project, frontend[], backend[] }`。
  - `package.json` 加 `"gen:licenses"` 脚本；`license-checker` 加入 devDependencies。
- **类型与数据访问** `frontend/src/data/licenses.ts`：import 上述 JSON 并以 `LicensesData` 类型导出，页面与测试都引用此模块（测试 `vi.mock('@/data/licenses')` 注入小 fixture，避免依赖真实大 JSON）。类型定义入 `frontend/src/types/index.ts`。
- **页脚**：`AppLayout` 用 `AppShell.Footer`（`footer={{ height }}`），mount 时取 `/api/system/info` 的 `app_version`（复用 `systemApi.getSystemInfo`），失败静默不显版本（不阻塞布局）；右侧 React Router `Link` 到 `/licenses`。
- **协议页** `frontend/src/pages/LicensesPage.tsx`：Mantine 卡片 + 搜索 `TextInput` + 折叠（`Collapse`/`Accordion` 或受控展开）。协议全文以 `<Code block>` 或 `pre` 纯文本展示（非 markdown，协议是纯文本）。
- **路由**：`App.tsx` 加 `/licenses`（`ProtectedRoute` + `AppLayout`）。

## 4. 任务拆分

- [x] spec + PRD 状态置「开发中」
- [x] 加 devDep `license-checker`，写 `gen-licenses.mjs` + `gen:licenses` 脚本，生成 `licenses.json`
- [x] 类型 `LicensesData` 入 types，`data/licenses.ts` 类型化导出
- [x] 测试先行：AppLayout 页脚测试 + LicensesPage 测试（小 fixture）
- [x] 实现 footer + LicensesPage + 路由
- [x] 文档同步：PRD 状态、ARCHITECTURE 前端路由/页面段、CHANGELOG 未发布段

## 5. 验收标准

- `node frontend/scripts/gen-licenses.mjs` 产出 `licenses.json`，含前端全部生产依赖（与 `package.json` dependencies 数量对得上）+ 后端 `go.mod` 直接依赖（剔除 indirect）+ 项目 MIT。
- vitest：
  - 页脚渲染版本号 + 「开源协议」链接（href `/licenses`）。
  - LicensesPage 渲染项目 MIT/作者；渲染前端依赖列表（fixture）；搜索过滤生效；点击展开协议全文；后端无全文依赖渲染外链 `target=_blank`。
- `npx tsc --noEmit` + `npx vitest run` 全量绿；eslint 改动文件干净。
- 真机（用户确认）：页脚显版本 + 链接，点进协议页见前端/后端依赖与项目 MIT、搜索 / 展开可用。

## 6. 风险 / 待定

- 后端协议全文可得性取决于本机 Go module cache 是否已下载该 module；未下载则只给 pkg.go.dev 外链（已纳入设计，可接受）。
- 生成的 `licenses.json` 体积随依赖全文增大，但仅在 `/licenses` 页面按需 import，且随 go:embed 内嵌、运行时不联网，符合约束。
