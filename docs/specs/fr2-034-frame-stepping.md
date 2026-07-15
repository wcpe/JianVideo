# 功能规格：逐帧前后步进

> 状态：开发中　·　关联 PRD：FR2-034　·　阶段：P3 `0.24.x`　·　分支：`feature/fr2-034-frame-stepping`

## 1. 背景与目标

视频素材审核、镜头定位和动作观察需要在暂停态按实际画面逐帧前进或后退。仅对 `currentTime` 加减固定秒数会受关键帧、帧率、浏览器 Seek 行为和异步呈现影响，不能证明到达了目标帧，还可能在快速操作时把预期取消误报为 `Network Error`。

本规格依赖 [FR2-036](fr2-036-player-core.md) 的纯控制层与 `PlaybackBackend` 契约。player-core 只定义逐帧命令、目标计算、验证和降级语义；Web 后端继续使用既有 mpegts.js/MSE、hls.js/MSE 与原文件直出内核，并由后端观测实际呈现帧。

目标：

- 暂停态支持向前一帧和向后一帧；P3 仅提供桌面按钮与键盘入口，并在 Windows 桌面 Web/安装态 PWA 验收。
- 精确路径必须到达方向性的相邻源帧：`next` 的最终帧严格晚于起始帧，`previous` 的最终帧严格早于起始帧；编号 fixture 必须证明帧号恰好 `+1` 或 `-1`。
- 支持 `requestVideoFrameCallback` 时，以回调提供的实际 `mediaTime` 和 FR2-036 `PresentedFrame.sourceFrameIndex` 或 `stableFrameId` 验证相邻目标；±1 个源帧时长只作为时间戳容差，不得用于接受原帧、反向帧或跨过多个帧，也不得仅凭 `mediaTime` 推导或证明帧号。
- 不支持该 API、缺少可靠帧时间基准，或无法提供稳定相邻源帧身份时，明确进入“近似逐帧”降级，不虚报帧准确。
- 起点、终点和可 Seek 区间边界行为确定，快速连续操作不触发伪 `Network Error`。

## 2. 需求（要什么）

- 逐帧仅在暂停态执行；若命令来自播放态，Web 壳层须先请求暂停并等待后端确认，再执行步进，完成后保持暂停，不自动续播。
- 支持两个方向：`previous` 与 `next`；非边界的精确命令必须只移动一个源帧，边界夹取可保持原帧，近似路径只承诺方向与名义帧步长而不宣称已证明相邻帧。
- P3 所有入口映射到同一 player-core 命令：
  - 桌面播放器前一帧、后一帧按钮。
  - 桌面播放器聚焦且焦点不在输入控件时的键盘快捷键。
  - Windows 桌面安装态 PWA 复用相同按钮和键盘入口。
- 移动端逐帧手势、触控专用入口和 Android/iOS 真机验收留到 P7，不计入 P3。
- 精确路径：
  - Web 后端通过 FR2-036 的 `FramePresentationFacet` 声明当前源支持 `requestVideoFrameCallback`，并能为起始帧、相邻目标帧和最终呈现帧提供稳定源帧身份。身份必须使用 `PresentedFrame.sourceFrameIndex`，或使用可与后端相邻目标一一对应的 `PresentedFrame.stableFrameId`；仅有呈现序号或 `mediaTime` 不满足精确能力。
  - 除已处于对应边界并返回 `clamped=true` 外，`next` 的目标必须是起始帧之后的第一帧，最终确认帧必须严格大于起始 `mediaTime`；`previous` 的目标必须是起始帧之前的第一帧，最终确认帧必须严格小于起始 `mediaTime`。
  - 每次 Seek 后必须等待实际呈现帧，使用回调的 `mediaTime` 验证方向与时间戳容差，并使用 `sourceFrameIndex` 或 `stableFrameId` 验证恰好相邻；不得使用 `currentTime`、预计目标、Seek 完成事件或单独的 `mediaTime` 冒充帧号/稳定身份。
  - 编号 fixture 上必须以稳定身份验证最终帧号恰好为起始帧号 `+1` 或 `-1`，相同帧号、反方向帧号和跳过一帧以上均失败。
  - 以相邻目标帧的期望时间戳为基准，实际 `mediaTime` 允许 ±1 个源帧时长的时间戳容差；该容差只处理时间基准/呈现采样偏差，不能放宽方向性或恰好相邻的帧号断言。超过时执行有上限的校正，未收敛不得报告精确成功。
