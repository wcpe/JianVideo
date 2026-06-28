import { render, screen, act } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { MantineProvider } from '@mantine/core'
import VideoPlayer, { loadVolumePref, saveVolumePref, clampVolume, VOLUME_PREF_KEY } from './VideoPlayer'

// 走 streamType='mp4' 原生 video 分支，避免依赖 mpegts.js / hls.js 真实内核
vi.mock('mpegts.js', () => ({ default: { createPlayer: () => ({}) } }))
vi.mock('hls.js', () => ({ default: class { static isSupported() { return false } } }))

// jsdom 不实现 HTMLMediaElement.play；打桩为成功 resolve
function stubVideoPlay() {
  const proto = Object.getPrototypeOf(
    Object.getPrototypeOf(document.createElement('video')),
  ) as HTMLMediaElement
  Object.defineProperty(proto, 'play', { configurable: true, writable: true, value: vi.fn(() => Promise.resolve()) })
  Object.defineProperty(proto, 'pause', { configurable: true, writable: true, value: vi.fn() })
}

// 在 jsdom 中桩 video 的 currentTime/duration（默认只读/不可控）
function stubVideoTime(video: HTMLVideoElement, duration: number) {
  let cur = 0
  Object.defineProperty(video, 'currentTime', {
    configurable: true,
    get: () => cur,
    set: (v: number) => { cur = v },
  })
  Object.defineProperty(video, 'duration', { configurable: true, get: () => duration })
}

function renderPlayer(props?: Partial<React.ComponentProps<typeof VideoPlayer>>) {
  return render(
    <MantineProvider>
      <VideoPlayer url="/api/play/1/stream" streamType="mp4" autoPlay={false} {...props} />
    </MantineProvider>,
  )
}

describe('记忆音量纯函数（FR-104）', () => {
  beforeEach(() => localStorage.clear())

  it('无存储时返回 null', () => {
    expect(loadVolumePref()).toBeNull()
  })

  it('saveVolumePref 写入后 loadVolumePref 读回相同偏好', () => {
    saveVolumePref({ volume: 0.4, muted: false })
    expect(loadVolumePref()).toEqual({ volume: 0.4, muted: false })
    expect(localStorage.getItem(VOLUME_PREF_KEY)).toContain('0.4')
  })

  it('记忆静音态', () => {
    saveVolumePref({ volume: 0.8, muted: true })
    expect(loadVolumePref()).toEqual({ volume: 0.8, muted: true })
  })

  it('损坏的存储内容返回 null（不抛）', () => {
    localStorage.setItem(VOLUME_PREF_KEY, '{不是合法json')
    expect(loadVolumePref()).toBeNull()
  })

  it('越界 volume 被夹取到 [0,1]', () => {
    saveVolumePref({ volume: 5, muted: false })
    expect(loadVolumePref()?.volume).toBe(1)
  })

  it('clampVolume 把越界 / 非有限值夹取到 [0,1]', () => {
    expect(clampVolume(50)).toBe(1)
    expect(clampVolume(0.5)).toBe(0.5)
    expect(clampVolume(-0.2)).toBe(0)
    expect(clampVolume(Number.NaN)).toBe(0)
  })
})

describe('音量滑块域（FR-104 修复 IndexSizeError）', () => {
  beforeEach(() => { localStorage.clear(); stubVideoPlay() })

  it('音量滑块域为 [0,1]，避免 onChange 输出 0-100 致 video.volume 越界崩溃', () => {
    renderPlayer()
    // 修复前音量 Slider 用 Mantine 默认 max=100，拖动时 onChange 给 0-100、handleVolume 直接赋给 v.volume 触发 IndexSizeError；
    // 修复后音量 Slider min=0 max=1，故必有一个滑块 aria-valuemax 为 '1'
    const sliders = screen.getAllByRole('slider')
    expect(sliders.some((s) => s.getAttribute('aria-valuemax') === '1')).toBe(true)
  })
})

