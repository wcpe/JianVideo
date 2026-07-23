/** 媒体库目录 */
export type LibraryKind = 'movie' | 'series' | 'home_video' | 'mixed';

export interface LibraryKindInfo {
  kind: LibraryKind;
  name: string;
  description: string;
  naming_hint: string;
  scan_strategy: string;
}

export interface LibraryPath {
  id: number;
  path: string;
  type: string;
  library_kind?: LibraryKind;
  library_profile_json?: string;
  label: string;
  enabled: boolean;
  created_at: string;
  /** 该库已索引（未软删）的媒体数量，由 GET /api/library/paths 返回 */
  media_count?: number;
}

/** 媒体文件 */
export type WatchEventType = 'progress' | 'pause' | 'seek' | 'ended';
export type WatchEventReason = 'user' | 'ab_loop' | 'restore' | 'system';

/** Space 内媒体观看状态真源 DTO。 */
export interface WatchState {
  space_id: string;
  media_id: number;
  position_seconds: number;
  completed: boolean;
  last_watched_at: string;
  completed_at?: string | null;
  revision: number;
  last_session_id: string;
  last_event_seq: number;
  created_at: string;
  updated_at: string;
}

/** 单次观看事件请求，session 内 event_seq 严格递增。 */
export interface WatchStateEvent {
  position_seconds: number;
  duration_seconds?: number;
  expected_revision: number;
  session_id: string;
  event_seq: number;
  event_type: WatchEventType;
  reason: WatchEventReason;
}

export interface WatchStateUpdateResult {
  applied: boolean;
  current: WatchState;
}

export interface WatchStateConflictResponse extends WatchStateUpdateResult {
  applied: false;
  code: 'WATCH_STATE_CONFLICT';
  message: string;
}

export interface MediaFile {
  id: number;
  /** 媒体所属 Space；旧缓存数据可能缺省。 */
  space_id?: string;
  library_id: number;
  file_path: string;
  file_name: string;
  file_size: number;
  format: string;
  video_codec: string;
  audio_codec: string;
  duration: number;
  width: number;
  height: number;
  bitrate: number;
  subtitle_tracks: string;
  added_at: string;
  modified_at: string;
  /** 内容哈希（FR2-061）：SHA-256 精确去重使用，旧数据可能缺省 */
  content_hash?: string;
  content_hash_algo?: string;
  content_hash_computed_at?: string | null;
  content_hash_stale?: boolean;
  /** 库内显示名（FR-30）：空则展示回退到 file_name，不影响磁盘真实文件名；旧数据可能缺省 */
  display_name?: string;
  /** 收藏标记（FR-41），旧数据可能缺省 */
  favorite?: boolean;
  /** 库内备注（FR-137）：用户自由文本备注，纳入基础搜索；空表示无备注，旧数据可能缺省 */
  notes?: string;
  /** 上次播放位置（秒，FR-44），旧数据可能缺省 */
  last_position?: number;
  /** 是否已看完（FR-44），旧数据可能缺省 */
  watched?: boolean;
  /** 最近一次观看时间（FR-44），旧数据可能缺省 */
  last_watched_at?: string | null;
  /** 观看次数（FR-75）：每看完一次 +1，旧数据可能缺省 */
  view_count?: number;
  /** 最近一次查看（打开）时间（FR-120）：覆盖图片+视频的「打开」，旧数据可能缺省 */
  last_viewed_at?: string | null;
  /** 软删时间（FR-25/FR2-054）：回收站列表项有值；常规列表通常为 null */
  deleted_at?: string | null;
  /** 内容分级（FR2-051）：G / PG / PG-13 / R / UNRATED；空视为未分级，旧数据可能缺省 */
  content_rating?: string;

  /** 媒体时间与 EXIF（FR-31 提取、FR-38 展示），旧数据可能缺省 */
  media_time?: string | null;
  /** 媒体时间来源：exif / filename / created / modified */
  media_time_source?: string;
  camera?: string;
  lens?: string;
  aperture?: string;
  shutter?: string;
  iso?: number;
  gps_lat?: number;
  gps_lon?: number;
  /** 逆地理编码地名（FR-146 接线 FR-147）：由 GPS 就近解析的「省·市」，无 GPS 则空，旧数据可能缺省 */
  location?: string;
  /** 列表批量附带的本地影视信息推断（FR2-031），尚无结果时缺省 */
  inference?: MediaInference | null;
}

