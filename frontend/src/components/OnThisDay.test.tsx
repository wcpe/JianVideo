import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { MemoryRouter } from 'react-router-dom'
import { MantineProvider } from '@mantine/core'
import { http, HttpResponse } from 'msw'
import { server } from '@/mocks/beforeAll'
import OnThisDay from './OnThisDay'
import type { MediaFile } from '@/types'

const mockNavigate = vi.fn()

vi.mock('react-router-dom', async (importOriginal) => {
  const actual = await importOriginal<typeof import('react-router-dom')>()
  return { ...actual, useNavigate: () => mockNavigate }
})

// 缩略图组件依赖图片加载，测试中替换为占位，专注回忆区块逻辑。
vi.mock('@/components/MediaThumbnail', () => ({
  default: () => <div data-testid="thumb" />,
}))

// 构造一条「往年同一天」媒体：media_time 用今年减 yearsAgo 年的同一天。
function buildMemory(id: number, name: string, yearsAgo: number): MediaFile {
  const now = new Date()
  const mt = new Date(now.getFullYear() - yearsAgo, now.getMonth(), now.getDate(), 12, 0, 0)
  return {
    id, library_id: 1, file_path: `D:/Photos/${name}`, file_name: name,
    file_size: 1024, format: 'jpg', video_codec: '', audio_codec: '',
    duration: 0, width: 1920, height: 1080, bitrate: 0, subtitle_tracks: '',
    added_at: '2025-01-01T12:00:00Z', modified_at: '2025-01-01T12:00:00Z',
    media_time: mt.toISOString(),
  }
}

function renderComp() {
  return render(
    <MantineProvider>
      <MemoryRouter>
        <OnThisDay />
      </MemoryRouter>
    </MantineProvider>,
  )
}

describe('OnThisDay', () => {
  beforeEach(() => { vi.clearAllMocks() })

  it('展示那年今日回忆并标注 X 年前的今天，点击进入播放', async () => {
    server.use(
      http.get('*/api/library/on-this-day', () =>
        HttpResponse.json({ items: [buildMemory(7, '海边日落.jpg', 3)] }),
      ),
    )

    renderComp()

    await waitFor(() => {
      expect(screen.getByText('那年今日')).toBeInTheDocument()
      expect(screen.getByText('海边日落.jpg')).toBeInTheDocument()
      expect(screen.getByText('3 年前的今天')).toBeInTheDocument()
    })

    await userEvent.click(screen.getByRole('button', { name: /那年今日 海边日落\.jpg/ }))
    expect(mockNavigate).toHaveBeenCalledWith('/play/7')
  })

  it('列表为空时不渲染区块', async () => {
    server.use(
      http.get('*/api/library/on-this-day', () => HttpResponse.json({ items: [] })),
    )

    renderComp()

    await waitFor(() => {
      expect(screen.queryByTestId('on-this-day')).not.toBeInTheDocument()
    })
    expect(screen.queryByText('那年今日')).not.toBeInTheDocument()
  })
})
