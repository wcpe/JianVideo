import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MantineProvider } from '@mantine/core'
import { describe, it, expect, vi, beforeEach } from 'vitest'

import ExternalPlayerDialog from './ExternalPlayerDialog'
import type { Share } from '@/types'

const mockCreateShare = vi.fn<(rt: string, id: number, hours: number) => Promise<Share>>()
vi.mock('@/api/share', () => ({
  createShare: (rt: string, id: number, hours: number) => mockCreateShare(rt, id, hours),
}))

function renderDialog(lastPosition?: number) {
  return render(
    <MantineProvider>
      <ExternalPlayerDialog opened onClose={() => {}} mediaID={7} lastPosition={lastPosition} />
    </MantineProvider>,
  )
}

describe('ExternalPlayerDialog 外部播放器深链（FR-79）', () => {
  beforeEach(() => vi.clearAllMocks())

  it('选有效期 → 生成 → 展示含 token 的绝对 share 流地址（带续播点）', async () => {
    mockCreateShare.mockResolvedValue({
      token: 'abc123token',
      resource_type: 'media',
      resource_id: 7,
      expires_at: null,
      created_at: '2026-06-24T00:00:00Z',
    })

    renderDialog(123.4)
    const user = userEvent.setup()
    await user.click(screen.getByRole('button', { name: '生成地址' }))

    // 以 media 资源、默认有效期（0=永不过期）创建分享
    await waitFor(() => expect(mockCreateShare).toHaveBeenCalledWith('media', 7, 0))

    const input = await screen.findByLabelText<HTMLInputElement>('流地址')
    // 绝对地址 + share 流端点 + 续播点片段
    await waitFor(() =>
      expect(input.value).toContain('/api/share/abc123token/media/7/stream#t=123'),
    )
    expect(input.value).toMatch(/^https?:\/\//)
    expect(screen.getByRole('button', { name: /复制/ })).toBeInTheDocument()
  })

  it('last_position 为 0 时生成地址不带续播点片段', async () => {
    mockCreateShare.mockResolvedValue({
      token: 'tok0',
      resource_type: 'media',
      resource_id: 7,
      expires_at: null,
      created_at: '2026-06-24T00:00:00Z',
    })

    renderDialog(0)
    const user = userEvent.setup()
    await user.click(screen.getByRole('button', { name: '生成地址' }))

    const input = await screen.findByLabelText<HTMLInputElement>('流地址')
    await waitFor(() => expect(input.value).toContain('/api/share/tok0/media/7/stream'))
    expect(input.value).not.toContain('#t=')
  })
})
