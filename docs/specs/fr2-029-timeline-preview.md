# 功能规格：视频进度条悬停预览

> 状态：开发中　·　关联 PRD：FR2-029　·　阶段：P3 `0.24.x`

## 1. 背景与目标

播放器当前只能通过进度条定位时间，缺少目标时间画面反馈。长视频中盲目拖动会增加反复 seek、网络请求和等待成本。FR2-029 需要生成可重建的 ffmpeg 预览帧 sprite 与 WebVTT 索引，并在桌面端悬停、移动端长按时显示对应预览。

目标：

- 使用 ffmpeg 生成 sprite 图片集和 WebVTT 时间索引，通过统一任务队列异步构建。
- 预览产物按 Space/media/profile/源指纹/生成代次隔离，登记为可安全清理、可重建的缓存资产。
- 在 `packages/player-core` 通过与 FR2-036 可选能力扩展口径一致的 `PreviewFacet` 定义预览轨解析、时间命中和请求竞态语义，Web 端只负责资源准备、交互与展示。
- 使用真实视频 fixture、真实 ffmpeg 产物、E2E 与 headed 人工验收证明功能可用，而非只验证 mock。

前置依赖：FR2-007（Space/索引）、FR2-030（媒体时长与流信息）、FR2-037（任务队列）、FR2-048（缓存资产）、ADR-0057（player-core 边界）。

## 2. 需求（要什么）

- 预览轨由一份 WebVTT 清单和一组 sprite 图片组成；每个 cue 使用 `#xywh=` 指向对应 sprite 单元格。
- 生成任务必须可幂等入队、查看进度、取消、失败重试，并支持显式安全重建。
- profile 至少定义版本、采样间隔、单帧宽度、网格行列数和图片格式；profile 变化不得覆盖旧 profile 产物。
- 正式产物路径固定为 `timeline_previews/{space_id}/{media_id}/{profile_id}/{source_fingerprint}/{generation_id}/`；任务幂等键、缓存 `cache_key` 与相对路径是三个独立字段，均不得省略源指纹和生成代次。
- 预览轨登记为 `cache_assets(kind=timeline_preview)`，属于可重建缓存，可按 Space、媒体或 profile 清理；`timeline_preview` kind 与 `timeline_previews/` root 必须同时加入 FR2-048 的类型、路径、盘点和清理白名单映射。
- 清理和重建不得修改原媒体、数据库、字幕应用数据或同一媒体的其他缓存类型。
- 缺少预览轨时，播放请求不阻塞：返回待生成状态与任务信息，播放器继续正常播放并隐藏预览浮层。
- 桌面端鼠标或支持 hover 的指针悬停进度条时显示时间与预览帧；离开进度条立即隐藏，点击定位语义保持不变。
- 移动端在进度条上长按 400 ms 后进入预览模式；长按后横向移动更新目标时间，抬手执行一次 seek，取消手势则不 seek。
- 指针快速移动或媒体/profile 切换时，必须取消旧的 VTT/sprite 请求并忽略迟到响应；旧响应不得覆盖最新时间点的预览。
- 范围内：预览 profile、ffmpeg 生成、任务化、缓存登记、安全清理重建、player-core 命中逻辑、桌面 hover、Playwright Pointer/touch 仿真的移动长按，以及 Windows 安装态 PWA headed 验收。
- 不做（范围外）：逐帧精确编辑、视频内容理解选帧、在预览浮层内播放动画、原生多端 UI，以及真实 Android/iOS 设备触摸验收；真实移动设备长按与各端接入留到 P7，并复用本期 player-core 契约。

## 3. 设计（怎么做）

### 3.1 Profile、任务与产物

- 首版提供版本化默认 profile；profile ID 由影响产物的参数稳定计算，禁止只用可变显示名称定位。
- 每次生成先固定 `source_fingerprint` 与不可复用的 `generation_id`。正式目录固定为 `timeline_previews/{space_id}/{media_id}/{profile_id}/{source_fingerprint}/{generation_id}/`；API 只返回受鉴权的资源 URL，不暴露磁盘绝对路径。
- 任务类型：`preview.timeline.generate`；payload 包含 `space_id`、`media_id`、`profile_id`、`source_fingerprint`、`generation_id` 和 `force_rebuild`。
- 任务幂等键固定为 `preview.timeline.generate:{space_id}:{media_id}:{profile_id}:{source_fingerprint}:{generation_id}`；同一 generation 的重试复用该键。普通缺失请求复用当前未完成 generation，`force_rebuild=true` 每次请求都创建新的 `generation_id`，不得命中或覆盖旧 generation。
- 缓存登记使用独立 `cache_key=timeline_preview:{space_id}:{media_id}:{profile_id}:{source_fingerprint}:{generation_id}`；`cache_key` 不以相对路径充当，也不承担任务去重语义。
- ffmpeg 按 profile 采样并缩放帧，拼接固定网格 sprite；生成器再输出 WebVTT，cue 覆盖完整媒体时长且不重叠。
- 生成过程先写目标 generation 同级的受控临时目录，校验 VTT 引用、图片可解码、cue 时间范围和文件数量后原子发布；失败或取消只删除本任务临时产物，不修改当前预览指针。

