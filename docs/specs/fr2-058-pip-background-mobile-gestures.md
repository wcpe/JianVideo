# 功能规格：画中画、后台音频与移动手势

> 状态：开发中　·　关联 PRD：FR2-058　·　阶段：P3 `0.24.x`（Web/PWA）/ P7 `0.28.x`（Android/iOS 收口）　·　分支：待定

## 1. 背景与目标

FR2-058 面向播放期间离开主窗口、使用系统媒体控制，以及在触摸屏上快速调整播放位置和音量的场景。P3 只承诺浏览器标准能力可覆盖的 Web/PWA 范围，不把原生移动端能力提前包装成 Web 已交付能力。

目标：

- 在支持的浏览器中使用原生 Picture-in-Picture API 提供真实画中画，不用站内浮层冒充系统 PiP。
- 使用 Media Session API 暴露媒体信息和播放控制，并在浏览器仍允许媒体元素运行时支持后台音频。
- 为触摸设备提供横向滑动快进/快退、右侧纵向媒体音量与左侧纵向播放器视觉亮度调节，且不破坏点击、双击、进度条拖动与页面滚动。
- 明确浏览器平台边界：PiP、Media Session 与 Pointer Events 由 `WebPlatformAdapter` 或 `frontend` 壳层承载，不扩展 player-core 的 `PlaybackBackend`。
- 明确 Web 没有标准系统亮度控制 API：P3 的左侧纵向手势只调整播放器视觉层亮度，不修改系统亮度，也不把视觉效果描述为系统能力。
- 以 Windows 真实 Chrome 与已安装 PWA 的 headed 实跑作为 P3 阻断真机门；Android/iOS 的后台、锁屏、系统亮度和原生手势收口留到 P7。

前置依赖：FR2-036（可复用播放器核心）、FR-45（PWA 基线）以及现有播放器全屏、PiP、播放状态和音量控制能力。

## 2. 需求（要什么）

### 2.1 P3 Web/PWA 范围

- PiP：
  - PiP API 探测、调用、事件订阅和资源清理由 `WebPlatformAdapter` 或 `frontend` 壳层负责，不进入 `PlaybackBackend`。
  - 仅在浏览器声明支持、当前视频可进入 PiP 且播放源可用时展示入口。
  - 进入和退出必须调用浏览器原生 PiP API，并根据 `enterpictureinpicture` / `leavepictureinpicture` 事件同步 UI。
  - 请求被浏览器拒绝、媒体尚未就绪或已有其他元素占用 PiP 时，显示明确中文错误，不显示虚假成功态。
  - 不支持时隐藏或禁用入口并给出能力说明；不得回退为站内绝对定位小窗并继续称为“画中画”。
- Media Session 与后台音频：
  - Media Session API 探测、元数据写入、action handler 注册和清理由 `WebPlatformAdapter` 或 `frontend` 壳层负责，不进入 `PlaybackBackend`。
  - 浏览器支持 `navigator.mediaSession` 时，设置当前媒体标题、应用名、封面，以及播放/暂停、前进、后退、定位和停止等 action handler。
  - 媒体时长和位置有效时同步 position state；直播、未知时长或浏览器拒绝时跳过，不影响播放。
  - 页面进入后台、窗口最小化或 PWA 被遮挡后，只要浏览器没有暂停或冻结媒体元素，音频应继续；恢复前台后播放器状态必须一致。
  - Media Session 只提供元数据与系统控制入口，不授予后台执行权，也不保证所有 Windows 策略、锁屏状态、省电模式或浏览器节流条件下持续播放。
  - P3 不创建静默音轨、不循环播放无声媒体、不使用 Web Audio 保活、不规避操作系统与浏览器的后台策略。