- 近似降级：
  - 浏览器不支持 `requestVideoFrameCallback`、后端无法提供可靠帧时长/帧时间轴，或 `PresentedFrame` 缺少可验证相邻关系的 `sourceFrameIndex`/`stableFrameId` 时，使用实际位置加减名义帧时长并 Seek。
  - 后端能力和结果必须标记 `approximate`，UI 明确显示“近似逐帧”或等价提示；不得显示“帧准确”。
  - 降级仍须支持前后方向、边界夹取和受控取消，不得直接禁用全部逐帧能力，除非后端连基本 Seek 也不支持。
- 帧时长来源优先级：后端提供的帧时间轴、媒体元数据中的有效帧率、运行期已验证的呈现帧间隔；player-core 不自行解析媒体或请求元数据。
- 可变帧率媒体只有在后端提供相邻帧时间轴时才可声明精确逐帧；仅有名义帧率时必须标记近似。
- 边界规则：
  - 向前越过可 Seek 区间终点时夹取到最后可呈现帧。
  - 向后越过可 Seek 区间起点时夹取到第一可呈现帧。
  - 已处于边界时重复同方向操作返回 `completed` 且 `clamped=true`，位置保持不变，不发起越界网络请求。
- 快速连续逐帧必须按接受顺序串行执行；每个命令以上一个已确认呈现帧为基准，不得用尚未确认的预测时间累加。
- 命令取代、暂停、切源、卸载和边界夹取不是网络失败，不得显示 `Network Error`。

**范围内**：

- player-core 的逐帧命令、精度状态、目标计算、校正上限、边界夹取和串行队列。
- Web `FramePresentationFacet` 的当前源能力探测，以及 `requestVideoFrameCallback` 实际 `mediaTime` 与可选稳定 `sourceFrameIndex`/`stableFrameId` 回传。
- P3 桌面按钮、键盘与 Windows 桌面安装态 PWA 到统一命令的映射。
- 真实编号帧 fixture、自动化验证与 Windows 桌面 Web/PWA headed 真机验收。

**不做（范围外）**：

- 不在 player-core 中解码视频、解析容器、扫描关键帧或调用 FFmpeg。
- 不替换 mpegts.js/MSE/hls.js，也不为逐帧新增第二套播放内核。
- 不承诺在缺少 `requestVideoFrameCallback` 和帧时间轴的环境中达到帧准确，只提供明确的近似降级。
- 不在本规格内定义通用阶梯 Seek；1 帧以外的步长由 [FR2-035](fr2-035-tiered-seek.md) 负责。
- 不在 P3 实现移动端逐帧手势或触控专用入口，也不验收 Android/iOS 移动真机；这些与 Desktop、Android、iOS、TV、车机原生后端一并留到 P7 复用同一语义。

## 3. 设计（怎么做）

### 3.1 能力与结果模型

当前源的能力快照中，逐帧能力至少区分：

- `exact-verified`：能取得方向性的相邻帧目标，能观测实际呈现 `mediaTime`，并能通过 FR2-036 `PresentedFrame.sourceFrameIndex` 或 `stableFrameId` 验证稳定的相邻源帧身份；仅能证明“接近一个帧时长”不足以声明精确。
- `approximate`：能 Seek，但无法同时取得实际呈现观测和稳定相邻源帧身份；包括身份字段缺失、身份不稳定或只有 `mediaTime` 的情况。
- `unsupported`：无法完成基本 Seek。

逐帧结果至少包含：方向、起始实际 `mediaTime`、起始 `sourceFrameIndex`/`stableFrameId`、相邻目标帧时间、相邻目标稳定身份、最终实际 `mediaTime`、最终 `sourceFrameIndex`/`stableFrameId`、帧时长、时间戳误差、精度等级、是否夹取、完成状态和请求代次。精确成功必须同时满足方向正确、稳定身份证明最终帧恰好相邻和时间戳在容差内；`mediaTime` 不承担帧号证明。

### 3.2 精确步进流程

