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
}

/** 媒体文件列表响应 */
export interface MediaListResponse {
  items: MediaFile[]
  total: number
  page: number
  page_size: number
}

/** 登录请求 */
export interface LoginRequest {
  username: string
  password: string
}

/** 扫描响应 */
export interface ScanResponse {
  scanned?: number
  status?: string
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

/** 单项硬件加速能力 */
export interface HWAccelCapability {
  name: string
  device_type: string
  h264_encoder: string
  h265_encoder: string
  available: boolean
}

/** 硬件加速汇总信息 */
export interface HWAccelInfo {
  available: HWAccelCapability[]
  preferred: string
  intel_gpu: boolean
  intel_gpu_detail: string
  h264_supported: boolean
  h265_supported: boolean
  software_fallback: boolean
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
