import { describe, it, expect } from 'vitest'
import { http, HttpResponse } from 'msw'
import { server } from '@/mocks/beforeAll'
import { negotiate } from './play'

describe('negotiate（FR-53 编码协商 API）', () => {
  it('上报客户端能力并把相对 URL 绝对化', async () => {
    const captured: { caps?: Record<string, boolean> } = {}
    server.use(
      http.post('*/api/play/9/negotiate', async ({ request }) => {
        const reqBody = await request.json() as { client_caps?: Record<string, boolean> }
        captured.caps = reqBody.client_caps
        return HttpResponse.json({
          codec: 'av1', path: 'fmp4', url: '/api/play/hls/9/index.m3u8',
          mime: 'video/mp4; codecs="av01.0.05M.08"', fallback_url: '/api/play/9/stream',
        })
      }),
    )

    const desc = await negotiate(9, { h265: false, av1: true, vp9: false })

    expect(captured.caps).toEqual({ h265: false, av1: true, vp9: false })
    expect(desc.codec).toBe('av1')
    expect(desc.path).toBe('fmp4')
    expect(desc.url).toMatch(/^https?:\/\/.+\/api\/play\/hls\/9\/index\.m3u8$/)
    expect(desc.fallbackUrl).toMatch(/^https?:\/\/.+\/api\/play\/9\/stream$/)
  })

  it('h264/TS 描述符无 fallbackUrl', async () => {
    server.use(
      http.post('*/api/play/2/negotiate', () =>
        HttpResponse.json({ codec: 'h264', path: 'ts', url: '/api/play/hls/2/master' }),
      ),
    )

    const desc = await negotiate(2, { h265: false, av1: false, vp9: false })

    expect(desc.codec).toBe('h264')
    expect(desc.path).toBe('ts')
    expect(desc.fallbackUrl).toBeUndefined()
  })
})
