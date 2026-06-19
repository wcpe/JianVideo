# 功能规格：前端完善 + Mantine UI + 全功能对接

> 状态：开发中 · 关联 PRD：FR-01, FR-13, FR-14, FR-16 · 分支：feature/frontend-mantine

## 1. 背景与目标

当前前端只有空壳页面（登录页、媒体库列表、播放页），UI 为手写 Tailwind 原子类，不统一、不美观。需要：
1. 引入 Mantine UI 组件库，统一视觉风格
2. 完善所有页面功能，完整对接后端 API
3. 修复路由模式不一致（BrowserRouter vs Hash 链接）
4. 补全媒体库管理（添加路径、扫描、删除）
5. 补全播放页（VideoPlayer 接入 HLS）
6. 补全认证流程（登录/登出/状态恢复）

属于第一期（MVP）范围：单用户认证（FR-13）、时间轴视图（FR-14）、多目录聚合管理（FR-01）、mpegts.js 播放内核（FR-16）。

## 2. 需求（要什么）

### 范围内
- 安装 Mantine UI + 暗色主题配置
- 全局布局：顶部导航栏（Logo + 导航 + 用户信息 + 退出）
- 登录页：表单验证、错误提示、登录后跳转
- 媒体库管理页：
  - 左侧面板：路径列表 + 添加路径表单 + 扫描/删除按钮
  - 主区域：媒体文件卡片网格 + 搜索 + 分页 + 排序
  - 点击卡片进入播放页
- 视频播放页：
  - VideoPlayer 组件接入 HLS 流
  - 返回按钮 + 媒体信息展示
- 路由守卫：未登录跳转登录页
- 认证状态恢复：页面刷新时从 localStorage 恢复 token
- 路由统一使用 react-router-dom（BrowserRouter）

### 不做（范围外）
- SMB 网络共享支持（P2）
- 自适应码率 ABR（P2）
- 文件目录视图（P2）
- 双进度条（P2）
- 外挂字幕渲染（P2）
- PWA（P3）
- 多语言（P3）

## 3. 设计（怎么做）

### 3.1 技术选型
- **Mantine UI**：React 组件库，内置暗色主题、表单、通知等，与 React 19 兼容
- **@tabler/icons-react**：Mantine 推荐的图标库
- **zustand**：轻量状态管理（已安装，用于认证状态）
- **axios**：HTTP 客户端（已安装，用于 API 请求 + 认证拦截）
- **react-router-dom**：路由（已安装，BrowserRouter 模式）

### 3.2 文件结构
```
frontend/src/
  api/
    client.ts          — axios 实例 + 认证拦截器
    auth.ts            — 认证 API
    library.ts         — 媒体库 API
  stores/
    auth.ts            — zustand 认证 store
  pages/
    LoginPage.tsx      — 登录页
    LibraryPage.tsx    — 媒体库管理页
    PlayPage.tsx       — 视频播放页
  components/
    AppLayout.tsx      — 全局布局（Mantine AppShell）
    VideoPlayer.tsx    — mpegts.js 播放器（已有，不改）
  types/
    index.ts           — 共享类型
  App.tsx              — 路由配置
  main.tsx             — 入口（包裹 MantineProvider）
```

### 3.3 架构决策
- MantineProvider 在 main.tsx 层包裹，全局暗色主题
- 认证状态用 zustand + localStorage 持久化
- API 拦截器在 axios 层统一处理 401
- 路由守卫用 ProtectedRoute 组件包裹

## 4. 任务拆分
- [ ] 安装 Mantine UI + 图标库依赖
- [ ] 改造 main.tsx：包裹 MantineProvider + 暗色主题
- [ ] 创建 types/index.ts（共享类型）
- [ ] 创建 api/client.ts（axios + 拦截器）
- [ ] 创建 api/auth.ts + api/library.ts（API 封装）
- [ ] 创建 stores/auth.ts（zustand 认证 store）
- [ ] 创建 components/AppLayout.tsx（Mantine AppShell）
- [ ] 创建 pages/LoginPage.tsx
- [ ] 创建 pages/LibraryPage.tsx
- [ ] 创建 pages/PlayPage.tsx
- [ ] 改造 App.tsx（路由 + 守卫）
- [ ] 删除 pages/TimelinePage.tsx
- [ ] 更新 index.css（移除旧样式，保留 CSS 变量）
- [ ] 构建验证（npm run build + go build）
- [ ] 文档同步：PRD 状态、ARCHITECTURE、API、CHANGELOG

## 5. 验收标准
- AC-1：`npm run build` 成功，无 TypeScript 错误
- AC-2：`go build` 成功，前端产物嵌入 exe
- AC-3：服务启动无 panic，`/` 返回含 Mantine 样式的 HTML
- AC-4：登录页渲染正常，输入用户名密码可登录
- AC-5：媒体库页可添加路径、扫描、查看媒体列表
- AC-6：点击媒体卡片可进入播放页，VideoPlayer 正常渲染
- AC-7：未登录访问 /library 或 /play/:id 跳转 /login
- AC-8：刷新页面后认证状态保持

## 6. 风险 / 待定
- Mantine UI 与 React 19 的兼容性需验证
- mpegts.js 在 Mantine 暗色主题下的样式需适配
- 后端 API 返回 500（数据库查询错误）时前端需优雅降级
