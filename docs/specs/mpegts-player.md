# 功能规格：mpegts.js 播放内核

> 状态：开发中　·　关联 PRD：FR-16　·　分支：feature/mpegts-player

## 1. 背景与目标

前端需要播放后端实时转码输出的 MPEG-TS 流。原生 `<video>` 标签无法直接播放裸 TS 流，必须通过 MSE（Media Source Extensions）API 手动将 TS 数据送入浏览器解码管线。

本功能属于第一期（MVP），是 FR-06（TS 流强制输出）和 FR-17（边下边播）的前端配套组件——没有播放器内核，后端输出的 TS 流无法在浏览器中播放。

## 2. 需求（要什么）

- 前端播放器组件使用 `mpegts.js` 库创建 TS 播放器实例，通过 MSE API 播放 TS 流
- 禁止原生 `<video>` 标签**直接处理 TS 流**（HLS 在 Safari 原生播放也不作为主路径）。
  注：对**不需要转码、不走 TS**的直出场景（如 H.264+AAC 的 MP4 经 `/api/play/:id/stream` 输出，FR-05 兜底路径），允许使用原生 `<video>` 直出（VideoPlayer 通过 `streamType='mp4'` 切换）；mpegts.js 仅约束 TS/HLS 转码流。
- 播放器配置：
  - `enableWorker: true`（在 Web Worker 中解析 TS，避免阻塞主线程）
  - `enableStashBuffer: true`（启用内部缓冲，应对网络抖动）
  - `stashInitialSize: 1024 * 1024`（1MB 初始缓冲，约 3-5 秒追播延迟）
  - `accurateSeek: true`（精确 Seek 到目标位置）
  - `seekType: 'range'`（使用 HTTP Range 请求进行 Seek）
- 播放器组件暴露播放控制接口：播放/暂停、Seek、音量控制
- 组件挂载时创建并初始化 mpegts.js 实例，卸载时销毁并释放 MSE 资源
- 支持传入 `url` prop 动态切换播放源

**范围内**：
- mpegts.js 播放器组件的创建、配置、生命周期管理
- 基础播放控制 UI（播放/暂停按钮、进度条、音量）

**不做（范围外）**：
- ABR 多码率切换（P2）
- 外挂字幕渲染（P2）
- 双进度条（P2）
- 倍速播放、画质选择等高级功能（后续迭代）

## 3. 设计（怎么做）

### 3.1 组件位置

`frontend/src/components/VideoPlayer.tsx` — 独立播放器组件，可被 FR-14/15 的视图页面引用。

### 3.2 技术方案

```
VideoPlayer.tsx
  ├── <video> 元素（仅作为 mpegts.js 的渲染目标，不设置 src）
  ├── mpegts.Player 实例
  │     ├── enableWorker: true
  │     ├── enableStashBuffer: true
  │     ├── stashInitialSize: 1048576  (1MB)
  │     ├── accurateSeek: true
  │     └── seekType: 'range'
  └── 播放控制 UI
        ├── 播放/暂停按钮
        ├── 进度条（可拖拽 Seek）
        └── 音量控制
```

### 3.3 生命周期

1. **挂载**：创建 `<video>` DOM 元素 → 实例化 `mpegts.Player` → 绑定 video 元素 → 加载 URL → `player.play()`
2. **更新**：URL 变化时 `player.unload()` → 重新 `player.load()` → `player.play()`
3. **卸载**：`player.pause()` → `player.unload()` → `player.destroy()` → 清理引用

### 3.4 依赖引用

- 架构决策：[ADR-0004](adr/0004-mpegts-js-player.md) — 选择 mpegts.js 作为播放内核
- 新增 ADR：[ADR-0019](adr/0019-mpegts-player.md) — 前端项目初始化与播放器组件实现细节

## 4. 任务拆分

- [x] 初始化前端项目（Vite + React + TypeScript + shadcn/ui + Tailwind CSS v4）
- [x] 安装 mpegts.js 依赖
- [x] 编写 VideoPlayer.tsx 组件
- [x] 编写组件测试（红→绿验证）
- [x] 文档同步：PRD 状态、ARCHITECTURE、CHANGELOG

## 5. 验收标准

- **AC-16-1**：VideoPlayer 组件可渲染，挂载时创建 mpegts.js Player 实例并绑定到 video 元素（单元测试验证）
- **AC-16-2**：播放器配置严格匹配指定值：`enableWorker=true`、`enableStashBuffer=true`、`stashInitialSize=1048576`、`accurateSeek=true`、`seekType='range'`（单元测试验证）
- **AC-16-3**：组件卸载时调用 `player.destroy()` 释放 MSE 资源（单元测试验证）
- **AC-16-4**：URL 变化时播放器正确 unload 并重新 load 新 URL（单元测试验证）
- **AC-16-5**：播放控制 UI 包含播放/暂停按钮、可拖拽进度条、音量控制（渲染验证）
- **手动验收**：启动后端转码服务，在浏览器中打开播放器页面，确认 TS 流可正常播放、Seek 响应正常

## 6. 风险 / 待定

- **FR-14/15 并发开发**：可能同时修改 `frontend/src/App.tsx`，预期会产生 rebase 冲突，通过协调解决
- **mpegts.js 类型声明**：`mpegts.js` 自带 TypeScript 类型声明，无需额外 `@types` 包
- **MSE 兼容性**：所有现代浏览器均支持 MSE，但需确保 video 元素的 `src` 不被设置（由 mpegts.js 内部通过 `MediaSource` 管理）
