import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MantineProvider } from '@mantine/core'
import { describe, it, expect, vi, beforeEach } from 'vitest'

import ShareDialog from './ShareDialog'
import type { Share } from '@/types'

const mockCreateShare = vi.fn<() => Promise<Share>>()
vi.mock('@/api/share', () => ({
  createShare: () => mockCreateShare(),
}))

function renderDialog() {
  return render(
    <MantineProvider>
      <ShareDialog opened onClose={() => {}} resourceType="media" resourceID={7} />
    </MantineProvider>,
  )
}

describe('ShareDialog 创建分享链接（FR-43）', () => {
  beforeEach(() => vi.clearAllMocks())

  it('生成链接后展示含 token 的免登地址与复制按钮', async () => {
    mockCreateShare.mockResolvedValue({
      token: 'abc123token',
      resource_type: 'media',
      resource_id: 7,
      expires_at: null,
      created_at: '2026-06-22T00:00:00Z',
    })

    renderDialog()
    const user = userEvent.setup()
    await user.click(screen.getByRole('button', { name: '生成链接' }))

    // 生成后展示链接输入框，值为 /s/<token>
    const input = await screen.findByLabelText<HTMLInputElement>('分享链接')
    await waitFor(() => expect(input.value).toContain('/s/abc123token'))
    expect(screen.getByRole('button', { name: /复制/ })).toBeInTheDocument()
    expect(mockCreateShare).toHaveBeenCalledTimes(1)
  })
})
