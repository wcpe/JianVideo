# ADR-0019：前端项目初始化与 mpegts.js 播放器组件实现

## 状态
已接受

## 背景
FR-01 创建了 Go 后端项目骨架，但前端项目目录 `frontend/` 尚未初始化。FR-16 要求实现基于 mpegts.js 的 TS 流播放内核，需要先初始化前端项目再编写播放器组件。同时 FR-14/15 也在并行开发前端视图层，可能共享 `App.tsx` 等文件。

## 决策
1. 在 `frontend/` 下初始化独立的 Vite + React + TypeScript 项目
2. 使用 shadcn/ui + Tailwind CSS v4 作为 UI 组件库和样式方案
3. 将播放器组件放在 `frontend/src/components/VideoPlayer.tsx`，作为可复用组件供 FR-14/15 的视图引用
4. 使用 `mpegts.js` v1.8.0 作为 TS 流播放内核，严格配置指定的播放参数

## 理由
- **独立前端项目**：前端拥有独立的 `package.json`、`tsconfig.json`、`vite.config.ts`，与后端 Go 项目解耦，开发期可独立运行
- **shadcn/ui + Tailwind CSS v4**：项目技术栈要求，shadcn/ui 提供无头组件，Tailwind CSS v4 提供原子化样式
- **组件独立放置**：`VideoPlayer.tsx` 作为独立组件，不耦合特定视图，可被 FR-14（时间轴视图）和 FR-15（文件目录视图）共同引用
- **mpegts.js 指定配置**：`enableWorker` 避免主线程阻塞，`enableStashBuffer` + `stashInitialSize=1MB` 提供 3-5 秒追播延迟，`accurateSeek` + `seekType='range'` 实现精准 Seek

## 后果
- 前端需要独立的 `npm install` 和 `npm run build` 步骤
- 后端 `go:embed` 需要嵌入 `frontend/dist/` 目录
- FR-14/15 同时开发前端视图层，可能产生文件冲突

## 备选方案
- **共用已有前端项目**：不适用，FR-01 未创建前端目录
- **不使用 shadcn/ui**：纯手写 UI 组件会增加开发时间，不符合项目技术栈要求
- **使用 video.js + mpegts.js 插件**：增加不必要的抽象层，直接使用 mpegts.js 更简洁