export interface WatchMediaItem {
  media: MediaFile;
  watch_state: WatchState;
}

export interface WatchHistoryPage {
  items: WatchMediaItem[];
  next_cursor?: string;
}

/** 当前媒体封面选择语义（FR2-059） */
export interface MediaCover {
  media_id: number;
  space_id: string;
  selected_asset_id: number;
  selected_source: 'video_frame' | 'image' | string;
  selected_timestamp_seconds: number;
  selected_fingerprint: string;
  manual: boolean;
  updated_at: string;
}

/** 本地抽帧封面候选（FR2-059） */
export interface CoverCandidate {
  id: number;
  media_id: number;
  space_id: string;
  asset_id: number;
  source: 'video_frame' | 'image' | string;
  timestamp_seconds: number;
  fingerprint: string;
  score: number;
  image_url: string;
  created_at: string;
  updated_at: string;
}

export interface MediaCoversResponse {
  cover: MediaCover | null;
  candidates: CoverCandidate[];
  cover_url?: string;
}

/** 文件自带元数据当前记录（FR2-030） */
export interface MediaMetadata {
  id: number;
  media_id: number;
  space_id: string;
  source: 'ffprobe' | 'image' | string;
  tool: string;
  tool_version: string;
  raw_json: string;
  normalized_json: string;
  parsed_at: string;
  stale: boolean;
}

/** 文件自带元数据规范化结构（FR2-030） */
export interface NormalizedEmbeddedMetadata {
  media_type?: string;
  container?: { format_name?: string; duration_seconds?: number; bitrate?: number };
  video_streams?: Array<{
    codec_name?: string;
    width?: number;
    height?: number;
    frame_rate?: string;
    average_frame_rate?: string;
    bitrate?: number;
    color?: { range?: string; space?: string; transfer?: string; primaries?: string };
  }>;
  audio_streams?: Array<{
    codec_name?: string;
    language?: string;
    title?: string;
    channels?: number;
  }>;
  subtitle_streams?: Array<{
    codec_name?: string;
    language?: string;
    title?: string;
    forced?: boolean;
  }>;
  image?: {
    exif?: Record<string, unknown>;
    iptc?: Record<string, string>;
    xmp?: Record<string, string>;
  };
  tags?: Record<string, string>;
}

/** 本地离线影视信息推断（FR2-031）：自动候选或人工纠正结果 */
export interface MediaInference {
  id: number;
  media_id: number;
  space_id: string;
  kind: LibraryKind;
  title: string;
  year: number;
  season: number;
  episode: number;
  episode_title: string;
  confidence: number;
  source: 'offline_rule' | 'manual' | string;
  rule_version: string;
  manual: boolean;
  created_at: string;
  updated_at: string;
}

/** 人工纠正影视信息请求（FR2-031）：留空字段表示清除对应库内推断字段 */
export interface MediaInferenceInput {
  kind?: LibraryKind;
  title: string;
  year?: number;
  season?: number;
  episode?: number;
  episode_title?: string;
}

/** 感知哈希去重重复组（FR-70）：一组互为近似重复的媒体，至少 2 项 */
export type DuplicateGroup = MediaFile[];

/** 内容哈希精确重复组（FR2-061）：一组 SHA-256 完全相同的媒体，至少 2 项 */
export interface ExactDuplicateGroup {
  content_hash: string;
  file_size: number;
  items: MediaFile[];
}

/** 内容哈希回填任务响应（FR2-061） */
export interface FileHashBackfillResponse {
  status: string;
  task_id: string;
}

/** 标签（FR-41） */
export interface Tag {
  id: number;
  name: string;
  created_at?: string;
}

/** 媒体文件列表响应 */
export interface MediaListResponse {
  items: MediaFile[];
  total: number;
  page: number;
  page_size: number;
}

/** 相册（FR-40）：跨目录手动归类媒体的集合 */
export interface Album {
  id: number;
  name: string;
  description: string;
  cover_media_id: number;
  created_at: string;
  updated_at: string;
  /** 成员数量（列表接口附带，详情接口可能缺省） */
  item_count?: number;
}

