# 功能规格：可复用播放核心

> 状态：候选发布@v0.24.0-rc.1　·　关联 PRD：FR2-036　·　阶段：P3 `0.24.x`　·　分支：`feature/fr2-036-player-core`

## 1. 背景与目标

现有 Web 播放能力分布在播放器组件、mpegts.js、MSE、hls.js 与原生媒体元素适配逻辑中。逐帧、阶梯 Seek、HLS 自适应、进度预览等控制语义若继续直接写入 Web 组件，P7 的 Desktop、Android、iOS、TV 与车机将重复实现并产生行为差异。

[ADR-0057](../adr/0057-player-core-package.md) 已决定新增 `packages/player-core`，以 `PlaybackBackend` 隔离控制逻辑与各端播放后端。本规格负责落实该边界，并为 [FR2-034](fr2-034-frame-stepping.md) 与 [FR2-035](fr2-035-tiered-seek.md) 提供共同契约。

目标：

- 把播放状态机、时间轴命令、并发控制、能力降级和结果事件收敛到 `packages/player-core`。
- 让 player-core 成为纯控制层，可使用伪后端做确定性单元测试。
- 保持 Web 既有播放内核不变，只在当前 `frontend/` 中增加薄的 `PlaybackBackend` 适配层，不假定或前置 `apps/web` 迁移。
- P3 先完成当前 Web/PWA 达标；P7 各端实现同一契约，不复制核心控制语义。

## 2. 需求（要什么）

- `packages/player-core` 只处理与端无关的控制语义和状态，不直接接触：
  - DOM、`HTMLVideoElement`、React 组件或原生视图。
  - URL 拼接、`fetch`、鉴权头、Range 请求、清单下载或任何网络访问。
  - 容器解析、解复用、MSE SourceBuffer、编解码器或硬件解码。
  - mpegts.js、hls.js、ExoPlayer、AVPlayer 等具体内核实例。
- player-core 接收由端壳准备好的播放源描述符，并把加载、播放、暂停、Seek、快照、事件和销毁委托给基础 `PlaybackBackend`；帧呈现、轨道、清晰度/倍速和加载控制通过可选能力分面接入。
- Web 必须保持既有内核和分发规则：
  - H.264/TS 路径继续使用 mpegts.js + MSE。
  - 多码率 HLS 与既有 fMP4/CMAF 路径继续使用 hls.js/MSE 的现有实现。
  - 原文件直连继续沿用既有可直出路径。
  - 不替换、不绕过、不复制 mpegts.js/MSE/hls.js 内核；TS 流不得改为原生媒体元素直接播放。
- player-core 对外暴露受控命令，不让页面组件直接修改内部时间轴状态；按钮、键盘或端侧手势只负责在壳层映射为同一命令。
- PiP、Media Session 与 Pointer Events 属于 Web 平台集成：由当前 `frontend/` 的 `WebPlatformAdapter` 或壳层实现，只消费 player-core 命令和状态，不进入 player-core 或基础 `PlaybackBackend`。
- 快速连续 Seek、播放源切换与组件卸载必须使用请求代次或等价机制隔离旧结果；被新命令取代的操作属于受控取消，不得上报为 `Network Error`。
- 后端错误必须归一为可判定类别；只有真实网络失败才能归为网络错误，命令取代、主动取消、边界夹取和能力降级均不得伪装为网络错误。
- 当前规格同时定义逐帧、阶梯 Seek 与时间轴预览所需的最小契约；音轨/字幕使用 `TrackFacet`，清晰度/变速使用 `QualityFacet`，预加载调度使用 `LoadControlFacet`，帧呈现观测使用 `FramePresentationFacet`，时间轴预览使用 `PreviewFacet`，各自完整交互由对应 FR 规格验收。

**范围内**：

- `packages/player-core` 的边界、状态模型、命令模型、事件模型和 `PlaybackBackend` 契约。
- 当前 `frontend/` 内的 Web `PlaybackBackend` 薄适配层、可选能力分面及既有播放内核回归保护。
- FR2-034、FR2-035 所需的能力快照、呈现帧观测、Seek 结果与取消语义。
- `WebPlatformAdapter` 与 player-core 的单向边界：PiP、Media Session、Pointer Events 保留在 Web 平台层。
- 纯逻辑单元测试、后端契约测试、Web/PWA headed 真机验收基线。

**不做（范围外）**：

