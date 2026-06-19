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
  /** TS 流 URL */
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
 * mpegts.js 播放内核组件
 *
 * 通过 MSE API 播放 MPEG-TS 流，支持边下边播和精准 Seek。
 * 禁止原生 video 标签直接处理 TS 流。
 */
export default function VideoPlayer({
  url,
  autoPlay = true,
  subtitleEntries,
  subtitleVisible = false,
}: VideoPlayerProps) {
  const videoRef = useRef<HTMLVideoElement>(null)
  const playerRef = useRef<mpegts.Player | null>(null)
  const [isPlaying, setIsPlaying] = useState(false)
  const [currentTime, setCurrentTime] = useState(0)
  const [duration, setDuration] = useState(0)
  const [volume, setVolume] = useState(1)
  const [isMuted, setIsMuted] = useState(false)
  const [bufferedProgress, setBufferedProgress] = useState(0)
  const [subtitleText, setSubtitleText] = useState('')

  // 销毁播放器
  const destroyPlayer = useCallback(() => {
    const player = playerRef.current
    if (player) {
      // 移除所有已注册的事件监听器
      player.off('loadeddata')
      player.off('playing')
      player.off('pause')
      player.pause()
      player.unload()
      player.destroy()
      playerRef.current = null
    }
  }, [])

  // 初始化播放器
  const initPlayer = useCallback(
    (streamUrl: string) => {
      if (!videoRef.current) return

      // 清理旧实例
      destroyPlayer()

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

      // 使用字符串事件名（mpegts.js 运行时触发的事件，不在 d.ts 中声明）
      player.on('loadeddata', () => {
        if (autoPlay) {
          // play() 返回 Promise<void> | void
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

      playerRef.current = player
    },
    [autoPlay, destroyPlayer]
  )

  // 挂载 / URL 变化时初始化
  useEffect(() => {
    initPlayer(url)

    return () => {
      destroyPlayer()
    }
  }, [url, initPlayer, destroyPlayer])

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
    const player = playerRef.current
    if (!player) return
    if (isPlaying) {
      player.pause()
    } else {
      void (player.play() as Promise<void>)?.catch?.(() => {})
    }
  }

  const handleSeek = (e: React.ChangeEvent<HTMLInputElement>) => {
    const time = parseFloat(e.target.value)
    const player = playerRef.current
    if (!player) return
    player.currentTime = time
    setCurrentTime(time)
  }

  const handleVolumeChange = (e: React.ChangeEvent<HTMLInputElement>) => {
    const vol = parseFloat(e.target.value)
    const player = playerRef.current
    if (!player) return
    player.volume = vol
    player.muted = vol === 0
    setVolume(vol)
    setIsMuted(vol === 0)
  }

  const toggleMute = () => {
    const player = playerRef.current
    if (!player) return
    player.muted = !isMuted
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
      </div>
    </div>
  )
}

// 导出工具函数供其他模块使用
export { parseWebVTT }
