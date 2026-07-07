# ADR-0052：采用 apps/packages 工作区支持多端演进

## 状态
已被 [ADR-0054](0054-apps-workspace-toolchain-quality-gates.md) 取代

## 背景

当前项目的 Go 服务、前端 Vite 应用、Mock、测试与发布脚本都集中在仓库根目录和 `frontend/` 下。这个结构在早期单 Web 页面阶段足够直接，但随着时间轴重做、组件博物馆、Mock/Benchmark、PWA/桌面壳等需求进入 v2 路线，继续把所有前端和运行端放在一个 `frontend/` 目录会导致边界不清、复用困难、测试基建与产品页面互相挤压。

用户明确要求让当前应用支持多个端，并且可运行目标都放在 `apps/*` 下。目标结构采用：可运行应用放 `apps/*`，可复用能力放 `packages/*`，服务端保持模块化单体，前端/文档站/Mock 基建彼此独立。

## 决策

从 `v0.21.0` 开始，JianVideo 的目标仓库结构采用 `apps/*` + `packages/*`：

```text
apps/
  server/      Go 模块化单体服务，承载 API、媒体库、转码、SQLite 与静态资源服务
  web/         主 Web/PWA 客户端
  museum/      组件博物馆，渲染项目自定义组件、主题与代码片段
  desktop/     可选桌面壳，后续复用 web 构建产物与 API client
packages/
  ui/          共享 UI 组件、布局、媒体卡片、查看器基础件
  media-client/统一 API client、类型与请求封装
  theme/       主题 token、品牌资源、密度、颜色与运行时主题切换
  mock/        MSW handlers、超大数据生成器、Mock 场景
  benchmark/   时间轴/目录/缩略图性能基准与报告工具
```

迁移采用分阶段方式：先落文档与边界，再迁移前端，再拆共享包，最后移动 Go 服务。迁移过程中必须保持现有 API、SQLite 数据、配置、发布包与 `go:embed` 单二进制部署能力兼容。

## 理由

- `apps/*` 能把主 Web、组件博物馆、未来桌面壳分开，避免继续把所有页面、Mock、预览、测试都压在 `frontend/`。
- `packages/*` 能让主题、组件、API client、Mock 和 Benchmark 成为可复用能力，后续多个端不会复制粘贴。
- Go 后端继续保持模块化单体，符合现有轻量部署与架构不变量，不引入微服务。
- 不直接照搬完整 Next.js/Turbo monorepo 形态，避免在没有确认收益前引入新框架和新依赖。
- 先文档后迁移，避免在当前混乱状态下继续叠 UI 修补。

## 后果

- `docs/ARCHITECTURE.md` 在代码实际迁移前仍以当前真貌为准；目标结构由本 ADR 与 `docs/specs/v0.21-v2-restructure.md` 承载。
- 未来移动目录时必须同步更新构建脚本、CI、测试路径、go:embed 路径、README、OPERATIONS 与 API 文档。
- 任何新增端必须先复用 `packages/media-client` 与 `packages/theme`，禁止绕过共享边界直接复制一套 API 调用和主题实现。
- 如果后续决定引入 pnpm workspace、Turbo、Next.js、Wails/Tauri 等新依赖或工具链，必须单独说明收益并经过确认。

## 备选方案

- **继续保留根目录 + `frontend/` 单应用**：短期改动最少，但无法解决组件文档站、多端复用和性能基建边界问题。
- **一次性迁移到完整 Turbo/Next monorepo**：与完整多应用模板更一致，但会同时改变构建、路由、部署和依赖，风险过高。
- **直接上 Canvas/WebGL 重写时间轴**：可能提升滚动性能，但它是渲染层决策，不能替代仓库结构和多端边界治理。
