import { useState, useEffect, useRef } from 'react'
import * as libApi from '@/api/library'
import type { ScanStatus } from '@/types'

// 订阅扫描进度 SSE；扫描完成时回调 onComplete（用 ref 持有最新回调，避免反复重订阅）
export function useScanProgress(onComplete?: () => void) {
  const [progress, setProgress] = useState<ScanStatus | null>(null)
  const cbRef = useRef(onComplete)
  // 在 effect 中更新 ref，避免在渲染期写 ref
  useEffect(() => { cbRef.current = onComplete }, [onComplete])
  useEffect(() => libApi.createScanProgressSSE((s) => {
    setProgress(s)
    if (s.status === 'completed') cbRef.current?.()
  }), [])
  return progress
}