/** 登录请求 */
export interface LoginRequest {
  username: string;
  password: string;
}

/** 分享资源类型（FR-43） */
export type ShareResourceType = 'media' | 'album';

/** 分享链接（FR-43；密码/限次见 FR-78；禁下载见 FR2-055） */
export interface Share {
  token: string;
  resource_type: ShareResourceType;
  resource_id: number;
  /** 过期时间；null 表示永不过期 */
  expires_at: string | null;
  /** 最大访问次数（FR-78）；0 表示无限 */
  max_uses: number;
  /** 已访问次数（FR-78） */
  used_count: number;
  /** 是否允许公开下载原文件（FR2-055）；默认 true；旧数据可能缺省 */
  allow_download?: boolean;
  created_at: string;
}

/** 公开分享元信息（FR-43）：媒体分享含 media，相册分享含 album 与 items */
export interface ShareInfo {
  resource_type: ShareResourceType;
  expires_at?: string | null;
  media?: MediaFile;
  album?: Album;
  items?: MediaFile[];
  /** 是否需要访问密码（FR-78）；为 true 且未带正确密码时不含 media/album 内容 */
  requires_password?: boolean;
  /** 是否允许下载原文件（FR2-055）；false 时公开页隐藏下载；旧数据缺省视为 true */
  allow_download?: boolean;
}

/** 扫描模式（FR-27）：增量更新只索引新增文件；全量扫描遍历并对账已删文件 */
export type ScanMode = 'incremental' | 'full';

/** 扫描响应 */
export interface ScanResponse {
  scanned?: number;
  status?: string;
  task_id?: number; // 入队任务 ID（FR-29，队列启用时返回）
}

/** Web 上传命名规则（FR-149）：保留原样 / 按日期整齐归档 */
export type UploadNamingRule = 'original' | 'date';

/** Web 上传入库响应（FR-149，见 ADR-0051） */
export interface UploadResponse {
  status: string; // "uploaded"
  library_id: number; // 落盘归属的库 ID
  file_path: string; // 最终落盘路径
  scan_task: number; // 触发的扫描任务 ID（队列启用时非 0）
}

/** 扫描进度状态 */
export interface ScanStatus {
  status: string; // "idle", "scanning", "completed", "error"
  library_id: number;
  current_path: string;
  total_files: number;
  scanned_files: number;
  error: string;
  started_at: string;
  completed_at: string;
}

/** 扫描任务（队列，FR-29） */
export interface ScanTask {
  id: number;
  library_id: number;
  scan_type: string; // "full" / "incremental"
  status: string; // "pending" / "running" / "completed" / "error" / "canceled"
  scanned_files: number;
  total_files: number;
  error: string;
  created_at: string;
  started_at?: string;
  completed_at?: string;
}

/** 扫描任务列表响应（FR-29） */
export interface ScanTasksResponse {
  tasks: ScanTask[];
  current: ScanTask | null;
}

/** 转码目标编码（FR-77） */
export type TranscodeCodec = 'h264' | 'h265' | 'av1' | 'vp9';

/** 转码预设（FR-77）：可复用的目标编码/分辨率模板，width/height 为 0 表示沿用源分辨率 */
export interface TranscodePreset {
  id: number;
  name: string;
  codec: TranscodeCodec;
  width: number;
  height: number;
  created_at: string;
  updated_at: string;
}

/** 转码预生成任务（FR-77）：把「某媒体 + 某预设」入队，单 worker 串行预转码预热首播 */
export interface TranscodeTask {
  id: number;
  media_id: number;
  preset_id: number;
  codec: string;
  width: number;
  height: number;
  status: string; // "pending" / "running" / "completed" / "error"
  error: string;
  created_at: string;
  started_at?: string;
  completed_at?: string;
}

/** 媒体健康问题类型（FR-73） */
export type HealthIssueType = 'broken' | 'zero_byte' | 'missing' | 'no_thumbnail';

/** 媒体健康问题项（FR-73）：问题记录 + 附带的媒体基本信息 */
export interface HealthIssue {
  id: number;
  media_id: number;
  issue_type: HealthIssueType;
  detail: string;
  checked_at: string;
  /** 以下为巡检接口附带的媒体基本信息，媒体已被删除时可能缺省 */
  file_name?: string;
  file_path?: string;
  library_id?: number;
  display_name?: string;
}

