/** 媒体库目录 */
export interface LibraryPath {
  id: number
  path: string
  type: string
  label: string
  enabled: boolean
  created_at: string
  /** 该库已索引（未软删）的媒体数量，由 GET /api/library/paths 返回 */
  media_count?: number
}

/** 媒体文件 */
export interface MediaFile {
  id: number
  library_id: number
  file_path: string
  file_name: string
  file_size: number
  format: string
  video_codec: string
  audio_codec: string
  duration: number
  width: number
  height: number
  bitrate: number
  subtitle_tracks: string
  added_at: string
  modified_at: string
  /** 库内显示名（FR-30）：空则展示回退到 file_name，不影响磁盘真实文件名；旧数据可能缺省 */
  display_name?: string
  /** 收藏标记（FR-41），旧数据可能缺省 */
  favorite?: boolean
  /** 上次播放位置（秒，FR-44），旧数据可能缺省 */
  last_position?: number
  /** 是否已看完（FR-44），旧数据可能缺省 */
  watched?: boolean
  /** 最近一次观看时间（FR-44），旧数据可能缺省 */
  last_watched_at?: string | null

  /** 媒体时间与 EXIF（FR-31 提取、FR-38 展示），旧数据可能缺省 */
  media_time?: string | null
  /** 媒体时间来源：exif / filename / created / modified */
  media_time_source?: string
  camera?: string
  lens?: string
  aperture?: string
  shutter?: string
  iso?: number
  gps_lat?: number
  gps_lon?: number
}

/** 标签（FR-41） */
export interface Tag {
  id: number
  name: string
  created_at?: string
}

/** 媒体文件列表响应 */
export interface MediaListResponse {
  items: MediaFile[]
  total: number
  page: number
  page_size: number
}

/** 相册（FR-40）：跨目录手动归类媒体的集合 */
export interface Album {
  id: number
  name: string
  description: string
  cover_media_id: number
  created_at: string
  updated_at: string
  /** 成员数量（列表接口附带，详情接口可能缺省） */
  item_count?: number
}

/** 登录请求 */
export interface LoginRequest {
  username: string
  password: string
}

/** 分享资源类型（FR-43） */
export type ShareResourceType = 'media' | 'album'

/** 分享链接（FR-43） */
export interface Share {
  token: string
  resource_type: ShareResourceType
  resource_id: number
  /** 过期时间；null 表示永不过期 */
  expires_at: string | null
  created_at: string
}

/** 公开分享元信息（FR-43）：媒体分享含 media，相册分享含 album 与 items */
export interface ShareInfo {
  resource_type: ShareResourceType
  expires_at: string | null
  media?: MediaFile
  album?: Album
  items?: MediaFile[]
}

/** 扫描模式（FR-27）：增量更新只索引新增文件；全量扫描遍历并对账已删文件 */
export type ScanMode = 'incremental' | 'full'

/** 扫描响应 */
export interface ScanResponse {
  scanned?: number
  status?: string
  task_id?: number   // 入队任务 ID（FR-29，队列启用时返回）
}

/** 扫描进度状态 */
export interface ScanStatus {
  status: string        // "idle", "scanning", "completed", "error"
  library_id: number
  current_path: string
  total_files: number
  scanned_files: number
  error: string
  started_at: string
  completed_at: string
}

/** 扫描任务（队列，FR-29） */
export interface ScanTask {
  id: number
  library_id: number
  scan_type: string     // "full" / "incremental"
  status: string        // "pending" / "running" / "completed" / "error"
  scanned_files: number
  total_files: number
  error: string
  created_at: string
  started_at?: string
  completed_at?: string
}

/** 扫描任务列表响应（FR-29） */
export interface ScanTasksResponse {
  tasks: ScanTask[]
  current: ScanTask | null
}

/** 媒体库后缀类型 */
export type MediaExtensionType = 'video' | 'image'

/** 媒体库后缀配置 */
export interface MediaExtension {
  id?: number
  library_id: number
  extension: string
  type: MediaExtensionType
  is_builtin: number
  created_at?: string
}

/** 面包屑路径段 */
export interface BreadcrumbItem {
  name: string
  path: string
}

/** 子目录信息 */
export interface DirInfo {
  name: string
  path: string
}

/** 目录浏览响应 */
export interface BrowseResponse {
  breadcrumbs: BreadcrumbItem[]
  directories: DirInfo[]
  files: MediaFile[]
}

/** 当前用户 */
export interface CurrentUser {
  username: string
}

