# 功能规格：字幕与多音轨

> 状态：候选发布@v0.24.0-rc.1　·　关联 PRD：FR2-044　·　阶段：P3 `0.24.x`

## 1. 背景与目标

现有能力以“同目录外挂字幕转换为 WebVTT”为主，字幕来源、稳定标识、上传持久化、内嵌字幕和多音轨尚未形成统一播放契约。不同播放路径对音轨枚举与切换的支持也不一致，前端不能把“存在轨道”误判为“当前后端可切换”。

目标：

- 统一外挂字幕、用户上传字幕、内嵌字幕与音轨的播放轨道模型和稳定选择语义。
- 明确上传格式、大小、应用数据路径、删除和非缓存边界，绝不写回媒体原目录。
- 由 `packages/player-core` 通过与 FR2-036 可选能力扩展口径一致的 `TrackFacet` 管理字幕/音轨选择，`PlaybackBackend` 显式报告切换能力并在不支持时可理解地降级。
- 使用真实多音轨、外挂字幕和内嵌字幕 fixture，在浏览器与已安装 PWA 中验证枚举、渲染、切换、降级与持久化。

前置依赖：FR2-007（Space/索引）、FR2-030（内嵌 stream 元数据）、FR2-040（审计事件）、ADR-0057（player-core 与 PlaybackBackend 契约）。

## 2. 需求（要什么）

- 字幕来源统一为：`sidecar`（媒体目录外挂）、`uploaded`（用户上传）、`embedded`（容器内嵌）。
- 轨道模型同时覆盖 `subtitle` 与 `audio`，提供稳定轨道 ID、来源、格式/codec、语言、标题、默认/强制标记、可用性和不支持原因。
- 外挂字幕继续只读发现，不修改、移动或删除媒体目录中的原字幕文件；枚举阶段跳过符号链接与非普通文件，读取入口只接受媒体目录根和已枚举 basename，并通过 `os.OpenInRoot` 打开后再次确认普通文件。路径逃逸、链接替换、文件消失或非普通文件统一按不存在处理。
- 用户上传仅接受 SRT、ASS、SSA、VTT；单文件最大 `10 MiB`（10 × 1024 × 1024 bytes），超出即拒绝且不得留下部分文件。
- 上传字幕保存到应用数据 `subtitles/{space}/{media}`；使用服务端生成的文件名，不信任客户端路径或文件名，不写媒体原目录。
- 上传字幕是用户持久数据，不登记到 `cache_assets`，不参与缓存盘点、一键清理或可重建缓存回收。
- 上传字幕支持显式删除；删除必须同时删除数据库关联和应用数据文件并写审计。外挂和内嵌字幕不提供“删除源文件”操作。
- SRT/ASS/SSA/VTT 与受支持的内嵌文本字幕统一输出规范化 WebVTT；原始上传文件保留在应用数据中，转换固定为按内容请求即时执行，不写持久转换产物、不登记缓存，也不引入字幕转换缓存。
- 内嵌图片字幕或当前转换器不支持的 codec 必须标记为不可渲染并返回原因，不得伪装为空字幕成功。
- SMB 媒体的同目录外挂字幕发现与读取本期不支持；轨道响应必须显式返回来源能力 `available=false` 与 `unsupported_reason=SMB_SIDECAR_UNSUPPORTED`，不得以静默空列表表示“没有外挂字幕”。
- 用户可选择字幕轨、关闭字幕，并调整基础样式：字号、文字颜色、背景透明度和垂直位置；偏好属于用户/客户端设置，不写回字幕源文件。
- 播放器枚举所有音轨并显示语言、标题、codec、声道和默认标记；用户选择后统一调用当前 `TrackFacet.selectTrack('audio', trackId)` 执行切换。状态必须区分用户意图 `selected_track_id` 与 HLS 状态确认的实际生效轨道 `effective_track_id`，UI 的“正在播放”标记以后者为准；在 HLS 源实际可播放前绝不推测或伪造有效值。
- 音轨能力显式分为 `seamless`、`reload`、`unsupported`。P3 Web 的真实 `reload` 仅面向本地、至少两个可确认内嵌音频 stream 的媒体：服务端按目标 `stream_index` 生成与其他音轨隔离的单音轨 H.264/AAC HLS profile。SMB、单音轨、缺少或无法确认 `stream_index` 的媒体必须严格标记为 `unsupported`。
- `unsupported` 时保留当前可播放音轨，禁用不可执行的切换并显示原因；不得触发 Network Error、播放中断、伪造的 `effective_track_id` 或虚假的已选中状态。
- `reload` 候选源只有在 hls.js 可用、清单解析成功且媒体达到 `canplay` 后才能提交。hls.js 不可用、manifest/fatal 错误或 ready 超时都必须恢复切换前完整 source 与 `selected/effective` 快照，不得改用 mpegts.js 伪降级；初始 `effective_track_id=null` 时回滚仍须保留 `null`。
- 范围内：统一模型、外挂/上传/内嵌字幕、上传删除、字幕样式、多音轨能力与切换、真实 fixture、自动化和 headed 验收。
- 不做（范围外）：在线字幕搜索/下载、字幕 OCR、字幕编辑器、时间轴偏移校正、修改或删除媒体目录源字幕、把上传字幕写入媒体容器。