- 移动触摸手势：
  - Pointer Events 的监听、捕获、默认行为协调和事件清理由 `WebPlatformAdapter` 或 `frontend` 壳层负责，不把 Pointer API 放入 `PlaybackBackend`。
  - 在播放器视频交互区横向滑动调整目标播放位置：向右快进、向左快退；移动过程中显示“目标时间 / 总时长”和相对偏移，抬手后只提交一次 seek。
  - 在播放器右半区纵向滑动调整媒体音量：向上增大、向下降低，范围夹取到 `0–1`；调整结果复用现有音量状态和持久化逻辑。
  - 手势必须在超过最小位移后锁定单一方向；锁定后不得在同一次触摸中从 seek 切换为音量。
  - 起点位于进度条、按钮、菜单、字幕设置等控件热区时，不启动播放器面手势。
  - 单击仍负责显示/隐藏控件，双击等已有交互不得被短距离移动误触；手势取消时不提交 seek 或额外音量变化。
  - 媒体画面手势面使用 `touch-action: none` 可靠接管 Pointer Events，避免真实触摸在方向锁定前被浏览器滚动取消；该边界只覆盖视频画面，控件栏与播放器外区域仍保留页面滚动，用户可从这些区域滚动普通页面。
  - 触摸手势采用 Pointer Events 或等价统一输入层；鼠标拖动不自动解释为移动端滑动手势。
- 播放器视觉亮度与系统能力边界：
  - Web/PWA 的左半区纵向滑动可调整当前播放器视觉层亮度，效果只覆盖播放器画面，不修改页面主题、其他媒体或系统屏幕亮度。
  - 能力模型必须同时保留 `systemBrightness='unsupported'`，并以独立的 `playerVisualBrightness` 字段描述播放器视觉亮度；UI 文案必须明确“仅调整播放器画面，浏览器不支持调节系统亮度”。
  - 视觉亮度范围夹取到 `0.5–1.5`，默认值为 `1`；退出全屏时保留当前值，切换播放源与播放器卸载时重置视觉层，不持久化为系统或全局偏好。
  - 视觉实现只能作用于播放器媒体画面层，不得覆盖字幕、控制器和手势提示；亮度提示须使用可读文本与 `aria-live`，不得只依赖颜色或图标表达数值。
  - 手势取消时恢复开始前亮度；重置入口必须可通过键盘和触摸访问，并恢复为 `1`。
  - 若未来浏览器提供经过验证的标准系统亮度 API，须另行更新规格与兼容矩阵；不得自动把播放器视觉亮度迁移为系统亮度控制。

### 2.2 P7 Android/iOS 收口

- Android/iOS 原生应用或受控 WebView 的后台音频模式、音频焦点、锁屏控制、系统 PiP、系统亮度权限与手势接入由 P7 多端规格定义和验收。
- P3 的 player-core 只承载端无关的播放、定位、音量控制语义，并可消费壳层映射后的能力状态输入；PiP、Media Session、Pointer Events 及其原生对象不进入 player-core 或 `PlaybackBackend`，也不预设 Android/iOS 原生桥接协议。
- Android Chrome、iOS Safari、添加到主屏后的移动 PWA 可作为兼容性观察项记录结果，但不作为 P3 交付阻断门，也不得据此宣称 Android/iOS 已收口。

### 2.3 范围外

- 不实现站内悬浮播放器来替代系统 PiP。
- 不承诺 Web 在操作系统锁屏、休眠、浏览器冻结或进程回收后继续播放。
- 不实现系统亮度控制；播放器视觉亮度只属于当前播放器画面效果，不作为系统亮度或跨页面全局亮度。
- 不新增服务端转码、媒体下载、离线媒体缓存或保活服务。
- 不在 P3 验收 Android/iOS 原生后台权限、系统 PiP、锁屏面板和亮度手势。

## 3. 设计（怎么做）

### 3.1 能力模型与分层

`WebPlatformAdapter` 或 `frontend` 壳层负责探测浏览器平台能力，不用单一 `isMobile` 或 user-agent 字符串推断：

