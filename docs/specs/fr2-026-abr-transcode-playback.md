# 功能规格：多码率自动转码与自适应播放

> 状态：已交付@v0.23.0　·　关联 PRD：FR2-026　·　阶段：P2 `0.23.x`　·　分支：`feature/p2-fr2-026-complete`

## 1. 背景与目标

当前 `MultiPipeline` 已能生成 1080p/720p/480p 多档 HLS，前端 `hls.js` 支持 ABR，但入库自动生成、任务配置、弱网降档验收、缓存登记和失败重试尚未形成端到端能力。FR2-026 要让多码率转码走任务队列，并在播放端验证自适应降档。

目标：

- 定义多码率 ladder 与任务 profile。
- 入队生成多档 HLS master playlist，产物可清理重建。
- P2 交付多码率产物、ABR manifest 与 hls.js level 切换 smoke；弱网平滑体验的完整用户体验验收需与 ROADMAP P3 播放体验口径二次确认。

前置依赖：FR2-008（HLS 任务化）、FR2-037（任务队列）、FR2-048（缓存资产）、FR2-056（硬件策略）。

## 2. 需求（要什么）

- 默认 ladder：1080p、720p、480p，按源分辨率跳过高于源的档位。
- 转码任务走 FR2-037，支持优先级、失败重试、取消、进度。
- 转码设置从 FR2-024 registry 读取：默认不自动触发高成本转码，并发上限、目标编码、ladder。
- 硬件编码选择由 FR2-056 提供，未配置时软件 fallback。
- 产物登记到 FR2-048 缓存资产。
- 播放协商直连优先；选择 HLS 时使用 master playlist 进行 ABR。
- P2 弱网验收限定为受控 mock manifest 或 hls.js 测试桩观察 level 切换；真实弱网平滑降档体验留到 P3 收口。
- 范围内：ladder、任务、产物、播放 ABR 验收、测试。
- 不做（范围外）：手动锁档/省流量（FR2-057）、字幕音轨高级选择、P3 播放核心包抽离。

## 3. 设计（怎么做）

任务：

- `transcode.hls.abr`，payload 包含 media、ladder、codec、hwaccel preference、force rebuild。
- 进度按输出档位与分片阶段汇总。

转码：

- 复用 `MultiPipeline`，补齐 profile 参数和错误摘要。
- P2 默认 ABR ladder 输出 H.264 TS HLS，保持现有 HLS/TS 播放兼容；H.265/AV1/VP9 的 fMP4/CMAF ABR 不纳入默认验收，需另行规格或 ADR。
- 输出目录按 Space/media/profile 隔离。
- 成功后生成 master playlist 并登记每档缓存资产。

前端：

- 播放页 HLS path 使用 hls.js ABR。
- E2E 可通过 mock manifest + 网络节流或 hls.js 事件断言验证 level 切换。

## 4. 任务拆分

- [x] 定义默认 ladder 与配置 registry。
- [x] 将多码率转码封装为统一任务处理器。
- [x] 接入硬件选择 fallback 与缓存登记。
- [x] 播放协商返回 ABR master 状态。
- [x] 前端播放端补 ABR 状态/事件断言。
- [x] 补单元测试：ladder 过滤、ffmpeg 参数、master playlist。
- [x] 补集成测试：多档生成、失败重试、取消、缓存清理后重建。
- [x] 补 E2E：HLS ABR 播放与弱网降档。
- [x] 文档同步：ARCHITECTURE、API、CHANGELOG，并在最终全量门禁通过后同步 PRD 验收状态。

## 5. 验收标准

- 测试视频能生成多档 HLS 与 master playlist。
- 源分辨率低于某档时自动跳过该档；源低于 480p 时生成单档源高，不向上放大。
- 转码任务状态、进度、取消、失败重试在任务中心可见。
- 受控 mock manifest 或 hls.js 测试桩下能观察到 ABR level 切换事件；真实弱网平滑体验不作为 P2 必过项。
- 直连可播时仍优先直连，不强制所有播放走 HLS。
- `go test`、转码集成测试、Playwright 弱网 E2E、`pnpm run quality` 全绿。
- Go 单二进制 serve 后，多码率生成与播放实跑通过。

## 6. 风险 / 待定

- 已确认：默认不在入库时自动触发多码率转码，采用手动或按需低优先级触发，避免首次扫描高负载。
- 已确认：弱网平滑降档完整体验按 ROADMAP P3 收口。
- 弱网 E2E 在不同浏览器环境可能不稳定，需要保留 hls.js 事件级断言。
