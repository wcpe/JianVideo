import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MantineProvider } from '@mantine/core'
import { describe, it, expect, beforeEach, vi } from 'vitest'

import ScanTaskIndicator from './ScanTaskIndicator'
import type { ScanTasksResponse } from '@/types'

// mock 扫描任务 API，避免真实轮询
const mockGetScanTasks = vi.fn<() => Promise<ScanTasksResponse>>()
vi.mock('@/api/library', () => ({
  getScanTasks: () => mockGetScanTasks(),
}))

function renderIndicator() {
  return render(
    <MantineProvider>
      <ScanTaskIndicator />
    </MantineProvider>,
  )
}

describe('ScanTaskIndicator 页眉扫描任务指示器', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('无进行中任务时不显示指示器', async () => {
    mockGetScanTasks.mockResolvedValue({ tasks: [], current: null })
    renderIndicator()
    // 等首次轮询完成
    await waitFor(() => expect(mockGetScanTasks).toHaveBeenCalled())
    expect(screen.queryByRole('button', { name: /扫描任务/ })).not.toBeInTheDocument()
  })

  it('有进行中任务时显示指示器，点开展示任务列表与进度', async () => {
    const running = {
      id: 1, library_id: 1, scan_type: 'full', status: 'running',
      scanned_files: 30, total_files: 120, error: '',
      created_at: '2026-06-22T10:00:00Z', started_at: '2026-06-22T10:00:00Z',
    }
    const pending = {
      id: 2, library_id: 2, scan_type: 'full', status: 'pending',
      scanned_files: 0, total_files: 0, error: '',
      created_at: '2026-06-22T10:01:00Z',
    }
    mockGetScanTasks.mockResolvedValue({ tasks: [pending, running], current: running })

    renderIndicator()

    // 指示器按钮出现
    const btn = await screen.findByRole('button', { name: /扫描任务/ })
    expect(btn).toBeInTheDocument()

    // 点开 Popover，展示任务列表
    const user = userEvent.setup()
    await user.click(btn)

    // 进行中任务展示进度（30/120）
    await waitFor(() => {
      expect(screen.getByText(/30\s*\/\s*120/)).toBeInTheDocument()
    })
    // 列表含两条任务（库 1、库 2）
    expect(screen.getByText(/库 1/)).toBeInTheDocument()
    expect(screen.getByText(/库 2/)).toBeInTheDocument()
  })
})