describe('VideoPlayer 倍速（FR-104）', () => {
  beforeEach(() => { stubVideoPlay(); localStorage.clear() })

  it('选择倍速后置 video.playbackRate', async () => {
    const { container } = renderPlayer()
    const video = container.querySelector('video') as HTMLVideoElement

    await act(async () => { await userEvent.click(screen.getByRole('button', { name: '播放速度' })) })
    await act(async () => { await userEvent.click(screen.getByRole('menuitem', { name: '1.5×' })) })

    expect(video.playbackRate).toBe(1.5)
  })
})

describe('VideoPlayer ±10s 快进快退（FR-104）', () => {
  beforeEach(() => { stubVideoPlay(); localStorage.clear() })

  it('快进 +10s 增加 currentTime', async () => {
    const { container } = renderPlayer()
    const video = container.querySelector('video') as HTMLVideoElement
    stubVideoTime(video, 600)
    video.currentTime = 100

    await act(async () => { await userEvent.click(screen.getByRole('button', { name: '快进 10 秒' })) })
    expect(video.currentTime).toBe(110)
  })

  it('快退 -10s 减少 currentTime 且不为负', async () => {
    const { container } = renderPlayer()
    const video = container.querySelector('video') as HTMLVideoElement
    stubVideoTime(video, 600)
    video.currentTime = 5

    await act(async () => { await userEvent.click(screen.getByRole('button', { name: '快退 10 秒' })) })
    expect(video.currentTime).toBe(0)
  })
})

describe('VideoPlayer 键盘快捷键（FR-104）', () => {
  beforeEach(() => { stubVideoPlay(); localStorage.clear() })

  it('空格切换播放/暂停', async () => {
    const { container } = renderPlayer()
    const video = container.querySelector('video') as HTMLVideoElement
    const root = screen.getByTestId('video-player-root')
    root.focus()

    await act(async () => { await userEvent.keyboard(' ') })
    expect(video.play).toHaveBeenCalled()
  })

  it('M 键切换静音', async () => {
    const { container } = renderPlayer()
    const video = container.querySelector('video') as HTMLVideoElement
    const root = screen.getByTestId('video-player-root')
    root.focus()

    expect(video.muted).toBe(false)
    await act(async () => { await userEvent.keyboard('m') })
    expect(video.muted).toBe(true)
  })

  it('→ 键快进 10s、← 键快退 10s', async () => {
    const { container } = renderPlayer()
    const video = container.querySelector('video') as HTMLVideoElement
    stubVideoTime(video, 600)
    video.currentTime = 100
    const root = screen.getByTestId('video-player-root')
    root.focus()

    await act(async () => { await userEvent.keyboard('{ArrowRight}') })
    expect(video.currentTime).toBe(110)
    await act(async () => { await userEvent.keyboard('{ArrowLeft}') })
    expect(video.currentTime).toBe(100)
  })

  it('焦点在输入框时不触发快捷键', async () => {
    const { container } = render(
      <MantineProvider>
        <div>
          <input data-testid="text-input" />
          <VideoPlayer url="/api/play/1/stream" streamType="mp4" autoPlay={false} />
        </div>
      </MantineProvider>,
    )
    const video = container.querySelector('video') as HTMLVideoElement
    const input = screen.getByTestId('text-input') as HTMLInputElement
    input.focus()

    await act(async () => { await userEvent.keyboard(' ') })
    // 焦点在输入框，空格不应触发播放
    expect(video.play).not.toHaveBeenCalled()
  })
})

describe('VideoPlayer 进度条品牌紫（FR-104 修偏 FR-93）', () => {
  beforeEach(() => { stubVideoPlay(); localStorage.clear() })

  it('播放进度条使用品牌紫（不再用蓝）', () => {
    const { container } = renderPlayer()
    // Mantine Slider 以 --slider-color CSS 变量承载 color；品牌紫命名色为 purple
    const purpleSlider = container.querySelector('[style*="--slider-color: purple"]')
    expect(purpleSlider).not.toBeNull()
    // 不应再出现蓝色 slider（既有 color="blue" 已收敛为 purple）
    const blueSlider = container.querySelector('[style*="--slider-color: blue"]')
    expect(blueSlider).toBeNull()
  })
})
