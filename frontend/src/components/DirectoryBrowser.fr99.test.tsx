import { render, screen, fireEvent, within } from '@testing-library/react'
import { MantineProvider } from '@mantine/core'
import { describe, it, expect, vi } from 'vitest'
import DirectoryBrowser from './DirectoryBrowser'
import type { MediaFile } from '@/types'

function media(over: Partial<MediaFile>): MediaFile {
  return {
    id: 1, library_id: 1, file_path: 'D:/m/a.jpg', file_name: 'a.jpg', file_size: 1000,
    format: 'jpg', video_codec: '', audio_codec: '', duration: 0, width: 0, height: 0,
    bitrate: 0, subtitle_tracks: '', added_at: '2025-01-01T00:00:00Z', modified_at: '2025-01-01T00:00:00Z', ...over,
  } as MediaFile
}

// 一个视频 + 一个图片
const aVideo = media({ id: 1, file_name: 'v.mp4', format: 'mp4', duration: 95, video_codec: 'h264' })
const anImage = media({ id: 2, file_name: 'p.png', format: 'png' })

function renderBrowser(props: Partial<React.ComponentProps<typeof DirectoryBrowser>>) {
  return render(
    <MantineProvider>
      <DirectoryBrowser
        breadcrumbs={[]} directories={[]} files={[aVideo, anImage]}
        loading={false} error={null} customImageExtensions={{}}
        onEnterDir={() => {}} onBreadcrumbNavigate={() => {}}
        onErrorClose={() => {}} onOpenFile={() => {}}
        displayMode="large" sort="name"
        {...props}
      />
    </MantineProvider>,
  )
}

describe('DirectoryBrowser 图标档媒体卡重设计（FR-99）', () => {
  it('视频卡渲染时长角标与播放叠层；图片卡不渲染', () => {
    renderBrowser({ onBatchDelete: vi.fn() })
    expect(screen.getByText('1:35')).toBeTruthy()
    expect(screen.getAllByLabelText('视频').length).toBe(1)
  })

  it('图标档卡片含 hover 浮层操作（打开/更多）', () => {
    renderBrowser({ onBatchDelete: vi.fn() })
    expect(screen.getAllByLabelText('打开').length).toBeGreaterThan(0)
    expect(screen.getAllByLabelText('更多操作').length).toBeGreaterThan(0)
  })

  it('选中 ≥1 项时浮现 sticky 批量条且批量回调可触发', () => {
    const onBatchDelete = vi.fn()
    const onBatchAddToAlbum = vi.fn()
    renderBrowser({ onBatchDelete, onBatchAddToAlbum })
    expect(screen.queryByRole('toolbar', { name: '批量操作' })).toBeNull()
    // 单击图标档卡片即单选（目录浏览非画廊式）
    const img = screen.getByAltText('v.mp4')
    fireEvent.click(img)
    const bar = screen.getByRole('toolbar', { name: '批量操作' })
    expect(within(bar).getByText(/已选\s*1\s*项/)).toBeTruthy()
    fireEvent.click(within(bar).getByRole('button', { name: '加入相册' }))
    expect(onBatchAddToAlbum).toHaveBeenCalledWith([1])
    fireEvent.click(within(bar).getByRole('button', { name: '删除' }))
    expect(onBatchDelete).toHaveBeenCalledWith([1])
  })
})