## 3. 设计（怎么做）

### 3.1 统一轨道模型

播放轨道响应使用同一基础结构：

- `id`：在同一 Space/media 下稳定，禁止使用会随排序变化的数组下标作为外部标识。
- `kind`：`subtitle` 或 `audio`。
- `source`：字幕为 `sidecar` / `uploaded` / `embedded`，音轨为 `embedded` 或播放路径提供的派生轨。
- `format` / `codec`、`language`、`title`、`is_default`、`is_forced`。
- `available`、`capability`、`unsupported_reason`；前端只依据能力字段启用操作，不根据轨道数量猜测。
- `stream_index` 仅用于容器内嵌流；磁盘路径和应用数据相对路径不得暴露给客户端。

轨道响应按 `audio` 与 `subtitle` kind 分别返回 `selected_track_id` 与 `effective_track_id`，不得用一组字段混合两类轨道：前者表示 player-core 当前接受的用户选择意图，后者表示 `PlaybackBackend` 已确认实际输出的轨道。切换进行中二者可以暂时不同；成功后收敛，失败或回滚后两者恢复为原实际轨道。字幕关闭使用字幕 kind 的 `selected_track_id=null`，实际字幕关闭后对应 `effective_track_id` 也为 `null`。

统一列表由三类来源组装：FR2-030 的 ffprobe stream 元数据、扫描发现的同媒体外挂字幕、上传字幕持久记录。排序固定为默认轨优先，其次语言/标题，最后稳定 ID；刷新元数据不得无故改变用户保存的轨道选择。响应另含来源级能力，避免无法发现轨道时只能返回空列表；SMB 媒体必须返回 `sidecar.available=false` 与 `sidecar.unsupported_reason=SMB_SIDECAR_UNSUPPORTED`。

### 3.2 字幕持久化与安全

- `media_subtitle_tracks` 记录 `id`、`space_id`、`media_id`、`source`、`source_ref`、`storage_relative_path`、`stream_index`、`format`、`language`、`title`、`is_default`、`is_forced`、`created_at`、`updated_at`。
- 唯一性按来源定义：内嵌轨使用 Space/media/stream index，外挂轨使用 Space/media/规范化来源引用，上传轨使用服务端轨道 ID。
- 上传采用 multipart 流式读取并设置硬上限；先写 `subtitles/{space}/{media}` 下临时文件，完成大小、扩展名、内容基本校验后原子改名并提交数据库记录。
- 允许扩展名大小写不敏感，但扩展名和内容类型/文本结构必须一致；拒绝路径穿越、空文件、二进制伪装和超限文件。
- 存储文件名使用服务端生成的轨道 ID 与规范化扩展名；原始文件名只可作为经清理的展示信息，不参与路径拼接。
- 删除仅允许 `source=uploaded`，并同时校验 Space、media 和轨道归属；删除失败不得留下“记录已删但文件仍被错误复用”的可见状态。
- `subtitles/` 是应用持久数据根目录，不加入 FR2-048 缓存白名单；缓存清理和 timeline/HLS/thumbnail 重建不得触碰该目录。
- 外挂内容读取的受限根是媒体文件所在目录：枚举拒绝 symlink/非普通文件，内容入口仅接受 basename，通过 `os.OpenInRoot` 打开并在文件句柄上复验普通文件；越界、悬空链接、替换后的链接或文件消失统一拒绝，不能仅靠字符串前缀判断。上传内容只按数据库记录与服务端生成文件名对应的受控应用数据相对路径读取。

### 3.3 字幕转换与渲染

