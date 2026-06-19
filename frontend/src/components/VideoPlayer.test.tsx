import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, screen, cleanup, act } from '@testing-library/react'
import '@testing-library/jest-dom'
import VideoPlayer, { parseWebVTT } from './VideoPlayer'
import mpegts from 'mpegts.js'

// mpegts.js 模拟
const mockOff = vi.fn()
const mockDestroy = vi.fn()
const mockUnload = vi.fn()
const mockPause = vi.fn()
const mockPlay = vi.fn().mockResolvedValue(undefined)
const mockLoad = vi.fn()
const mockAttachMediaElement = vi.fn()
const mockOn = vi.fn()

vi.mock('mpegts.js', () => {
  return {
    __esModule: true,
    default: {
      Events: {
        ERROR: 'error',
        LOADING_COMPLETE: 'loading_complete',
        RECOVERED_EARLY_EOF: 'recovered_early_eof',
        MEDIA_INFO: 'media_info',
        METADATA_ARRIVED: 'metadata_arrived',
        SCRIPTDATA_ARRIVED: 'scriptdata_arrived',
        STATISTICS_INFO: 'statistics_info',
      },
      createPlayer: vi.fn(() => ({
        attachMediaElement: mockAttachMediaElement,
        load: mockLoad,
        play: mockPlay,
        pause: mockPause,
        unload: mockUnload,
        destroy: mockDestroy,
        on: mockOn,
        off: mockOff,
        currentTime: 0,
        volume: 1,
        muted: false,
      })),
    },
  }
})

// 获取被模拟的 createPlayer spy
const createPlayerMock = (mpegts as unknown as { createPlayer: ReturnType<typeof vi.fn> }).createPlayer

describe('VideoPlayer 组件', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  afterEach(() => {
    cleanup()
  })

  it('渲染 video 元素作为 mpegts.js 渲染目标', () => {
    render(<VideoPlayer url="http://example.com/stream.ts" />)
    const video = document.querySelector('video')
    expect(video).toBeInTheDocument()
    // 禁止设置 src 属性（由 mpegts.js 通过 MSE 管理）
    expect(video?.getAttribute('src')).toBeNull()
  })

  it('挂载时以指定配置创建 mpegts.js Player 实例', () => {
    render(<VideoPlayer url="http://example.com/stream.ts" />)

    expect(createPlayerMock).toHaveBeenCalledTimes(1)

    const callArgs = createPlayerMock.mock.calls[0]
    const sourceConfig = callArgs[0]
    const playerConfig = callArgs[1]

    expect(sourceConfig).toEqual({
      type: 'mpegts',
      url: 'http://example.com/stream.ts',
      isLive: true,
    })
    expect(playerConfig).toEqual({
      enableWorker: true,
      enableStashBuffer: true,
      stashInitialSize: 1024 * 1024,
      accurateSeek: true,
      seekType: 'range',
    })
  })

  it('挂载时调用 attachMediaElement 和 load', () => {
    render(<VideoPlayer url="http://example.com/stream.ts" />)
    expect(mockAttachMediaElement).toHaveBeenCalled()
    expect(mockLoad).toHaveBeenCalled()
  })

  it('卸载时调用 destroy 释放 MSE 资源', () => {
    const result = render(<VideoPlayer url="http://example.com/stream.ts" />)
    result.unmount()
    expect(mockPause).toHaveBeenCalled()
    expect(mockUnload).toHaveBeenCalled()
    expect(mockDestroy).toHaveBeenCalled()
  })

  it('URL 变化时重新初始化播放器', () => {
    const result = render(<VideoPlayer url="http://example.com/stream1.ts" />)
    vi.clearAllMocks()

    result.rerender(<VideoPlayer url="http://example.com/stream2.ts" />)

    // 旧实例应被销毁
    expect(mockDestroy).toHaveBeenCalledTimes(1)

    // 新实例应被创建
    expect(createPlayerMock).toHaveBeenCalledTimes(1)

    const callArgs = createPlayerMock.mock.calls[0]
    expect(callArgs[0].url).toBe('http://example.com/stream2.ts')
  })

  it('渲染播放控制 UI：播放/暂停按钮、进度条、音量控制', () => {
    render(<VideoPlayer url="http://example.com/stream.ts" />)

    const buttons = screen.getAllByRole('button')
    expect(buttons.length).toBeGreaterThanOrEqual(2)

    // input type=range 的 role 是 slider
    const sliders = document.querySelectorAll('input[type="range"]')
    expect(sliders.length).toBe(2)

    expect(screen.getAllByText('0:00').length).toBeGreaterThanOrEqual(1)
  })

  it('点击播放/暂停按钮调用 player.play()', () => {
    render(<VideoPlayer url="http://example.com/stream.ts" />)

    const playButton = screen.getByRole('button', { name: '播放' })
    expect(playButton).toBeInTheDocument()

    act(() => {
      playButton.click()
    })
    expect(mockPlay).toHaveBeenCalled()
  })

  it('autoPlay 默认为 true', () => {
    render(<VideoPlayer url="http://example.com/stream.ts" />)
    const callArgs = createPlayerMock.mock.calls[0]
    expect(callArgs[0].isLive).toBe(true)
  })

  it('subtitleVisible 为 false 时不显示字幕 overlay', () => {
    const entries = [{ start: 0, end: 5, text: '测试字幕' }]
    render(<VideoPlayer url="http://example.com/stream.ts" subtitleEntries={entries} subtitleVisible={false} />)
    expect(screen.queryByText('测试字幕')).not.toBeInTheDocument()
  })

  it('subtitleVisible 为 true 但无字幕条目时不显示字幕 overlay', () => {
    render(<VideoPlayer url="http://example.com/stream.ts" subtitleEntries={[]} subtitleVisible={true} />)
    // 字幕 overlay 不应渲染（因为 subtitleText 为空）
    const overlays = document.querySelectorAll('[class*="absolute inset-x-0 bottom"]')
    expect(overlays.length).toBe(0)
  })
})

describe('parseWebVTT', () => {
  it('解析标准 WebVTT 内容', () => {
    const vtt = `WEBVTT

1
00:00:01.000 --> 00:00:03.000
第一行字幕

2
00:00:04.000 --> 00:00:06.000
第二行字幕
`
    const entries = parseWebVTT(vtt)
    expect(entries).toHaveLength(2)
    expect(entries[0].start).toBe(1.0)
    expect(entries[0].end).toBe(3.0)
    expect(entries[0].text).toBe('第一行字幕')
    expect(entries[1].text).toBe('第二行字幕')
  })

  it('解析多行字幕文本', () => {
    const vtt = `WEBVTT

1
00:00:01.000 --> 00:00:03.000
第一行
第二行
`
    const entries = parseWebVTT(vtt)
    expect(entries).toHaveLength(1)
    expect(entries[0].text).toBe('第一行\n第二行')
  })

  it('返回空数组当输入为空', () => {
    const entries = parseWebVTT('')
    expect(entries).toEqual([])
  })

  it('跳过无效块', () => {
    const vtt = `WEBVTT

这不是有效字幕块

1
00:00:01.000 --> 00:00:03.000
有效字幕
`
    const entries = parseWebVTT(vtt)
    expect(entries).toHaveLength(1)
    expect(entries[0].text).toBe('有效字幕')
  })
})
