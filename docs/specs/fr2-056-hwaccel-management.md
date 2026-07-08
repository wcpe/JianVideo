# 功能规格：硬件转码加速管理面板

> 状态：已审核接受　·　关联 PRD：FR2-056　·　阶段：P2 `0.23.x`　·　分支：待定

## 1. 背景与目标

当前系统已有硬件能力探测、SQLite 缓存、系统页展示和强制重测，但“管理”能力尚未落地：用户不能持久选择 QSV/NVENC/AMF/VAAPI/VideoToolbox/GPU，也不能配置 fallback 策略，转码任务执行器也未消费用户选择。

目标：

- 在系统设置中提供硬件转码偏好选择与可视化状态。
- 转码任务按用户偏好选择编码器，失败时按策略 fallback。
- 保持无硬件环境下软件编码可用。

前置依赖：FR2-024（配置 registry）、FR2-037（任务队列）、FR2-040（审计核心）。硬件重测属于系统级操作，系统级任务/审计的 Space 归属需按 FR2-037/FR2-040 的 `scope=system` 或等价 ADR 口径执行。

## 2. 需求（要什么）

- 展示硬件能力探测结果：家族、编码器、codec、是否编译、实测是否可用、测试时间、ffmpeg 版本。
- 用户可选择默认编码策略：自动、软件、NVENC、QSV、AMF、VAAPI、VideoToolbox。
- 可配置 fallback：硬件失败后自动软件重试或直接失败。
- 设置通过 FR2-024 registry 保存，任务执行器读取。
- 强制重测能力保留，重测动作写审计。
- 范围内：设置项、面板 UI、转码执行器消费、fallback 测试。
- 不做（范围外）：跨机器 GPU 调度、驱动安装、GPU 温度/显存监控。

## 3. 设计（怎么做）

配置：

- `transcode_hwaccel_mode`：`auto/software/nvenc/qsv/amf/vaapi/videotoolbox`。
- `transcode_hwaccel_fallback`：bool。

执行器：

- 转码任务开始前读取配置和能力缓存。
- `auto` 选择当前可用优先编码器，优先级与现有 `hardwarePriority` 口径保持一致；指定家族不可用时按 fallback 策略处理。
- 指定 NVENC/QSV/AMF/VAAPI/VideoToolbox 时必须影响 ffmpeg encoder 选择，而不只是设置 `-hwaccel` 解码参数。
- 错误摘要写任务错误，并可触发软件重试。

前端：

- 系统页/设置页展示能力矩阵、当前选择、fallback 开关、重测按钮。
- 使用单选/分段控件和开关，不用自由文本。

## 4. 任务拆分

- [ ] 增加硬件转码设置 registry。
- [ ] 转码执行器消费硬件偏好与 fallback 策略。
- [ ] 系统页增加管理控件与状态说明。
- [ ] 重测动作写审计事件。
- [ ] 补单元测试：编码器选择、fallback、不可用策略。
- [ ] 补集成测试：无硬件环境软件 fallback、缓存能力读取。
- [ ] 补 E2E：选择策略、保存、刷新回读、强制重测。
- [ ] 文档同步：PRD 状态、ARCHITECTURE、API、CHANGELOG。

## 5. 验收标准

- 用户能选择硬件策略并持久化，刷新后回读一致。
- 无硬件环境下选择 auto 能 fallback 到软件编码。
- 指定不可用硬件且关闭 fallback 时，任务失败并返回明确中文错误。
- 指定硬件模式会反映到 ffmpeg encoder 参数；`auto` 优先级与既有硬件优先级一致。
- 强制重测和策略变更写审计事件。
- `go test`、前端测试、Playwright 系统页 E2E、`pnpm run quality` 全绿。
- Go 单二进制 serve 后，软件 fallback 转码任务实跑通过；真实硬件路径按本机能力如实报告。

## 6. 风险 / 待定

- 硬件矩阵跨平台差异大，自动化验收以软件 fallback 为必过；真实 NVENC/QSV/AMF 等按本机环境记录。
- 已确认：本规格只做全局默认硬件策略，不允许按媒体或任务单独覆盖。
