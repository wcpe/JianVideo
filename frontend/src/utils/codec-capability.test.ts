import { describe, it, expect, vi, afterEach } from 'vitest'
import { codecMIME, isCodecSupported, probeClientCapabilities } from './codec-capability'

// 桩 MediaSource.isTypeSupported：按需返回支持/不支持
function stubIsTypeSupported(fn: (mime: string) => boolean) {
  vi.stubGlobal('MediaSource', { isTypeSupported: fn } as unknown as typeof MediaSource)
}

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('codecMIME（与后端 FMP4CodecMIME 契约一致）', () => {
  it('h265 返回 hvc1 MIME 串', () => {
    expect(codecMIME('h265')).toBe('video/mp4; codecs="hvc1.1.6.L93.B0"')
  })

  it('av1 返回 av01 MIME 串', () => {
    expect(codecMIME('av1')).toBe('video/mp4; codecs="av01.0.05M.08"')
  })

  it('vp9 返回 vp09 MIME 串', () => {
    expect(codecMIME('vp9')).toBe('video/mp4; codecs="vp09.00.10.08"')
  })

  it('hevc 归一化为 h265', () => {
    expect(codecMIME('hevc')).toBe('video/mp4; codecs="hvc1.1.6.L93.B0"')
  })

  it('大小写无关', () => {
    expect(codecMIME('AV1')).toBe('video/mp4; codecs="av01.0.05M.08"')
  })

  it('h264 / 空 / 未知编码返回空串', () => {
    expect(codecMIME('h264')).toBe('')
    expect(codecMIME('')).toBe('')
    expect(codecMIME('foo')).toBe('')
  })
})

describe('isCodecSupported（调用 MediaSource.isTypeSupported）', () => {
  it('isTypeSupported 返回 true 时归类为支持', () => {
    stubIsTypeSupported(() => true)
    expect(isCodecSupported('av1')).toBe(true)
  })

  it('isTypeSupported 返回 false 时归类为不支持', () => {
    stubIsTypeSupported(() => false)
    expect(isCodecSupported('av1')).toBe(false)
  })

  it('用对应编码的 MIME 串调用 isTypeSupported', () => {
    const spy = vi.fn(() => true)
    stubIsTypeSupported(spy)
    isCodecSupported('h265')
    expect(spy).toHaveBeenCalledWith('video/mp4; codecs="hvc1.1.6.L93.B0"')
  })

  it('非高级编码（无 MIME 串）直接返回 false，不调用 isTypeSupported', () => {
    const spy = vi.fn(() => true)
    stubIsTypeSupported(spy)
    expect(isCodecSupported('h264')).toBe(false)
    expect(spy).not.toHaveBeenCalled()
  })

  it('环境无 MediaSource 时返回 false', () => {
    vi.stubGlobal('MediaSource', undefined)
    expect(isCodecSupported('av1')).toBe(false)
  })
})

describe('probeClientCapabilities（三高级编码能力描述）', () => {
  it('全部支持时三项均为 true', () => {
    stubIsTypeSupported(() => true)
    expect(probeClientCapabilities()).toEqual({ h265: true, av1: true, vp9: true })
  })

  it('全部不支持时三项均为 false', () => {
    stubIsTypeSupported(() => false)
    expect(probeClientCapabilities()).toEqual({ h265: false, av1: false, vp9: false })
  })

  it('按 MIME 串区分各编码支持情况', () => {
    // 仅 av1 支持
    stubIsTypeSupported((mime) => mime.includes('av01'))
    expect(probeClientCapabilities()).toEqual({ h265: false, av1: true, vp9: false })
  })

  it('无 MediaSource 时三项均为 false', () => {
    vi.stubGlobal('MediaSource', undefined)
    expect(probeClientCapabilities()).toEqual({ h265: false, av1: false, vp9: false })
  })
})