/** 健康巡检进度状态（FR-73） */
export interface HealthScanStatus {
  status: string; // "idle" / "scanning" / "completed" / "error"
  total: number;
  checked: number;
  issue_count: number;
  error: string;
  started_at: string;
  completed_at: string;
}

/** 观看时间线的一天（FR-75）：本地日期 YYYY-MM-DD 与当天观看媒体数 */
export interface TimelineBucket {
  date: string;
  count: number;
}

/** 某存储库的已看媒体数（FR-75） */
export interface LibraryWatchCount {
  library_id: number;
  label: string;
  watched: number;
}

/** 某容器格式的已看媒体数（FR-75） */
export interface FormatWatchCount {
  format: string;
  watched: number;
}

/** 观看统计聚合（FR-75）：各维度均仅统计未软删媒体 */
export interface WatchStats {
  total: number;
  watched: number;
  unwatched: number;
  /** 最近观看时间线（按天，倒序） */
  recent_timeline: TimelineBucket[];
  /** 续播位置分布（10 档，下标 0=0-10%…9=90-100%） */
  position_heatmap: number[];
  /** 各存储库已看分布 */
  by_library: LibraryWatchCount[];
  /** 各格式已看分布 */
  by_format: FormatWatchCount[];
  /** 观看次数 Top N */
  top_viewed: MediaFile[];
}

/** 概览看板各库聚合行（FR-117）：单个媒体库的媒体数量与体量统计 */
export interface LibrarySummaryRow {
  library_id: number;
  /** 库展示名，取自 library_paths（LEFT JOIN） */
  label: string;
  /** 该库未软删媒体总数 */
  media_count: number;
  /** 该库视频数（format 不在内置图片后缀名单内） */
  video_count: number;
  /** 该库图片数（format 在内置图片后缀名单内） */
  image_count: number;
  /** 该库文件大小求和（字节） */
  total_size: number;
  /** 该库时长求和（秒，图片为 0 不影响） */
  total_duration: number;
}

/** 概览看板媒体库总量聚合（FR-117）：一次性聚合，所有计数/求和仅统计未软删媒体 */
export interface LibrarySummary {
  /** 未软删媒体总数 */
  total: number;
  /** 视频总数 */
  video_count: number;
  /** 图片总数 */
  image_count: number;
  /** 文件大小求和（字节） */
  total_size: number;
  /** 时长求和（秒） */
  total_duration: number;
  /** 启用库数（与 /paths 口径一致，enabled=1） */
  library_count: number;
  /** 各库聚合（按 library_id 分组） */
  by_library: LibrarySummaryRow[];
}

/** 按天新增媒体的一天（FR-118）：本地日期 YYYY-MM-DD 与当天新增的数量/体量/时长 */
export interface MediaTrendPoint {
  /** 本地日期 YYYY-MM-DD */
  date: string;
  /** 当天新增媒体数 */
  count: number;
  /** 当天新增媒体文件大小求和（字节） */
  size: number;
  /** 当天新增媒体时长求和（秒） */
  duration: number;
}

/** 媒体新增趋势（FR-118）：按天分桶、升序，仅含有新增的天；前端据此累加得累计增长曲线 */
export interface MediaTrends {
  media_added: MediaTrendPoint[];
}

/** 系统指标采样点（FR-119）：一个时间桶的 CPU / 内存 / 磁盘 / 转码并发 / goroutine 快照 */
export interface MetricPoint {
  /** 采样时刻（RFC3339，UTC） */
  t: string;
  /** 系统 CPU 使用率 %（0-100） */
  cpu_percent: number;
  /** 进程已用内存（runtime MemStats Alloc，字节） */
  mem_used_bytes: number;
  /** 进程向 OS 申请内存（Sys，字节） */
  mem_sys_bytes: number;
  /** 数据盘已用（字节） */
  disk_used_bytes: number;
  /** 数据盘总量（字节） */
  disk_total_bytes: number;
  /** 活跃转码 / 播放会话数 */
  transcode_active: number;
  /** goroutine 数 */
  goroutines: number;
}

