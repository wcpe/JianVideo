import { useState, useEffect, useRef } from 'react';
import * as libApi from '@/api/library';
import type { ScanStatus } from '@/types';

// 订阅扫描进度 SSE；在「出现一次新的扫描完成」时回调 onComplete（用 ref 持有最新回调，避免反复重订阅）。
// 以 completed_at 是否变化判定新完成，而非依赖观察到 scanning——快扫描（<SSE 轮询间隔）可能跳过 scanning，
// 但每次完成都有新的 completed_at。initializedRef 跳过连接建立后的首条快照，避免「挂载即刷新」与
// SSE 重连重复收到同一 completed 时反复空转重拉（Bug B）。
export function useScanProgress(onComplete?: () => void) {
  const [progress, setProgress] = useState<ScanStatus | null>(null);
  const cbRef = useRef(onComplete);
  const initializedRef = useRef(false);
  const seenCompletedAtRef = useRef<string | null>(null);
  // 在 effect 中更新 ref，避免在渲染期写 ref
  useEffect(() => {
    cbRef.current = onComplete;
  }, [onComplete]);
  useEffect(
    () =>
      libApi.createScanProgressSSE((s) => {
        setProgress(s);
        if (s.status === 'completed' && s.completed_at) {
          if (initializedRef.current && s.completed_at !== seenCompletedAtRef.current) {
            cbRef.current?.();
          }
          seenCompletedAtRef.current = s.completed_at;
        }
        initializedRef.current = true;
      }),
    [],
  );
  return progress;
}