- 不实现或替换任何解码、解复用、MSE、mpegts.js、hls.js 内核。
- 不在 player-core 中访问 DOM、网络、数据库、本地存储或 `packages/media-client` 请求实现。
- 不改变服务端播放 API、Range/HLS 产物、转码策略或编码协商。
- 不在 P3 完成 Android、iOS、TV、车机后端；这些端在 P7 按同一契约接入。
- 不把 React 控件、快捷键文案、手势识别、Pointer Events、PiP、Media Session 或视觉样式放进 player-core。
- P3 不要求把当前 `frontend/` 迁移到 `apps/web`，也不得以未来目录结构作为 Web adapter 的落位前提。
- 不在本规格内完成字幕、音轨、A-B 循环、章节书签、投屏或跨端续播的完整产品交互。

## 3. 设计（怎么做）

### 3.1 分层与依赖方向

```text
当前 frontend/ 页面与控件
  ├→ WebPlatformAdapter（PiP / Media Session / Pointer Events）
  │   └→ player-core 受控命令与状态
  └→ player-core 受控命令
      → PlaybackBackend 基础契约
        → 可选能力分面
          → frontend/ Web 适配层
            → 既有 mpegts.js/MSE、hls.js/MSE、原文件直出路径
```

依赖必须单向：

- `packages/player-core` 不依赖 `frontend/`、React、DOM、浏览器类型或具体播放库。
- 当前 `frontend/` 依赖 player-core，并在应用侧实现 Web `PlaybackBackend`、可选能力分面和 `WebPlatformAdapter`；P3 不假定目录已迁移到 `apps/web`。
- `packages/media-client` 或 Web 壳层负责取得播放描述符、清单和续播数据；player-core 只消费已准备的数据，不发请求。
- Web 后端适配层可以接触媒体元素和既有内核，但不得把端专有对象泄漏到 player-core 状态。
- `WebPlatformAdapter` 可以接触 PiP、Media Session 和 Pointer Events，但只能调用 player-core 公共命令、订阅公共状态，不得反向成为 player-core 依赖。

### 3.2 核心状态与命令

player-core 维护最小状态机：

```text
idle → loading → ready → playing ↔ paused
                  ↓          ↓
                seeking ←────┘
                  ↓
              ready/paused/playing
任一可运行状态 → ended/error/disposed
```

- 状态快照至少包含播放状态、实际媒体时间、时长、可 Seek 区间、缓冲区间、播放速率、能力快照和当前命令代次。
- 能力快照属于当前播放源与当前后端实例的瞬时事实；加载新源、清单更新、媒体元数据就绪或后端重建时均可变化，不得缓存首次探测结果贯穿整个会话。
- `seekTo`、`stepFrame`、`seekByTier` 等命令以同一实际媒体时间快照为基准，不以 UI 中尚未确认的预测值连续累加。
- player-core 为命令分配单调递增代次；旧代次返回时不得覆盖新状态。
- 逐帧命令按接受顺序串行执行，避免一次按键被吞掉或重复跨帧；普通连续 Seek 可采用最后意图优先，但必须保留确定性的取消结果。

### 3.3 `PlaybackBackend` 契约

契约使用端无关的数据类型；具体语言签名可按工作区约定实现，但语义不得改变。

```ts
interface PlaybackBackend {
  load(source: PlaybackSource, requestId: number): Promise<void>
  play(requestId: number): Promise<void>
  pause(requestId: number): Promise<void>
  seek(request: SeekRequest): Promise<SeekResult>
  getSnapshot(): PlaybackSnapshot
  subscribe(listener: PlaybackBackendListener): () => void
  dispose(): void
}

interface PresentedFrame {
  mediaTime: number
  presentationSequence: number
  sampleSource: 'video-frame-callback' | 'backend'
  sourceFrameIndex?: number
  stableFrameId?: string
}

interface FramePresentationFacet {
  waitForPresentedFrame(requestId: number): Promise<PresentedFrame>
}

interface PreviewFacet {
  setTrack(track: PreparedPreviewTrack | null, requestId: number): PreviewTrackState
  hitTest(mediaTime: number, requestId: number): PreviewHit | null
  getState(): PreviewTrackState
}

type TrackKind = 'audio' | 'subtitle'

interface TrackSelectionState {
  kind: TrackKind
  selectedTrackId: string | null
  effectiveTrackId: string | null
}

interface TrackFacet {
  getTracks(kind: TrackKind): readonly PlaybackTrack[]
  getSelectionState(kind: TrackKind): TrackSelectionState
  selectTrack(kind: TrackKind, trackId: string | null, requestId: number): Promise<void>
}

interface QualityFacet {
  getQualities(): readonly PlaybackQuality[]
  selectQuality(selection: QualitySelection, requestId: number): Promise<void>
  setPlaybackRate(rate: number, requestId: number): Promise<void>
}

interface LoadControlFacet {
  startLoading(requestId: number): Promise<void>
  stopLoading(requestId: number): Promise<void>
}

type PlaybackBackendBinding = {
  backend: PlaybackBackend
  facets?: {
    framePresentation?: FramePresentationFacet
    preview?: PreviewFacet
    tracks?: TrackFacet
    quality?: QualityFacet
    loadControl?: LoadControlFacet
  }
}
```

