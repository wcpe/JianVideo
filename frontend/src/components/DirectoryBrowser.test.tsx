import { render, screen, fireEvent } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MantineProvider } from '@mantine/core'
import { describe, it, expect, vi } from 'vitest'

import DirectoryBrowser, { sortFiles, type DisplayMode, type DirSort } from './DirectoryBrowser'
import type { MediaFile, DirInfo } from '@/types'

function file(over: Partial<MediaFile>): MediaFile {
  return {
    id: 1, library_id: 1, file_path: 'D:/m/a.jpg', file_name: 'a.jpg', file_size: 1000,
    format: 'jpg', video_codec: '', audio_codec: '', duration: 0, width: 0, height: 0,
    bitrate: 0, subtitle_tracks: '', added_at: '2025-01-01T00:00:00Z', modified_at: '2025-01-01T00:00:00Z', ...over,
  } as MediaFile
}

function renderBrowser(opts: {
  files: MediaFile[]; dirs?: DirInfo[]; displayMode?: DisplayMode; sort?: DirSort
  onOpenFile?: (f: MediaFile, i: number) => void; onEnterDir?: (dir: DirInfo) => void
}) {
  return render(
    <MantineProvider>
      <DirectoryBrowser
        breadcrumbs={[]} directories={opts.dirs ?? []} files={opts.files}
        loading={false} error={null} customImageExtensions={{}}
        onEnterDir={opts.onEnterDir ?? (() => {})} onBreadcrumbNavigate={() => {}}
        onErrorClose={() => {}} onOpenFile={opts.onOpenFile ?? (() => {})}
        displayMode={opts.displayMode ?? 'list'} sort={opts.sort ?? 'name'}
      />
    </MantineProvider>,
  )
}

describe('sortFiles（FR-33）', () => {
  it('按大小/类型/名称排序', () => {
    const files = [file({ id: 1, file_name: 'b.png', format: 'png', file_size: 300 }),
      file({ id: 2, file_name: 'a.jpg', format: 'jpg', file_size: 100 }),
      file({ id: 3, file_name: 'c.mp4', format: 'mp4', file_size: 200 })]
    expect(sortFiles(files, 'size').map(f => f.id)).toEqual([2, 3, 1])
    expect(sortFiles(files, 'name').map(f => f.id)).toEqual([2, 1, 3])
    expect(sortFiles(files, 'type').map(f => f.id)).toEqual([2, 3, 1]) // jpg<mp4<png
  })

  it('按修改时间排序', () => {
    const files = [file({ id: 1, modified_at: '2025-03-01T00:00:00Z' }),
      file({ id: 2, modified_at: '2025-01-01T00:00:00Z' }),
      file({ id: 3, modified_at: '2025-02-01T00:00:00Z' })]
    expect(sortFiles(files, 'time').map(f => f.id)).toEqual([2, 3, 1])
  })
})

// 取某文件卡片的 data-selected 状态
function selectedOf(name: string): boolean {
  const card = screen.getByText(name).closest('.mantine-Card-root')
  return card?.getAttribute('data-selected') === 'true'
}

describe('DirectoryBrowser 资源管理器视图（FR-33）', () => {
  it('列表模式：目录在前，文件显示类型/大小徽标', () => {
    renderBrowser({
      dirs: [{ name: '子目录', path: 'D:/m/sub' } as DirInfo],
      files: [file({ id: 1, file_name: '照片.jpg', format: 'jpg', file_size: 2048 })],
      displayMode: 'list',
    })
    expect(screen.getByText('子目录')).toBeInTheDocument()
    expect(screen.getByText('照片.jpg')).toBeInTheDocument()
    expect(screen.getByText('JPG')).toBeInTheDocument()
  })

  it('单击选中、双击打开（FR-33）', async () => {
    const onOpen = vi.fn()
    const user = userEvent.setup()
    renderBrowser({
      files: [file({ id: 1, file_name: 'a.jpg' }), file({ id: 2, file_name: 'b.jpg' })],
      sort: 'name', onOpenFile: onOpen,
    })

    // 单击选中 a.jpg：不触发打开
    await user.click(screen.getByText('a.jpg'))
    expect(onOpen).not.toHaveBeenCalled()
    const cardA = screen.getByText('a.jpg').closest('.mantine-Card-root')
    expect(cardA).toHaveAttribute('data-selected')

    // 双击 b.jpg：打开，index=1（按名称排序 a,b）
    await user.dblClick(screen.getByText('b.jpg'))
    expect(onOpen).toHaveBeenCalledTimes(1)
    expect(onOpen.mock.calls[0][1]).toBe(1)
  })

  it('目录恒在文件前渲染（列表模式）', () => {
    renderBrowser({
      dirs: [{ name: 'zzz目录', path: 'D:/m/zzz' } as DirInfo],
      files: [file({ id: 1, file_name: 'aaa.jpg' })],
      displayMode: 'list', sort: 'name',
    })
    const cards = Array.from(document.querySelectorAll('.mantine-Card-root'))
    const dirIdx = cards.findIndex((c) => c.textContent?.includes('zzz目录'))
    const fileIdx = cards.findIndex((c) => c.textContent?.includes('aaa.jpg'))
    expect(dirIdx).toBeGreaterThanOrEqual(0)
    expect(dirIdx).toBeLessThan(fileIdx) // 目录(zzz)即便名称靠后也排在文件(aaa)前
  })

  it('Shift 区间选与 Ctrl 切换（FR-33）', () => {
    renderBrowser({
      files: [file({ id: 1, file_name: 'a.jpg' }), file({ id: 2, file_name: 'b.jpg' }), file({ id: 3, file_name: 'c.jpg' })],
      sort: 'name',
    })
    // 单击 a → 仅 a 选中
    fireEvent.click(screen.getByText('a.jpg'))
    expect(selectedOf('a.jpg')).toBe(true)
    expect(selectedOf('c.jpg')).toBe(false)

    // Shift+点击 c → a,b,c 区间全选
    fireEvent.click(screen.getByText('c.jpg'), { shiftKey: true })
    expect(selectedOf('a.jpg')).toBe(true)
    expect(selectedOf('b.jpg')).toBe(true)
    expect(selectedOf('c.jpg')).toBe(true)

    // Ctrl+点击 b → 从区间中切换掉 b（a,c 仍选中）
    fireEvent.click(screen.getByText('b.jpg'), { ctrlKey: true })
    expect(selectedOf('b.jpg')).toBe(false)
    expect(selectedOf('a.jpg')).toBe(true)
    expect(selectedOf('c.jpg')).toBe(true)
  })

  it('大图标模式渲染缩略图', () => {
    renderBrowser({
      files: [file({ id: 7, file_name: '图.jpg' })],
      displayMode: 'large',
    })
    expect(screen.getByRole('img', { name: '图.jpg' }).getAttribute('src')).toContain('/api/library/thumbnail/7')
  })
})
