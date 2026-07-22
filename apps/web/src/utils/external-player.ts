// 外部播放器深链工具（FR-79）。
// 复用 FR-43 免登分享流端点把库内视频拼成 VLC / IINA 可直接消费的网络串流地址。

/**
 * 拼出外部播放器可消费的免登流地址。
 *
 * 形如 `{origin}/api/share/{token}/media/{mediaID}/stream`，走原文件直链 + Range（非 HLS）。
 * 当 `lastPosition` 为有限正数时追加媒体片段 `#t={取整秒}` 续播点；该 fragment 不发往服务端，
 * 由播放器客户端处理（支持 media fragment 的播放器从该秒起播）。
 *
 * @param origin 站点源（如 `window.location.origin`），用于生成绝对地址
 * @param token  FR-43 分享 token
 * @param mediaID 媒体 ID
 * @param lastPosition 上次播放位置（秒，可选）；非有限正数时不带续播点
 */
export function buildExternalStreamURL(
  origin: string,
  token: string,
  mediaID: number,
  lastPosition?: number,
): string {
  const url = `${origin}/api/share/${token}/media/${mediaID}/stream`;
  if (lastPosition !== undefined && Number.isFinite(lastPosition) && lastPosition > 0) {
    return `${url}#t=${Math.floor(lastPosition)}`;
  }
  return url;
}