### 3.2 缓存登记、当前指针与安全重建

- 每个 generation 以目录级资产登记到 `cache_assets`，记录 `kind=timeline_preview`、`asset_level=directory`、`space_id`、`media_id`、`profile_id`、`variant=source_fingerprint:generation_id`、独立 `cache_key`、相对路径、聚合大小、文件数和 `rebuildable=true`。
- 新增当前预览指针记录（实现时可命名为 `media_timeline_previews`），唯一键为 `space_id + media_id + profile_id`，至少保存 `source_fingerprint`、`generation_id`、`asset_id` 和 `updated_at`。读取 API 只能解析该指针指向的已发布 generation，不通过目录排序猜测“最新”。
- generation 完成完整性校验并登记缓存资产后，在一个数据库事务内锁定或条件更新当前指针：再次确认 Space、media、profile 与源指纹仍匹配，切换 `generation_id/asset_id` 后提交；事务失败、源指纹已变化或任务被取代时保持旧指针。
- 只有当前指针事务提交成功后，旧 generation 才可经 FR2-048 安全清理回收；`force_rebuild` 始终生成新 generation，失败时旧 generation 与当前指针继续可用。
- 源文件指纹变化时，旧资产标记过期；下一次请求创建新指纹和新 generation 的任务，不把旧 VTT 与新 sprite 混用。
- FR2-048 必须显式维护 `kind=timeline_preview` ↔ `timeline_previews/` 的双向白名单映射。盘点仅接受精确的 `timeline_previews/{space}/{media}/{profile}/{source_fingerprint}/{generation_id}/` 目录层级，并据此还原资产字段；清理按 kind 反查该 root 后再校验登记路径，禁止仅凭传入路径删除。盘点不得按目录时间推断或改写当前预览指针。
- 普通清理和强制重建都必须调用 FR2-048 缓存服务，不得直接删除 media 父目录或使用未经校验的递归删除。删除前解析真实路径并验证目标 generation 位于 `timeline_previews/` 白名单根目录且身份字段逐级匹配；数据库、媒体库、应用数据与其他缓存根目录一律拒绝。若通用清理命中当前 generation，必须在同一数据库事务内按 `asset_id + generation_id` 条件清空当前指针后再删除资产登记，避免指针继续指向已清理目录。

### 3.3 API 与播放核心

- `GET /api/play/:id/timeline-preview?profile=<id>`：可用时从当前预览指针返回 profile、时长、`source_fingerprint`、`generation_id`、VTT URL 和版本；缺失或过期时幂等入队并返回 `202`、任务 ID 与状态。
- `POST /api/play/:id/timeline-preview/rebuild`：对当前 Space 下指定 profile 发起安全重建，每次调用创建新 `generation_id`，不在 HTTP 请求内执行 ffmpeg，也不提前切换或删除当前预览。
- VTT 与 sprite 资源请求必须同时校验媒体归属、Space、profile、`source_fingerprint` 和 `generation_id`；不能通过猜测 ID 跨 Space 读取，也不能把不同 generation 的文件组合返回。
- `packages/player-core` 沿用 [FR2-036](fr2-036-player-core.md) 的向后兼容可选能力扩展口径，定义可选 `PreviewFacet`。该分面负责消费调用方已准备的预览描述符/VTT 内容、按时间二分命中 cue、计算 sprite 坐标，并维护“当前媒体 + profile + generation + 请求代次”；不直接访问网络、DOM 或具体播放内核。
- media client 支持可取消请求；新指针目标、媒体切换、profile/generation 切换或组件卸载时触发取消。即使底层取消失败，也以递增代次丢弃迟到结果。
- VTT 按媒体/profile/generation 只加载一次，sprite 按命中图片懒加载并复用浏览器缓存；指针移动本身不得逐次请求后端 API。

### 3.4 Web 交互

- 桌面端：进入进度条即显示时间占位；VTT 和 sprite 就绪后显示帧图，横向移动实时更新，离开后隐藏。
- 移动端：长按计时期间若发生滚动意图、指针取消或明显纵向移动，则取消预览；进入预览后阻止与该手势冲突的页面滚动。
- 预览浮层需限制在视口内，显示目标时间；图片未就绪、任务生成中或格式不支持时只显示时间，不影响拖动与播放。
- 键盘 seek、屏幕阅读器和现有点击/拖动定位行为不得因 hover/长按预览退化。

## 4. 任务拆分