/** 系统监控时序（FR-119）：下采样后的时序点 + 最新一条原始样本快照（current） */
export interface SystemMetrics {
  /** 时间范围标识：1h / 24h / 7d */
  range: string;
  /** 下采样时序点（按时间升序，点数有界） */
  points: MetricPoint[];
  /** 当前值快照（最新一条原始样本，≤15s 旧）；刚启动无样本时为 null */
  current: MetricPoint | null;
}

/** 媒体类型规则能力 */
export type MediaTypeCapability = 'scan' | 'transcode' | 'thumbnail' | 'metadata';

/** 媒体库后缀类型 */
export type MediaExtensionType = 'video' | 'image' | 'audio' | 'subtitle' | 'sidecar';

/** 媒体类型定义 */
export interface MediaTypeDefinition {
  type: MediaExtensionType;
  name: string;
  description: string;
  default_extensions: string[];
  capabilities: MediaTypeCapability[];
}

/** 媒体类型后缀规则 */
export interface MediaTypeRule {
  id: number | string;
  space_id: string;
  library_id?: number | null;
  extension: string;
  type: MediaExtensionType;
  label: string;
  description: string;
  enabled: boolean;
  builtin: boolean;
  capabilities: MediaTypeCapability[];
}

/** 媒体类型规则列表响应 */
export interface MediaTypesResponse {
  types: MediaTypeDefinition[];
  rules: MediaTypeRule[];
}

/** 新增媒体类型规则请求 */
export interface CreateMediaTypeRuleInput {
  library_id?: number;
  type: MediaExtensionType;
  extension: string;
  label?: string;
  description?: string;
  enabled?: boolean;
}

/** 更新媒体类型规则请求 */
export interface UpdateMediaTypeRuleInput {
  library_id?: number;
  enabled?: boolean;
  label?: string;
  description?: string;
}

/** 媒体库后缀配置 */
export interface MediaExtension {
  id?: number | string;
  library_id: number;
  extension: string;
  type: MediaExtensionType;
  is_builtin?: number;
  builtin?: boolean;
  enabled?: boolean;
  label?: string;
  description?: string;
  capabilities?: MediaTypeCapability[];
  type_name?: string;
  type_description?: string;
  created_at?: string;
}

/** 面包屑路径段 */
export interface BreadcrumbItem {
  name: string;
  path: string;
}

/** 子目录信息 */
export interface DirInfo {
  name: string;
  path: string;
  /** 仅聚合虚拟根（FR-66）的库目录项填充，标识点进去用哪个媒体库 */
  library_id?: number;
}

/** 目录浏览响应 */
export interface BrowseResponse {
  breadcrumbs: BreadcrumbItem[];
  directories: DirInfo[];
  files: MediaFile[];
}

/** 当前用户 */
export interface CurrentUser {
  username: string;
}

/** 单个编码（codec）的支持情况：编入与试编码结果 */
export interface CodecSupport {
  /** 编码格式，如 h264 / h265 / av1 / vp9 */
  codec: string;
  /** 编码器名，如 h264_amf */
  encoder: string;
  /** 是否编入当前 ffmpeg */
  compiled: boolean;
  /** 试编码是否成功 */
  tested_ok: boolean;
}

/** 单项硬件加速能力（per-codec：逐家族 × 逐编码） */
export interface HWAccelCapability {
  /** 家族显示名，如 "AMD AMF"/"软件编码" */
  name: string;
  /** 家族标识，如 software/amf/nvenc/qsv/vaapi/videotoolbox/vulkan */
  family: string;
  /** 设备类型，如 d3d11va/cuda/qsv（software 为 ""） */
  device_type: string;
  /** 该家族至少一个编码可用 */
  available: boolean;
  /** 该家族逐编码支持情况 */
  codecs: CodecSupport[];
}

/** 硬件加速汇总信息 */
export interface HWAccelInfo {
  available: HWAccelCapability[];
  preferred: string;
  /** 系统可输出编码并集，如 ["h264","h265","av1","vp9"] */
  codecs: string[];
  intel_gpu: boolean;
  intel_gpu_detail: string;
  software_fallback: boolean;
  /** 结果是否来自缓存 */
  from_cache: boolean;
  /** 实测所用 ffmpeg 版本 */
  ffmpeg_version: string;
  /** 实测时间（RFC3339），未测为 "" */
  tested_at: string;
}

