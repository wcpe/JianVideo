# 功能规格：API client 与多端基础

> 状态：已实现待发布　·　关联 PRD：FR2-006　·　阶段：P1 `0.22.x`　·　分支：`feature/fr2-006-api-client-multiend`

## 1. 背景与目标

v2 是自托管、视频优先、多端复用的 AI 媒体中心。P0.5 已在 [ADR-0054](../adr/0054-apps-workspace-toolchain-quality-gates.md) 冻结 `apps/*` + `packages/*` 工作区、前端技术栈和 mock 先行方式，其中 `packages/media-client` 被定为 API client、TanStack Query keys、Space 上下文和任务状态类型的统一归属。

进入 P1 做 mockup、UI 博物馆和 PixiJS 原型时，需要一层统一的媒体数据访问基础，供 Web/Desktop/Mobile/TV/车机复用，而不是每端各写一套请求、缓存和状态类型。此时真实后端尚未落地（P2 才有存储库与索引），因此本能力**对着 MSW mock 先行**，先把 client 契约、query key 约定、领域类型和端能力检测定稳。

目标：把 `packages/media-client` 建成多端共享的数据访问层，对 `packages/mock` 跑通全部读写路径，不引入对真实后端的依赖。

## 2. 需求（要什么）

- 统一 API client：请求封装、错误规范化、鉴权（承接现单用户 Cookie + JWT，预留 Space 上下文头）、超时与重试策略。
- TanStack Query keys 规范：统一的 query key 工厂、失效策略、分页（游标/页码）约定、任务轮询模式。
- 任务状态类型：对齐 [ADR-0055](../adr/0055-task-queue-persistence.md) 通用任务队列的任务模型（任务类型、状态枚举 `pending`/`running`/`succeeded`/`failed`/`canceled`、优先级、进度、错误信息、Space 归属）。
- Space 上下文：对齐 [ADR-0056](../adr/0056-space-permission-model.md) 的 Space 归属，client 携带当前 Space 维度参与请求，P1 默认单一 Space。
- 端能力检测：端平台（Web/Desktop/Mobile/TV/车机）、触控、网络能力探测，供多端复用，与 `packages/theme` 协同做密度与端适配。
- 全部对 `packages/mock` 的 MSW handlers 跑通；浏览器 worker 与测试 server 共用同源 handlers。
- 范围内：client 核心、query key 工厂、领域类型（media/task/space）、端能力检测、对 mock 的读写与轮询链路、在 `apps/wiki` 演示用法。
- 不做（范围外）：真实后端接入（P2）、具体页面与 PixiJS 渲染（P4）、真实多端应用壳（P7）、完整 Space 角色权限校验（P5，本阶段只携带 Space 维度不做多角色）。

## 3. 设计（怎么做）

实现在 `packages/media-client`，模块划分：

- **client 核心**：单一请求入口，统一 base URL、超时、重试与错误规范化（把 HTTP 状态与后端错误体映射为统一 `ApiError`）。鉴权承接现单用户，预留 Space 上下文（携带当前 `space_id` 维度）。
- **query keys**：集中的 query key 工厂与失效策略，统一分页与任务轮询模式，供各端 TanStack Query 复用，避免 key 散落。
- **领域类型**：`media`（列表/详情/分页）、`task`（对齐 [ADR-0055](../adr/0055-task-queue-persistence.md) 任务模型）、`space`（对齐 [ADR-0056](../adr/0056-space-permission-model.md) Space 归属）三组类型与查询函数。
- **端能力检测**：探测端平台、触控、网络能力，输出统一能力对象，供 `packages/theme` 做密度与端适配。

边界（依 [ADR-0054](../adr/0054-apps-workspace-toolchain-quality-gates.md)）：

- 服务端状态（请求、缓存、失效、分页、任务轮询）走 TanStack Query；`media-client` 提供 query 函数与 key，不自建缓存层。
- 客户端 UI 状态（选择态、布局态）留给 Zustand，不进 `media-client`。
- 所有请求默认打到 MSW（`packages/mock`）；client 不感知 mock 与真实后端差异，P2 接入真实后端时只换端点实现，不改各端调用。
- 主题与端适配的落地在 `packages/theme`；`media-client` 只提供端能力检测原语，不做主题渲染。
- `apps/web` 与其它端复用同一 `media-client`，不各写请求层；`apps/wiki` 展示用法样例。

## 4. 任务拆分

- [x] 建立 `packages/media-client` client 核心：请求封装、错误规范化、超时、鉴权与 Space 上下文承载。
- [x] 补齐 `packages/media-client` 重试策略。
- [x] 定义 query key 工厂与失效策略，统一分页与任务轮询模式。
- [x] 定义 media/task/space 领域类型与查询函数，task 对齐 ADR-0055、space 对齐 ADR-0056。
- [x] 实现端能力检测（端平台/触控/网络），与 `packages/theme` 约定协同接口。
- [x] 对 `packages/mock` MSW handlers 跑通媒体列表/详情/分页/任务轮询/Space 切换全链路。
- [x] 在 `apps/wiki` 增加 client 用法样例（列表、详情、分页、任务轮询、Space 切换）。
- [x] 接入 `tsc --noEmit` 与 ESLint strict-type-checked 门禁并全绿。
- [x] 文档同步：PRD 状态、ARCHITECTURE、API、CHANGELOG。

## 5. 验收标准

- client 对 MSW mock 能跑通媒体列表、详情、分页、任务轮询和 Space 上下文切换。
- query keys 有统一约定，且能按约定失效对应查询（如切换 Space 后失效并重取列表）。
- media/task/space 领域类型与 ADR-0055 任务模型、ADR-0056 Space 归属一致，任务状态枚举与进度字段可被消费。
- 端能力检测能区分端平台/触控/网络能力，并被 `packages/theme` 协同消费。
- `tsc --noEmit` 与 ESLint strict-type-checked 在 `packages/media-client` 全绿。
- `apps/wiki` 能演示 client 用法样例（列表/详情/分页/任务轮询/Space 切换）。
- 全程无真实后端依赖：断开真实后端仍能对 mock 跑通全部验收项。

## 6. 风险 / 待定

- P1 只对 mock 跑通，client 契约与真实后端可能有偏差；P2 接入真实后端时需回归本 spec 的契约，偏差项走取代 spec 或 ADR。
- 分页方式（游标 vs 页码）与任务轮询间隔需在 mock 阶段定稳，避免各端各写；若与后端索引（FR2-007）冲突需再对齐。
- Space 上下文 P1 默认单一 Space，多角色权限校验在 P5（ADR-0056）；本阶段只携带 Space 维度，不得提前实现角色矩阵。
- 端能力检测与 `packages/theme` 的协同接口边界需明确，避免能力探测逻辑在两个包重复。
