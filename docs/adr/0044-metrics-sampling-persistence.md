# ADR-0044：系统指标采样与持久化（gopsutil 采集 + SQLite 样本表 + 保留期）

## 状态

已接受

## 背景

系统监控页（FR-119）需要展示 CPU / 内存 / 磁盘 / 转码并发**随时间的趋势**与当前值。现状有两个缺口：

1. **拿不到指标**：Go 标准库给不了系统 CPU 使用率与磁盘用量（`runtime` 只有进程内存/GC/goroutine）。
2. **不存历史**：`/api/system/info` 只返回**瞬时**运行时信息，无任何时序持久化——刷新即丢，画不出趋势。

需要决定「怎么采集」与「怎么存历史」，且不得违反架构不变量（禁 Redis / 时序数据库等重型件，SQLite 为唯一持久化）。

## 决策

- **采集**：引入 `github.com/shirou/gopsutil/v4` 采集系统 CPU% 与磁盘用量；进程内存/goroutine 仍用标准库 `runtime`；转码并发由播放/转码服务注入的计数 provider 提供。
- **持久化**：新增 `internal/metrics` 采样服务——后台 `time.Ticker` 按固定周期（15s）采样一行写入 SQLite `metric_samples` 表；按固定保留期（7 天）裁剪旧样本防表膨胀；采样器随服务生命周期启停。
- **查询**：`GET /api/system/metrics?range=1h|24h|7d` 按 range 选窗口与桶大小**下采样**（GROUP BY 时间桶 + AVG/MAX）返回时序，点数有界（≤ ~300）。
- 采样只落 SQLite，不引时序数据库 / Redis；采样器独立 `metrics` 服务层、`db` 仅 `metric_samples` 读写，依赖方向 `metrics → db` 单向。

## 理由

- **gopsutil**：跨平台（Windows/Linux/macOS）、成熟、纯 Go 友好，一个库拿齐 CPU/内存/磁盘，省去各平台手写 syscall 的维护负担；非架构禁用的重型基础设施件，仅需依赖审批（已获用户批准）。
- **SQLite 存样本**：复用现有唯一持久化、零新增基础设施，符合「简单优先 / 单一持久化」不变量；保留期裁剪 + 下采样保证表与响应都有界。
- **固定周期/保留期常量**：YAGNI——当前无需可配置，留常量即可，需要时再外置。

## 后果

- 新增后端依赖 `gopsutil/v4`（及少量子依赖）。
- 新增后台采样 goroutine：必须随服务关闭干净退出（`Stop` 停 ticker、goroutine 退出），否则泄漏——纳入测试高风险区。
- 新增 `metric_samples` 表（注册进现有 AutoMigrate）；保留期裁剪防膨胀。
- 采样写入与端点查询并发：SQLite WAL 保证并发读写安全，纳入测试高风险区。
- 转码并发计数需播放/转码服务暴露一个只读计数，经 provider 注入采样器，避免 `metrics` 反向依赖业务服务。

## 备选方案

- **x/sys 手写平台代码**：零新增依赖（x/sys 已在间接依赖），但需为 Windows/Linux 各写磁盘与进程 CPU 的 syscall 代码，系统级 CPU% 尤其难，维护成本高。落选（gopsutil 更省、更稳）。
- **时序数据库（Prometheus/InfluxDB 等）**：违反「禁重型件 / SQLite 唯一持久化」架构不变量。落选。
- **不持久化、前端实时采样**：页面打开时前端轮询当前值自行累积成曲线——刷新即丢、跨会话无历史、关页即断，达不到「看历史趋势」目标。落选。
