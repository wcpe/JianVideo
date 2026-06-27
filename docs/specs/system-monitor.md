# 功能规格：系统监控页（System Monitor）

> 状态：开发中　·　关联 PRD：FR-119　·　分支：feature/system-monitor

## 1. 背景与目标

现有 `/system` 只给运行时**瞬时**信息（内存/GC/运行时长），看不到「随时间的变动」。本特性新增 `/monitor` 系统监控页，展示 CPU / 内存 / 磁盘 / 转码并发的**当前值 + 时序折线**（range 1h/24h/7d）。后端新增「定时采样器 + SQLite 样本表 + 保留期 + 下采样端点」机制（见 ADR-0044）。前端复用 FR-118 的 Recharts `TrendChart`。属第十二期、本批最重的一块。

## 2. 需求（要什么）

- 新增 `/monitor` 页，导航「系统」组（紧挨「系统」诊断）。
- 指标：**CPU%（系统）/ 内存（进程已用，复用 NFR-04 口径）/ 磁盘（数据盘已用/总量）/ 转码并发（活跃会话数）**，外加 goroutine 数。
- 每指标：当前值卡（`MetricCard`）+ 时序折线（`TrendChart`），range 选择 1h / 24h / 7d。
- 后端定时采样落 SQLite、保留期裁剪、`GET /api/system/metrics?range=` 下采样返回时序 + 当前快照。

- 范围内：上述采样机制 + 端点 + `/monitor` 页 + gopsutil 采集。
- 不做（范围外）：每库磁盘明细（监控页只看数据盘整体）、磁盘 I/O 吞吐（只看用量）、告警/通知、可配置采样周期（用固定常量，YAGNI）。

## 3. 设计（怎么做）

### 3.1 依赖

引入 `github.com/shirou/gopsutil/v4`（cpu / mem / disk）——用户已批准，见 ADR-0044。非架构禁用的重型件。

### 3.2 后端：采样机制

- **数据模型** `internal/db/models/metric_sample.go`：
  ```
  MetricSample {
    ID int64
    SampledAt time.Time   // 索引；采样时刻（UTC 存储）
    CPUPercent float64     // 系统 CPU 使用率 %
    MemUsedBytes int64     // 进程已用内存（runtime MemStats Alloc）
    MemSysBytes int64      // 进程向 OS 申请内存（Sys）
    DiskUsedBytes int64    // 数据盘已用
    DiskTotalBytes int64   // 数据盘总量
    TranscodeActive int    // 活跃转码/播放会话数
    Goroutines int         // goroutine 数
  }
  ```
  在 `internal/db` 现有 AutoMigrate 列表注册该模型。
- **采样服务** `internal/metrics`（新包）：
  - `Sampler`：`time.Ticker` 周期 `SampleInterval = 15 * time.Second`（常量）采样一次：CPU 用 `gopsutil/cpu.Percent(0, false)`（非阻塞、返回距上次调用的均值；首次可能为 0）、内存用 `runtime.ReadMemStats`（Alloc/Sys，与 system/info 口径一致）、磁盘用 `gopsutil/disk.Usage(dataDir)`、转码并发用**注入的 `func() int`**（构造时由播放/转码服务提供活跃会话数）、goroutine 用 `runtime.NumGoroutine()`。写入一行。
  - **保留期** `Retention = 7 * 24 * time.Hour`（常量）：每次采样后（或每 N 次）`DELETE FROM metric_samples WHERE sampled_at < ?`，防表无限增长。
  - **生命周期**：随服务启动 `Start(ctx)`（起 goroutine + ticker）、随关闭 `Stop()`（ctx 取消 / ticker 停），不泄漏 goroutine。由主入口装配（采样器构造时注入 db 与转码计数 provider）。
  - 架构红线：采样逻辑在 `metrics` 服务层；`db` 仅 `metric_samples` 读写、不写业务逻辑；依赖方向 `metrics → db` 单向。
- **查询/下采样** `GetMetrics(range)`：按 range 选窗口与桶大小下采样，保证返回点数有界（≤ ~300）：
  - `1h` → 桶 60s（窗口近 1 小时）；`24h` → 桶 300s；`7d` → 桶 1800s。缺省 `24h`。
  - SQL：`WHERE sampled_at >= ?` + 按 `(unixepoch(sampled_at) / 桶秒)` 分桶 `GROUP BY` + `AVG(cpu_percent)`、`AVG(mem_used_bytes)`、`AVG(mem_sys_bytes)`、`MAX(transcode_active)`、`AVG(goroutines)`、各桶最后一条的 disk（或 `MAX`）。升序。
