import { useRef, useEffect, useState, useCallback } from 'react'
import { Text, Box, ActionIcon, Slider, Menu } from '@mantine/core'
import {
  IconPlayerPlay, IconPlayerPause, IconVolume, IconVolumeOff,
  IconMaximize, IconMinimize, IconPictureInPicture, IconRewindBackward10,
  IconRewindForward10, IconAdjustments,
} from '@tabler/icons-react'
import mpegts from 'mpegts.js'
import type Hls from 'hls.js'
import type { SubtitleEntry, PlaybackDescriptor } from '@/types'
import { isCodecSupported } from '@/utils/codec-capability'

interface VideoPlayerProps {
  /** 流 URL（支持 master.m3u8 触发 ABR 模式）。传入 descriptor 时由其覆盖。 */
  url: string
  /**
   * 播放描述符（FR-52）：按目标编码 + 路径分发到对应内核。
   * 缺省时保持现有 url/isABR/streamType 行为不变（现有调用方零改动）。
   */
  descriptor?: PlaybackDescriptor
  /** 自动播放 */
  autoPlay?: boolean
  /** 解析后的字幕条目列表 */
  subtitleEntries?: SubtitleEntry[]
  /** 是否显示字幕 */
  subtitleVisible?: boolean
  /**
   * 显式指定是否走 ABR（hls.js）模式。
   * 缺省时按 URL 是否以 master.m3u8 结尾推断。
   */
  isABR?: boolean
  /**
   * 显式声明流类型为 mp4 时使用浏览器原生 video 标签直接加载。
   */
  streamType?: 'mpegts' | 'mp4'
  /**
   * 填充模式（FR-103）：为真时视频区放弃固定 16:9 比例，改为 flex 填充父容器剩余高度
   * （video 以 object-fit:contain letterbox 黑边铺满），供播放页全屏沉浸布局使用。
   * 缺省 false 时维持固定 16:9（灯箱 / 分享等用法零改动）。
   */
  fill?: boolean
  /**
   * 续播起始位置（秒，FR-44）。大于 1 时在媒体可定位后 seek 到该位置一次。
   */
  initialPosition?: number
  /**
   * 定期上报当前播放位置（秒，FR-44）。约每 10s 触发一次，暂停时补报一次。
   */
  onPositionReport?: (position: number) => void
  /**
   * 播放接近结束时回调一次（FR-44），用于标记「已看」。
   */
  onEnded?: () => void
}

// 播放位置上报节流间隔（秒，FR-44）
const POSITION_REPORT_INTERVAL = 10
// 「看完」判定：剩余时长小于该秒数即视为已看完（FR-44）
const WATCHED_REMAINING_THRESHOLD = 15
// 快进 / 快退步长（秒，FR-104）
const SEEK_STEP = 10
// 键盘音量调节步长（FR-104）
const VOLUME_STEP = 0.1
// 控件自动隐藏延迟（毫秒，FR-104）：播放中鼠标静止超过该时长则淡出控件
const CONTROLS_HIDE_DELAY = 3000
// 可选倍速档位（FR-104）
const PLAYBACK_RATES = [0.5, 0.75, 1, 1.25, 1.5, 1.75, 2] as const
// 字幕字号档位（FR-104）：映射到字幕文本 fontSize
const SUBTITLE_SCALES = {
  small: 'var(--mantine-font-size-xs)',
  medium: 'var(--mantine-font-size-sm)',
  large: 'var(--mantine-font-size-lg)',
} as const
type SubtitleScale = keyof typeof SUBTITLE_SCALES

// 音量 / 静音偏好持久化键（localStorage，FR-104）
export const VOLUME_PREF_KEY = 'jianvideo.player.volume'

/** 音量 / 静音偏好（FR-104）。 */
export interface VolumePref {
  /** 音量 [0,1] */
  volume: number
  /** 是否静音 */
  muted: boolean
}

/**
 * 读取持久化的音量 / 静音偏好（纯函数，FR-104）。
 * 无存储 / 内容损坏 / 字段非法时返回 null（不抛），volume 夹取到 [0,1]。
 */
export function loadVolumePref(): VolumePref | null {
  try {
    const raw = localStorage.getItem(VOLUME_PREF_KEY)
    if (!raw) return null
    const parsed = JSON.parse(raw) as { volume?: unknown; muted?: unknown }
    if (typeof parsed.volume !== 'number' || isNaN(parsed.volume)) return null
    const volume = Math.min(1, Math.max(0, parsed.volume))
    return { volume, muted: Boolean(parsed.muted) }
  } catch {
    return null
  }
}

