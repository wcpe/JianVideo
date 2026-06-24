import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MantineProvider } from '@mantine/core'
import { MemoryRouter, Routes, Route } from 'react-router-dom'
import { describe, it, expect, vi, beforeEach } from 'vitest'

import SharePage from './SharePage'
import type { ShareInfo } from '@/types'

// 捕获密码入参以验证带头重拉（FR-78）
const mockGetShareInfo = vi.fn<(token: string, password?: string) => Promise<ShareInfo>>()
vi.mock('@/api/share', async () => {
  const actual = await vi.importActual<typeof import('@/api/share')>('@/api/share')
  return { ...actual, getShareInfo: (token: string, password?: string) => mockGetShareInfo(token, password) }
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

  it('需密码分享先弹密码门，输入正确密码后展示内容（FR-78）', async () => {
    // 首次（无密码）只回 requires_password；带密码重拉返回完整 media
    mockGetShareInfo.mockImplementation((_token, password) =>
      Promise.resolve(
        password === 'pw'
          ? { resource_type: 'media', expires_at: null, requires_password: true, media: imageMedia }
          : { resource_type: 'media', requires_password: true },
      ),
    )
    renderAt('locked')

    // 先展示密码输入门
    const pwdInput = await screen.findByLabelText('访问密码')
    const user = userEvent.setup()
    await user.type(pwdInput, 'pw')
    await user.click(screen.getByRole('button', { name: '访问' }))

    // 带正确密码后展示图片
    const img = await screen.findByRole('img', { name: '风景.jpg' })
    expect(img).toHaveAttribute('src', '/api/share/locked/media/7/raw')
    // 第二次调用带上了密码
    expect(mockGetShareInfo).toHaveBeenCalledWith('locked', 'pw')
  })

  it('密码错误展示不泄露提示（FR-78）', async () => {
    // 始终只回 requires_password（密码错）
    mockGetShareInfo.mockResolvedValue({ resource_type: 'media', requires_password: true })
    renderAt('locked')

    const pwdInput = await screen.findByLabelText('访问密码')
    const user = userEvent.setup()
    await user.type(pwdInput, 'bad')
    await user.click(screen.getByRole('button', { name: '访问' }))

    expect(await screen.findByText(/分享不存在或已过期/)).toBeInTheDocument()
  })
})
