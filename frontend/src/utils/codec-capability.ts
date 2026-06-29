/**
 * 客户端编码能力探测（FR-52）
 *
 * 用 `MediaSource.isTypeSupported` 探测本浏览器在 fMP4 容器下可解码哪些高级编码
 * （H.265/AV1/VP9），为自适应播放器分发与 FR-53 编码协商上报提供依据。
 */

/**
 * 各高级编码在 fMP4 容器下的 MSE codec MIME 串。
 *
 * 真源为后端 `internal/transcoder/fmp4.go` 的 `fmp4CodecMIME` 常量，此处为消费副本，
 * 必须与后端字节级一致（参数串为各编码通用安全档：HEVC Main@L3.1、AV1 Main 8-bit、
 * VP9 Profile0 8-bit）。FR-53 整合时可改为随清单/会话信息由后端下发，届时本表可删。
 */
const FMP4_CODEC_MIME: Record<string, string> = {
  h265: 'video/mp4; codecs="hvc1.1.6.L93.B0"',
  av1: 'video/mp4; codecs="av01.0.05M.08"',
  vp9: 'video/mp4; codecs="vp09.00.10.08"',
};

/** 本 FR 探测的高级编码集合。 */
const ADVANCED_CODECS = ['h265', 'av1', 'vp9'] as const;

/** 客户端高级编码能力描述（供 FR-53 协商上报）。 */
export interface ClientCapabilities {
  h265: boolean;
  av1: boolean;
  vp9: boolean;
}

/** 归一化目标编码标识：大小写无关，hevc 视为 h265（与后端 normalizeCodec 一致）。 */
function normalizeCodec(codec: string): string {
  const c = codec.trim().toLowerCase();
  return c === 'hevc' ? 'h265' : c;
}

/**
 * 目标编码 → fMP4 容器 MSE codec MIME 串。
 * 非高级编码（h264 / 空 / 未知）返回空串。
 */
export function codecMIME(codec: string): string {
  return FMP4_CODEC_MIME[normalizeCodec(codec)] ?? '';
}

/**
 * 探测当前浏览器是否可在 fMP4 容器下解码指定高级编码。
 * 无对应 MIME 串（非高级编码）或环境无 `MediaSource.isTypeSupported` 时返回 false。
 */
export function isCodecSupported(codec: string): boolean {
  const mime = codecMIME(codec);
  if (!mime) return false;
  const ms = (globalThis as { MediaSource?: typeof MediaSource }).MediaSource;
  if (!ms?.isTypeSupported) return false;
  return ms.isTypeSupported(mime);
}

/**
 * 探测客户端对各高级编码的解码能力，返回能力描述。
 * 供 FR-53 在播放发起时与「首选优先级 ∩ 硬件可产出」做协商。
 */
export function probeClientCapabilities(): ClientCapabilities {
  return ADVANCED_CODECS.reduce(
    (caps, codec) => ({ ...caps, [codec]: isCodecSupported(codec) }),
    {} as ClientCapabilities,
  );
}
