import { describe, it, expect, vi, beforeEach } from 'vitest'
import { http, HttpResponse } from 'msw'

import { server } from '@/mocks/beforeAll'

// 拦截全局错误处理：断言网络错误时是否弹出全局 toast
const handleApiError = vi.fn()
vi.mock('@/utils/error', () => ({
  handleApiError: (...args: unknown[]) => handleApiError(...args),
  extractErrorMessage: (_: unknown, fallback: string) => fallback,
}))

// 被测模块在 mock 之后再导入，确保拦截器内引用的是 mock 版本
import client from './client'

describe('axios 客户端网络错误 toast 去重（FR-115 后续修复·通知白屏）', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('普通请求网络失败：弹一次全局网络 toast', async () => {
    // 用 msw 模拟网络层错误（无响应）
    server.use(http.get('*/api/test-normal', () => HttpResponse.error()))

    await expect(client.get('/api/test-normal')).rejects.toBeTruthy()
    expect(handleApiError).toHaveBeenCalledTimes(1)
  })

  it('带 silent 标记的请求网络失败：不弹全局网络 toast（防轮询失败堆积白屏）', async () => {
    server.use(http.get('*/api/test-silent', () => HttpResponse.error()))

    await expect(client.get('/api/test-silent', { silent: true })).rejects.toBeTruthy()
    expect(handleApiError).not.toHaveBeenCalled()
  })
})