- `GET /api/play/:id/tracks` 返回统一音轨、字幕轨、按 kind 分组的 `selected_track_id` / `effective_track_id`、来源级能力与当前播放后端能力。
- `POST /api/play/:id/subtitles` 上传字幕；`DELETE /api/play/:id/subtitles/:track_id` 仅删除上传字幕。
- `GET /api/play/:id/subtitles/:track_id/content` 返回规范化 `text/vtt`；所有端点必须 Space scoped。
- SRT/ASS/SSA 转换为 WebVTT，VTT 做安全解析与规范化；内嵌文本字幕通过配置的 ffmpeg 工具按 stream index 提取并转换。每次内容请求独立转换并直接流式或以请求级临时文件返回，响应结束后清理临时文件。
- 本期不持久化任何规范化 WebVTT，不新增 `cache_assets` kind，不复用上次转换结果，也不把字幕转换目录加入 FR2-048 盘点/清理白名单。
- 转换输出必须转义不受支持的标记，前端字幕层不得直接注入未消毒 HTML。ASS/SSA 首版保留文本和基础换行，不承诺卡拉 OK、矢量绘图和复杂定位完整还原。
- 字幕选择统一调用 `TrackFacet.selectTrack('subtitle', trackId)`，关闭字幕统一传入 `trackId=null`；轨道失效时回退到关闭或可用默认轨，并向 UI 暴露回退原因。
- Web 字幕层消费统一 cue，样式偏好只影响渲染层；切换字幕不得重载视频或改变当前播放时间。

### 3.4 音轨能力与切换

- `packages/player-core` 沿用 [FR2-036](fr2-036-player-core.md) 的向后兼容可选能力扩展口径，定义可选 `TrackFacet`；轨道 kind 固定为 `'audio' | 'subtitle'`，统一使用 `selectTrack(kind, trackId)`，并按 kind 暴露 `selected_track_id` 与 `effective_track_id` 状态、请求代次和切换结果，但不直接访问网络、DOM 或具体播放内核。
- 播放后端通过 `TrackFacet` 提供音轨/字幕轨枚举、选择能力探测、后端确认状态与统一 `selectTrack(kind, trackId)`；字幕关闭传入 `kind='subtitle'`、`trackId=null`。player-core 不直接操作 mpegts.js、MSE 或 DOM。
- `seamless`：后端在当前播放源内切换，保持播放时间、播放/暂停状态和速率；只有后端事件确认后才更新 `effective_track_id`。
- `reload` 能力判定必须同时满足：媒体来源为本地文件、可确认的内嵌音频 stream 数量大于 1、目标轨具有有效 `stream_index`。任一条件不满足都返回 `unsupported`；不得仅因轨道列表非空或只有一个音轨就宣称可重载。
- `POST /api/play/:id/audio-reload` 使用冻结 HTTP 契约：成功创建任务时返回 `202`，响应必须包含字符串 `task_id`、`profile_id` 与受鉴权 HLS `url`。服务端以目标 `stream_index` 执行单音轨映射，生成独立 H.264/AAC HLS profile，不能复用包含其他音轨的通用 profile；音轨 profile 保留命名空间只允许该专用入口创建，普通 preview 入队不得伪造。worker 在清理旧产物和启动转码前还必须读取本地源文件当前 size/mtime，与入队源指纹交叉校验，数据库快照未刷新时的真实文件变化同样必须拒绝。
- 音轨 HLS 以完整任务身份隔离：目录固定为 `hls/{space}/{media}/{profile}/tasks/{task_id}/`，URL 固定为 `/api/play/hls/{media}/profiles/{profile}/tasks/{task_id}/master.m3u8`，缓存登记固定为 `kind=hls + profile_id={profile} + variant={字符串 task_id}`。创建响应、状态响应、实际 URL、磁盘目录与缓存资产必须指向同一个 task；同 profile 的不同任务不得覆盖、回退或串读。任务级清单与切片读取前必须确认数据库中存在同 Space/media/profile/payload 的 `succeeded` 任务，并通过受限根打开的同一文件句柄完成校验与响应，不能仅把 `task_id` 当目录名。
- `GET /api/play/:id/hls-status` 查询音轨重载任务时必须携带创建响应中的 `task_id` 并精确查询该任务；不得按 media/profile 猜测“最新任务”。只有该任务成功、对应任务目录清单存在且状态返回目标轨的 `effective_track_id` 后，前端才拥有可提交的实际轨道依据。
- 切换前固定保存原播放 source 与完整 `selected_track_id/effective_track_id` 快照；播放控制态则使用递增 revision 跟踪事务期间后发的时间、播放/暂停状态和速率。回滚原 source 或提交候选 source 时都必须恢复最新 revision，而不是覆盖为请求发起瞬间的旧控制态；字幕选择随完整快照恢复。`effective_track_id` 允许为 `null`，快照和回滚不得用目标轨或默认轨填补该空值。
- 前端只把 HLS 作为 audio reload 候选源：hls.js 必须声明支持，使用带认证与 Space 凭据的请求加载清单，并同时通过 manifest parsed 与媒体 `canplay` 门槛。hls.js 不可用、manifest/fatal、候选期间媒体元素 `error`、ready 超时或任务失败时，不得改用 mpegts.js；必须取消目标代次、销毁候选 HLS 实例并恢复旧 source、完整轨道快照与事务期间最新播放控制态。提交前候选错误属于事务失败，不发布终态播放错误。
- 候选 HLS ready 后，从该 `task_id` 的 HLS 状态读取并提交目标 `effective_track_id`，再按最新控制 revision 恢复时间、播放/暂停状态、速率和字幕选择。任一步失败都回滚；若回滚本身失败，UI 进入明确失败态并展示最后确认的实际轨道，不得把目标轨标为生效。提交完成后当前 HLS 再发生 fatal 时不回滚旧 source、不创建 mpegts.js，只销毁当前 HLS 并发布一次 `category=media`、`code=HLS_FATAL` 的受控错误。
- `unsupported`：UI 显示轨道信息但禁用切换，说明当前来源、音轨数量、stream index、转码 profile 或播放后端不支持；单音轨必须保持 `unsupported`，避免误导性的 reload 能力。
- 切换请求带递增代次；用户连续快速选择时取消旧协商并忽略迟到结果。当前代次轨道清单刷新若移除目标轨或把它标记为不可用，必须立即取消对应字幕/音轨请求并失效其 generation，迟到内容或 HLS 结果不得重新提交；仍存在的已完成稳定选择继续保留。最终 UI 与 HLS 状态确认的 `effective_track_id` 一致。当前播放路径无法输出所选音轨时，服务端返回结构化不支持结果，不静默切回另一音轨并声称成功。

