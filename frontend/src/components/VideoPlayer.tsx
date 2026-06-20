import { useRef, useEffect, useState, useCallback } from 'react'
import { Text, Box, ActionIcon, Slider } from '@mantine/core'
import { IconPlayerPlay, IconPlayerPause, IconVolume, IconVolumeOff } from '@tabler/icons-react'
import mpegts from 'mpegts.js'
import type Hls from 'hls.js'
import { parseWebVTT } from '@/utils/subtitle'
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
}

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
}: VideoPlayerProps) {
  const videoRef = useRef<HTMLVideoElement>(null)
  const mpegtsPlayerRef = useRef<mpegts.Player | null>(null)
  const hlsRef = useRef<Hls | null>(null)
  const [isPlaying, setIsPlaying] = useState(false)
  const [autoPlayBlocked, setAutoPlayBlocked] = useState(false)
  const [currentTime, setCurrentTime] = useState(0)
  const [duration, setDuration] = useState(0)
  const [volume, setVolume] = useState(1)
  const [isMuted, setIsMuted] = useState(false)
  const [bufferedProgress, setBufferedProgress] = useState(0)
  const [subtitleText, setSubtitleText] = useState('')

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
      player.on('playing', () => setIsPlaying(true))
      player.on('pause', () => setIsPlaying(false))
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

  // 监听时间/音量/缓冲
  useEffect(() => {
    const v = videoRef.current; if (!v) return
    const onTime = () => setCurrentTime(v.currentTime)
    const onDur = () => setDuration(v.duration)
    const onVol = () => { setVolume(v.volume); setIsMuted(v.muted) }
    const onBuf = () => { if (v.buffered.length > 0 && v.duration > 0) setBufferedProgress((v.buffered.end(v.buffered.length - 1) / v.duration) * 100) }
    v.addEventListener('timeupdate', onTime); v.addEventListener('durationchange', onDur)
    v.addEventListener('volumechange', onVol); v.addEventListener('progress', onBuf)
    return () => { v.removeEventListener('timeupdate', onTime); v.removeEventListener('durationchange', onDur); v.removeEventListener('volumechange', onVol); v.removeEventListener('progress', onBuf) }
  }, [])

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

// 导出工具函数供其他模块使用
export { parseWebVTT }
