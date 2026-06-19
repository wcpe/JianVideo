import { useRef, useEffect, useState, useCallback } from 'react'
import mpegts from 'mpegts.js'
import { Progress } from '@mantine/core'

/** 一条解析后的字幕条目 */
interface SubtitleEntry {
  start: number
  end: number
  text: string
}

interface VideoPlayerProps {
  /** 流 URL（支持 master.m3u8 触发 ABR 模式） */
  url: string
  /** 自动播放 */
  autoPlay?: boolean
  /** 解析后的字幕条目列表 */
  subtitleEntries?: SubtitleEntry[]
  /** 是否显示字幕 */
  subtitleVisible?: boolean
}

/**
 * 轻量 WebVTT 解析器。
 * 将 WebVTT 文本解析为按时间排序的字幕条目数组。
 */
function parseWebVTT(vttText: string): SubtitleEntry[] {
  const entries: SubtitleEntry[] = []
  const blocks = vttText.split(/\n\n+/)

  for (const block of blocks) {
    const lines = block.trim().split('\n')
    const timingIdx = lines.findIndex(l => l.includes('-->'))
    if (timingIdx < 0) continue

    const timeLine = lines[timingIdx]
    const parts = timeLine.split('-->')
    if (parts.length !== 2) continue

    const start = parseVTTTime(parts[0].trim())
    const end = parseVTTTime(parts[1].trim())
    if (start < 0 || end < 0) continue

    const text = lines.slice(timingIdx + 1).join('\n').trim()
    if (text) entries.push({ start, end, text })
  }

  return entries
}

/**
 * 解析 WebVTT 时间戳 (HH:MM:SS.mmm 或 MM:SS.mmm) 为秒数。
 */