契约语义：

- 基础 `PlaybackBackend` 只包含加载、播放、暂停、Seek、快照、事件订阅和幂等销毁；任何业务扩展不得继续向基础接口追加方法。
- `PlaybackSource` 由调用方准备，player-core 只识别稳定的源标识、播放模式和媒体元数据；URL、请求头、内核配置等端专有载荷仅由后端解释。
- `PlaybackSnapshot.capabilities` 是当前播放源的能力快照，至少声明基本 Seek、已挂载分面及各分面的当前可用等级；不得通过捕获异常猜测能力，也不得因分面对象存在就推断当前源可用。
- 能力快照变化必须发布独立的 `capabilitiesChanged` 事件，并携带源标识与请求代次；player-core 原子替换快照，旧源或旧代次事件不得回写。
- `SeekRequest` 包含目标时间、请求代次、发起原因和边界策略；目标必须先按后端报告的可 Seek 区间夹取。
- `SeekResult` 至少返回请求时间、夹取后目标、后端确认时间、是否被夹取及完成状态。
- `PresentedFrame` 至少返回实际 `mediaTime`、呈现序号和采样来源，并可选返回稳定源帧身份：可按源帧顺序比较的 `sourceFrameIndex`，或由后端保证在当前源/代次内稳定且可与相邻目标一一对应的 `stableFrameId`。Web 支持 `requestVideoFrameCallback` 时必须由 `FramePresentationFacet` 回传该 API 的实际 `mediaTime`，不得用 `currentTime` 冒充；呈现序号只表示观测顺序，不等同于稳定源帧身份。
- 只有 `FramePresentationFacet` 能为当前源提供稳定相邻帧身份，并通过 `sourceFrameIndex` 或 `stableFrameId` 返回该身份时，能力快照才可声明逐帧 `exact`；字段缺失、身份不稳定或只能提供 `mediaTime` 时必须声明 `approximate`。`mediaTime` 只用于方向与时间戳容差校验，不能单独证明源帧号或恰好相邻。
- 当前源不支持帧呈现观测时，能力快照必须明确声明；player-core 据此进入可见的近似降级，不得虚报帧准确。
- `PreparedPreviewTrack` 由调用方提供已取得的预览描述符与 VTT 文本，并携带媒体、profile、源指纹、生成代次和请求代次；`PreviewFacet` 只负责 VTT 解析、时间命中、sprite 坐标计算、generation/请求代次隔离和预览状态，不访问网络或自行获取 VTT/sprite。
- 预览描述符、VTT 与 sprite 数据统一由 `packages/media-client` 获取并交给端壳；sprite 的资源加载与展示也属于端壳。player-core/`PreviewFacet` 不拼接 URL、不发送请求、不持有 DOM 或图片对象。
- `TrackFacet` 的 kind 只允许 `'audio' | 'subtitle'`，统一通过 `selectTrack(kind, trackId)` 选择或关闭轨道，并通过 `getSelectionState(kind)` 暴露用户意图 `selectedTrackId` 与后端确认的 `effectiveTrackId`；两者在切换期间可暂时不同，成功后收敛，失败或回滚后恢复为实际轨道。
- `TrackFacet` 只承载音轨/字幕轨枚举、选择和 selected/effective 状态；`QualityFacet` 只承载清晰度与倍速，播放速率只通过 `QualityFacet.setPlaybackRate` 设置；`LoadControlFacet` 只承载主动加载启停；`PreviewFacet` 只承载预览轨解析、命中、代次与状态，不得混入 PiP、Media Session 或输入事件。
- `subscribe` 传递状态、时间、缓冲、能力、轨道、预览状态、呈现帧和错误事件；事件必须携带源标识与请求代次，预览状态还必须携带 profile/generation，旧事件不得污染当前源。
- `dispose` 必须幂等，负责让待处理命令以受控取消结束；资源释放细节属于端后端。

统一完成状态至少包括：`completed`、`superseded`、`canceled`、`unsupported`、`failed`。统一错误类别至少包括：`network`、`media`、`decode`、`unsupported`、`unknown`。`superseded`、`canceled`、`unsupported` 不是网络错误。