/** FFmpeg 可用性信息 */
export interface FFmpegInfo {
  available: boolean;
  path: string;
  version: string;
}

/** 运行环境运行时信息（FR-60）：进程与 Go 运行时层面的诊断字段 */
export interface RuntimeInfo {
  /** 进程 PID */
  pid: number;
  /** 当前工作目录 */
  work_dir: string;
  /** 可执行文件路径 */
  executable: string;
  /** SQLite 数据库文件路径 */
  db_path: string;
  /** 运行时长（秒） */
  uptime_seconds: number;
  /** 当前堆上已分配且仍在用的字节数 */
  mem_alloc: number;
  /** 进程向 OS 申请的总字节数 */
  mem_sys: number;
  /** 已完成的 GC 次数 */
  num_gc: number;
  /** GOMAXPROCS（最大并行执行的 OS 线程数） */
  gomaxprocs: number;
}

/** 系统信息 */
export interface SystemInfo {
  app_version: string;
  os: string;
  arch: string;
  num_cpu: number;
  hostname: string;
  go_version: string;
  ffmpeg: FFmpegInfo;
  hwaccel: HWAccelInfo;
  /** 运行环境运行时信息（FR-60） */
  runtime: RuntimeInfo;
}

/** 单个编码器探测结果 */
export interface EncoderProbeResult {
  encoder: string;
  family: string;
  codec: string;
  compiled: boolean;
  tested_ok: boolean;
  detail: string;
}

/** 编解码器测试响应 */
export interface CodecTestResult {
  ffmpeg_available: boolean;
  results: EncoderProbeResult[];
  /** 结果是否来自缓存 */
  from_cache: boolean;
  /** 实测所用 ffmpeg 版本 */
  ffmpeg_version: string;
  /** 实测时间（RFC3339），未测为 "" */
  tested_at: string;
}

// 自更新检测结果（FR-46）
export interface UpdateCheckResult {
  current: string;
  latest: string;
  has_update: boolean;
  tag: string;
  prerelease: boolean;
  channel: 'stable' | 'prerelease';
  notes: string;
  asset_name: string;
  /** 是否存在可回滚的上一版（.old 备份），用于回滚按钮显隐（FIX-2） */
  rollback_available?: boolean;
}

// 自更新下载进度（FR-90）：前端轮询 GET /api/system/update/progress 展示进度条
export interface UpdateProgress {
  /** 进度阶段：idle 空闲 / downloading 下载中 / verifying 校验中 / done 完成 / failed 失败 */
  state: 'idle' | 'downloading' | 'verifying' | 'done' | 'failed';
  /** 已下载字节数 */
  downloaded: number;
  /** 总字节数（0 表示未知，此时 percent 为 0、退化为展示已下载字节） */
  total: number;
  /** 下载百分比 0-100（total=0 时为 0） */
  percent: number;
}

/** 运行期设置键值映射（key → value，值统一为字符串） */
export type SettingsMap = Record<string, string>;

/** 配置分层（FR2-024）：启动固定、运行期可改、派生只读 */
export type SettingLayer = 'startup' | 'runtime' | 'readonly';

/** 配置值类型（FR2-024）：用于前端选择合适控件与提示 */
export type SettingValueType = 'string' | 'int' | 'bool' | 'json' | 'url' | 'path' | 'enum';

/** 枚举型配置的可选项 */
export interface SettingOption {
  value: string;
  label: string;
}

/** 单个配置定义（FR2-024）：由后端 registry 暴露，前端按此渲染分组和说明 */
export interface SettingDefinition {
  key: string;
  label: string;
  description: string;
  layer: SettingLayer;
  value_type: SettingValueType;
  default_value: string;
  sensitive: boolean;
  hot_apply: boolean;
  consumer: string;
  options?: SettingOption[];
}

/** 单个环境变量（FR-56）：只读查看，敏感项 value 为掩码、不含明文 */
export interface EnvVar {
  /** 环境变量名 */
  key: string;
  /** 中文用途说明 */
  description: string;
  /** 是否敏感（敏感项 value 为掩码，绝不回显明文） */
  sensitive: boolean;
  /** 当前是否已设置（非空） */
  set: boolean;
  /** 展示值：非敏感为明文，敏感为固定掩码 */
  value: string;
}