function parseVTTTime(ts: string): number {
  const parts = ts.split(':').map(s => parseFloat(s.trim()))
  if (parts.some(isNaN)) return -1
  if (parts.length === 3) return parts[0] * 3600 + parts[1] * 60 + parts[2]
  if (parts.length === 2) return parts[0] * 60 + parts[1]
  return -1
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
}: VideoPlayerProps) {
  const videoRef = useRef<HTMLVideoElement>(null)
  const mpegtsPlayerRef = useRef<mpegts.Player | null>(null)
  const hlsRef = useRef<unknown>(null)
  const [isPlaying, setIsPlaying] = useState(false)
  const [currentTime, setCurrentTime] = useState(0)
  const [duration, setDuration] = useState(0)
  const [volume, setVolume] = useState(1)
  const [isMuted, setIsMuted] = useState(false)
  const [bufferedProgress, setBufferedProgress] = useState(0)
  const [subtitleText, setSubtitleText] = useState('')

  // 判断是否为 ABR 模式（URL 以 master.m3u8 结尾）
  const isABR = url.endsWith('master.m3u8')

  // 销毁 mpegts.js 播放器
  const destroyMpegtsPlayer = useCallback(() => {
    const player = mpegtsPlayerRef.current
    if (player) {
      player.off('loadeddata')
      player.off('playing')
      player.off('pause')
      player.pause()
      player.unload()
      player.destroy()
      mpegtsPlayerRef.current = null
    }
  }, [])

  // 销毁 hls.js 播放器
  const destroyHlsPlayer = useCallback(async () => {
    const hls = hlsRef.current as { destroy: () => void } | null
    if (hls) {
      hls.destroy()
      hlsRef.current = null
    }
  }, [])

  // 初始化 mpegts.js 播放器
  const initMpegtsPlayer = useCallback(
    (streamUrl: string) => {
      if (!videoRef.current) return

      destroyMpegtsPlayer()

      const player = mpegts.createPlayer(
        {
          type: 'mpegts',
          url: streamUrl,
          isLive: true,
        },
        {
          enableWorker: true,
          enableStashBuffer: true,
          stashInitialSize: 1024 * 1024, // 1MB，约 3-5 秒追播延迟
          accurateSeek: true,
          seekType: 'range',
        }
      )

      player.attachMediaElement(videoRef.current)
      player.load()

      player.on('loadeddata', () => {
        if (autoPlay) {
          void (player.play() as Promise<void>)?.catch?.(() => {
            // 自动播放可能被浏览器策略阻止
          })
        }
      })

      player.on('playing', () => {
        setIsPlaying(true)
      })

      player.on('pause', () => {
        setIsPlaying(false)
      })

      mpegtsPlayerRef.current = player
    },
    [autoPlay, destroyMpegtsPlayer]
  )

  // 初始化 hls.js 播放器（动态 import）
  const initHlsPlayer = useCallback(
    async (masterUrl: string) => {
      if (!videoRef.current) return

      destroyHlsPlayer()

      try {
        const Hls = (await import('hls.js')).default

        if (!Hls.isSupported()) {
          console.warn('[VideoPlayer] hls.js 不支持当前浏览器，回退到 mpegts.js')
          initMpegtsPlayer(masterUrl)
          return
        }

        const hls = new Hls({
          enableWorker: true,
          lowLatencyMode: true,
        })

        hls.loadSource(masterUrl)
        hls.attachMedia(videoRef.current)

        hls.on(Hls.Events.MANIFEST_PARSED, () => {
          if (autoPlay) {
            void videoRef.current?.play()?.catch?.(() => {
              // 自动播放可能被浏览器策略阻止
            })
          }
        })

        hls.on(Hls.Events.LEVEL_SWITCHED, (_event, data) => {
          const level = hls.levels[data.level]
          if (level) {
            console.info(`[VideoPlayer] ABR 切换到: ${level.width}x${height}, ${level.bitrate}`)
          }
        })

        hls.on(Hls.Events.ERROR, (_event, data) => {
          if (data.fatal) {
            console.error('[VideoPlayer] hls.js 致命错误，回退到 mpegts.js', data)
            hls.destroy()
            hlsRef.current = null
            initMpegtsPlayer(masterUrl)
          }
        })

        hlsRef.current = hls
      } catch (err) {
        console.error('[VideoPlayer] 加载 hls.js 失败，回退到 mpegts.js', err)
        initMpegtsPlayer(masterUrl)
      }
    },
    [autoPlay, destroyHlsPlayer, initMpegtsPlayer]
  )

  // 挂载 / URL 变化时初始化
  useEffect(() => {
    if (isABR) {
      void initHlsPlayer(url)
    } else {
      initMpegtsPlayer(url)
    }

    return () => {
      void destroyHlsPlayer()
      destroyMpegtsPlayer()
    }
  }, [url, isABR, initHlsPlayer, initMpegtsPlayer, destroyHlsPlayer, destroyMpegtsPlayer])

  // 监听时间更新
  useEffect(() => {
    const video = videoRef.current
    if (!video) return

    const onTimeUpdate = () => setCurrentTime(video.currentTime)
    const onDurationChange = () => setDuration(video.duration)
    const onVolumeChange = () => {
      setVolume(video.volume)
      setIsMuted(video.muted)
    }
    const onProgress = () => {
      // 计算缓冲区进度：取最后一个缓冲段的结束位置
      if (video.buffered.length > 0 && video.duration > 0) {
        const lastBuffered = video.buffered.end(video.buffered.length - 1)
        setBufferedProgress((lastBuffered / video.duration) * 100)
      }
    }

    video.addEventListener('timeupdate', onTimeUpdate)
    video.addEventListener('durationchange', onDurationChange)
    video.addEventListener('volumechange', onVolumeChange)
    video.addEventListener('progress', onProgress)

    return () => {
      video.removeEventListener('timeupdate', onTimeUpdate)
      video.removeEventListener('durationchange', onDurationChange)
      video.removeEventListener('volumechange', onVolumeChange)
      video.removeEventListener('progress', onProgress)
    }
  }, [])

  // 字幕同步：根据当前播放时间匹配字幕文本
  useEffect(() => {
    if (!subtitleVisible || !subtitleEntries || subtitleEntries.length === 0) {
      setSubtitleText('')
      return
    }
    const entry = subtitleEntries.find(e => currentTime >= e.start && currentTime < e.end)
    setSubtitleText(entry?.text ?? '')
  }, [currentTime, subtitleEntries, subtitleVisible])

  const togglePlay = () => {
    const video = videoRef.current
    if (!video) return
    if (isPlaying) {
      video.pause()
    } else {
      void video.play()?.catch?.(() => {})
    }
  }

  const handleSeek = (e: React.ChangeEvent<HTMLInputElement>) => {
    const time = parseFloat(e.target.value)
    const video = videoRef.current
    if (!video) return
    video.currentTime = time
    setCurrentTime(time)
  }

  const handleVolumeChange = (e: React.ChangeEvent<HTMLInputElement>) => {
    const vol = parseFloat(e.target.value)
    const video = videoRef.current
    if (!video) return
    video.volume = vol
    video.muted = vol === 0
    setVolume(vol)
    setIsMuted(vol === 0)
  }

  const toggleMute = () => {
    const video = videoRef.current
    if (!video) return
    video.muted = !isMuted
    setIsMuted(!isMuted)
  }

  const formatTime = (seconds: number) => {
    if (!isFinite(seconds) || isNaN(seconds)) return '0:00'
    const mins = Math.floor(seconds / 60)
    const secs = Math.floor(seconds % 60)
    return `${mins}:${secs.toString().padStart(2, '0')}`
  }

  return (
    <div className="flex flex-col w-full bg-black rounded-lg overflow-hidden">
      {/* video 容器 — 包含 video 元素和字幕 overlay */}
      <div className="relative w-full aspect-video bg-black">
        {/* video 元素 — 仅作为 mpegts.js 渲染目标，不设置 src */}
        <video
          ref={videoRef}
          className="w-full h-full bg-black"
          playsInline
        />

        {/* 字幕 overlay */}
        {subtitleVisible && subtitleText && (
          <div className="absolute inset-x-0 bottom-[8%] flex justify-center pointer-events-none px-4">
            <span
              className="inline-block px-3 py-1 rounded text-white text-center text-sm sm:text-base leading-relaxed max-w-[80%]"
              style={{ backgroundColor: 'rgba(0,0,0,0.6)' }}
            >
              {subtitleText}
            </span>
          </div>
        )}
      </div>

      {/* 播放控制栏 */}
      <div className="flex items-center gap-3 px-4 py-2 bg-slate-900">
        {/* 播放/暂停 */}
        <button
          onClick={togglePlay}
          className="text-white hover:text-blue-400 transition-colors"
          aria-label={isPlaying ? '暂停' : '播放'}
        >
          {isPlaying ? (
            <svg width="24" height="24" viewBox="0 0 24 24" fill="currentColor">
              <rect x="6" y="4" width="4" height="16" />
              <rect x="14" y="4" width="4" height="16" />
            </svg>
          ) : (
            <svg width="24" height="24" viewBox="0 0 24 24" fill="currentColor">
              <polygon points="5,3 19,12 5,21" />
            </svg>
          )}
        </button>

        {/* 双进度条：播放进度 + 缓冲区进度 */}
        <div className="flex-1 flex items-center gap-2">
          <span className="text-xs text-slate-400 tabular-nums">
            {formatTime(currentTime)}
          </span>
          <Progress
            className="flex-1"
            size={4}
            radius="xl"
            animated
            sections={[
              {
                value: duration > 0 ? (currentTime / duration) * 100 : 0,
                color: 'blue',
                label: '播放进度',
              },
              {
                value: Math.max(0, bufferedProgress - (duration > 0 ? (currentTime / duration) * 100 : 0)),
                color: 'cyan',
                label: '缓冲进度',
              },
            ]}
          />
          <span className="text-xs text-slate-400 tabular-nums">
            {formatTime(duration)}
          </span>
        </div>

        {/* 音量控制 */}
        <div className="flex items-center gap-1">
          <button
            onClick={toggleMute}
            className="text-white hover:text-blue-400 transition-colors"
            aria-label={isMuted ? '取消静音' : '静音'}
          >
            {isMuted || volume === 0 ? (
              <svg width="20" height="20" viewBox="0 0 24 24" fill="currentColor">
                <path d="M16.5 12c0-1.77-1.02-3.29-2.5-4.03v2.21l2.45 2.45c.03-.2.05-.41.05-.63zm2.5 0c0 .94-.2 1.82-.54 2.64l1.51 1.51A8.796 8.796 0 0021 12c0-4.28-2.99-7.86-7-8.77v2.06c2.89.86 5 3.54 5 6.71zM4.27 3L3 4.27 7.73 9H3v6h4l5 5v-6.73l4.25 4.25c-.67.52-1.42.93-2.25 1.18v2.06a8.99 8.99 0 003.69-1.81L19.73 21 21 19.73l-9-9L4.27 3zM12 4L9.91 6.09 12 8.18V4z" />
              </svg>
            ) : (
              <svg width="20" height="20" viewBox="0 0 24 24" fill="currentColor">
                <path d="M3 9v6h4l5 5V4L7 9H3zm13.5 3c0-1.77-1.02-3.29-2.5-4.03v8.05c1.48-.73 2.5-2.25 2.5-4.02zM14 3.23v2.06c2.89.86 5 3.54 5 6.71s-2.11 5.85-5 6.71v2.06c4.01-.91 7-4.49 7-8.77s-2.99-7.86-7-8.77z" />
              </svg>
            )}
          </button>
          <input
            type="range"
            min={0}
            max={1}
            step={0.01}
            value={isMuted ? 0 : volume}
            onChange={handleVolumeChange}
            className="w-20 h-1 accent-blue-500 cursor-pointer"
          />
        </div>

        {/* ABR 模式标识 */}
        {isABR && (
          <span className="text-xs text-green-400 font-medium">ABR</span>
        )}
      </div>
    </div>
  )
}

// 导出工具函数供其他模块使用
export { parseWebVTT }
