import { render, screen } from '@testing-library/react'
import { MantineProvider } from '@mantine/core'
import { MemoryRouter, Routes, Route } from 'react-router-dom'
import { describe, it, expect, vi, beforeEach } from 'vitest'

import SharePage from './SharePage'
import type { ShareInfo } from '@/types'

const mockGetShareInfo = vi.fn<() => Promise<ShareInfo>>()
vi.mock('@/api/share', async () => {
  const actual = await vi.importActual<typeof import('@/api/share')>('@/api/share')
  return { ...actual, getShareInfo: () => mockGetShareInfo() }
})

function renderAt(token: string) {
  return render(
    <MantineProvider>
      <MemoryRouter initialEntries={[`/s/${token}`]}>
        <Routes>
          <Route path="/s/:token" element={<SharePage />} />
        </Routes>
      </MemoryRouter>
    </MantineProvider>,
  )
}

const imageMedia = {
  id: 7, library_id: 1, file_path: 'D:/Photos/风景.jpg', file_name: '风景.jpg',
  file_size: 100, format: 'jpg', video_codec: '', audio_codec: '', duration: 0,
  width: 0, height: 0, bitrate: 0, subtitle_tracks: '', added_at: '', modified_at: '',
}

describe('SharePage 公开分享查看页（FR-43）', () => {
  beforeEach(() => vi.clearAllMocks())

  it('媒体分享展示图片与下载入口，下载指向公开端点', async () => {
    mockGetShareInfo.mockResolvedValue({ resource_type: 'media', expires_at: null, media: imageMedia })
    renderAt('tok1')

    const img = await screen.findByRole('img', { name: '风景.jpg' })
    expect(img).toHaveAttribute('src', '/api/share/tok1/media/7/raw')
    const dl = screen.getByRole('link', { name: /下载原文件/ })
    expect(dl).toHaveAttribute('href', '/api/share/tok1/media/7/download')
  })

  it('无效分享展示过期提示', async () => {
    mockGetShareInfo.mockRejectedValue(new Error('分享不存在或已过期'))
    renderAt('bad')
    expect(await screen.findByText(/分享不存在或已过期/)).toBeInTheDocument()
  })
})
