/** 媒体库目录 */
export interface LibraryPath {
  id: number
  path: string
  type: string
  label: string
  enabled: boolean
  created_at: string
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
  scanned: number
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