- `pictureInPicture`：浏览器 PiP API、文档能力和当前媒体元素状态均满足时为可用。
- `mediaSession`：Media Session API 存在时可用；各 action handler 仍需逐项容错。
- `backgroundAudio`：值为 `best-effort`，表示复用当前媒体元素的浏览器行为，不得标记为 `guaranteed`。
- `touchSeek`：触摸/笔输入且媒体时长可定位时可用。
- `touchVolume`：触摸/笔输入且媒体元素音量可写时可用。
- `playerVisualBrightness`：播放器视觉层可用时为 `available`，只代表当前播放器画面效果。
- `systemBrightness`：P3 Web/PWA 固定为 `unsupported`，不得由 `playerVisualBrightness` 推断为可用。

分层约束：

- PiP、Media Session、Pointer Events 属平台交互，不是解码或播放后端能力，不得加入 `PlaybackBackend` 契约。
- 壳层可把上述结果映射为布尔值、稳定枚举、数值或端无关快照，作为 player-core 的只读输入或命令来源；player-core 不负责主动探测浏览器。
- player-core 的输入、状态和事件不得出现 `HTMLVideoElement`、`Document`、`Navigator`、`PointerEvent`、`MediaSession`、`PictureInPictureWindow` 等 DOM/浏览器类型，也不得保存对应对象引用。
- `PlaybackBackend` 继续只承载 FR2-036 定义的播放内核操作与观测；平台能力适配与其保持正交。

能力变化后 UI 和映射给 player-core 的端无关状态必须实时收敛，例如离开 PiP、媒体源切换、时长从未知变为可定位，不缓存一次探测结果贯穿整个会话。

### 3.2 PiP 状态机

- 状态机由 `WebPlatformAdapter` 或 `frontend` 壳层持有；如需向 player-core 暴露，只传递端无关状态枚举，不传递 PiP 窗口、媒体元素或事件对象。
- 状态至少区分：`unsupported`、`idle`、`requesting`、`active`、`exiting`、`error`。
- 用户操作是进入/退出的唯一主动触发源；请求期间禁止重复点击。
- 真实浏览器事件是最终状态真源，Promise resolve 不能替代 `enterpictureinpicture` / `leavepictureinpicture` 同步。
- 媒体切换、播放器卸载或文档退出 PiP 时清理监听器和悬挂状态。
- PiP 中的播放、暂停、seek 和结束事件继续走同一播放器状态，不创建第二个 video 元素。

### 3.3 Media Session 与后台边界

- Media Session 生命周期由 `WebPlatformAdapter` 或 `frontend` 壳层持有；action handler 只把用户意图映射为 player-core 受控命令，不把 Media Session 对象或 action 事件传入核心。
- 元数据来源使用当前媒体的库内标题和现有封面 URL；封面不可用时省略 artwork，不为本功能生成新图片。
- action handler 复用 player-core 的 `play`、`pause`、`seekBy`、`seekTo`、`stop`，所有位置均夹取到合法范围。
- `playbackState` 随真实播放事件同步；`setPositionState` 只在 duration、position、playbackRate 均为有限合法值时调用。
- 页面 `visibilitychange` 只用于状态记录和恢复同步，不主动暂停，也不启动额外定时保活。
- 后台播放失败时保留普通前台播放能力，并在诊断信息中区分“不支持 Media Session”“浏览器暂停媒体”“操作系统冻结/休眠”，不统一伪装为网络错误。

本功能不新增后端数据模型或 API；媒体元数据、封面和播放 URL 继续使用现有接口。

### 3.4 手势判定

- Pointer Events 的接线与生命周期留在 `WebPlatformAdapter` 或 `frontend` 壳层；方向、位移、归一化坐标等可转换为端无关数值交给纯函数或 player-core 命令，不传递 Pointer 事件对象。
- 手势开始时记录 pointer id、起点、当前时间、当前音量和播放器区域尺寸。
- 未超过方向锁定阈值前不改变媒体；超过阈值后按主轴锁定：横向为 seek，右半区纵向为媒体音量，左半区纵向为播放器视觉亮度；锁定后不得切换方向。
- seek 偏移按播放器宽度映射，并设置固定最大跨度，避免短视频过敏或长视频一次滑到结尾；映射函数作为纯函数测试，最终值夹取到 `[0, duration]`。
- 音量和播放器视觉亮度按播放器高度映射，移动中可预览并实时应用；取消时恢复手势开始前的值，正常结束时保留当前会话结果。
- 多指触摸、第二 pointer 加入、浏览器 `pointercancel`、失去捕获或播放器卸载时安全取消；卸载还必须移除视觉亮度效果并恢复默认值。
- 视觉提示使用播放器现有 overlay 层，不遮挡系统 PiP；提示必须分别写明“媒体音量”和“播放器画面亮度”，不得宣称改变系统音量或系统亮度。

