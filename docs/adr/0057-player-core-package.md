# ADR-0057：把播放核心封装为 packages/player-core 及多端复用契约

## 状态
已接受

## 背景

v2 的播放器要在 Web 达标后于 P7 复用到 Android、iOS、TV、车机等端（[ADR-0052](0052-apps-workspace-multi-client.md)、[ADR-0054](0054-apps-workspace-toolchain-quality-gates.md) 的 `apps/*` + `packages/*` 结构）。PRD 对播放能力提出大量交互要求：

- 逐帧前后步进、阶梯快进快退（1 帧 / 0.5s / 1s / 5s / 30s / 1m）（FR2-031、FR2-032）。
- HLS 多码率自适应、进度条悬停预览、字幕与音轨切换、变速与 A-B 循环、章节书签（FR2-033~036）。
- 跨端续播状态与进度上报（FR2-041、FR2-044、FR2-045、FR2-056~060）。

这些是**播放控制逻辑**，与底层解码内核无关。Web 端解码内核已由 [ADR-0004](0004-mpegts-js-player.md)/[ADR-0019](0019-mpegts-player.md) 锁定为 mpegts.js + MSE，架构不变量 §4 明确「禁止用原生 video 播 TS 流、禁止替换播放内核」；[ADR-0035](0035-fmp4-mse-playback-path.md)/[ADR-0036](0036-codec-negotiation.md) 又在其上扩展了 fMP4/CMAF 与编码协商。

若每端各写一套播放逻辑，帧步进、阶梯 seek、追播、续播等语义会各端不一致且重复维护。需要决策：把播放控制逻辑抽到哪、如何与各端解码后端解耦、如何不触碰 Web 内核红线。

## 决策

新增共享包 **`packages/player-core`**，承载与端无关的播放控制逻辑与状态；解码内核不进 player-core，而是由各端实现的 `PlaybackBackend` 适配接口注入。

- **player-core 职责边界**（纯控制层、可测）：时间轴与播放状态机；帧步进与阶梯 seek 语义；HLS 多码率自适应策略；预览轨（sprite/vtt）解析与命中；字幕与音轨选择；变速；A-B 循环；章节书签；跨端续播 state 的读写与上报编排。**不自带解码、不直接碰 DOM/原生视图。**
- **端适配接口 `PlaybackBackend`**：定义加载媒体、seek、帧步进能力探测、缓冲状态查询、可用音轨/字幕枚举与切换、变速等能力。各端实现同一契约：
  - Web：mpegts.js + MSE（H.264/TS 路径）与 fMP4/CMAF + 原生 MSE（高级编码路径），沿用 ADR-0004/0035/0036，**不替换、不新增内核**。
  - Android：ExoPlayer 实现。
  - iOS：AVPlayer 实现。
- **React 壳层通过明确 API 嵌入**：`apps/web` 经 player-core 暴露的受控 API 使用播放器，不把播放器内部状态散落进页面组件（与 `packages/render-pixi` 同原则，见 ADR-0054）。
- **与 `packages/media-client` 协同**：拉取 HLS 清单、进度与续播数据由 media-client 负责；player-core 消费其数据、把续播/进度回写交给它上报，不自建请求层。
- **落地节奏**：P3 先在 Web 端把 player-core + Web `PlaybackBackend` 做达标；P7 各端实现同一契约复用同一核心，1.0 先保证 Web。

## 理由

- **控制逻辑与解码内核天然可分层**：帧步进、阶梯 seek、A-B、续播都是时间轴上的控制语义，与「谁在解码」无关，抽成纯控制层后可穷举单测，且各端只需实现一层薄后端。
- **不碰 Web 内核红线**：player-core 不含解码，Web 后端仍是 mpegts.js/MSE，TS 流不走原生 video，架构不变量 §4 与 ADR-0004 完全成立；这是抽象而非替换。
- **一处定义、各端复用**：同一套阶梯/帧步进/追播/续播语义由 player-core 唯一实现，避免各端行为漂移和重复维护。
- **接口注入而非硬编码内核**：`PlaybackBackend` 让 ExoPlayer/AVPlayer 与 MSE 各自封装端能力差异（如帧步进能力探测），player-core 面向契约编程，端能力不足时可优雅降级。
- **与既有播放决策一致**：Web 后端内部仍按 ADR-0035/0036 分发 TS 与 fMP4/CMAF 路径、做编码协商，player-core 不介入容器与编码协商细节，只消费其结果。

## 后果

- **正**：播放控制逻辑成为 P3 交付、P7 多端复用的接缝；1.0 只需在 Web 达标，多端复用同一核心时行为一致。
- **正**：player-core 为纯控制层，追播、并发 Seek、弱网降档等高风险路径可作纯逻辑单测（挂 `testing-and-quality.md` 高风险区：并发 Range/Seek、追播边界、硬件/码率降级）；帧步进需帧准确（关键帧 + 解码定位），由后端能力探测决定精度并在测试中覆盖。
- **边界**：Web 端**不替换 mpegts.js**，只把控制逻辑抽出；TS 流仍禁原生 video；fMP4/CMAF 与编码协商仍归 Web 后端与既有 ADR。
- **边界**：`PlaybackBackend` 是新契约，属目标结构；实际引入 player-core、落地 Web/端后端实现须各自走实现 spec 并按依赖管理确认（[ADR-0054](0054-apps-workspace-toolchain-quality-gates.md) 后果同款约束）。
- **文档**：`docs/PRD.md`（FR2-031~036、FR2-041、FR2-044/045、FR2-056~060）与 `docs/ROADMAP.md` 的 P3/P7 以本 ADR 为播放核心边界依据；`docs/ARCHITECTURE.md` 在代码迁移完成前仍描述现状。

## 备选方案

- **每端各写一套播放逻辑**：贴近各端原生形态，但帧步进/阶梯/追播/续播语义会各端重复实现且互相漂移，维护成本高、行为不一致。否。
- **直接用某成品播放器库统一各端**：省一层抽象，但逐帧步进、阶梯快进快退、追播/边下边播、跨端续播等定制语义成品库难以完整满足，且会把实现绑死在该库上，日后换端或换内核代价大。否。
- **把控制逻辑塞进 `apps/web` 播放器组件**：短期最省事，但无法被 P7 多端复用，且播放状态会散落进页面，违背受控 API 原则。否。
- **让 player-core 直接内嵌解码内核**：会把 Web 的 mpegts.js/MSE 与移动端原生播放器耦进同一包，破坏「控制与解码分层」，且触碰 Web 内核红线。否。
