import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MantineProvider } from '@mantine/core'
import { MemoryRouter } from 'react-router-dom'
import { describe, it, expect, vi, beforeEach } from 'vitest'

import MediaDetailPanel from './MediaDetailPanel'
import type { MediaFile } from '@/types'

const mockNavigate = vi.fn()
vi.mock('react-router-dom', async (importOriginal) => {
  const actual = await importOriginal<typeof import('react-router-dom')>()
  return { ...actual, useNavigate: () => mockNavigate }
})

function mediaFile(over: Partial<MediaFile>): MediaFile {
  return {
    id: 1, library_id: 1, file_path: 'D:/m/a.jpg', file_name: 'a.jpg',
    file_size: 1000, format: 'jpg', video_codec: '', audio_codec: '', duration: 0,
    width: 800, height: 600, bitrate: 0, subtitle_tracks: '', added_at: '2025-01-01T00:00:00Z',
    modified_at: '2025-01-01T00:00:00Z', ...over,
  } as MediaFile
}

function renderPanel(files: MediaFile[], initialIndex: number | null) {
  return render(
    <MantineProvider>
      <MemoryRouter>
        <MediaDetailPanel files={files} initialIndex={initialIndex} onClose={() => {}} customImageExtensions={{}} />
      </MemoryRouter>
    </MantineProvider>,
  )
}

describe('MediaDetailPanel 文件详情面板（FR-34）', () => {
  beforeEach(() => vi.clearAllMocks())

  it('关闭态（initialIndex=null）不渲染对话框', () => {
    renderPanel([mediaFile({})], null)
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  })

  it('图片：左侧渲染 raw 预览，右侧提供下载原文件入口', async () => {
    renderPanel([mediaFile({ id: 7, file_name: '风景.jpg', format: 'jpg' })], 0)
    const dialog = await screen.findByRole('dialog')
    expect(within(dialog).getByRole('img', { name: '风景.jpg' })).toHaveAttribute('src', '/api/library/media/7/raw')
    const dl = within(dialog).getByRole('link', { name: /下载原文件/ })
    expect(dl).toHaveAttribute('href', '/api/library/media/7/download')
    // 图片不显示「打开播放」
    expect(within(dialog).queryByRole('button', { name: /打开播放/ })).not.toBeInTheDocument()
  })

  it('视频：显示缩略图 + 打开播放按钮，点击跳转播放页', async () => {
    const user = userEvent.setup()
    renderPanel([mediaFile({ id: 9, file_name: '电影.mp4', format: 'mp4', duration: 120 })], 0)
    const dialog = await screen.findByRole('dialog')
    expect(within(dialog).getByRole('img', { name: '电影.mp4' })).toHaveAttribute('src', '/api/library/thumbnail/9')
    await user.click(within(dialog).getByRole('button', { name: /打开播放/ }))
    expect(mockNavigate).toHaveBeenCalledWith('/play/9')
  })

  it('上一项/下一项在已加载列表内切换并在端点夹紧', async () => {
    const user = userEvent.setup()
    const files = [
      mediaFile({ id: 1, file_name: '第一张.jpg' }),
      mediaFile({ id: 2, file_name: '第二张.jpg' }),
    ]
    renderPanel(files, 0)
    const dialog = await screen.findByRole('dialog')

    // 起始第一项：上一项禁用
    expect(within(dialog).getByRole('button', { name: '上一项' })).toBeDisabled()
    expect(within(dialog).getByRole('img', { name: '第一张.jpg' })).toBeInTheDocument()

    // 下一项 → 第二张，且下一项按钮在末项禁用
    await user.click(within(dialog).getByRole('button', { name: '下一项' }))
    expect(await within(dialog).findByRole('img', { name: '第二张.jpg' })).toHaveAttribute('src', '/api/library/media/2/raw')
    expect(within(dialog).getByRole('button', { name: '下一项' })).toBeDisabled()
  })
})