/** 写入音量 / 静音偏好（纯函数，FR-104）。失败静默忽略（如隐私模式禁写）。 */
export function saveVolumePref(pref: VolumePref): void {
  try {
    const volume = Math.min(1, Math.max(0, pref.volume))
    localStorage.setItem(VOLUME_PREF_KEY, JSON.stringify({ volume, muted: Boolean(pref.muted) }))
  } catch {
    /* 静默：localStorage 不可用时不影响播放 */
  }
}

/** 解析后的有效播放输入（FR-52）。 */
interface ResolvedPlayback {
  url: string
  /** 是否 ABR（hls.js）；undefined 时由 url 是否 master.m3u8 推断 */
  isABR?: boolean
  streamType: 'mpegts' | 'mp4'
  /** 目标编码客户端不支持且无回退源：不初始化内核，展示提示 */
  unsupported: boolean
}

/**
 * 解析播放描述符为有效播放输入（纯函数，FR-52）。
 *
 * - 无描述符：沿用现有 url/isABR/streamType 入参，行为不变。
 * - `ts` / `mp4` 路径：直接映射到现有 mpegts.js / 原生 video 分支。
 * - `fmp4` 路径：先 isCodecSupported 校验客户端能力——支持则走 hls.js（fMP4，按 ABR 模式
 *   加载 index.m3u8）；不支持则回退 fallbackUrl（按 TS 路径），无回退源则标记 unsupported。
 */
function resolveDescriptor(
  descriptor: PlaybackDescriptor | undefined,
  fallback: { url: string; isABR?: boolean; streamType: 'mpegts' | 'mp4' },
): ResolvedPlayback {
  if (!descriptor) {
    return { url: fallback.url, isABR: fallback.isABR, streamType: fallback.streamType, unsupported: false }
  }
  if (descriptor.path === 'mp4') {
    return { url: descriptor.url, isABR: false, streamType: 'mp4', unsupported: false }
  }
  if (descriptor.path === 'fmp4') {
    if (isCodecSupported(descriptor.codec)) {
      // hls.js 原生支持 fMP4 分片，按 ABR 模式加载 HLS-fMP4 清单
      return { url: descriptor.url, isABR: true, streamType: 'mpegts', unsupported: false }
    }
    // 客户端不支持目标编码：回退 H.264/TS 源；无回退源则标记不可播
    if (descriptor.fallbackUrl) {
      return { url: descriptor.fallbackUrl, isABR: undefined, streamType: 'mpegts', unsupported: false }
    }
    return { url: descriptor.url, isABR: undefined, streamType: 'mpegts', unsupported: true }
  }
  // ts 路径：等价现有 mpegts.js / hls.js-ABR 分支（由 url 是否 master.m3u8 推断）
  return { url: descriptor.url, isABR: undefined, streamType: 'mpegts', unsupported: false }
}

/**
 * 视频播放组件
 *
 * - URL 为 master.m3u8 时启用 ABR 模式（hls.js 动态加载）
 * - 其他 URL 使用 mpegts.js 播放
 */
