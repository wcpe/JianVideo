import { describe, it, expect, vi, beforeEach } from 'vitest'
import { registerPwa } from './pwa'

describe('registerPwa', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('调用注入的注册函数完成 Service Worker 注册', () => {
    const registerSW = vi.fn(() => vi.fn())
    registerPwa(registerSW)

    expect(registerSW).toHaveBeenCalledTimes(1)
    // 采用 autoUpdate，注册时传入立即更新选项
    expect(registerSW).toHaveBeenCalledWith(
      expect.objectContaining({ immediate: true }),
    )
  })

  it('注册函数抛错时静默吞掉，不向外抛出', () => {
    const registerSW = vi.fn(() => {
      throw new Error('注册失败')
    })

    // 注册失败不应中断应用启动
    expect(() => registerPwa(registerSW)).not.toThrow()
  })

  it('未提供注册函数时安全返回，不抛错', () => {
    expect(() => registerPwa(undefined)).not.toThrow()
  })
})
