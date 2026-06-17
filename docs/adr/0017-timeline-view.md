# ADR-0017：时间轴视图前端架构

## 状态
已接受

## 背景
FR-14 需要实现时间轴视图，作为媒体库的主要浏览界面。后端已在 FR-01 中完成媒体文件列表 API（支持排序、分页），前端需要消费该 API 并以信息流形式展示。

## 决策
采用纯前端分页方案，前端通过 axios 调用后端 `GET /api/library/media` 接口，使用 zustand 管理列表状态（items、total、page、sort），Tailwind CSS v4 构建 UI 组件。

## 理由
- 后端 API 已完整支持排序和分页，前端无需重复实现
- zustand 轻量，适合当前单页应用规模，不引入 Redux 的复杂度
- Tailwind CSS v4 与 Vite 集成良好，构建性能优

## 后果
- 前端项目作为独立 Vite 项目在 `frontend/` 目录维护
- 开发时需独立运行 `npm run dev`（端口 5173），通过代理访问后端 API
- 生产构建产物通过 `go:embed` 内嵌到 Go 二进制

## 备选方案
- **React Query / TanStack Query**：自动缓存和分页管理，但增加额外依赖，当前规模下 zustand + axios 已够用
- **服务端渲染（SSR）**：首屏加载更快，但增加架构复杂度，不符合当前轻量定位
