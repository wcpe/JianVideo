# 变更日志

本项目所有重要变更记录于此。

格式遵循 [Keep a Changelog](https://keepachangelog.com/zh-CN/1.1.0/)，版本号遵循 [语义化版本](https://semver.org/lang/zh-CN/)。

## 未发布版本

### 新增
- 初始化项目结构与 SDD 治理文档
- 多目录聚合管理的目录注册、媒体文件 CRUD API（internal/library + internal/api）
- NVIDIA NVENC 硬件加速检测：通过 FFmpeg C API 检测 CUDA 设备与 h264_nvenc/hevc_nvenc 编码器，支持 H.264 和 H.265 双编码检测
- 单用户认证（JWT + bcrypt）：登录/登出 API、认证中间件、首次启动自动创建默认用户

### 变更
（无）

### 修复
（无）

### 移除
（无）

> 发版时把"未发布版本"段切成 `## [X.Y.Z] - YYYY-MM-DD`，再新建空的"未发布版本"段。
