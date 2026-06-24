// 媒体查看器共享地址构造（FR-107）：图片灯箱与播放页此前各自内联同样的 raw / stream 地址逻辑，
// 这里统一收敛，消除重复、保证两处查看器地址行为完全一致。

/** 原图地址：图片预览与相邻预加载共用 */
export function mediaRawUrl(mediaID: number): string {
  return `/api/library/media/${mediaID}/raw`
}

/**
 * 视频流播放地址（非 ABR，浏览器原生 / mpegts 直链）。
 * 绝对化避免 mpegts.js 在 Web Worker 中 fetch 相对 URL 失败；
 * 与播放页探测失败时的降级路径保持一致。
 */
export function mediaStreamUrl(mediaID: number): string {
  return new URL(`/api/play/${mediaID}/stream`, window.location.href).toString()
}

/** HLS master 播放列表地址：播放页探测 ABR 可用性时使用 */
export function mediaHlsMasterUrl(mediaID: number): string {
  return new URL(`/api/play/hls/${mediaID}/master`, window.location.href).toString()
}