### 3.4 Web 后端与平台适配约束

- P3 Web adapter 明确落在当前 `frontend/`，包裹现有播放器初始化、加载、销毁和事件，不重写内核，也不以迁移到 `apps/web` 为前置任务。
- mpegts.js 的 Range Seek、追播与 MSE 生命周期保持既有行为。
- hls.js 的多码率、自适应和 fMP4/CMAF 行为保持既有行为。
- 适配层只把内核事件翻译为基础 `PlaybackBackend` 或对应分面事件；不得让 player-core 直接订阅 mpegts.js/hls.js 事件。
- 每次加载、切源、清单更新和后端重建后重新计算能力快照；变化时发布 `capabilitiesChanged`，不沿用上一播放源的轨道、清晰度、帧呈现或加载控制能力。
- 旧加载被新源或新 Seek 取代时，适配层必须吞并预期内的中止信号并返回 `superseded` 或 `canceled`；只有内核确认的真实网络失败才上报 `network`。
- 支持 `requestVideoFrameCallback` 的浏览器由 `FramePresentationFacet` 负责注册、取消并回传实际 `mediaTime`；player-core 不接触媒体元素。
- PiP、Media Session 和 Pointer Events 由 `frontend/` 的 `WebPlatformAdapter` 或壳层负责；它们不得成为 `PlaybackBackend` 分面，也不得把浏览器对象写入 player-core 快照。

### 3.5 统一依赖与集成顺序

- 架构前置：[ADR-0057](../adr/0057-player-core-package.md)、[FR2-002](fr2-002-workspace-toolchain-quality.md)。
- Web 内核前置：ADR-0004、ADR-0019、ADR-0026、ADR-0035、ADR-0036，以及已交付的 [FR2-026](fr2-026-abr-transcode-playback.md)。
- 统一顺序固定为：FR2-036 契约与当前 `frontend/` adapter → [FR2-034](fr2-034-frame-stepping.md) → [FR2-035](fr2-035-tiered-seek.md) → [FR2-029](fr2-029-timeline-preview.md) → [FR2-044](fr2-044-subtitle-audio-tracks.md) → [FR2-045](fr2-045-cross-device-watch-history.md) → [FR2-057](fr2-057-quality-rate-ab-loop.md) → [FR2-058](fr2-058-pip-background-mobile-gestures.md) → [FR2-060](fr2-060-chapters-bookmarks.md)。该顺序是统一集成与回归顺序，不把相邻规格虚构为业务强依赖。
- FR2-034 使用 `FramePresentationFacet` 及 `PresentedFrame.sourceFrameIndex`/`stableFrameId` 的稳定身份语义；FR2-029 使用 `PreviewFacet`；FR2-044 使用 `TrackFacet`；FR2-057 使用 `QualityFacet` 与 `LoadControlFacet`。FR2-035、FR2-045、FR2-060 复用基础快照、Seek、事件和代次语义。
- FR2-058 的 PiP、Media Session 与 Pointer Events 只依赖 player-core 公共命令/状态和 `WebPlatformAdapter` 边界，不扩展基础 `PlaybackBackend` 或任何后端分面。
- 每个后续规格接入前先运行基础契约与已接入规格回归；P7 的多端实现依赖本契约稳定，但不阻塞 P3 的当前 `frontend/` 交付。

## 4. 任务拆分

- [x] 测试先行：为状态机、边界夹取、命令代次、受控取消和错误归类编写失败测试。
- [x] 测试先行：建立可复用的基础 `PlaybackBackend` 与五个可选分面契约测试套件和确定性伪后端。
- [x] 测试先行：覆盖同一后端加载不同播放源时能力快照变化、`capabilitiesChanged` 事件、旧源事件隔离和分面不可用降级。
- [x] 测试先行：覆盖 `FramePresentationFacet` 仅在返回稳定 `sourceFrameIndex` 或 `stableFrameId` 时声明 `exact`，字段缺失、身份不稳定或只有 `mediaTime` 时声明 `approximate`；同时覆盖 `PreviewFacet` 的 VTT 解析、命中边界、generation/请求代次隔离与无网络访问约束。
- [x] 定义端无关的播放源、能力快照、状态、Seek、呈现帧稳定身份、预览轨、`TrackKind='audio' | 'subtitle'`、selected/effective 轨道状态、清晰度、事件和错误类型。
- [x] 实现 player-core 状态机、命令调度、请求代次和过期结果隔离。
- [x] 在当前 `frontend/` 实现薄 `PlaybackBackend` 与所需分面，包裹既有 mpegts.js/MSE、hls.js/MSE 与原文件直出路径。
- [x] 在当前 `frontend/` 保持 `WebPlatformAdapter`/壳层边界，确认 PiP、Media Session、Pointer Events 未进入 player-core 或后端分面。
- [x] 接入 FR2-034 逐帧命令与 FR2-035 阶梯 Seek 命令。
- [x] 补回归测试：现有直连、TS、HLS ABR、fMP4/CMAF、追播、续播和播放源切换不回归。
- [x] 使用真实编号帧 fixture 完成 Windows 桌面 Web 与桌面安装态 PWA headed 真机验收，不以 jsdom 或 headless 结果替代。
- [x] 文档同步：PRD 状态、ARCHITECTURE、API、CHANGELOG。

