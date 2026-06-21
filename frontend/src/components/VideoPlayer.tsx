import { useRef, useEffect, useState, useCallback } from 'react'
import { Text, Box, ActionIcon, Slider } from '@mantine/core'
import { IconPlayerPlay, IconPlayerPause, IconVolume, IconVolumeOff } from '@tabler/icons-react'
import mpegts from 'mpegts.js'
import type Hls from 'hls.js'
import type { SubtitleEntry } from '@/types'

interface VideoPlayerProps {
  /** 流 URL（支持 master.m3u8 触发 ABR 模式） */
  url: string
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

/**
 * 视频播放组件
 *
 * - URL 为 master.m3u8 时启用 ABR 模式（hls.js 动态加载）
 * - 其他 URL 使用 mpegts.js 播放
 */
export default function VideoPlayer({
  url,
  autoPlay = true,
  subtitleEntries,
  subtitleVisible = false,
  isABR: isABRProp,
  streamType = 'mpegts',
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
  const [currentTime, setCurrentTime] = useState(0)
  const [duration, setDuration] = useState(0)
  const [volume, setVolume] = useState(1)
  const [isMuted, setIsMuted] = useState(false)
  const [bufferedProgress, setBufferedProgress] = useState(0)
  const [subtitleText, setSubtitleText] = useState('')
  // 末端缓冲等待（FR-18）：mpegts.js ERROR 或 video stalled 时进入等待，1s 后自动重载
  const [isWaiting, setIsWaiting] = useState(false)

  // 判断是否为 ABR 模式
  const isABR = isABRProp ?? url.endsWith('master.m3u8')

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
      player.on('loadeddata', () => { if (autoPlay) void (player.play() as Promise<void>)?.catch?.(() => setAutoPlayBlocked(true)) })
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
    [autoPlay, destroyMpegtsPlayer]
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
        hls.on(Hls.Events.MANIFEST_PARSED, () => { if (autoPlay) void videoRef.current?.play()?.catch?.(() => setAutoPlayBlocked(true)) })
        hls.on(Hls.Events.LEVEL_SWITCHED, (_e, d) => { const l = hls.levels[d.level]; if (l) console.info(`[VideoPlayer] ABR: ${l.width}x${l.height}`) })
        hls.on(Hls.Events.ERROR, (_e, d) => { if (d.fatal) { hls.destroy(); hlsRef.current = null; initMpegtsPlayer(masterUrl) } })
        hlsRef.current = hls
      } catch { initMpegtsPlayer(masterUrl) }
    },
    [autoPlay, destroyHlsPlayer, initMpegtsPlayer]
  )

  // 挂载 / URL 变化
  useEffect(() => {
    if (streamType === 'mp4' && videoRef.current) {
      destroyMpegtsPlayer(); void destroyHlsPlayer()
      videoRef.current.src = url
      if (autoPlay) void videoRef.current.play()?.catch?.(() => setAutoPlayBlocked(true))
      return () => { if (videoRef.current) videoRef.current.removeAttribute('src') }
    }
    if (isABR) void initHlsPlayer(url); else initMpegtsPlayer(url)
    return () => { void destroyHlsPlayer(); destroyMpegtsPlayer() }
  }, [url, isABR, streamType, autoPlay, initHlsPlayer, initMpegtsPlayer, destroyHlsPlayer, destroyMpegtsPlayer])

  // FR-44：URL 变化（切换媒体）时重置续播 / 上报 / 看完的一次性标志
  useEffect(() => {
    hasSeekedRef.current = false
    lastReportRef.current = 0
    endedReportedRef.current = false
  }, [url])

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

  const togglePlay = () => {
    const v = videoRef.current; if (!v) return
    if (isPlaying) v.pause(); else { setAutoPlayBlocked(false); void v.play()?.catch?.(() => {}) }
  }

  const handleSeek = (val: number) => { const v = videoRef.current; if (v && duration) v.currentTime = (val / 100) * duration }
  const handleVolume = (val: number) => { const v = videoRef.current; if (!v) return; v.volume = val; v.muted = val === 0; setVolume(val); setIsMuted(val === 0) }
  const toggleMute = () => { const v = videoRef.current; if (!v) return; v.muted = !isMuted; setIsMuted(!isMuted) }

  const fmt = (s: number) => { if (!isFinite(s) || isNaN(s)) return '0:00'; const m = Math.floor(s / 60); return `${m}:${Math.floor(s % 60).toString().padStart(2, '0')}` }
  const playPct = duration > 0 ? (currentTime / duration) * 100 : 0

  return (
    <Box style={{ display: 'flex', flexDirection: 'column', width: '100%', backgroundColor: 'black', borderRadius: 'var(--mantine-radius-lg)', overflow: 'hidden' }}>
      <Box style={{ position: 'relative', width: '100%', aspectRatio: '16/9', backgroundColor: 'black' }}>
        <video ref={videoRef} style={{ width: '100%', height: '100%', backgroundColor: 'black' }} playsInline />
        {autoPlayBlocked && !isPlaying && (
          <Box style={{ position: 'absolute', inset: 0, display: 'flex', alignItems: 'center', justifyContent: 'center', backgroundColor: 'rgba(0,0,0,0.5)', cursor: 'pointer' }} onClick={togglePlay}>
            <Text c="white" size="lg">点击播放</Text>
          </Box>
        )}
        {isWaiting && !autoPlayBlocked && (
          <Box aria-label="缓冲等待" role="status" style={{ position: 'absolute', inset: 0, display: 'flex', alignItems: 'center', justifyContent: 'center', backgroundColor: 'rgba(0,0,0,0.45)', pointerEvents: 'none' }}>
            <Text c="white" size="sm">等待新数据…</Text>
          </Box>
        )}
        {subtitleVisible && subtitleText && (
          <Box style={{ position: 'absolute', insetInline: 0, bottom: '8%', display: 'flex', justifyContent: 'center', pointerEvents: 'none', padding: '0 1rem' }}>
            <Text component="span" c="white" size="sm" ta="center" style={{ display: 'inline-block', padding: '0.25rem 0.75rem', borderRadius: 'var(--mantine-radius-sm)', backgroundColor: 'rgba(0,0,0,0.6)', maxWidth: '80%' }}>{subtitleText}</Text>
          </Box>
        )}
      </Box>

      <Box style={{ display: 'flex', alignItems: 'center', gap: 'var(--mantine-spacing-sm)', padding: '0.5rem 1rem', backgroundColor: 'var(--mantine-color-dark-8, #1a1b1e)' }}>
        <ActionIcon variant="subtle" color="white" onClick={togglePlay} aria-label={isPlaying ? '暂停' : '播放'}>
          {isPlaying ? <IconPlayerPause size={22} /> : <IconPlayerPlay size={22} />}
        </ActionIcon>

        <Box style={{ flex: 1, display: 'flex', alignItems: 'center', gap: 'var(--mantine-spacing-xs)' }}>
          <Text size="xs" c="dimmed" style={{ fontVariantNumeric: 'tabular-nums' }}>{fmt(currentTime)}</Text>
          <Box style={{ flex: 1, position: 'relative', height: 4 }}>
            <Slider value={Math.min(bufferedProgress, 100)} size={4} color="cyan" radius="xl" thumbSize={0} style={{ position: 'absolute', width: '100%', top: 0, pointerEvents: 'none' }} />
            <Slider value={playPct} onChange={handleSeek} size={4} color="blue" radius="xl" />
          </Box>
          <Text size="xs" c="dimmed" style={{ fontVariantNumeric: 'tabular-nums' }}>{fmt(duration)}</Text>
        </Box>

        <Box style={{ display: 'flex', alignItems: 'center', gap: '4px' }}>
          <ActionIcon variant="subtle" color="white" onClick={toggleMute} aria-label={isMuted ? '取消静音' : '静音'}>
            {isMuted || volume === 0 ? <IconVolumeOff size={18} /> : <IconVolume size={18} />}
          </ActionIcon>
          <Slider value={isMuted ? 0 : volume} onChange={handleVolume} size={4} color="blue" radius="xl" showLabelOnHover={false} style={{ width: '5rem' }} />
        </Box>

        {isABR && <Text size="xs" c="green" fw={500}>ABR</Text>}
      </Box>
    </Box>
  )
}
