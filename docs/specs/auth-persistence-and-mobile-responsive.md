# 规格：认证状态持久化 + 移动端响应式

> 状态：修复中 · 关联 PRD：FR-13（单用户认证）、FR-14/FR-15（前端视图） · 类型：Bug 修复

## 1. 背景与目标

两个已交付功能的缺陷需要修复：
- **刷新页面退出登录**：FR-13 单用户认证已交付，但页面刷新后 Zustand store 丢失认证状态，`init()` 从未被调用，导致用户需要重新登录
- **手机端导航不可用**：FR-14/FR-15 前端视图已交付，但移动端（< 768px）侧边栏自动隐藏后没有汉堡菜单按钮，手机端无法访问导航

## 2. 需求（要什么）

### Bug 1: 认证状态持久化
- 页面刷新后，应用应从 `localStorage` 恢复认证状态
- 如果后端 cookie 已过期（`getMe()` 返回 401），应清除 `localStorage` 并跳转登录页
- 如果后端 cookie 有效，应恢复 `isAuthenticated` 和 `username` 状态

### Bug 2: 移动端响应式
- 手机端（< 768px）侧边栏默认折叠
- Header 左侧显示汉堡菜单按钮，点击展开/收起侧边栏
- 展开侧边栏时主内容区不被遮挡

## 3. 设计（怎么做）

### Bug 1 修复
- 在 `AppLayout.tsx` 中添加 `useEffect(() => { init() }, [])`，应用挂载时恢复认证状态
- `init()` 已在 `auth.ts` 中实现，逻辑完整：检查 localStorage → 调 getMe → 恢复/清除状态
- 无需修改 `init()` 本身，只需确保它被调用

### Bug 2 修复
- 使用 Mantine `AppShell` 的 `navbar` 配置：`opened` / `onToggle` 状态控制
- 在 Header 添加 `Burger` 按钮（`@mantine/core`），小屏幕显示
- 使用 `useMantineMediaQuery` 或内联判断控制按钮显示/隐藏

## 4. 任务拆分
- [x] 创建规格文档 `docs/specs/auth-persistence-and-mobile-responsive.md`
- [ ] 修复 `AppLayout.tsx`：添加 `init()` 调用
- [ ] 修复 `AppLayout.tsx`：添加移动端汉堡菜单
- [ ] 更新 PRD FR-13 验收标准
- [ ] 更新 CHANGELOG

## 5. 验收标准
- 登录后刷新页面，右上角仍显示用户名，不跳转登录页
- 后端 cookie 过期后刷新页面，自动跳转登录页
- 手机端（Chrome DevTools 模拟 375px）侧边栏默认隐藏
- 点击汉堡菜单按钮可展开/收起侧边栏

## 6. 风险 / 待定
- 无架构变更，仅修改前端布局组件和初始化逻辑