/** 单个编码（codec）的支持情况：编入与试编码结果 */
export interface CodecSupport {
  /** 编码格式，如 h264 / h265 / av1 / vp9 */
  codec: string
  /** 编码器名，如 h264_amf */
  encoder: string
  /** 是否编入当前 ffmpeg */
  compiled: boolean
  /** 试编码是否成功 */
  tested_ok: boolean
}

/** 单项硬件加速能力（per-codec：逐家族 × 逐编码） */
export interface HWAccelCapability {
  /** 家族显示名，如 "AMD AMF"/"软件编码" */
  name: string
  /** 家族标识，如 software/amf/nvenc/qsv/vaapi/videotoolbox/vulkan */
  family: string
  /** 设备类型，如 d3d11va/cuda/qsv（software 为 ""） */
  device_type: string
  /** 该家族至少一个编码可用 */
  available: boolean
  /** 该家族逐编码支持情况 */
  codecs: CodecSupport[]
}

/** 硬件加速汇总信息 */
export interface HWAccelInfo {
  available: HWAccelCapability[]
  preferred: string
  /** 系统可输出编码并集，如 ["h264","h265","av1","vp9"] */
  codecs: string[]
  intel_gpu: boolean
  intel_gpu_detail: string
  software_fallback: boolean
  /** 结果是否来自缓存 */
  from_cache: boolean
  /** 实测所用 ffmpeg 版本 */
  ffmpeg_version: string
  /** 实测时间（RFC3339），未测为 "" */
  tested_at: string
}

/** FFmpeg 可用性信息 */
export interface FFmpegInfo {
  available: boolean
  path: string
  version: string
}

/** 系统信息 */
export interface SystemInfo {
  app_version: string
  os: string
  arch: string
  num_cpu: number
  hostname: string
  go_version: string
  ffmpeg: FFmpegInfo
  hwaccel: HWAccelInfo
}

/** 单个编码器探测结果 */
export interface EncoderProbeResult {
  encoder: string
  family: string
  codec: string
  compiled: boolean
  tested_ok: boolean
  detail: string
}

/** 编解码器测试响应 */
export interface CodecTestResult {
  ffmpeg_available: boolean
  results: EncoderProbeResult[]
  /** 结果是否来自缓存 */
  from_cache: boolean
  /** 实测所用 ffmpeg 版本 */
  ffmpeg_version: string
  /** 实测时间（RFC3339），未测为 "" */
  tested_at: string
}

// 自更新检测结果（FR-46）
export interface UpdateCheckResult {
  current: string
  latest: string
  has_update: boolean
  tag: string
  prerelease: boolean
  channel: 'stable' | 'prerelease'
  notes: string
  asset_name: string
}

/** 运行期设置键值映射（key → value，值统一为字符串） */
export type SettingsMap = Record<string, string>

/** 单个环境变量（FR-56）：只读查看，敏感项 value 为掩码、不含明文 */
export interface EnvVar {
  /** 环境变量名 */
  key: string
  /** 中文用途说明 */
  description: string
  /** 是否敏感（敏感项 value 为掩码，绝不回显明文） */
  sensitive: boolean
  /** 当前是否已设置（非空） */
  set: boolean
  /** 展示值：非敏感为明文，敏感为固定掩码 */
  value: string
}

/** FFmpeg 路径检测结果（FR-56） */
export interface FFmpegDetectResult {
  /** 指定路径的 ffmpeg 是否可用 */
  ffmpeg_available: boolean
  /** 可用时的版本首行，不可用为空串 */
  ffmpeg_version: string
}

/** 回收站清理结果统计（FR-26）：成功移动数与失败跳过数 */
export interface RecycleCleanupResult {
  moved: number
  failed: number
}

/** 外挂字幕轨道 */
export interface SubtitleTrack {
  index: number
  file_name: string
  format: string
  url: string
}

/** 一条解析后的字幕条目 */
export interface SubtitleEntry {
  start: number
  end: number
  text: string
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
export interface PlaybackDescriptor {
  /** 目标视频编码（h264/h265/av1/vp9），用于 fmp4 路径前的客户端能力校验 */
  codec: string
  /** 清单 / 流 URL（fmp4 为 index.m3u8，ts 为流地址，mp4 为原文件地址） */
  url: string
  /** 播放路径 */
  path: 'ts' | 'fmp4' | 'mp4'
  /** 客户端不支持目标编码时的 H.264/TS 回退源（缺省则展示不支持提示，由 FR-53 提供真实回退源） */
  fallbackUrl?: string
}