### 3.5 证据分层

| 层级 | 环境 | 可证明内容 | 不能替代 |
|---|---|---|---|
| L1 单元/组件自动化 | Vitest/jsdom，浏览器 API mock | 能力探测、状态机、action handler、手势方向锁定、边界夹取、取消清理、播放器视觉亮度与系统亮度能力隔离 | 真实 PiP 窗口、系统媒体面板、后台调度、真实触摸手感 |
| L2 浏览器自动化 | Playwright Chromium，含触摸设备仿真与 headed 用例 | UI 可见性、事件接线、横滑 seek、右侧纵滑音量、左侧纵滑播放器视觉亮度、控件热区避让、前后台状态恢复 | 已安装 PWA、操作系统级 PiP/媒体键、锁屏与省电策略 |
| L3 Windows headed 真机 | Windows 11 实体机、当前稳定版 Google Chrome、真实 Go 单二进制服务 | 原生 PiP 窗口、浏览器后台音频、Media Session/媒体键、错误降级 | 安装后的 standalone PWA 生命周期 |
| L4 Windows PWA 真机 | 同一实体机安装 JianVideo PWA 并从系统入口启动 | standalone 下 PiP、最小化后台音频、系统媒体控制、恢复前台状态 | Android/iOS 原生能力 |
| P7 多端真机 | Android/iOS 支持矩阵 | 原生/受控 WebView PiP、后台与锁屏、系统亮度和手势最终收口 | P3 Web/PWA 门 |

L1/L2 全绿不能替代 L3/L4。L3 与 L4 均为 P3 阻断门，证据需记录 Chrome 版本、Windows 版本、运行模式、测试媒体、操作步骤、结果和 headed 截图/录屏；截图/录屏只作为验收证据，不进入产品媒体数据。

## 4. 任务拆分

- [x] 在 `WebPlatformAdapter` 或 `frontend` 壳层建立 PiP、Media Session、触摸手势和系统亮度的独立能力模型，不扩展 `PlaybackBackend`。
- [x] 定义映射给 player-core 的端无关能力状态与命令，静态禁止 DOM/浏览器类型进入核心契约。
- [x] 接入原生 PiP 状态机、错误提示和资源清理。
- [x] 接入 Media Session 元数据、action handler、播放状态和位置同步。
- [x] 实现横向 seek、右半区纵向媒体音量、左半区纵向播放器视觉亮度手势及控件热区避让。
- [x] 保持 Web 系统亮度为 unsupported，补播放器视觉亮度的独立能力、无障碍提示和重置边界。
- [x] 补单元/组件测试：能力探测、PiP 状态机、Media Session handler、手势纯函数、取消和边界。
- [x] 补 Playwright：触摸仿真、前后台切换、能力降级和播放器视觉亮度不冒充系统能力。
- [ ] 完成 Windows 真实 Chrome headed 阻断验收并保存证据。
- [ ] 完成 Windows 已安装 PWA headed 阻断验收并保存证据。
- [ ] 文档同步：实现完成后更新 PRD 状态、兼容矩阵、API/架构说明和 CHANGELOG；Android/iOS 收口留 P7。

## 5. 验收标准

### 5.1 自动化门