## 4. 任务拆分

- [x] 定义统一播放轨道 DTO、稳定 ID、来源枚举、来源级能力、`selected_track_id`、`effective_track_id` 与不支持原因。
- [x] 定义并迁移 `media_subtitle_tracks`，接入内嵌 stream、外挂发现和上传记录的统一组装；SMB 外挂来源返回 `SMB_SIDECAR_UNSUPPORTED`。
- [x] 实现 SRT/ASS/SSA/VTT 上传校验、10 MiB 硬上限和 `subtitles/{space}/{media}` 原子持久化。
- [x] 实现上传字幕显式删除、Space 鉴权、文件与记录一致性及审计事件。
- [x] 明确 `subtitles/` 非缓存边界，补缓存盘点/清理不触碰应用字幕数据的回归测试。
- [x] 实现外挂、上传和内嵌文本字幕的按请求 WebVTT 转换接口、请求级临时文件清理及不支持 codec 响应，不新增转换缓存。
- [x] 在 player-core 可选 `TrackFacet` 固定 `'audio' | 'subtitle'` kind，统一实现 `selectTrack(kind, trackId)`、关闭/失效回退与 selected/effective 轨道状态。
- [x] 在 Web 播放后端的 `TrackFacet` 实现音轨/字幕轨枚举、`seamless` / `reload` / `unsupported` 能力、实际轨道确认与 reload 失败回滚；本地多音轨使用隔离的单音轨 H.264/AAC HLS profile。
- [x] 在 Web 播放器实现字幕菜单、上传/删除入口、基础样式状态与样式控件、音轨菜单及不支持提示。
- [x] 补单元测试：稳定 ID、来源排序、SMB 来源能力、单音轨 unsupported、上传边界、内容校验、路径安全、删除权限、HTML 转义、按请求转换、快速切换竞态与 reload 回滚。
- [x] 补真实集成测试：真实多音轨/多字幕 fixture 的 ffprobe 枚举、内嵌提取、本地外挂关联、SMB unsupported、单音轨 unsupported、四种上传格式、删除和缓存清理隔离。
- [x] 补 Playwright E2E：字幕关闭/切换/样式、上传/删除、真实 HLS 音轨切换、冻结 API 契约、`selected/effective` 状态、快照恢复、HLS 凭据与 unsupported 降级。
- [x] CI 的 Linux e2e job 显式安装并校验 ffmpeg/ffprobe；安装或版本探测失败直接使 job 失败，FR2-044 真实 fixture 不再把 CI 缺工具解释为可接受 skip。Windows headed 继续作为开发者本机门，不塞入无交互 CI。
- [x] 在浏览器与已安装 PWA 中完成真实媒体 headed 人工验收并由用户确认：Windows Chromium headed 真服务自动验收与安装态 PWA 人工听辨均已通过。
- [x] 文档同步：本功能规格、API、ARCHITECTURE 与 CHANGELOG 已同步真实 audio reload、任务资产隔离、字幕受限读取、前端事务契约与最终发布状态。