export default function VideoPlayer({
  url,
  descriptor,
  autoPlay = true,
  subtitleEntries,
  subtitleVisible = false,
  isABR: isABRProp,
  streamType = 'mpegts',
  fill = false,
  initialPosition,
  onPositionReport,
  onEnded,
}: VideoPlayerProps) {
  const videoRef = useRef<HTMLVideoElement>(null)
  const mpegtsPlayerRef = useRef<mpegts.Player | null>(null)
  const hlsRef = useRef<Hls | null>(null)
  // FR-44：续播 / 上报 / 看完状态。用 ref 持有最新回调与一次性标志，避免重建监听器。
  const initialPositionRef = useRef(initialPosition)
  const hasSeekedRef = useRef(false)
  const onPositionReportRef = useRef(onPositionReport)
  const onEndedRef = useRef(onEnded)
  const lastReportRef = useRef(0)
  const endedReportedRef = useRef(false)
  initialPositionRef.current = initialPosition
  onPositionReportRef.current = onPositionReport
  onEndedRef.current = onEnded
  const [isPlaying, setIsPlaying] = useState(false)
  const [autoPlayBlocked, setAutoPlayBlocked] = useState(false)
  // FR-102：静音自动播。浏览器允许 muted autoplay，进页即出画面；autoMuted 为真时
  // 在角落展示「点击取消静音」入口，用户主动调音量 / 切静音时清除该标记。
  const [autoMuted, setAutoMuted] = useState(false)
  const [currentTime, setCurrentTime] = useState(0)
  const [duration, setDuration] = useState(0)
  const [volume, setVolume] = useState(1)
  const [isMuted, setIsMuted] = useState(false)
  const [bufferedProgress, setBufferedProgress] = useState(0)
  const [subtitleText, setSubtitleText] = useState('')
  // 末端缓冲等待（FR-18）：mpegts.js ERROR 或 video stalled 时进入等待，1s 后自动重载
  const [isWaiting, setIsWaiting] = useState(false)
  // FR-104：播放器内核与控件增强
  const containerRef = useRef<HTMLDivElement>(null)
  // 全屏态：由 fullscreenchange 同步
  const [isFullscreen, setIsFullscreen] = useState(false)
  // 倍速：映射到 video.playbackRate
  const [rate, setRate] = useState(1)
  // 进度条 hover 时间预览：null 表示未 hover
  const [hoverPct, setHoverPct] = useState<number | null>(null)
  // 控件可见性：播放中鼠标静止数秒后淡出
  const [controlsVisible, setControlsVisible] = useState(true)
  // 字幕字号档（FR-104）
  const [subtitleScale, setSubtitleScale] = useState<SubtitleScale>('medium')
  // 画中画能力探测（FR-104）：浏览器不支持时隐藏 PiP 按钮
  const pipSupported = typeof document !== 'undefined' && Boolean(document.pictureInPictureEnabled)
  // 控件自动隐藏定时器句柄
  const hideTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null)

  // FR-52：解析播放描述符为有效播放输入（url / 是否 ABR / 流类型）。
  // 缺省描述符时直接沿用现有入参，行为不变；fmp4 路径前先做客户端能力校验，
  // 不支持时回退 fallbackUrl（按 TS 路径加载），无回退源则标记为不可播。
  const resolved = resolveDescriptor(descriptor, { url, isABR: isABRProp, streamType })

  // 判断是否为 ABR 模式
  const isABR = resolved.isABR ?? resolved.url.endsWith('master.m3u8')

  const destroyMpegtsPlayer = useCallback(() => {
    const player = mpegtsPlayerRef.current
    if (player) {
      player.off('loadeddata', () => {})
      player.off('playing', () => {})
      player.off('pause', () => {})
      player.pause()
      player.unload()
      player.destroy()
      mpegtsPlayerRef.current = null
    }
  }, [])

  const destroyHlsPlayer = useCallback(async () => {
    const hls = hlsRef.current as { destroy: () => void } | null
    if (hls) { hls.destroy(); hlsRef.current = null }
  }, [])

  // FR-102：统一的静音自动播。先置 muted=true（浏览器允许 muted autoplay）再触发播放，
  // 进页即出画面并标记 autoMuted（展示取消静音入口）；muted 仍被拦时落到「点击播放」兜底遮罩。
  // mpegts.js 经由 player.play() 播放，故允许传入自定义 play 触发器；缺省走原生 video.play()。
  const attemptAutoPlay = useCallback((play?: () => Promise<void> | void) => {
    const v = videoRef.current; if (!v) return
    v.muted = true
    setIsMuted(true)
    setAutoMuted(true)
    const result = play ? play() : v.play()
    void (result as Promise<void> | undefined)?.catch?.(() => setAutoPlayBlocked(true))
  }, [])

  const initMpegtsPlayer = useCallback(
    (streamUrl: string) => {
      if (!videoRef.current) return
      destroyMpegtsPlayer()
      const player = mpegts.createPlayer(
        { type: 'mpegts', url: streamUrl, isLive: true },
        { enableWorker: true, enableStashBuffer: true, stashInitialSize: 1024 * 1024, accurateSeek: true, seekType: 'range' }
      )
      player.attachMediaElement(videoRef.current)
      player.load()
      player.on('loadeddata', () => { if (autoPlay) attemptAutoPlay(() => player.play() as Promise<void>) })
      player.on('playing', () => { setIsPlaying(true); setIsWaiting(false) })
      player.on('pause', () => setIsPlaying(false))
      // FR-18：mpegts.js 报错（常见于追上文件写入末端、TS 流暂时断流）→ 标记等待，
      // 1s 后重载播放器；新数据可用时 loadeddata/playing 会自动复位 isWaiting。
      // 使用 'error' 字符串避免依赖 mpegts.Events 类型常量。
      player.on('error', () => {
        setIsWaiting(true)
        setTimeout(() => {
          if (mpegtsPlayerRef.current === player) { try { player.unload(); player.load() } catch { /* 静默 */ } }
        }, 1000)
      })
      mpegtsPlayerRef.current = player
    },
    [autoPlay, destroyMpegtsPlayer, attemptAutoPlay]
  )

  const initHlsPlayer = useCallback(
    async (masterUrl: string) => {
      if (!videoRef.current) return
      destroyHlsPlayer()
      try {
        const Hls = (await import('hls.js')).default
        if (!Hls.isSupported()) { console.warn('[VideoPlayer] hls.js 不支持当前浏览器'); initMpegtsPlayer(masterUrl); return }
        const hls = new Hls({ enableWorker: true, lowLatencyMode: true })
        hls.loadSource(masterUrl)
        hls.attachMedia(videoRef.current)
        hls.on(Hls.Events.MANIFEST_PARSED, () => { if (autoPlay) attemptAutoPlay() })
        hls.on(Hls.Events.LEVEL_SWITCHED, (_e, d) => { const l = hls.levels[d.level]; if (l) console.info(`[VideoPlayer] ABR: ${l.width}x${l.height}`) })
        hls.on(Hls.Events.ERROR, (_e, d) => { if (d.fatal) { hls.destroy(); hlsRef.current = null; initMpegtsPlayer(masterUrl) } })
        hlsRef.current = hls
      } catch { initMpegtsPlayer(masterUrl) }
    },
    [autoPlay, destroyHlsPlayer, initMpegtsPlayer, attemptAutoPlay]
  )

  // 挂载 / URL 变化
  useEffect(() => {
    // FR-52：目标编码不受支持且无回退源 → 不初始化任一内核，仅展示提示（不抛 Network Error）。
    if (resolved.unsupported) {
      destroyMpegtsPlayer(); void destroyHlsPlayer()
      return
    }
    if (resolved.streamType === 'mp4' && videoRef.current) {
      destroyMpegtsPlayer(); void destroyHlsPlayer()
      videoRef.current.src = resolved.url
      if (autoPlay) attemptAutoPlay()
      return () => { if (videoRef.current) videoRef.current.removeAttribute('src') }
    }
    if (isABR) void initHlsPlayer(resolved.url); else initMpegtsPlayer(resolved.url)
    return () => { void destroyHlsPlayer(); destroyMpegtsPlayer() }
  }, [resolved.url, resolved.streamType, resolved.unsupported, isABR, autoPlay, initHlsPlayer, initMpegtsPlayer, destroyHlsPlayer, destroyMpegtsPlayer, attemptAutoPlay])

  // FR-44：URL 变化（切换媒体）时重置续播 / 上报 / 看完的一次性标志
  useEffect(() => {
    hasSeekedRef.current = false
    lastReportRef.current = 0
    endedReportedRef.current = false
  }, [resolved.url])

  // 续播定位（FR-44）：媒体可定位后 seek 到上次位置一次。
  // 接近片尾的位置不回跳（剩余小于阈值时忽略），避免「看完」后又跳回末尾。
  const seekToInitialOnce = useCallback(() => {
    const v = videoRef.current; if (!v) return
    if (hasSeekedRef.current) return
    const pos = initialPositionRef.current
    if (!pos || pos <= 1 || !isFinite(v.duration) || v.duration <= 0) return
    if (v.duration - pos <= WATCHED_REMAINING_THRESHOLD) { hasSeekedRef.current = true; return }
    try { v.currentTime = pos } catch { /* 静默：部分流尚不可定位 */ }
    hasSeekedRef.current = true
  }, [])

  // 监听时间/音量/缓冲/等待
  useEffect(() => {
    const v = videoRef.current; if (!v) return
    const onTime = () => {
      setCurrentTime(v.currentTime)
      // FR-44：约每 10s 上报一次播放位置
      const report = onPositionReportRef.current
      if (report && v.currentTime - lastReportRef.current >= POSITION_REPORT_INTERVAL) {
        lastReportRef.current = v.currentTime
        report(v.currentTime)
      }
      // FR-44：接近片尾（剩余小于阈值）视为看完，仅回调一次
      const ended = onEndedRef.current
      if (ended && !endedReportedRef.current && isFinite(v.duration) && v.duration > 0
        && v.duration - v.currentTime <= WATCHED_REMAINING_THRESHOLD) {
        endedReportedRef.current = true
        ended()
      }
    }
    const onDur = () => { setDuration(v.duration); seekToInitialOnce() }
    const onLoadedMeta = () => seekToInitialOnce()
    // FR-44：暂停时补报一次当前位置，避免离开页面丢失最新进度
    const onPause = () => {
      const report = onPositionReportRef.current
      if (report && v.currentTime > 0) { lastReportRef.current = v.currentTime; report(v.currentTime) }
    }
    // FR-44：原生 ended 事件兜底标记看完
    const onNativeEnded = () => {
      const ended = onEndedRef.current
      if (ended && !endedReportedRef.current) { endedReportedRef.current = true; ended() }
    }
    const onVol = () => { setVolume(v.volume); setIsMuted(v.muted) }
    const onBuf = () => { if (v.buffered.length > 0 && v.duration > 0) setBufferedProgress((v.buffered.end(v.buffered.length - 1) / v.duration) * 100) }
    // FR-18：原生 video stalled/waiting → 进入末端缓冲等待；playing/canplay 自动复位
    const onWaiting = () => setIsWaiting(true)
    const onCanPlay = () => setIsWaiting(false)
    v.addEventListener('timeupdate', onTime); v.addEventListener('durationchange', onDur)
    v.addEventListener('loadedmetadata', onLoadedMeta); v.addEventListener('pause', onPause)
    v.addEventListener('ended', onNativeEnded)
    v.addEventListener('volumechange', onVol); v.addEventListener('progress', onBuf)
    v.addEventListener('waiting', onWaiting); v.addEventListener('stalled', onWaiting)
    v.addEventListener('canplay', onCanPlay); v.addEventListener('playing', onCanPlay)
    return () => {
      v.removeEventListener('timeupdate', onTime); v.removeEventListener('durationchange', onDur)
      v.removeEventListener('loadedmetadata', onLoadedMeta); v.removeEventListener('pause', onPause)
      v.removeEventListener('ended', onNativeEnded)
      v.removeEventListener('volumechange', onVol); v.removeEventListener('progress', onBuf)
      v.removeEventListener('waiting', onWaiting); v.removeEventListener('stalled', onWaiting)
      v.removeEventListener('canplay', onCanPlay); v.removeEventListener('playing', onCanPlay)
    }
  }, [seekToInitialOnce])

  // 字幕同步
  useEffect(() => {
    if (!subtitleVisible || !subtitleEntries?.length) { setSubtitleText(''); return }
    const entry = subtitleEntries.find(e => currentTime >= e.start && currentTime < e.end)
    setSubtitleText(entry?.text ?? '')
  }, [currentTime, subtitleEntries, subtitleVisible])

  // FR-104：挂载时恢复记忆的音量档（与 FR-102 协调）。
  // 首次仍走静音自动播（attemptAutoPlay 置 muted），这里只把滑块/音量值恢复为上次偏好，
  // 取消静音后即按记忆音量出声；若上次本就是手动静音则记忆静音态。
  useEffect(() => {
    const pref = loadVolumePref()
    if (!pref) return
    const v = videoRef.current
    setVolume(pref.volume)
    if (v) v.volume = pref.volume
    // 上次为手动静音 → 保留静音态（autoPlay 关闭时直接体现）；不强行解除 FR-102 自动静音
    if (pref.muted && !autoPlay) { setIsMuted(true); if (v) v.muted = true }
  }, [autoPlay])

  // FR-104：倍速同步到 video.playbackRate（媒体重建后也复位为当前倍速）
  useEffect(() => {
    const v = videoRef.current; if (v) v.playbackRate = rate
  }, [rate, resolved.url])

  // FR-104：全屏态同步——监听 fullscreenchange，按当前全屏元素是否为本容器更新按钮态
  useEffect(() => {
    const onFsChange = () => setIsFullscreen(document.fullscreenElement === containerRef.current)
    document.addEventListener('fullscreenchange', onFsChange)
    return () => document.removeEventListener('fullscreenchange', onFsChange)
  }, [])

  const togglePlay = () => {
    const v = videoRef.current; if (!v) return
    if (isPlaying) v.pause(); else { setAutoPlayBlocked(false); void v.play()?.catch?.(() => {}) }
  }

  const handleSeek = (val: number) => { const v = videoRef.current; if (v && duration) v.currentTime = (val / 100) * duration }
  const handleVolume = (val: number) => {
    const v = videoRef.current; if (!v) return
    v.volume = val; v.muted = val === 0
    setVolume(val); setIsMuted(val === 0); setAutoMuted(false)
    // FR-104：用户主动调音量 → 记忆其偏好
    saveVolumePref({ volume: val, muted: val === 0 })
  }
  const toggleMute = () => {
    const v = videoRef.current; if (!v) return
    const next = !isMuted
    v.muted = next; setIsMuted(next); setAutoMuted(false)
    saveVolumePref({ volume, muted: next })
  }
  // FR-102：取消静音自动播的静音态，恢复有声并撤下「点击取消静音」入口
  // FR-104：恢复有声后记忆当前音量为非静音偏好
  const unmute = () => {
    const v = videoRef.current; if (!v) return
    v.muted = false; setIsMuted(false); setAutoMuted(false)
    saveVolumePref({ volume, muted: false })
  }

  // FR-104：相对快进 / 快退，夹取到 [0, duration]
  const seekBy = (delta: number) => {
    const v = videoRef.current; if (!v) return
    const dur = isFinite(v.duration) && v.duration > 0 ? v.duration : duration
    const max = dur > 0 ? dur : Number.MAX_SAFE_INTEGER
    v.currentTime = Math.min(max, Math.max(0, v.currentTime + delta))
  }

  // FR-104：调整倍速
  const changeRate = (next: number) => {
    const v = videoRef.current
    setRate(next)
    if (v) v.playbackRate = next
  }

  // FR-104：键盘音量调节（夹取到 [0,1]，并解除静音）
  const nudgeVolume = (delta: number) => {
    const next = Math.min(1, Math.max(0, (isMuted ? 0 : volume) + delta))
    handleVolume(next)
  }

  // FR-104：全屏切换——对最外层容器调原生 Fullscreen API（不支持时静默）
  const toggleFullscreen = () => {
    const el = containerRef.current; if (!el) return
    if (document.fullscreenElement === el) {
      void document.exitFullscreen?.()
    } else {
      void el.requestFullscreen?.()
    }
  }

  // FR-104：画中画切换（仅在浏览器支持时可达）
  const togglePip = () => {
    const v = videoRef.current; if (!v) return
    if (document.pictureInPictureElement === v) {
      void document.exitPictureInPicture?.()
    } else {
      void v.requestPictureInPicture?.()?.catch?.(() => { /* 静默：部分流不支持 PiP */ })
    }
  }

  // FR-104：按百分比跳转（数字键 0-9）
  const seekToPercent = (pct: number) => {
    const v = videoRef.current; if (!v || !duration) return
    v.currentTime = (pct / 100) * duration
  }

  // FR-104：控件自动隐藏——鼠标移动时显示并重置定时器，播放中静止超时淡出
  const showControlsTemporarily = useCallback(() => {
    setControlsVisible(true)
    if (hideTimerRef.current) clearTimeout(hideTimerRef.current)
    hideTimerRef.current = setTimeout(() => {
      // 仅在播放中淡出；暂停时常驻可见
      if (videoRef.current && !videoRef.current.paused) setControlsVisible(false)
    }, CONTROLS_HIDE_DELAY)
  }, [])

  useEffect(() => () => { if (hideTimerRef.current) clearTimeout(hideTimerRef.current) }, [])

  // FR-104：键盘快捷键。仅在焦点不在输入控件时生效，避免干扰页面其它输入。
  const handleKeyDown = (e: React.KeyboardEvent<HTMLDivElement>) => {
    const target = e.target as HTMLElement | null
    const tag = target?.tagName
    if (tag === 'INPUT' || tag === 'TEXTAREA' || tag === 'SELECT' || target?.isContentEditable) return
    // 数字键 0-9：跳转到对应百分比
    if (/^[0-9]$/.test(e.key)) { e.preventDefault(); seekToPercent(parseInt(e.key, 10) * 10); return }
    switch (e.key) {
      case ' ': case 'Spacebar': e.preventDefault(); togglePlay(); break
      case 'ArrowRight': e.preventDefault(); seekBy(SEEK_STEP); break
      case 'ArrowLeft': e.preventDefault(); seekBy(-SEEK_STEP); break
      case 'ArrowUp': e.preventDefault(); nudgeVolume(VOLUME_STEP); break
      case 'ArrowDown': e.preventDefault(); nudgeVolume(-VOLUME_STEP); break
      case 'f': case 'F': e.preventDefault(); toggleFullscreen(); break
      case 'm': case 'M': e.preventDefault(); toggleMute(); break
      default: break
    }
  }

  const fmt = (s: number) => { if (!isFinite(s) || isNaN(s)) return '0:00'; const m = Math.floor(s / 60); return `${m}:${Math.floor(s % 60).toString().padStart(2, '0')}` }
  const playPct = duration > 0 ? (currentTime / duration) * 100 : 0

  // FR-104：hover 进度条时的时间气泡定位（百分比）
  const hoverTime = hoverPct !== null && duration > 0 ? (hoverPct / 100) * duration : 0
  // 控件区透明度：播放中静止淡出，其余常显
  const controlsOpacity = controlsVisible ? 1 : 0

  return (
    <Box
      ref={containerRef}
      data-testid="video-player-root"
      tabIndex={0}
      onKeyDown={handleKeyDown}
      onMouseMove={showControlsTemporarily}
      style={{
        display: 'flex',
        flexDirection: 'column',
        width: '100%',
        backgroundColor: 'black',
        borderRadius: 'var(--mantine-radius-lg)',
        overflow: 'hidden',
        outline: 'none',
        // FR-103 填充模式：撑满父容器剩余高度（播放页全屏沉浸用）
        ...(fill ? { flex: 1, height: '100%', minHeight: 0 } : {}),
      }}
    >
      <Box
        onDoubleClick={toggleFullscreen}
        style={{
          position: 'relative',
          width: '100%',
          backgroundColor: 'black',
          // FR-103：填充模式去掉固定 16:9，改为 flex 填充剩余高度；缺省维持 16:9
          ...(fill ? { flex: 1, minHeight: 0 } : { aspectRatio: '16/9' }),
        }}
      >
        <video ref={videoRef} style={{ width: '100%', height: '100%', backgroundColor: 'black', objectFit: 'contain' }} playsInline />
        {/* FR-52：目标编码不受支持且无回退源时的提示（不抛 Network Error） */}
        {resolved.unsupported && (
          <Box role="alert" style={{ position: 'absolute', inset: 0, display: 'flex', alignItems: 'center', justifyContent: 'center', backgroundColor: 'rgba(0,0,0,0.6)', padding: '0 1rem' }}>
            <Text c="white" size="sm" ta="center">当前浏览器不支持该视频编码，无法播放</Text>
          </Box>
        )}
        {autoPlayBlocked && !isPlaying && (
          <Box style={{ position: 'absolute', inset: 0, display: 'flex', alignItems: 'center', justifyContent: 'center', backgroundColor: 'rgba(0,0,0,0.5)', cursor: 'pointer' }} onClick={togglePlay}>
            <Text c="white" size="lg">点击播放</Text>
          </Box>
        )}
        {/* FR-102：静音自动播时角落提供取消静音入口，点击恢复有声 */}
        {autoMuted && isMuted && !autoPlayBlocked && (
          <Box
            component="button"
            type="button"
            onClick={unmute}
            aria-label="点击取消静音"
            style={{
              position: 'absolute', top: 12, right: 12, display: 'flex', alignItems: 'center', gap: 6,
              padding: '6px 12px', border: 'none', borderRadius: 'var(--mantine-radius-xl)',
              backgroundColor: 'rgba(0,0,0,0.6)', color: 'white', cursor: 'pointer', fontSize: 'var(--mantine-font-size-sm)',
            }}
          >
            <IconVolumeOff size={16} />
            <Text component="span" c="white" size="sm">点击取消静音</Text>
          </Box>
        )}
        {isWaiting && !autoPlayBlocked && (
          <Box aria-label="缓冲等待" role="status" style={{ position: 'absolute', inset: 0, display: 'flex', alignItems: 'center', justifyContent: 'center', backgroundColor: 'rgba(0,0,0,0.45)', pointerEvents: 'none' }}>
            <Text c="white" size="sm">等待新数据…</Text>
          </Box>
        )}
        {subtitleVisible && subtitleText && (
          <Box style={{ position: 'absolute', insetInline: 0, bottom: '8%', display: 'flex', justifyContent: 'center', pointerEvents: 'none', padding: '0 1rem' }}>
            {/* FR-104：字幕字号档（SUBTITLE_SCALES）映射到 fontSize */}
            <Text component="span" c="white" ta="center" style={{ display: 'inline-block', padding: '0.25rem 0.75rem', borderRadius: 'var(--mantine-radius-sm)', backgroundColor: 'rgba(0,0,0,0.6)', maxWidth: '80%', fontSize: SUBTITLE_SCALES[subtitleScale] }}>{subtitleText}</Text>
          </Box>
        )}
      </Box>

      {/* 控件栏（FR-104）：播放中鼠标静止数秒淡出，移动 / 暂停时重现 */}
      <Box
        style={{
          display: 'flex', alignItems: 'center', gap: 'var(--mantine-spacing-sm)',
          padding: '0.5rem 1rem', backgroundColor: 'var(--mantine-color-default)',
          opacity: controlsOpacity, transition: 'opacity 0.3s ease',
        }}
      >
        <ActionIcon variant="subtle" color="gray" onClick={togglePlay} aria-label={isPlaying ? '暂停' : '播放'}>
          {isPlaying ? <IconPlayerPause size={22} /> : <IconPlayerPlay size={22} />}
        </ActionIcon>

        {/* FR-104：快退 / 快进 10 秒 */}
        <ActionIcon variant="subtle" color="gray" onClick={() => seekBy(-SEEK_STEP)} aria-label="快退 10 秒">
          <IconRewindBackward10 size={20} />
        </ActionIcon>
        <ActionIcon variant="subtle" color="gray" onClick={() => seekBy(SEEK_STEP)} aria-label="快进 10 秒">
          <IconRewindForward10 size={20} />
        </ActionIcon>

        <Box style={{ flex: 1, display: 'flex', alignItems: 'center', gap: 'var(--mantine-spacing-xs)' }}>
          <Text size="xs" c="dimmed" style={{ fontVariantNumeric: 'tabular-nums' }}>{fmt(currentTime)}</Text>
          {/* FR-104：进度条加大热区（容器高度抬升）+ hover 时间气泡；FR-93 修偏配色由蓝转品牌紫 */}
          <Box
            style={{ flex: 1, position: 'relative', height: 16, display: 'flex', alignItems: 'center' }}
            onMouseMove={(e) => {
              const rect = e.currentTarget.getBoundingClientRect()
              if (rect.width > 0) setHoverPct(Math.min(100, Math.max(0, ((e.clientX - rect.left) / rect.width) * 100)))
            }}
            onMouseLeave={() => setHoverPct(null)}
          >
            <Slider value={Math.min(bufferedProgress, 100)} size={4} color="gray" radius="xl" thumbSize={0} style={{ position: 'absolute', width: '100%', pointerEvents: 'none' }} />
            <Slider value={playPct} onChange={handleSeek} size={4} color="purple" radius="xl" thumbSize={12} style={{ position: 'absolute', width: '100%' }} />
            {/* hover 时间气泡 */}
            {hoverPct !== null && (
              <Box
                style={{
                  position: 'absolute', bottom: '100%', left: `${hoverPct}%`, transform: 'translateX(-50%)',
                  marginBottom: 6, padding: '2px 6px', borderRadius: 'var(--mantine-radius-sm)',
                  backgroundColor: 'rgba(0,0,0,0.8)', color: 'white', fontSize: 'var(--mantine-font-size-xs)',
                  whiteSpace: 'nowrap', pointerEvents: 'none',
                }}
              >
                {fmt(hoverTime)}
              </Box>
            )}
          </Box>
          <Text size="xs" c="dimmed" style={{ fontVariantNumeric: 'tabular-nums' }}>{fmt(duration)}</Text>
        </Box>

        {/* FR-104：倍速菜单 */}
        <Menu position="top" withinPortal>
          <Menu.Target>
            <ActionIcon variant="subtle" color="gray" aria-label="播放速度" style={{ width: 'auto', paddingInline: 6 }}>
              <Text size="xs" fw={600}>{rate}×</Text>
            </ActionIcon>
          </Menu.Target>
          <Menu.Dropdown>
            <Menu.Label>播放速度</Menu.Label>
            {PLAYBACK_RATES.map((r) => (
              <Menu.Item key={r} onClick={() => changeRate(r)} fw={r === rate ? 700 : 400}>{r}×</Menu.Item>
            ))}
          </Menu.Dropdown>
        </Menu>

        {/* FR-104：字幕字号档（仅在有字幕时展示） */}
        {subtitleVisible && (
          <Menu position="top" withinPortal>
            <Menu.Target>
              <ActionIcon variant="subtle" color="gray" aria-label="字幕字号">
                <IconAdjustments size={18} />
              </ActionIcon>
            </Menu.Target>
            <Menu.Dropdown>
              <Menu.Label>字幕字号</Menu.Label>
              <Menu.Item onClick={() => setSubtitleScale('small')} fw={subtitleScale === 'small' ? 700 : 400}>小</Menu.Item>
              <Menu.Item onClick={() => setSubtitleScale('medium')} fw={subtitleScale === 'medium' ? 700 : 400}>中</Menu.Item>
              <Menu.Item onClick={() => setSubtitleScale('large')} fw={subtitleScale === 'large' ? 700 : 400}>大</Menu.Item>
            </Menu.Dropdown>
          </Menu>
        )}

        <Box style={{ display: 'flex', alignItems: 'center', gap: '4px' }}>
          {/* FR-104：音量图标语义灰（修偏 color="white" 亮色主题不可见）；音量条由蓝转品牌紫 */}
          <ActionIcon variant="subtle" color="gray" onClick={toggleMute} aria-label={isMuted ? '取消静音' : '静音'}>
            {isMuted || volume === 0 ? <IconVolumeOff size={18} /> : <IconVolume size={18} />}
          </ActionIcon>
          <Slider value={isMuted ? 0 : volume} onChange={handleVolume} size={4} color="purple" radius="xl" showLabelOnHover={false} style={{ width: '5rem' }} />
        </Box>

        {/* FR-104：画中画（浏览器不支持则隐藏） */}
        {pipSupported && (
          <ActionIcon variant="subtle" color="gray" onClick={togglePip} aria-label="画中画">
            <IconPictureInPicture size={18} />
          </ActionIcon>
        )}

        {/* FR-104：全屏切换 */}
        <ActionIcon variant="subtle" color="gray" onClick={toggleFullscreen} aria-label={isFullscreen ? '退出全屏' : '全屏'}>
          {isFullscreen ? <IconMinimize size={18} /> : <IconMaximize size={18} />}
        </ActionIcon>

        {isABR && <Text size="xs" c="green" fw={500}>ABR</Text>}
      </Box>
    </Box>
  )
}