- PiP、Media Session、Pointer Events 的生产实现只存在于 `WebPlatformAdapter` 或 `frontend` 壳层，`PlaybackBackend` 契约不包含这些平台 API；player-core 静态依赖检查中不存在 DOM/浏览器类型。
- 不支持 PiP 时入口不误导；支持时请求只操作当前 video 元素，状态由真实事件收敛，失败显示中文错误。
- Media Session 支持时元数据和 play/pause/seek/stop handler 正确注册、切换媒体后更新、卸载后清理；不支持时播放不受影响。
- 横向滑动只在越过阈值并锁定横轴后 seek，目标时间不越界，抬手只提交一次。
- 右半区纵向滑动只调整当前媒体音量，范围不越界；取消手势恢复原值。
- 左半区纵向滑动只调整播放器媒体画面视觉亮度，范围不越界；取消恢复原值，卸载恢复默认值，字幕和控制器不受滤镜影响。
- 控件热区、多指、短距离移动、`pointercancel`、普通页面纵向滚动均不会误触 seek、音量或视觉亮度。
- Web/PWA 能力模型中 `systemBrightness=unsupported` 且 `playerVisualBrightness=available`；产品 UI、无障碍文案和测试快照不得把视觉效果表述为系统亮度。
- 现有播放、全屏、键盘、进度条、音量、字幕和续播测试不回归，前端质量门与 Playwright 专项全绿。

### 5.2 Windows 真实 Chrome 阻断真机门

- 在 Windows 11 实体机当前稳定版 Google Chrome 中播放真实有声视频，进入系统 PiP 后出现浏览器原生画中画窗口，主页面与 PiP 的播放/暂停状态一致，退出后 UI 正确恢复。
- 浏览器窗口最小化或被其他窗口遮挡至少 3 分钟，音频不中断；系统媒体键或 Chrome 提供的系统媒体控制可执行播放/暂停，恢复前台后时间位置和状态一致。
- 浏览器拒绝 PiP 或系统媒体控制不可用时，界面如实降级并留下环境诊断，不出现站内假 PiP或“后台模式已开启”的虚假状态。
- 使用 Playwright headed 的触摸仿真完成横滑 seek、右侧纵滑音量、左侧纵滑播放器视觉亮度、页面滚动避让；若测试机具备真实触摸屏，再追加真实触摸手感记录，但不以 DevTools/jsdom 结果替代 PiP/后台真机项。

### 5.3 Windows 已安装 PWA 阻断真机门

- 从 Windows 系统入口启动已安装 JianVideo PWA，确认 `display: standalone`，真实视频可播放。
- standalone 窗口中原生 PiP 可进入和退出；最小化至少 3 分钟后音频继续，系统媒体播放/暂停可用，恢复窗口后状态一致。
- PWA 更新或重启后不保留悬挂 PiP/Media Session handler，不需要额外后台服务即可恢复普通播放。
- L3/L4 任一阻断项未通过时，FR2-058 不得标记 P3 已交付；必须记录失败环境和明确降级范围。

### 5.4 P7 留项

- Android/iOS 的后台、锁屏、系统 PiP、系统亮度与原生手势不计入 P3 通过项。
- P7 必须在各端真实设备重新验收，不得直接继承 Windows Web/PWA 结论。

## 6. 风险 / 待定

- 已确认：P3 以 Windows 真实 Chrome 与已安装 PWA 为 Web/PWA 阻断门，Android/iOS 收口留 P7。
- 已确认：Web 无标准系统亮度控制，`systemBrightness` 固定为 unsupported；左侧纵滑只通过播放器媒体画面层提供独立的视觉亮度效果，并明确无障碍文案、取消恢复和卸载重置边界。
- 已确认：PiP、Media Session 与 Pointer Events 由 `WebPlatformAdapter` 或 `frontend` 壳层承载，不进入 `PlaybackBackend`；player-core 只接收不含 DOM 类型的端无关能力状态和控制意图。
- 后台音频受浏览器、Windows 媒体策略、省电、锁屏和休眠影响；Media Session 不等于后台执行许可。P3 阻断门验证最小化/遮挡，不承诺休眠后持续播放。
- Chrome 能力和 UI 会随版本变化，真机证据必须记录精确版本；兼容性回归失败时应收紧支持矩阵，而不是绕过浏览器限制。
- 触摸手势与双击、进度条拖动、浏览器返回手势存在冲突风险，必须通过热区排除、方向锁定和 Pointer Events 取消路径控制。