/** FFmpeg 路径检测结果（FR-56） */
export interface FFmpegDetectResult {
  /** 指定路径的 ffmpeg 是否可用 */
  ffmpeg_available: boolean;
  /** 可用时的版本首行，不可用为空串 */
  ffmpeg_version: string;
}

/** 代理连通性测试结果（FR-89） */
export interface ProxyTestResult {
  /** 经待测代理（或直连）是否能连通探测目标 */
  reachable: boolean;
  /** 结果说明：可达为 HTTP 状态、不可达为脱敏后的原因 */
  detail: string;
  /** 本次探测耗时（毫秒） */
  latency_ms: number;
  /** 探测目标地址 */
  target: string;
}

/** 可自动下载的外部工具 */
export type ToolName = 'ffmpeg' | 'ffprobe' | 'magick';

/** 工具下载源（FR2-022）：内置源或后端返回的可选来源 */
export interface ToolSource {
  id: string;
  tool: ToolName;
  platform: string;
  arch: string;
  version: string;
  url: string;
  sha256: string;
  size: number;
  label: string;
  allow_http?: boolean;
}

/** 已安装工具记录 */
export interface ToolInstallRecord {
  version: string;
  path: string;
  updated_at: string;
}

/** 单个工具的运行期配置与安装状态 */
export interface ToolStatus {
  tool: ToolName;
  setting_key: string;
  configured_path: string;
  installed: ToolInstallRecord[];
}

/** 工具下载入队请求 */
export interface ToolDownloadInput {
  tool: ToolName;
  source_id?: string;
  custom_url?: string;
  sha256?: string;
  version?: string;
  allow_insecure_http?: boolean;
}

/** 工具下载入队响应 */
export interface ToolDownloadResponse {
  status: string;
  task_id: string;
}

/** 回收站清理结果统计（FR-26）：成功移动数与失败跳过数 */
export interface RecycleCleanupResult {
  moved: number;
  failed: number;
}

/** 外挂字幕轨道 */
export interface SubtitleTrack {
  index: number;
  file_name: string;
  format: string;
  url: string;
}

/** 一条解析后的字幕条目 */
export interface SubtitleEntry {
  start: number;
  end: number;
  text: string;
}

/**
 * 播放描述符（FR-52）：自适应播放器据此分发到对应内核。
 *
 * - `ts`：H.264/TS（mpegts.js，含 master.m3u8 ABR），追播路径不变。
 * - `fmp4`：高级编码（H.265/AV1/VP9）HLS-fMP4 清单，走 hls.js 原生 MSE（仅 VOD）。
 * - `mp4`：原文件直出，浏览器原生 video。
 *
 * 端到端「按客户端能力选编码 + 触发后端产 fMP4」的协商属 FR-53。
 */
export interface PlaybackFrameTimelineEntry {
  mediaTime: number;
  sourceFrameIndex?: number;
  stableFrameId?: string;
}

export interface PlaybackFrameMarkerDescriptor {
  bits: number;
  cellSize: number;
  threshold?: number;
  x: number;
  y: number;
}

export interface PlaybackFramePresentationDescriptor {
  marker: PlaybackFrameMarkerDescriptor;
  nominalFrameRate: number;
  timeline: PlaybackFrameTimelineEntry[];
}

export interface PlaybackDescriptor {
  /** 目标视频编码（h264/h265/av1/vp9），用于 fmp4 路径前的客户端能力校验 */
  codec: string;
  /** 清单 / 流 URL（fmp4 为 index.m3u8，ts 为流地址，mp4 为原文件地址） */
  url: string;
  /** 播放路径 */
  path: 'ts' | 'fmp4' | 'mp4';
  /** 客户端不支持目标编码时的 H.264/TS 回退源（缺省则展示不支持提示，由 FR-53 提供真实回退源） */
  fallbackUrl?: string;
  /** 显式声明的真实画面帧身份来源；未声明时绝不根据时间推断精确身份。 */
  framePresentation?: PlaybackFramePresentationDescriptor;
}