```text
读取已确认的暂停帧 mediaTime 与 sourceFrameIndex/stableFrameId
  → 按方向取得带稳定身份的严格相邻目标（next > 起始 / previous < 起始）
  → 按可 Seek 区间夹取
  → PlaybackBackend.seek
  → 通过 FramePresentationFacet 等待 requestVideoFrameCallback
  → 读取实际 mediaTime 与最终 sourceFrameIndex/stableFrameId
  → 用稳定身份验证方向正确且帧号恰好 ±1
  → 再验证目标时间戳 ±1 帧容差
  → 必要时有限校正
  → 发布已确认结果并保持暂停
```

- 校正必须有固定次数上限，防止浏览器在不可达目标附近无限 Seek；具体次数由实现测试确定，但不得用无限重试掩盖内核限制。
- 精确模式只有在方向、`sourceFrameIndex`/`stableFrameId` 证明恰好相邻和最终实际 `mediaTime` 容差全部验证通过后才发布成功状态；若稳定身份缺失则在执行前降级为 `approximate`，不得尝试仅凭 `mediaTime` 补证帧号。
- `next` 返回与起始相同或更早的帧、`previous` 返回与起始相同或更晚的帧时，即使时间戳落在 ±1 帧容差内也必须校正或失败。
- 最终帧跨过一个以上源帧时，即使方向正确也不得报告精确成功；校正失败时返回受控失败并保留最后实际位置，不得把未验证结果降格后静默宣称成功。
- 前一帧与后一帧使用镜像的方向性相邻判定、同一时间戳容差和边界规则，不允许只保证向前准确。

### 3.3 近似步进流程

```text
读取后端实际位置
  → 按名义帧时长加减
  → 按可 Seek 区间夹取
  → PlaybackBackend.seek
  → 以后端确认位置完成
  → 发布 approximate 结果
```

- 近似路径不调用不存在的 `requestVideoFrameCallback`。
- UI 必须能从结果中持续识别当前为近似模式，而不是只显示一次临时提示。
- 若后续同一播放会话获得可靠帧观测能力，可在下一次命令切换到精确模式；已完成的近似结果不得改写为精确。

### 3.4 真实编号帧 fixture

验收 fixture 必须是实际编码、可由现有播放内核加载的视频文件，不得只用 mock 事件或全关键帧图片序列代替。

- 基准 fixture：恒定 30 fps、10 秒、300 帧，画面烧录可见编号 `0000` 至 `0299`，同时保存帧号到期望 `mediaTime` 的机器可读映射。
- 精确路径断言以编号相邻为主：从编号 `NNNN` 执行 `next` 后必须显示 `NNNN+1`，执行 `previous` 后必须显示 `NNNN-1`；±1 帧只用于比较该相邻编号对应的期望 `mediaTime`，不得把编号相同或跳号视为通过。
- 编码应包含正常 GOP 和非关键帧，避免逐帧只在全关键帧素材上通过。
- 同一源至少覆盖原文件直出、mpegts.js/MSE 与 hls.js/MSE 可用路径；若转封装产生时间戳偏移，fixture 映射必须记录各路径的实际起始时间。
- 可增加一份可变帧率 fixture 验证精确能力判定与近似降级，但不得用它替代恒定帧率主验收。
- fixture 的生成方式必须可重复，并使用项目已有 FFmpeg 工具链，不为测试引入新的播放或解码依赖。

### 3.5 依赖关系

- 必须先完成 [FR2-036](fr2-036-player-core.md) 的基础 `PlaybackBackend`、`FramePresentationFacet`、能力变化事件、命令代次、呈现帧和错误语义。
- Web 播放路径依赖既有 mpegts.js/MSE、hls.js/MSE、原文件直出与编码协商，不改变其选择规则。
- [FR2-035](fr2-035-tiered-seek.md) 的“1 帧”档复用本规格的命令和精度结果，不另写逐帧算法。
- P7 多端后端可使用原生帧步进能力，但仍须返回同一精度等级和结果模型。

## 4. 任务拆分