- **端点** `GET /api/system/metrics?range=1h|24h|7d`（`internal/api`，`/api/system` 组）：
  ```json
  {
    "range": "24h",
    "points": [
      {"t": "2026-06-27T10:00:00Z", "cpu_percent": 38.2, "mem_used_bytes": 195000000, "mem_sys_bytes": 536870912, "disk_used_bytes": 1979900000000, "disk_total_bytes": 2600000000000, "transcode_active": 2, "goroutines": 120}
    ],
    "current": {"cpu_percent": 41.0, "mem_used_bytes": 198000000, "disk_used_bytes": 1979900000000, "disk_total_bytes": 2600000000000, "transcode_active": 2, "goroutines": 122}
  }
  ```
  `current` = 最新一条原始样本（≤15s 旧，作当前值卡）。无样本（刚启动）返回 `points: []`、`current: null`，HTTP 200。

### 3.3 前端

- `frontend/src/api/metrics.ts`：`getSystemMetrics(range): Promise<SystemMetrics>`，mock/real 双实现（仿 stats.ts）。类型 `SystemMetrics { range; points: MetricPoint[]; current: MetricPoint | null }`、`MetricPoint { t; cpu_percent; mem_used_bytes; mem_sys_bytes; disk_used_bytes; disk_total_bytes; transcode_active; goroutines }`。
- `frontend/src/pages/MonitorPage.tsx`：range 选择（`SegmentedControl` 1h/24h/7d）+ 当前值卡（CPU%/内存/磁盘/转码并发，用 `MetricCard`，磁盘显已用/总量 + 百分比）+ 时序折线（CPU、内存、转码并发用 `TrendChart`；磁盘用量趋势可选）。复用 FR-118 的 `TrendChart`/`MetricCard` 与 `utils/format` 的 `formatBytes`。**懒加载**（`lazy()`）使 recharts 落该页 chunk（与 StatsPage 一致，主包不超 PWA 预缓存上限）。
- 进入页轮询刷新（如每 15s 重拉当前 range），离开停轮询；失败静默降级（沿用既有 silent 轮询惯例，不弹 toast 堆积）。
- 路由 `App.tsx`：新增 `/monitor` → `MonitorPage`（ProtectedRoute+AppLayout）。导航 `AppLayout`：「系统」组加「监控」项（`IconActivity`，置于「系统」诊断前/后）。

### 3.4 架构决策

见 ADR-0044（系统指标采样与持久化：gopsutil 采集 + SQLite 样本表 + 保留期）。

## 4. 任务拆分

- [ ] `go get github.com/shirou/gopsutil/v4` + ADR-0044
- [ ] `models.MetricSample` + 注册 AutoMigrate
- [ ] `internal/metrics` 采样器（采样/保留期/生命周期）+ `GetMetrics` 下采样
- [ ] 主入口装配采样器（注入 db + 转码计数 provider，启动/关闭）
- [ ] 端点 `GET /api/system/metrics`
- [ ] 后端测试（含高风险区，见 §5）
- [ ] 前端 `api/metrics.ts` + 类型 + `MonitorPage` + 路由 + 导航「监控」+ 测试
- [ ] 文档同步：PRD（AC-23）、ADR-0044、API.md、ARCHITECTURE、CHANGELOG

## 5. 验收标准

- **AC-23**（见 PRD §6）：`/monitor` 展示 CPU/内存/磁盘/转码并发当前值卡 + 时序折线，range 1h/24h/7d 可切；折线可 hover 看精确值；刚启动无样本时显占位不报错。`GET /api/system/metrics?range=` 返回下采样 points + current 快照。
- 后端测试（**含高风险区**，见 `testing-and-quality.md` §2）：
  - **SQLite WAL 并发读写**：采样写入与 `GetMetrics` 查询并发不冲突。
  - **保留期裁剪**：超 `Retention` 的样本被删、表不无限增长。
  - **采样器生命周期**：`Start`/`Stop` 不泄漏 goroutine（`Stop` 后 ticker 停、goroutine 退出）。
  - **下采样**：各 range 桶大小正确、点数有界、AVG/MAX 聚合正确、空样本返回空 points。
  - 转码计数 provider 注入正确反映活跃会话数。
- 受影响前端跑生产构建（`npm run build`，监控页懒加载、主包不超限）+ vitest 全绿。
- 视觉走查（折线/暗色/响应式/真实采样数据滚动）待真机。

## 6. 风险 / 待定

- gopsutil `cpu.Percent(0,false)` 首次返回 0 / 距上次调用均值：采样器持续周期调用即为「上个周期的系统 CPU%」，符合监控语义；首样本可能为 0，可接受。
- 磁盘指 `dataDir` 所在文件系统的整体用量（db/缩略图所在盘），非各媒体库盘明细（YAGNI，留后续）。
- 采样周期 15s × 保留 7 天 ≈ 4 万行，下采样查询走 `sampled_at` 索引；保留期裁剪保证有界，满足规模与性能。
- 采样器是后台 goroutine：必须随服务关闭干净退出，测试覆盖，避免泄漏（high-risk）。
