# 功能规格：存储库卡片网格布局（FR-65）

> 状态：已完成（待发版）　·　关联 PRD：FR-65　·　分支：feature/fr-66-directory

## 1. 背景与目标

存储库列表此前用 `Stack` 单列铺满，每卡把「信息 + 浏览/增量/全量/删除」挤在一排（`Group justify="space-between"`），FR-64 又在卡内加了后缀管理，单列下横向更挤。本 FR 把列表改为一行 2-3 个的卡片网格，并重排卡内结构使窄列下操作按钮不拥挤（P7，扩 FR-23）。

## 2. 需求（要什么）

- 存储库列表由单列 `Stack` 改为 `SimpleGrid` 一行 2-3 个（`cols={{ base:1, sm:2, lg:3 }}`）。
- 重排卡内结构：窄列下操作按钮不挤（信息与按钮纵向堆叠、按钮可换行）。
- 与 FR-64 后缀管理 UI 协调（同卡片内，先 FR-64 再 FR-65）。
- 不做（范围外）：卡片缩略图/封面；操作收进下拉菜单（本期用换行堆叠即可，避免镀金）。

## 3. 设计（怎么做）

纯前端布局重组，无数据/接口/架构改动，无需 ADR。

- `LibraryPathManager` 外层 `Stack` → `SimpleGrid cols={{ base:1, sm:2, lg:3 }} spacing="sm"`。
- 卡内结构由「信息 + 按钮同排」改为纵向：库信息行（可点进浏览）在上，操作按钮组（浏览/增量/全量/删除）在下并 `wrap="wrap"`，再下为 FR-64 后缀管理区。删除按钮补 `aria-label` 便于定位。

## 4. 任务拆分

- [x] 前端：`LibraryPathManager` `Stack`→`SimpleGrid` + 卡内纵向重排（按钮换行）
- [x] 文档同步：PRD 状态、ARCHITECTURE、CHANGELOG
- [x] vitest 断言网格渲染（`SimpleGrid` 容器 + 卡数）与既有交互不回归

## 5. 验收标准

- 前端 `npx tsc --noEmit` + `npx vitest run` 全绿；新增测试断言渲染为 `mantine-SimpleGrid-root` 网格、卡数正确、浏览/增量/全量/删除交互仍在。
- 真机维度：一行 2-3 个网格的实际视觉与窄屏自适应属界面行为，标「待真机验」。

## 6. 风险 / 待定

- 主题背景守护测试（theme-bg-token）依赖卡片仍用 `var(--mantine-color-default)`，重排后保持不变（已验证不回归）。