/** 项目自身协议信息（FR-57）：开源协议页顶部展示 */
export interface ProjectLicense {
  /** 项目名 */
  name: string;
  /** 协议标识（如 MIT） */
  license: string;
  /** 作者 / 版权方 */
  author: string;
  /** 协议全文 */
  text: string;
}

/** 前端依赖的协议信息（FR-57）：均可拿到全文 */
export interface FrontendLicense {
  /** 包名 */
  name: string;
  /** 版本号 */
  version: string;
  /** 协议标识 */
  license: string;
  /** 作者 / 发布者，未知为空串 */
  author: string;
  /** 协议全文，未知为空串 */
  text: string;
}

/** 后端依赖的协议信息（FR-57）：全文尽力而为，拿不到给 pkg.go.dev 链接 */
export interface BackendLicense {
  /** module 路径 */
  name: string;
  /** 版本号 */
  version: string;
  /** 协议标识，未知为空串 */
  license: string;
  /** 协议全文，从本机 module cache 读到则有、否则为空串 */
  text?: string;
  /** 无全文时的 pkg.go.dev 协议页外链 */
  url?: string;
}

/** 开源协议清单（FR-57）：构建期生成、嵌入仓库、运行时不联网 */
export interface LicensesData {
  /** 项目自身 */
  project: ProjectLicense;
  /** 前端生产依赖 */
  frontend: FrontendLicense[];
  /** 后端 go.mod 直接依赖 */
  backend: BackendLicense[];
}

export type AuditScope = 'space' | 'system';

export type AuditActorType = 'user' | 'system' | string;

export type AuditJsonValue =
  string | number | boolean | null | AuditJsonValue[] | { [key: string]: AuditJsonValue };

/** 审计事件（FR2-040）：接口已返回脱敏后的 JSON 字段，前端只负责展示 */
export interface AuditEvent {
  id: number;
  scope: AuditScope;
  space_id: string | null;
  actor_type: AuditActorType;
  actor_id: string;
  action: string;
  resource_type: string;
  resource_id: string;
  before_json: AuditJsonValue | null;
  after_json: AuditJsonValue | null;
  metadata_json: AuditJsonValue | null;
  request_id: string;
  created_at: string;
}

/** 审计事件查询参数（FR2-040）：与 GET /api/audit/events query 对齐 */
export interface AuditEventQuery {
  scope?: AuditScope;
  space_id?: string;
  action?: string;
  resource_type?: string;
  resource_id?: string;
  from?: string;
  to?: string;
  cursor?: string;
  limit?: number;
}

/** 审计事件 cursor 分页响应 */
export interface AuditEventPage {
  items: AuditEvent[];
  next_cursor: string | null;
}

/** 可回滚事件（FR2-041）：GET /api/rollback/events 列表项 */
export interface RollbackEvent {
  id: number;
  scope: AuditScope;
  space_id: string | null;
  action: string;
  resource_type: string;
  resource_id: string;
  before_json: AuditJsonValue | null;
  after_json: AuditJsonValue | null;
  created_at: string;
  rollbackable: boolean;
  reason_key?: string;
}

/** 可回滚事件查询参数 */
export interface RollbackEventQuery {
  scope?: AuditScope;
  days?: number;
  cursor?: string;
  limit?: number;
}

/** 可回滚事件分页响应 */
export interface RollbackEventPage {
  items: RollbackEvent[];
  next_cursor: string | null;
}

export type TaskStatus = 'pending' | 'running' | 'succeeded' | 'failed' | 'canceled';

export interface TaskItem {
  id: string;
  scope: AuditScope;
  space_id: string | null;
  type: string;
  status: TaskStatus;
  priority: number;
  attempts: number;
  max_attempts: number;
  progress: number;
  checkpoint?: string;
  resource_type?: string;
  resource_id?: string;
  error: string | null;
  created_at: string;
  updated_at: string;
  started_at?: string | null;
  finished_at?: string | null;
}

export interface TaskListQuery {
  scope?: AuditScope;
  type?: string;
  status?: TaskStatus | '';
  resource_type?: string;
  resource_id?: string;
  page?: number;
  page_size?: number;
}

export interface TaskListPage {
  items: TaskItem[];
  page: number;
  page_size: number;
  total: number;
}

export interface TaskStats {
  total: number;
  by_status: Record<TaskStatus, number>;
  by_type: Record<string, number>;
}
