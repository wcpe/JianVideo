# 功能规格：双进度条视觉反馈

> 状态：开发中 · 关联 PRD：FR-20 · 分支：feature/fr-20-dual-progress

## 1. 背景与目标

当前播放器只显示播放进度条，用户无法直观了解缓冲区加载情况。需要同时显示播放进度和缓冲区进度，提升观看体验。

属于第二期（P2）范围。

## 2. 需求

### 范围内
- 使用 Mantine Progress 的 sections 模式显示双进度条
- Section 1（蓝色）：已播放进度
- Section 2（青色）：已缓冲但未播放的进度
- 基于 HTML5 video.buffered（TimeRanges API）计算缓冲区范围
- 在 video 元素上监听 progress 事件触发缓冲区更新
- 无需后端改动

### 不做
- 缓冲区精细化管理（V2）
- 后端缓冲追踪增强（已有 BufferReport 机制）

## 3. 设计

### 3.1 技术选型
- Mantine Progress sections（原生支持多段进度条）
- HTML5 video.buffered + progress 事件（标准 API）

### 3.2 实现细节
- 新增 state: bufferedProgress（0-100 百分比）
- progress 事件处理: 取最后一个 buffered.end / duration * 100
- sections 计算: 播放进度 = currentTime/duration*100，缓冲进度 = bufferedProgress - 播放进度

### 3.3 文件清单
- 修改: frontend/src/components/VideoPlayer.tsx
- 修改: frontend/src/components/VideoPlayer.test.tsx

## 4. 验收标准
- AC-1: 播放视频时进度条显示蓝色（播放）+ 青色（缓冲）两段
- AC-2: 青色区域始终 >= 蓝色区域（缓冲 >= 播放）
- AC-3: 缓冲区数据增加时青色区域实时扩展
- AC-4: TypeScript 编译通过，测试全部通过
