import { render, screen, waitFor } from '@testing-library/react'
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { createMemoryRouter, RouterProvider } from 'react-router-dom'
import { MantineProvider } from '@mantine/core'
import { http, HttpResponse } from 'msw'
import { server } from '@/mocks/beforeAll'
import PlayPage from './PlayPage'

// mock VideoPlayer 组件，避免依赖 mpegts.js
vi.mock('@/components/VideoPlayer', () => ({
  default: (props: { url?: string; isABR?: boolean; streamType?: string; initialPosition?: number }) => <div data-testid="video-player" data-url={props.url} data-is-abr={String(!!props.isABR)} data-stream-type={props.streamType || ''} data-initial-position={props.initialPosition ?? ''} />,
}))

function renderPlayPage(route: string) {
  const router = createMemoryRouter(
    [{ path: '/play/:id', element: <PlayPage /> }],
    { initialEntries: [route] },
  )
  return render(
    <MantineProvider>
      <RouterProvider router={router} />
    </MantineProvider>,
  )
}

describe('PlayPage', () => {
  beforeEach(() => {
    vi.restoreAllMocks()
  })

  it('渲染加载状态', () => {
    renderPlayPage('/play/1')
    const skeleton = document.querySelector('.mantine-Skeleton-root')
    expect(skeleton).toBeInTheDocument()
  })

  it('无效 ID 显示错误提示', async () => {
    renderPlayPage('/play/0')
    await waitFor(() => {
      expect(screen.getByText('无效的媒体 ID')).toBeInTheDocument()
    })
  })

  it('master.m3u8 不可用时改用 /api/play/:id/stream', async () => {
    // master.m3u8 探测返回 404 + JSON，content-type 不是 mpegurl，应降级
    server.use(
      http.get('*/api/play/hls/1/master.m3u8', () =>
        HttpResponse.json({ code: 'NOT_FOUND' }, { status: 404 }),
      ),
    )

    renderPlayPage('/play/1')

    const player = await screen.findByTestId('video-player')
    await waitFor(() => {
      const url = player.getAttribute('data-url') || ''
      // URL 是绝对形式，避免 mpegts.js Web Worker 解析相对 URL 失败
      expect(url).toMatch(/\/api\/play\/1\/stream$/)
      expect(url).not.toContain('master.m3u8')
      // 降级路径必须显式关闭 ABR 并切换到原生 mp4 模式
      expect(player.getAttribute('data-is-abr')).toBe('false')
      expect(player.getAttribute('data-stream-type')).toBe('mp4')
    })
  })

  it('把媒体的 last_position 作为续播起点传给播放器（FR-44）', async () => {
    server.use(
      http.get('*/api/library/media/7', () =>
        HttpResponse.json({
          id: 7, library_id: 1, file_path: 'D:/Videos/a.mp4', file_name: 'a.mp4',
          file_size: 1024, format: 'mp4', video_codec: 'h264', audio_codec: 'aac',
          duration: 6600, width: 1920, height: 1080, bitrate: 7000000, subtitle_tracks: '',
          added_at: '2025-01-01T12:00:00Z', modified_at: '2025-01-01T12:00:00Z',
          last_position: 123.4, watched: false,
        }),
      ),
      http.get('*/api/play/hls/7/master.m3u8', () =>
        HttpResponse.json({ code: 'NOT_FOUND' }, { status: 404 }),
      ),
    )

    renderPlayPage('/play/7')

    const player = await screen.findByTestId('video-player')
    await waitFor(() => {
      expect(player.getAttribute('data-initial-position')).toBe('123.4')
    })
  })
})