- [ ] 定义版本化 timeline preview profile、源文件指纹、generation、任务幂等键和独立缓存 `cache_key`。
- [ ] 实现 ffmpeg 采样、sprite 拼接、WebVTT 生成及产物完整性校验。
- [ ] 接入 `preview.timeline.generate` 任务处理器、进度、取消、重试与恢复；`force_rebuild` 每次创建新 generation。
- [ ] 按 Space/media/profile/source_fingerprint/generation 发布产物并登记 `cache_assets(kind=timeline_preview)`。
- [ ] 增加当前预览指针记录与事务切换，实现旧 generation 延后安全回收和失败保留。
- [ ] 将 `timeline_preview` kind 与 `timeline_previews/` root 加入 FR2-048 类型、路径、盘点、清理白名单及双向映射测试。
- [ ] 新增预览状态、资源读取和重建 API，补齐 Space 与版本身份鉴权及路径安全校验。
- [ ] 在 player-core 可选 `PreviewFacet` 实现 VTT 解析、时间命中、sprite 坐标和请求代次控制。
- [ ] 在 Web 播放器实现桌面 hover、移动长按、时间占位、浮层边界和取消语义。
- [ ] 补单元测试：VTT 边界、cue 命中、profile/generation 隔离、幂等键、`cache_key`、指针事务、路径白名单、迟到响应丢弃。
- [ ] 补真实集成测试：真实 ffmpeg fixture 生成、VTT/sprite 一致性、取消、失败重试、清理后重建、源文件变化与强制重建换代。
- [ ] 补 Playwright E2E：桌面悬停、快速移动取消旧请求、媒体切换，以及基于 Pointer/touch 仿真的移动长按与抬手 seek。
- [ ] 在 Windows 安装态 PWA 完成 headed 人工验收并由用户确认：真实长视频上帧图、时间、交互延迟和仿真移动手势符合预期；真实 Android/iOS 触摸验收留 P7。
- [ ] 文档同步：PRD 状态、ARCHITECTURE、API、CHANGELOG。

## 5. 验收标准

- 真实视频 fixture 经配置的 ffmpeg 生成至少两张 sprite 和一份有效 WebVTT；所有 cue 均能命中存在且可解码的 `#xywh` 区域，首尾时间覆盖正确。
- 同一媒体不同 profile、源指纹或 generation，以及同一 media ID 在不同 Space 的任务、路径、VTT URL、任务幂等键与缓存登记完全隔离，跨 Space 或跨 generation 请求返回拒绝或不存在。
- 缺失预览轨时 API 在短请求内返回 `202` 和可查询任务，不阻塞播放；任务成功后再次查询只返回当前预览指针指向的可用轨道。
- `timeline_preview` kind 与 `timeline_previews/` root 可由缓存盘点双向识别，目录层级能还原 Space/media/profile/source_fingerprint/generation；清理只删除目标白名单 generation，原媒体 hash/mtime、字幕应用数据、HLS、缩略图、数据库和其他 Space 资产均不变。
- 每次强制重建都产生新 `generation_id`；失败时旧轨与当前指针仍可用，成功时缓存登记和当前指针在事务边界后切换，新 VTT 与 sprite 不出现跨 generation 混用。
- 桌面 hover 能显示指针对应时间与帧图；快速横向移动时旧请求被取消或迟到响应被丢弃，最终浮层始终对应最新指针位置。
- P3 通过 Playwright Pointer/touch 仿真验证长按 400 ms 进入预览、横向移动更新帧图、抬手只执行一次目标 seek；滚动、指针取消、短按和明显纵向移动不误触发预览 seek。
- Playwright 在真实后端与真实 fixture 下覆盖桌面和移动视口仿真；不得只以 mock VTT 或静态截图替代端到端验收，也不得把仿真结果表述为 Android/iOS 真机触摸结论。
- Windows 安装态 PWA 的 headed 模式人工复验真实长视频通过，并由用户明确确认桌面 hover、快速移动和仿真移动长按体验；真实 Android/iOS 设备触摸、浏览器手势冲突和长按体验在 P7 单独验收。
- `go test`、真实 ffmpeg 集成测试、Playwright E2E、player-core 测试与 `pnpm run quality` 全绿。

## 6. 风险 / 待定

- 已确认：预览轨属于可重建缓存，统一登记 `cache_assets`；`timeline_preview` kind 与 `timeline_previews/` root 必须纳入 FR2-048 白名单与盘点/清理映射，不作为媒体源数据或应用持久数据。
- 已确认：首版使用 ffmpeg sprite + WebVTT，不引入逐帧视频预览或额外播放器内核。
- 已确认：P3 的移动长按证据为 Playwright Pointer/touch 仿真与 Windows 安装态 PWA，真实 Android/iOS 设备触摸验收留 P7。
- 真实 ffmpeg 在不同平台可能产生轻微图像差异；自动化测试应断言尺寸、数量、时间与可解码性，不对像素做脆弱的全量精确比较。
- 长视频 profile 会影响生成耗时、磁盘和预览精度；默认参数在实现前以真实 fixture Benchmark 固化，不在本规格中承诺未经测量的性能数值。
