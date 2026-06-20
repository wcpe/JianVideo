import { renderHook, act } from '@testing-library/react'
import { describe, it, expect, vi } from 'vitest'

// 捕获 SSE 回调，手动驱动扫描状态变化
let emit: ((s: unknown) => void) | null = null
vi.mock('@/api/library', () => ({
  createScanProgressSSE: (onProgress: (s: unknown) => void) => {
    emit = onProgress
    return () => {}
  },
}))

import { useScanProgress } from './useScanProgress'

const st = (status: string, completedAt = '') => ({
  status, library_id: 1, current_path: '', total_files: 0,
  scanned_files: 0, error: '', started_at: '', completed_at: completedAt,
})

describe('useScanProgress', () => {
  it('连接后首条 completed 快照不触发 onComplete（避免挂载即刷新）', () => {
    const onComplete = vi.fn()
    renderHook(() => useScanProgress(onComplete))
    act(() => emit!(st('completed', '2026-06-20T10:00:00Z')))
    expect(onComplete).not.toHaveBeenCalled()
  })

  it('出现新的 completed_at（新扫描完成）触发一次，重复同一 completed_at 不再触发', () => {
    const onComplete = vi.fn()
    renderHook(() => useScanProgress(onComplete))
    act(() => emit!(st('idle')))                              // 基线快照
    act(() => emit!(st('completed', '2026-06-20T10:00:00Z'))) // 一次新完成 → 触发
    expect(onComplete).toHaveBeenCalledTimes(1)
    act(() => emit!(st('completed', '2026-06-20T10:00:00Z'))) // 重连/重复同一完成 → 不触发
    expect(onComplete).toHaveBeenCalledTimes(1)
    act(() => emit!(st('completed', '2026-06-20T11:00:00Z'))) // 又一次新完成 → 触发
    expect(onComplete).toHaveBeenCalledTimes(2)
  })

  it('快扫描未观察到 scanning，直接收到新 completed 也能触发（基线之后）', () => {
    const onComplete = vi.fn()
    renderHook(() => useScanProgress(onComplete))
    act(() => emit!(st('idle')))                              // 挂载基线
    act(() => emit!(st('completed', '2026-06-20T12:00:00Z'))) // 快扫描直接完成 → 触发
    expect(onComplete).toHaveBeenCalledTimes(1)
  })
})