## 5. 验收标准

- 真实 fixture 至少包含：两个可辨识语言或内容的音轨、一个内嵌文本字幕轨、一个内嵌不支持字幕轨，以及配套 SRT/ASS/SSA/VTT 外挂或上传样本；不得只使用 mock 轨道列表。
- 外挂、上传、内嵌字幕在同一 API 中具有稳定 ID 和明确来源；刷新或重新排序后，已选轨道不会因数组下标变化切到其他轨。SMB 媒体即使没有可枚举的外挂字幕，也返回 `unsupported_reason=SMB_SIDECAR_UNSUPPORTED`，不静默返回无法区分“没有字幕”与“不支持发现”的空结果。
- SRT、ASS、SSA、VTT 四种上传格式在不超过 10 MiB 时可保存到 `subtitles/{space}/{media}` 并渲染；大于 10 MiB、伪装格式、路径穿越和二进制内容被拒绝且不留临时文件。外挂枚举不暴露 symlink；即使已枚举文件随后被替换为指向目录内外的链接，也必须由受限根读取拒绝且不返回内容。
- 上传不会修改媒体原目录中任何文件；上传前后原媒体及外挂字幕 hash/mtime 不变。
- 上传字幕和按请求生成的规范化 WebVTT 均不产生 `cache_assets` 记录；连续请求同一字幕会分别执行转换，响应结束后无持久转换文件残留。执行全量缓存盘点与清理后，上传记录和原始文件仍存在且可播放。
- 用户显式删除上传字幕后，列表、内容接口和应用数据文件均不可再访问，并产生审计事件；外挂和内嵌字幕没有删除源文件入口。
- 受支持的内嵌文本字幕可按请求转换并显示；不支持的图片/codec 字幕显示明确原因，不返回伪成功空轨。
- 字幕可关闭和切换，基础样式生效；字幕文本经过安全处理，恶意标记不会执行脚本或注入 DOM。
- `TrackFacet` 契约只接受 `'audio' | 'subtitle'` kind，并由统一 `selectTrack(kind, trackId)` 完成音轨选择、字幕选择与字幕关闭；`seamless` 后端切换音轨时不中断时间线。真实 `reload` 必须经 `POST /api/play/:id/audio-reload` 获得 `202 + string task_id + profile_id + url`，再以同一 `task_id` 精确查询 `GET /api/play/:id/hls-status`；不得按最新任务或 profile 猜测结果。创建响应、状态、URL、任务目录与 `cache_assets.variant=task_id` 必须保持同一任务身份，任务级 URL 还必须拒绝不存在、身份不匹配或未成功的任务记录。
- 本地双音轨 fixture 的每个目标轨都生成只映射对应 `stream_index` 的独立 H.264/AAC HLS profile；同 profile 的不同成功任务使用各自 `tasks/{task_id}` 目录，不能互相覆盖或读到另一任务资产。入队后若本地源真实 size/mtime 发生变化，即使数据库记录尚未刷新，也必须在清理旧产物前拒绝执行。SMB、单音轨与无法确认 `stream_index` 的媒体保持 `unsupported`，不创建伪 reload 任务。
- 候选源只有在 hls.js 支持、manifest parsed 且媒体 `canplay` 后才可提交；`effective_track_id` 必须取自该任务的 HLS 状态。hls.js 不可用、提交前 manifest/fatal、候选媒体元素 `error`、任务失败或 ready 超时均不使用 mpegts.js 兜底，并恢复原 source、字幕、切换前完整 `selected/effective` 快照与事务期间最新播放控制态，包括原 `effective_track_id=null`。当前清单刷新移除或禁用在途目标时必须取消请求，迟到结果不得提交。提交后 fatal 不回滚旧源，只发布一次 `HLS_FATAL` 受控错误。
- `unsupported` 后端保留当前播放且显示不可切换原因，不出现 Network Error、黑屏、错误选中态、伪造的 `effective_track_id` 或静默成功。
- Playwright 使用真实后端与真实媒体 fixture 覆盖字幕和音轨主路径；Linux CI 必须能直接使用 ffmpeg/ffprobe，工具安装或探测失败时硬失败而非接受 FR2-044 skip。Windows Chromium headed 真服务验收保留为开发者本机门；安装态 PWA 仍需在 P3 整体验收中由用户明确确认两个音轨内容可听辨、字幕来源可辨识、上传删除、reload 回滚与降级行为正确。
- `go test`、Go race/vet/覆盖率、真实 ffmpeg/ffprobe 集成测试、player-core build/lint/test/coverage、前端 lint/typecheck/build/test/coverage 与 FR2-044 Playwright E2E 全绿。