- [ ] 测试先行：为 `next` 严格晚于起始、`previous` 严格早于起始、恰好相邻、暂停保持、起止边界、连续命令和受控取消编写失败测试。
- [ ] 测试先行：为相同帧、反方向帧、跨过多帧、时间戳超过 ±1 帧容差、有限校正失败和近似降级编写失败测试。
- [ ] 测试先行：覆盖 `PresentedFrame.sourceFrameIndex`、`stableFrameId` 两种精确身份路径，以及身份字段缺失、身份不稳定、只有 `mediaTime` 时能力降为 `approximate`；禁止以时间戳接近目标替代帧号断言。
- [ ] 建立真实编号帧 fixture 与帧号/`mediaTime` 期望映射。
- [ ] 实现 `FramePresentationFacet` 当前源能力和 `PresentedFrame.mediaTime`/`sourceFrameIndex`/`stableFrameId` 契约。
- [ ] 实现 player-core 逐帧队列、方向性相邻目标计算、边界夹取、验证和精度结果。
- [ ] Web 分面接入 `requestVideoFrameCallback`，并处理注册、取消、切源和卸载。
- [ ] 将桌面按钮、键盘与 Windows 桌面安装态 PWA 入口映射到同一逐帧命令；移动端手势留 P7。
- [ ] 补 Web 内核回归：直出、mpegts.js/TS、hls.js/HLS 均不替换内核且不出现伪 `Network Error`。
- [ ] 执行 Windows 桌面 Web 与安装态 PWA headed 真机验收并记录浏览器、Windows/PWA 运行模式、内核路径和精度模式；Android/iOS 移动真机留 P7。
- [ ] 文档同步：PRD 状态、ARCHITECTURE、API、CHANGELOG。

## 5. 验收标准

- 非边界暂停态点击或触发“后一帧”一次，最终帧严格晚于起始且恰好为下一源帧；“前一帧”一次，最终帧严格早于起始且恰好为上一源帧；完成后仍保持暂停。边界重复操作按 `clamped=true` 例外保持原帧。
- 支持 `requestVideoFrameCallback` 的 Web 环境中，每次精确模式成功都以回调返回的实际 `mediaTime` 和 `PresentedFrame.sourceFrameIndex`/`stableFrameId` 验证：稳定身份证明编号必须恰好 `+1`/`-1`，相邻目标时间戳允许 ±1 帧容差；相同编号、反方向或跳号均失败。
- 不支持 `requestVideoFrameCallback`、没有可靠帧时间基准，或不能提供稳定相邻源帧身份时，功能明确标记为“近似逐帧”，前后操作仍可用，但不宣称帧准确；即使 `mediaTime` 恰好相差名义一帧，也不能据此升级为精确。
- 真实 30 fps 编号帧 fixture 上，连续向前至少 60 次时每一步编号严格 `+1`，连续向后至少 60 次时每一步编号严格 `-1`，不得停留、反向或跳号；每个相邻编号对应的实际 `mediaTime` 符合 ±1 帧时间戳容差。
- 从第一帧继续后退、从最后一帧继续前进均稳定停在边界，返回 `clamped=true`，不发起越界请求，不显示 `Network Error`。
- 快速交替前后逐帧、切源和卸载不会让旧回调覆盖新状态，预期取消不会归类为网络错误。
- 原文件直出、mpegts.js/MSE、hls.js/MSE 的既有播放行为与内核选择不回归。
- Playwright headed 验收必须在 Windows 真实桌面 Web 和同一 Windows 实体机的桌面安装态 PWA 上执行；至少各覆盖一次精确路径，并在不支持 API 的浏览器或受控能力桩下覆盖近似降级。P3 不以移动端手势或 Android/iOS 真机为阻断门；这些留 P7。headless、jsdom 或纯 mock 结果不能替代 Windows headed 真机。
- player-core 单元测试、`FramePresentationFacet` 能力/契约测试、真实 fixture 集成测试和工作区质量门全绿；契约测试必须覆盖两种稳定身份字段及缺失身份时的近似降级。

## 6. 风险 / 待定

- 浏览器 Seek 可能落到邻近可解码帧，因此必须以实际呈现 `mediaTime` 验证方向/容差，并以稳定 `sourceFrameIndex` 或 `stableFrameId` 验证相邻源帧；`seeked` 事件或单独的 `mediaTime` 都不能作为帧号证明。
- 可变帧率素材若无帧时间轴无法证明相邻帧，必须降级为近似；不得用名义平均帧率伪装精确。
- mpegts.js、hls.js 与原文件直出路径可能存在不同时间戳基准，fixture 期望映射必须按实际媒体时间校准。
- Windows 桌面浏览器与安装态 PWA 对 `requestVideoFrameCallback`、窗口状态和媒体呈现的行为可能不同，P3 必须分别 headed 验收；移动浏览器手势与 Android/iOS 真机差异留 P7 收口。
