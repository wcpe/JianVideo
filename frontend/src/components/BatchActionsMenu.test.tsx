import { render, screen, fireEvent } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MantineProvider } from '@mantine/core'
import { describe, it, expect, vi } from 'vitest'

import DirectoryBrowser from './DirectoryBrowser'
import type { MediaFile } from '@/types'

function file(over: Partial<MediaFile>): MediaFile {
  return {
    id: 1, library_id: 1, file_path: 'D:/m/a.jpg', file_name: 'a.jpg', file_size: 1000,
    format: 'jpg', video_codec: '', audio_codec: '', duration: 0, width: 0, height: 0,
    bitrate: 0, subtitle_tracks: '', added_at: '2025-01-01T00:00:00Z', modified_at: '2025-01-01T00:00:00Z', ...over,
  } as MediaFile
}

function renderBrowser(props: Partial<React.ComponentProps<typeof DirectoryBrowser>>) {
  return render(
    <MantineProvider>
      <DirectoryBrowser
        breadcrumbs={[]} directories={[]} files={[]}
        loading={false} error={null} customImageExtensions={{}}
        onEnterDir={() => {}} onBreadcrumbNavigate={() => {}}
        onErrorClose={() => {}} onOpenFile={() => {}}
        displayMode="list" sort="name"
        {...props}
      />
    </MantineProvider>,
  )
}

const threeFiles = [
  file({ id: 1, file_name: 'a.jpg' }),
  file({ id: 2, file_name: 'b.jpg' }),
  file({ id: 3, file_name: 'c.jpg' }),
]

describe('右键批量菜单项（FR-91）', () => {
  it('「加入相册」按选中集触发 onBatchAddToAlbum', async () => {
    const onBatchAddToAlbum = vi.fn()
    renderBrowser({ files: threeFiles, onBatchAddToAlbum })
    fireEvent.click(screen.getByText('a.jpg'))
    fireEvent.click(screen.getByText('c.jpg'), { shiftKey: true })
    fireEvent.contextMenu(screen.getByText('b.jpg'))
    await userEvent.click(await screen.findByText('加入相册选中（3）'))
    expect(onBatchAddToAlbum).toHaveBeenCalledWith([1, 2, 3])
  })

  it('「打标签」按选中集触发 onBatchAddTag', async () => {
    const onBatchAddTag = vi.fn()
    renderBrowser({ files: threeFiles, onBatchAddTag })
    fireEvent.click(screen.getByText('b.jpg'))
    fireEvent.contextMenu(screen.getByText('b.jpg'))
    await userEvent.click(await screen.findByText('打标签选中（1）'))
    expect(onBatchAddTag).toHaveBeenCalledWith([2])
  })

  it('无选中时「打包下载」退化为对右键项触发 onBatchDownload', async () => {
    const onBatchDownload = vi.fn()
    renderBrowser({ files: threeFiles, onBatchDownload })
    fireEvent.contextMenu(screen.getByText('c.jpg'))
    await userEvent.click(await screen.findByText('打包下载'))
    expect(onBatchDownload).toHaveBeenCalledWith([3])
  })

  it('未传任何批量操作回调时菜单不出现这三项', async () => {
    renderBrowser({ files: threeFiles, onBatchDelete: vi.fn() })
    fireEvent.contextMenu(screen.getByText('a.jpg'))
    // 删除菜单仍在，但批量操作项不出现
    expect(await screen.findByText('删除')).toBeInTheDocument()
    expect(screen.queryByText('加入相册')).not.toBeInTheDocument()
    expect(screen.queryByText('打标签')).not.toBeInTheDocument()
    expect(screen.queryByText('打包下载')).not.toBeInTheDocument()
  })
})