## 5. 验收标准

- player-core 的生产代码不引用 DOM、React、浏览器媒体类型、网络 API、mpegts.js 或 hls.js；静态依赖检查可证明该边界。
- 伪后端下状态机、Seek 夹取、命令代次、旧结果隔离、受控取消和错误归类均有测试且确定性通过。
- 同一组基础 `PlaybackBackend` 契约测试可运行于伪后端和 Web 后端；各可选分面使用独立契约测试，不支持能力通过当前能力快照返回，不依赖异常探测。
- `FramePresentationFacet` 能力/契约测试证明：只有可提供稳定相邻源帧身份且实际 `PresentedFrame` 带 `sourceFrameIndex` 或 `stableFrameId` 的源可声明 `exact`；身份字段缺失、不稳定或仅有 `mediaTime` 的源均声明 `approximate`，不得以时间戳接近一个帧时长替代身份断言。
- `PreviewFacet` 契约测试证明：由 `packages/media-client` 准备的数据可完成 VTT 解析、首尾 cue 命中、sprite 坐标、generation/请求代次隔离和状态切换；player-core 与分面测试中不存在网络请求、URL 拼接或 DOM/图片加载。
- `TrackFacet` 契约测试证明：kind 只接受 `'audio' | 'subtitle'`，音轨选择、字幕选择与字幕关闭统一调用 `selectTrack(kind, trackId)`，并按 kind 暴露可暂时分离且最终收敛或回滚的 `selectedTrackId` / `effectiveTrackId` 状态。
- `QualityFacet` 契约测试证明播放速率只通过 `setPlaybackRate` 设置，基础 `PlaybackBackend` 不包含倍速方法。
- 契约测试证明能力快照可随播放源变化：加载能力不同的源会发布一次带正确源标识/代次的 `capabilitiesChanged`，旧源迟到事件被丢弃，UI 不保留上一源的分面可用状态。
- Web 仍由 mpegts.js/MSE、hls.js/MSE 和既有原文件直出路径播放；TS 流没有新增原生媒体元素直接播放分支。
- 快速连续 Seek、切源和卸载不会让旧事件覆盖新状态，不会因预期中止显示 `Network Error`。
- FR2-034、FR2-035 通过同一 player-core 命令 API 工作，页面组件不直接实现第二套时间轴算法。
- PiP、Media Session、Pointer Events 的生产代码仅位于当前 `frontend/` 的 `WebPlatformAdapter` 或壳层；player-core 与 `PlaybackBackend`/分面类型中不存在对应浏览器 API。
- P3 Web adapter 可在当前 `frontend/` 完成交付，验收和构建均不要求存在 `apps/web`。
- 真实编号帧 fixture 在桌面 Web 与 Windows 桌面安装态 PWA 的 headed 真机验收通过，并覆盖精确路径与近似降级路径。
- 现有直连、mpegts.js/TS、hls.js/HLS、fMP4/CMAF、ABR 切换和追播回归测试全绿。
- 单元测试、基础/分面契约测试、Playwright headed 测试与工作区质量门全绿；headless 或 mock 全绿不替代真机验收。

## 6. 风险 / 待定

- Web 内核事件模型不一致，适配层必须统一状态和错误语义，但不得借机重写内核。
- `requestVideoFrameCallback` 的支持度与实际行为由浏览器决定；精确逐帧与近似降级口径以 FR2-034 为准。
- 快速连续命令若没有严格代次隔离，最容易产生旧 Seek 回写和伪 `Network Error`，必须作为高风险并发路径测试。
- 基础 `PlaybackBackend` 是 P7 多端复用接缝；新增能力必须优先增加独立可选分面，不得继续膨胀基础接口，也不得让端专有对象进入公共契约。
- 能力随播放源变化若缺少事件和代次隔离，最容易让 UI 沿用上一源的轨道、清晰度或帧能力；必须由契约测试阻断。