## 6. 验收证据

- 后端回归：`TestGetTracksKeepsSingleAudioUnsupported` 先复现单音轨被误标为 `reload`，修复后与 `CreateAudioReload` 相关端点测试共同通过；全量 `go test ./...`、`go test -race ./...`、`go vet ./...` 与 Go 覆盖率策略门均通过。
- 真实转码：`internal/transcoder/audio_reload_hls_test.go` 使用真实 ffmpeg/ffprobe 验证按目标 stream index 生成隔离的单音轨 H.264/AAC HLS；FR2-044 真服务日志分别出现 `-map 0:2` 与 `-map 0:1`，两个目标 profile 均完成切片并可读取 master/segment。
- 前端契约：`WebPlaybackBackend.test.ts` 22/22 通过，覆盖 hls.js 请求的认证与 Space 凭据、manifest + `canplay` ready 门槛、fatal/超时销毁候选源和禁止 mpegts.js 伪降级；`VideoPlayer.tracks.test.tsx` 覆盖任务精确查询、effective 延迟提交与完整快照回滚。
- player-core 门禁：build、lint、全量测试与覆盖率全部通过；音轨事务只在后端确认后收敛 `selected/effective`，并保留原 `effective_track_id=null` 的回滚语义。
- 前端全量门禁：lint、typecheck、build 通过；Vitest 139 个文件、1033 个测试全部通过；V8 覆盖率为 statements 81.59%、branches 81.87%、functions 71.31%、lines 81.59%。测试运行仍有既有 React `act(...)`、jsdom `scrollTo/navigation` 与未匹配 MSW 请求的 stderr 痕迹，但进程与覆盖率门均成功。
- 真服务 E2E：`pnpm exec playwright test e2e/subtitle_audio_tracks_e2e.spec.ts` 在 Chromium headless 通过 1/1；同一用例追加 `--headed` 在 Windows Chromium headed 通过 1/1。用例覆盖双音轨 `reload`、单音轨 `unsupported`、冻结的 202/task/profile/url 契约、按 task 查询 HLS 状态、切片可读、HLS 请求头、播放位置/暂停/速率/字幕恢复。
- Windows 安装态 PWA 的真实听辨与用户确认已于 2026-07-17 完成；FR2-044 满足 P3 发布门并标记为 `候选发布@v0.24.0-rc.1`。

## 7. 风险 / 待定

- 已确认：上传仅支持 SRT、ASS、SSA、VTT，单文件最大 10 MiB。
- 已确认：上传字幕保存到应用数据 `subtitles/{space}/{media}`，不写原目录、不属于可清理缓存，并支持显式删除。
- 已确认：字幕转换固定为按内容请求执行，本期不持久化规范化 WebVTT，也不引入转换缓存。
- 已确认：显式删除只作用于用户上传字幕；外挂和内嵌字幕由源媒体管理，本功能不删除源文件或改写容器。
- 已确认：P3 不支持 SMB 同目录外挂字幕发现/读取，统一返回 `SMB_SIDECAR_UNSUPPORTED`，不得静默为空列表。
- 各播放后端的音轨切换能力可能不同，必须以运行时能力为准；P3 不以牺牲稳定播放换取表面一致的切换按钮。
- ASS/SSA 复杂排版和内嵌图片字幕完整渲染不在首版范围，必须通过能力与原因字段透明表达，不能静默丢失。
